// Package testinfra provides test infrastructure helpers.
package testinfra

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcopenfga "github.com/testcontainers/testcontainers-go/modules/openfga"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startupTimeoutEnv overrides how long a container may take to become
// ready. Point it at a larger value on a slow or heavily loaded machine.
const startupTimeoutEnv = "HMS_TEST_CONTAINER_TIMEOUT"

// defaultStartupTimeout is deliberately far above the time a container
// needs when it starts alone (a few seconds). `go test ./...` runs up to
// GOMAXPROCS package binaries at once and every harness here boots its
// own containers — a machine with 14 cores can therefore have a dozen
// Postgres, NATS and OpenFGA containers racing to start. The timeout
// exists to catch a container that will never come up, not to police how
// long a busy machine takes, so it is sized for the contended case.
const defaultStartupTimeout = 3 * time.Minute

// startupTimeout is the budget applied to every container below.
//
// Waiting longer than a minute takes TWO changes, and doing only one of
// them silently leaves the 60-second ceiling in place:
//
//  1. The outer deadline. testcontainers' WithWaitStrategy wraps the
//     strategies it is given in `wait.ForAll(...).WithDeadline(60s)`, so
//     the ...AndDeadline form is required to widen it.
//  2. The per-strategy timeout. Each wait.Strategy falls back to
//     wait.defaultStartupTimeout() — also 60 seconds — when no explicit
//     WithStartupTimeout is set on it, and that inner limit expires on
//     its own regardless of how generous the outer deadline is.
//
// Both are therefore set at every call site below. A container that
// exceeds this really is stuck, not merely sharing a busy machine.
func startupTimeout(t *testing.T) time.Duration {
	t.Helper()
	d, err := parseStartupTimeout(os.Getenv(startupTimeoutEnv))
	if err != nil {
		// Loud rather than falling back: a typo here would otherwise
		// hand back the default and leave someone debugging a timeout
		// they believe they already raised.
		t.Fatalf("%s: %v", startupTimeoutEnv, err)
	}
	return d
}

// parseStartupTimeout resolves the raw env value; empty means unset.
func parseStartupTimeout(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultStartupTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%q is not a Go duration such as 90s or 5m: %w", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q must be positive", raw)
	}
	return d, nil
}

// One Postgres server is shared by every test in a binary; each caller
// gets its own database on it. Booting a container costs well over a
// second and creating a database costs a fraction of that, so a package
// like iam — 22 tests, each of which used to boot its own server — spends
// its time running tests rather than starting Postgres (#765).
//
// Isolation is per database rather than per schema so each test still
// migrates from empty and cannot see another test's rows, and so this
// stays correct if these tests are ever run in parallel.
var (
	pgOnce   sync.Once
	pgShared struct {
		host, port string
	}
	pgErr  error
	pgSeq  atomic.Uint64
	pgCont *tcpostgres.PostgresContainer
)

// sharedPostgres boots the server on first use and returns its address.
//
// The container is deliberately never terminated by a t.Cleanup: it
// outlives the test that happened to trigger the boot, and killing it
// there would pull the server out from under every later test in the
// binary. testcontainers' reaper removes it when the process exits.
func sharedPostgres(t *testing.T) (host, port string) {
	t.Helper()
	pgOnce.Do(func() {
		ctx := context.Background()
		pgCont, pgErr = tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("hms"),
			tcpostgres.WithUsername("hms"),
			tcpostgres.WithPassword("hms"),
			testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t),
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(startupTimeout(t))),
		)
		if pgErr != nil {
			pgErr = fmt.Errorf("start postgres: %w", pgErr)
			return
		}
		// Roles are cluster-wide, so the app role is created once here;
		// the privileges it needs are per database and are granted in
		// newDatabase below.
		if _, _, err := pgCont.Exec(ctx, []string{"psql", "-U", "hms", "-d", "hms", "-c",
			`CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;`}); err != nil {
			pgErr = fmt.Errorf("create app role: %w", err)
			return
		}
		h, err := pgCont.Host(ctx)
		if err != nil {
			pgErr = fmt.Errorf("postgres host: %w", err)
			return
		}
		p, err := pgCont.MappedPort(ctx, "5432/tcp")
		if err != nil {
			pgErr = fmt.Errorf("postgres port: %w", err)
			return
		}
		pgShared.host, pgShared.port = h, p.Port()
	})
	if pgErr != nil {
		// Every later caller fails with the same reason rather than
		// blocking on a server that was never going to exist.
		t.Fatalf("%v", pgErr)
	}
	return pgShared.host, pgShared.port
}

// StartPostgres returns (appDSN, adminDSN) for a database of this test's
// own, on a Postgres server shared with the rest of the binary. The two
// roles mirror dev/init-db.sql: hms owns the schema, hms_app is the
// non-BYPASSRLS role the application connects as, which is what makes the
// forced-RLS policies meaningful under test.
func StartPostgres(t *testing.T) (string, string) {
	t.Helper()
	host, port := sharedPostgres(t)
	name := newDatabase(t)
	dsn := func(user, pass string) string {
		return "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + name + "?sslmode=disable"
	}
	return dsn("hms_app", "hms_app"), dsn("hms", "hms")
}

// newDatabase creates an empty database owned by hms and grants hms_app
// the same privileges it holds in dev.
func newDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("hms_test_%d", pgSeq.Add(1))

	// CREATE DATABASE cannot run inside a transaction block, and psql
	// wraps a multi-statement -c in one, so it gets an -c of its own.
	if _, _, err := pgCont.Exec(ctx, []string{"psql", "-U", "hms", "-d", "hms", "-c",
		`CREATE DATABASE ` + name + ` OWNER hms;`}); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	if _, _, err := pgCont.Exec(ctx, []string{"psql", "-U", "hms", "-d", name, "-c", `
		GRANT USAGE ON SCHEMA public TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;`}); err != nil {
		t.Fatalf("grant on %s: %v", name, err)
	}

	t.Cleanup(func() {
		// Best effort. FORCE closes the pools the test left open; if the
		// drop still fails the database simply dies with the container,
		// so a failure here must not fail an otherwise passing test.
		_, _, _ = pgCont.Exec(context.Background(), []string{"psql", "-U", "hms", "-d", "hms", "-c",
			`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE);`})
	})
	return name
}

// StartNATS boots nats:2.10-alpine and returns the connection URL.
func StartNATS(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	nats, err := tcnats.Run(ctx, "nats:2.10-alpine",
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t),
			wait.ForListeningPort("4222/tcp").WithStartupTimeout(startupTimeout(t))),
	)
	if err != nil {
		t.Fatalf("start nats: %v", err)
	}
	t.Cleanup(func() { _ = nats.Terminate(context.Background()) })

	uri, err := nats.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("nats dsn: %v", err)
	}

	return uri
}

// One OpenFGA server is shared by every test in a binary, for the same
// reason Postgres is: the per-container cost is dominated by starting and
// reaping it, not by the handful of API calls a test makes against it.
//
// Isolation here is by store rather than by database. Callers already
// name their store when they build a client, so a test that wants its own
// tuples asks for its own store name — see the callers, which pass
// t.Name(). Two callers naming the same store share it deliberately.
var (
	fgaOnce sync.Once
	fgaURL  string
	fgaErr  error
)

// StartOpenFGA returns the HTTP API URL of the shared OpenFGA server,
// booting it on first use. As with Postgres, it is never torn down by a
// t.Cleanup — it belongs to the binary, not to the test that happened to
// need it first.
func StartOpenFGA(t *testing.T) string {
	t.Helper()
	fgaOnce.Do(func() {
		fgaURL, fgaErr = startOpenFGA(t)
	})
	if fgaErr != nil {
		t.Fatalf("%v", fgaErr)
	}
	return fgaURL
}

func startOpenFGA(t *testing.T) (string, error) {
	t.Helper()
	ctx := context.Background()
	// The wait strategy is restated rather than inherited. The module's
	// own strategy is registered through WithWaitStrategy, so it carries
	// the same hard 60-second deadline described on startupTimeout, and
	// appending to it would not help: the module's strategy keeps its
	// deadline as a nested wait.ForAll, which expires on its own schedule
	// no matter what deadline wraps it. Replacing it is the only way the
	// timeout below actually governs.
	//
	// This mirrors the module's /healthz check (openfga.go: SERVING on
	// 8080) and drops its /playground check on 3000 — that port is a
	// developer UI these tests never call, whereas 8080 is the API port
	// StartOpenFGA hands back via HttpEndpoint.
	fga, err := tcopenfga.Run(ctx, "openfga/openfga:v1.8.4",
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t),
			wait.ForHTTP("/healthz").WithPort("8080/tcp").
				WithStartupTimeout(startupTimeout(t)).
				WithResponseMatcher(func(r io.Reader) bool {
					body, err := io.ReadAll(r)
					if err != nil {
						return false
					}
					return strings.Contains(string(body), "SERVING")
				})),
	)
	if err != nil {
		return "", fmt.Errorf("start openfga: %w", err)
	}
	url, err := fga.HttpEndpoint(ctx)
	if err != nil {
		return "", fmt.Errorf("openfga endpoint: %w", err)
	}
	return url, nil
}
