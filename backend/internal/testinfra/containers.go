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
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcopenfga "github.com/testcontainers/testcontainers-go/modules/openfga"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startupTimeoutEnv overrides how long a container may take to become
// ready. Point it at a larger value on a slow or heavily loaded machine.
const startupTimeoutEnv = "HELIVANTA_TEST_CONTAINER_TIMEOUT"

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
		// Roles are cluster-wide, so they are created once here; the
		// privileges they need are per database and are granted in
		// newDatabase below.
		//
		// THE OWNER IS NOT THE BOOTSTRAP SUPERUSER (#894). `hms` is the
		// image's POSTGRES_USER and is unavoidably a superuser — Postgres
		// refuses to strip SUPERUSER from the bootstrap role ("the
		// bootstrap user must have the SUPERUSER attribute"), so the owner
		// has to be a DIFFERENT role rather than a demoted `hms`. That is
		// also production's shape: CNPG has a `postgres` superuser and
		// `helivanta` as a plain owner.
		//
		// This matters because `iam_members` and `outbox_events` are FORCE
		// ROW LEVEL SECURITY, and FORCE binds the table OWNER — that is the
		// whole reason it is chosen over plain ENABLE. When the harness's
		// owner was the image superuser, every cross-tenant read the admin
		// pool made succeeded here and returned zero rows in production.
		// Three call sites shipped on that assumption with every test
		// green. dev/init-db.sql constrained `hms_app` and said nothing
		// about the owner; that omission is the whole bug, because it left
		// the divergence inexpressible.
		//
		// `hms` remains the harness's own plumbing (creating databases,
		// roles and grants) and is never handed to code under test.
		if err := runSQL(ctx, "hms", `
			CREATE ROLE hms_owner LOGIN PASSWORD 'hms_owner' NOSUPERUSER NOBYPASSRLS;
			CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;
			CREATE ROLE helivanta_system LOGIN PASSWORD 'helivanta_system' NOSUPERUSER BYPASSRLS;
			GRANT hms_owner TO hms;`); err != nil {
			pgErr = fmt.Errorf("create roles: %w", err)
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

// StartPostgres returns (appDSN, adminDSN, systemDSN) for a database of
// this test's own, on a Postgres server shared with the rest of the binary.
//
// The three roles mirror PRODUCTION, not dev (#894):
//
//   - hms_owner  — owns the schema. NOSUPERUSER, NOBYPASSRLS, so FORCE ROW
//     LEVEL SECURITY binds it exactly as it binds CNPG's `helivanta`.
//   - hms_app    — the non-BYPASSRLS role the application connects as.
//   - helivanta_system — BYPASSRLS, the only role that may cross tenants.
//
// All three are returned unconditionally so a test cannot accidentally
// exercise a two-pool shape production never runs. The previous signature
// returned the image's SUPERUSER as the admin DSN, which is why a
// whole-system read that returns zero rows in production returned every
// row here.
func StartPostgres(t *testing.T) (string, string, string) {
	t.Helper()
	host, port := sharedPostgres(t)
	name := newDatabase(t)
	dsn := func(user, pass string) string {
		return "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + name + "?sslmode=disable"
	}
	return dsn("hms_app", "hms_app"), dsn("hms_owner", "hms_owner"), dsn("helivanta_system", "helivanta_system")
}

// runSQL executes a psql command inside the Postgres container and reports
// failure properly.
//
// The subtlety it exists to remove: testcontainers' Exec returns
// (exitCode, output, err), and err reports only whether the exec mechanism
// worked — starting the process, talking to the daemon. A psql that runs
// fine and then REJECTS the SQL exits non-zero with err still nil. Reading
// only err therefore treats "the GRANT was refused" as success and lets the
// harness hand out a database that was never set up; the test then fails
// later, somewhere unrelated, with a symptom that looks nothing like the
// cause. See TestRunSQLReportsAFailedStatement.
//
// psql exits non-zero for a failing statement even inside a multi-statement
// -c (verified against postgres:16-alpine), so the exit code is the whole
// signal — but it is only a signal if somebody reads it.
func runSQL(ctx context.Context, database, sql string) error {
	// tcexec.Multiplexed() is LOAD-BEARING. Do not remove it as cosmetic.
	//
	// Two things depend on it. The obvious one: it strips docker's 8-byte
	// per-frame stream headers, without which psql's message reaches the
	// error prefixed with control bytes, exactly when someone is reading it
	// under pressure.
	//
	// The other is ordering. Exec starts the process with ExecAttach and
	// then POLLS ExecInspect for `!Running`; if the daemon has not yet
	// marked the exec running, the first poll can see Running=false with
	// ExitCode=0 and report success for a process that never ran. Multiplexed
	// blocks until the output stream EOFs, which cannot happen before the
	// process has run, so the window closes.
	//
	// Honesty about the evidence: that window is UNPROVEN as the cause of
	// the intermittent "database does not exist" failure pgForensics exists
	// to catch. 840 attempts (idle, 120-way concurrent, and under container
	// churn) produced no premature exit code. What is on record is only a
	// timeline — the failure appeared on the one CI run whose runSQL checked
	// the exit code WITHOUT this option, and has not appeared since it was
	// added. Suggestive, not conclusive. Keep the option; do not treat its
	// presence as proof the defect is fixed.
	code, out, err := pgCont.Exec(ctx, []string{"psql", "-U", "hms", "-d", database, "-c", sql},
		tcexec.Multiplexed())
	if err != nil {
		return fmt.Errorf("exec psql on %s: %w", database, err)
	}
	if code != 0 {
		// psql's own message is the only thing that explains WHICH
		// statement was refused, so it has to travel with the error.
		body, _ := io.ReadAll(out)
		return fmt.Errorf("psql exited %d on %s: %s", code, database, strings.TrimSpace(string(body)))
	}
	return nil
}

// pgForensics describes the server's actual state, for the failure path only.
//
// It exists because of an intermittent CI failure that has resisted
// reproduction: CREATE DATABASE reports exit 0, and the GRANT two statements
// later cannot connect because the database "does not exist". Everything
// cheap has been ruled out — names come from an atomic counter so they cannot
// collide, each package binary gets its own container, this file holds the
// only DROP, and Exec's exit code was measured truthful at 120-way
// concurrency. What is left needs evidence from the moment it happens, on a
// loaded runner, which is not something a local rerun can supply.
//
// So the next occurrence must arrive already explained rather than merely
// noticed. Best effort by construction: this runs when the test is failing
// anyway, so it must never mask the original error or fail in its own right.
func pgForensics(ctx context.Context, name string) string {
	var b strings.Builder
	b.WriteString("\n--- postgres forensics for " + name + " ---")
	for _, probe := range []struct{ label, sql string }{
		// Does the server think it exists? Distinguishes "never created"
		// from "created then removed".
		{"database present", `SELECT count(*) FROM pg_database WHERE datname = '` + name + `'`},
		// Every database, so a name we did not expect (or a missing
		// neighbour) is visible.
		{"all databases", `SELECT string_agg(datname, ' ' ORDER BY oid) FROM pg_database`},
		// If this moved, the server restarted underneath the run and the
		// data directory is the thing to look at, not this code.
		{"server started", `SELECT pg_postmaster_start_time()`},
		// A backend that reconnected to a NEW server would show a low
		// number here relative to how long the binary has been running.
		{"backends", `SELECT count(*) FROM pg_stat_activity`},
	} {
		code, out, err := pgCont.Exec(ctx,
			[]string{"psql", "-U", "hms", "-d", "hms", "-tAc", probe.sql},
			tcexec.Multiplexed())
		switch {
		case err != nil:
			b.WriteString("\n  " + probe.label + ": probe failed: " + err.Error())
		default:
			body, _ := io.ReadAll(out)
			fmt.Fprintf(&b, "\n  %s (exit %d): %s",
				probe.label, code, strings.TrimSpace(string(body)))
		}
	}
	return b.String()
}

// newDatabase creates an empty database owned by hms and grants hms_app
// the same privileges it holds in dev.
func newDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("hms_test_%d", pgSeq.Add(1))

	// CREATE DATABASE cannot run inside a transaction block, and psql
	// wraps a multi-statement -c in one, so it gets an -c of its own.
	if err := runSQL(ctx, "hms", `CREATE DATABASE `+name+` OWNER hms_owner;`); err != nil {
		t.Fatalf("create database %s: %v%s", name, err, pgForensics(ctx, name))
	}
	// If this is skipped the database still exists and still accepts
	// connections, so the harness looks healthy and the failure lands much
	// later as a bare "permission denied" from whichever test happens to
	// write first. runSQL is what stops that being silent.
	// postgres_fdw is installed here, by the bootstrap superuser, because
	// CREATE EXTENSION requires one and the owner deliberately is not one
	// any more (#894). Only TestLintRLSFlagsForeignTable needs it — it
	// proves LintRLS catches a foreign table, which is a genuine way to
	// smuggle data past RLS — and without this that test fails with
	// "permission denied to create extension", which looks like a bug in
	// LintRLS rather than missing harness setup. USAGE on the wrapper is
	// what then lets the owner CREATE SERVER.
	//
	// helivanta_system is deliberately NOT granted anything here.
	//
	// BYPASSRLS decides whether the row POLICIES apply; it confers no table
	// privileges at all, so the system role needs explicit GRANTs — and
	// those live in the MIGRATIONS, per table, next to the table they
	// concern. Granting them here instead would hand the harness a
	// privilege production does not have, which is precisely the shape of
	// #894: a harness that is more permissive than production keeps passing
	// while production fails. Verified against the live database — the role
	// existed with bypassrls and `has_table_privilege(...,'iam_members',
	// 'SELECT')` was false.
	if err := runSQL(ctx, name, `
		CREATE EXTENSION IF NOT EXISTS postgres_fdw;
		GRANT USAGE ON FOREIGN DATA WRAPPER postgres_fdw TO hms_owner;
		ALTER SCHEMA public OWNER TO hms_owner;
		GRANT USAGE, CREATE ON SCHEMA public TO hms_owner;
		GRANT USAGE ON SCHEMA public TO hms_app, helivanta_system;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms_owner IN SCHEMA public
		  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms_owner IN SCHEMA public
		  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;`); err != nil {
		t.Fatalf("grant on %s: %v%s", name, err, pgForensics(ctx, name))
	}

	t.Cleanup(func() {
		// Best effort, and the ONLY place here that may ignore psql's exit
		// code — deliberately, not by the oversight runSQL exists to
		// prevent. FORCE closes the pools the test left open; if the drop
		// still fails the database simply dies with the container, so a
		// failure here must not fail an otherwise passing test.
		_ = runSQL(context.Background(), "hms", `DROP DATABASE IF EXISTS `+name+` WITH (FORCE);`)
	})
	return name
}

// One NATS server is shared by every test in a binary, like Postgres and
// OpenFGA. Isolation cannot come from the server here: JetStream refuses
// two streams with overlapping subjects, so it comes from the subject
// space instead — each caller builds its Bus with its own namespace via
// events.NewBusInNamespace (see testutil.moduleHarness).
var (
	natsOnce sync.Once
	natsURL  string
	natsErr  error
)

// StartNATS returns the URL of the shared NATS server, booting it on
// first use. As with the other containers it is never torn down by a
// t.Cleanup — it belongs to the binary, not to one test.
func StartNATS(t *testing.T) string {
	t.Helper()
	natsOnce.Do(func() {
		natsURL, natsErr = startNATS(t)
	})
	if natsErr != nil {
		t.Fatalf("%v", natsErr)
	}
	return natsURL
}

func startNATS(t *testing.T) (string, error) {
	t.Helper()
	ctx := context.Background()
	// wait.ForListeningPort alone (the module's own default, and this
	// file's previous strategy) is a bare TCP-accept check, and NATS opens
	// its client port BEFORE it is actually ready to serve a handshake.
	// Measured on this exact image (#926):
	//
	//   [INF] Listening for client connections on 0.0.0.0:4222   <- port check returns HERE
	//   [ERR] Address "0.0.0.0" can not be resolved properly
	//   [INF] Server is ready                                    <- actually ready HERE
	//   [INF] Cluster name is my_cluster
	//
	// A client (nats.Connect in bus.go) that dials inside that window gets
	// its socket accepted and then dropped mid-handshake, which surfaces as
	// io.EOF out of readOp — not connection-refused, not a timeout. The
	// window is normally too narrow to hit, but this file's own comment on
	// startupTimeout explains why a `go test ./...` run is exactly the
	// contended, many-containers-booting-at-once case that widens it (#926).
	//
	// wait.ForLog("Server is ready") on its own would close that window,
	// and is the same shape Postgres already uses in this file. ForAll with
	// the port check kept alongside it is chosen instead, for the same
	// reason OpenFGA below keeps its own explicit strategy rather than
	// inheriting the module's: it costs nothing extra (the port is normally
	// open microseconds before the log line) and it keeps failure modes
	// distinguishable — "port never opened" vs "port opened, server never
	// declared ready" point at different problems (container never started,
	// vs. started but the log format changed under us) and collapsing them
	// into one strategy would make a future regression harder to diagnose
	// from CI output alone.
	//
	// wait.ForHTTP("/healthz").WithPort("8222/tcp") was also considered —
	// confirmed available on this image, monitoring is on by default
	// (monitor_port: 8222) — but rejected here: it would need its own
	// PortEndpoint plumbing this module does not expose ready-made, for no
	// evidence-backed gain over the log line, which is the server's own,
	// unambiguous statement of the same fact. 8222/tcp IS exposed by
	// tcnats.Run (nats.go's defaultOptions includes it in
	// WithExposedPorts), so the HTTP option remains available later if the
	// log line ever proves unreliable.
	nats, err := tcnats.Run(ctx, "nats:2.10-alpine",
		testcontainers.WithWaitStrategyAndDeadline(startupTimeout(t),
			wait.ForAll(
				wait.ForListeningPort("4222/tcp").WithStartupTimeout(startupTimeout(t)),
				wait.ForLog("Server is ready").WithStartupTimeout(startupTimeout(t)),
			)),
	)
	if err != nil {
		return "", fmt.Errorf("start nats: %w", err)
	}
	uri, err := nats.ConnectionString(ctx)
	if err != nil {
		return "", fmt.Errorf("nats dsn: %w", err)
	}
	return uri, nil
}

// One OpenFGA server is shared by every test in a binary, for the same
// reason Postgres is: the per-container cost is dominated by starting and
// reaping it, not by the handful of API calls a test makes against it.
//
// Isolation here is by store rather than by database. Callers already
// name their store when they build a client, so a test that wants its own
// tuples asks for its own store name — see the callers, which pass
// IsolationKey(t). Two callers naming the same store share it deliberately.
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
