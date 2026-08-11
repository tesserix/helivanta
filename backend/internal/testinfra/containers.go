// Package testinfra provides test infrastructure helpers.
package testinfra

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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

// StartPostgres boots postgres:16, creates the non-BYPASSRLS app role,
// and returns (appDSN, adminDSN). Mirrors dev/init-db.sql.
func StartPostgres(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("hms"),
		tcpostgres.WithUsername("hms"),
		tcpostgres.WithPassword("hms"),
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t),
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(startupTimeout(t))),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })

	adminDSN, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	_, _, err = pg.Exec(ctx, []string{"psql", "-U", "hms", "-d", "hms", "-c", `
		CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;
		GRANT USAGE ON SCHEMA public TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;`})
	if err != nil {
		t.Fatalf("create app role: %v", err)
	}
	host, _ := pg.Host(ctx)
	port, _ := pg.MappedPort(ctx, "5432/tcp")
	appDSN := "postgres://hms_app:hms_app@" + host + ":" + port.Port() + "/hms?sslmode=disable"
	return appDSN, adminDSN
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

// StartOpenFGA boots an in-memory OpenFGA and returns its HTTP API URL.
func StartOpenFGA(t *testing.T) string {
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
		t.Fatalf("start openfga: %v", err)
	}
	t.Cleanup(func() { _ = fga.Terminate(context.Background()) })

	url, err := fga.HttpEndpoint(ctx)
	if err != nil {
		t.Fatalf("openfga endpoint: %v", err)
	}
	return url
}
