package testinfra

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openDSN(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func hostPortAndDatabase(t *testing.T, dsn string) (hostPort, database string) {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	return u.Host, u.Path
}

// The point of sharing: two callers land on the same server, so a package
// boots Postgres once rather than once per test (#765).
func TestStartPostgresReusesOneServer(t *testing.T) {
	_, adminA := StartPostgres(t)
	_, adminB := StartPostgres(t)

	hostA, dbA := hostPortAndDatabase(t, adminA)
	hostB, dbB := hostPortAndDatabase(t, adminB)

	require.Equal(t, hostA, hostB, "callers should share one server")
	require.NotEqual(t, dbA, dbB, "callers should not share a database")
}

// Sharing a server must not mean sharing state: a row written by one
// caller has to be invisible to another, or tests would contaminate each
// other in ways that only show up as ordering-dependent failures.
func TestStartPostgresIsolatesCallers(t *testing.T) {
	_, adminA := StartPostgres(t)
	_, adminB := StartPostgres(t)

	dbA := openDSN(t, adminA)
	dbB := openDSN(t, adminB)

	require.NoError(t, dbA.Exec(`CREATE TABLE only_in_a (id int)`).Error)
	require.NoError(t, dbA.Exec(`INSERT INTO only_in_a VALUES (1)`).Error)

	var count int64
	err := dbB.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'only_in_a'`).Scan(&count).Error
	require.NoError(t, err)
	require.Zero(t, count, "table created by one caller leaked into another's database")
}

// OpenFGA is shared the same way Postgres is. Isolation is by store name,
// which callers choose (they pass t.Name()), so this only has to prove the
// server itself is not rebooted per caller.
func TestStartOpenFGAReusesOneServer(t *testing.T) {
	first := StartOpenFGA(t)
	second := StartOpenFGA(t)

	require.NotEmpty(t, first)
	require.Equal(t, first, second, "callers should share one OpenFGA server")
}

// hms_app is the role the application connects as, and forced-RLS policies
// only mean anything if it cannot bypass them. Creating it once per server
// rather than once per database must not change that.
func TestAppRoleCannotBypassRLS(t *testing.T) {
	_, adminDSN := StartPostgres(t)
	admin := openDSN(t, adminDSN)

	var bypassRLS bool
	require.NoError(t, admin.Raw(`SELECT rolbypassrls FROM pg_roles WHERE rolname = 'hms_app'`).Scan(&bypassRLS).Error)
	require.False(t, bypassRLS, "hms_app must not be able to bypass row-level security")
}

// The app role needs its privileges in every database, not just the one
// that happened to be created first — those grants are per database even
// though the role itself is cluster-wide.
func TestAppRoleCanUseANewDatabase(t *testing.T) {
	appDSN, adminDSN := StartPostgres(t)
	admin := openDSN(t, adminDSN)
	require.NoError(t, admin.Exec(`CREATE TABLE widgets (id int)`).Error)
	require.NoError(t, admin.Exec(`INSERT INTO widgets VALUES (1)`).Error)

	app := openDSN(t, appDSN)
	var got int64
	require.NoError(t, app.Raw(`SELECT count(*) FROM widgets`).Scan(&got).Error)
	require.Equal(t, int64(1), got)
}

func TestParseStartupTimeoutDefaultsWhenUnset(t *testing.T) {
	d, err := parseStartupTimeout("")
	require.NoError(t, err)
	require.Equal(t, defaultStartupTimeout, d)
}

func TestParseStartupTimeoutHonoursOverride(t *testing.T) {
	d, err := parseStartupTimeout("90s")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, d)
}

// A malformed or non-positive value must be an error rather than a quiet
// fall back to the default: someone who sets this env var is already
// chasing a timeout, and silently ignoring their value would send them
// looking in the wrong place.
func TestParseStartupTimeoutRejectsBadValues(t *testing.T) {
	for _, raw := range []string{"abc", "90", "0", "-30s"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseStartupTimeout(raw)
			require.Error(t, err)
			require.Contains(t, err.Error(), raw)
		})
	}
}

// Guards the trap documented on startupTimeout: raising only the outer
// ForAll deadline leaves each child strategy on wait's own 60-second
// default, so the container still gives up after a minute regardless.
// That was the exact mistake behind the flaky coverage gate (#763), and
// it is invisible at the call site — the code reads as though the longer
// timeout applies.
//
// Only the inner timeout is asserted: MultiStrategy keeps its deadline in
// an unexported field and its Timeout() accessor reports a different one,
// so the outer deadline cannot be read back from here. The inner value is
// the one that silently defaults, which makes it the one worth pinning.
func TestWaitStrategyCarriesInnerStrategyTimeout(t *testing.T) {
	const timeout = 4 * time.Minute

	req := testcontainers.GenericContainerRequest{}
	opt := testcontainers.WithWaitStrategyAndDeadline(timeout,
		wait.ForListeningPort("4222/tcp").WithStartupTimeout(timeout))
	require.NoError(t, opt.Customize(&req))

	outer, ok := req.WaitingFor.(*wait.MultiStrategy)
	require.True(t, ok, "expected the customizer to wrap strategies in a MultiStrategy")
	require.Len(t, outer.Strategies, 1)

	inner, ok := outer.Strategies[0].(*wait.HostPortStrategy)
	require.True(t, ok)
	require.NotNil(t, inner.Timeout(), "inner strategy left on wait's 60s default")
	require.Equal(t, timeout, *inner.Timeout(), "inner strategy timeout not applied")

	// The hazard itself: without the explicit call the strategy reports no
	// timeout and wait falls back to its 60-second default. If a future
	// version of testcontainers propagates the outer deadline to children,
	// this assertion breaks and the comment above can be simplified.
	bare := wait.ForListeningPort("4222/tcp")
	require.Nil(t, bare.Timeout(), "a bare strategy is expected to carry no timeout of its own")
}

// psql reports a failed statement through its EXIT CODE, not through the
// error testcontainers' Exec returns — that error only reports whether the
// exec mechanism itself worked. Every call site here used to read
// `if _, _, err := pgCont.Exec(...)`, discarding the exit code, so a psql
// command that failed left err nil and the harness carried on against a
// database that had never been set up.
//
// That is not hypothetical: it surfaced on PR #878 as
// TestCrossTenantWriteIsRejectedNotMalformed failing with "permission denied
// for table outbox_events" — a privilege error from a GRANT that had silently
// not applied, in a test whose entire job is to distinguish an RLS rejection
// from other failures. A security regression guard that can fail for a reason
// unrelated to the control it guards is not guarding it.
//
// This asserts the exit code is actually consulted. Without that check the
// call returns nil and this test fails.
func TestRunSQLReportsAFailedStatement(t *testing.T) {
	sharedPostgres(t)

	err := runSQL(t.Context(), "hms", `GRANT USAGE ON SCHEMA public TO no_such_role_exists;`)

	require.Error(t, err, "a psql command that fails must be reported, not swallowed")
	require.Contains(t, err.Error(), "no_such_role_exists",
		"the error must carry psql's own output, or the cause is invisible at the call site")
}

// The success path must stay quiet, so the check above cannot be satisfied by
// something that simply always errors.
func TestRunSQLAcceptsAWorkingStatement(t *testing.T) {
	sharedPostgres(t)

	require.NoError(t, runSQL(t.Context(), "hms", `SELECT 1;`))
}
