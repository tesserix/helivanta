package testinfra

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// execBuild returns a template build that runs stmts as the owner and counts its calls.
func execBuild(calls *atomic.Int32, stmts ...string) TemplateBuild {
	return func(_, admin, _ string) error {
		calls.Add(1)
		db, err := openSilent(admin)
		if err != nil {
			return err
		}
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		defer func() { _ = sqlDB.Close() }()
		for _, s := range stmts {
			if err := db.Exec(s).Error; err != nil {
				return err
			}
		}
		return nil
	}
}

func uniqueKey() string { return rand.Text() }

func count(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw(query, args...).Scan(&n).Error)
	return n
}

func TestMigratedPostgresClonesTheTemplate(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	key := uniqueKey()
	app, admin, system := StartMigratedPostgres(t, HarnessOwner, key, execBuild(&calls,
		`CREATE TABLE probe (id int PRIMARY KEY)`, `INSERT INTO probe VALUES (7)`))

	require.Equal(t, int64(7), count(t, openDSN(t, admin), `SELECT id FROM probe`))
	_, database := hostPortAndDatabase(t, app)
	require.True(t, strings.HasPrefix(database, "/hms_test_"), database)
	require.NotContains(t, database, "template", "callers get a clone, never the template")
	_, sysDatabase := hostPortAndDatabase(t, system)
	require.Equal(t, database, sysDatabase)

	tpl := templateName(t, HarnessOwner, key)
	require.True(t, strings.HasPrefix(tpl, "hms_test_template_"), tpl)
	u := strings.Replace(admin, database, "/"+tpl, 1)
	g, err := openSilent(u)
	if err == nil {
		err = g.Exec(`SELECT 1`).Error
	}
	require.ErrorContains(t, err, "not currently accepting connections")
}

func TestMigratedPostgresBuildsOncePerKey(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	key := uniqueKey()
	_, adminA, _ := StartMigratedPostgres(t, HarnessOwner, key, execBuild(&calls, `CREATE TABLE probe (id int)`))
	_, adminB, _ := StartMigratedPostgres(t, HarnessOwner, key, execBuild(&calls, `CREATE TABLE probe (id int)`))

	require.Equal(t, int32(1), calls.Load())
	_, dbA := hostPortAndDatabase(t, adminA)
	_, dbB := hostPortAndDatabase(t, adminB)
	require.NotEqual(t, dbA, dbB, "each caller gets its own clone")
}

func TestMigratedPostgresBuildsEachKeySeparately(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, adminA, _ := StartMigratedPostgres(t, HarnessOwner, uniqueKey(), execBuild(&calls, `CREATE TABLE only_in_a (id int)`))
	_, adminB, _ := StartMigratedPostgres(t, HarnessOwner, uniqueKey(), execBuild(&calls, `CREATE TABLE only_in_b (id int)`))

	require.Equal(t, int32(2), calls.Load())
	exists := `SELECT count(*) FROM information_schema.tables WHERE table_name = ?`
	require.Zero(t, count(t, openDSN(t, adminA), exists, "only_in_b"))
	require.Zero(t, count(t, openDSN(t, adminB), exists, "only_in_a"))
}

func TestMigratedPostgresIsolatesClones(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	key := uniqueKey()
	build := execBuild(&calls, `CREATE TABLE probe (id int)`, `INSERT INTO probe VALUES (1)`)
	_, adminA, _ := StartMigratedPostgres(t, HarnessOwner, key, build)
	require.NoError(t, openDSN(t, adminA).Exec(`INSERT INTO probe VALUES (2)`).Error)

	_, adminB, _ := StartMigratedPostgres(t, HarnessOwner, key, build)
	require.Equal(t, int64(1), count(t, openDSN(t, adminB), `SELECT count(*) FROM probe`),
		"a row written in one clone reached the template or another clone")
}

func TestMigratedPostgresReportsABuildFailureToEveryCaller(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	key := uniqueKey()
	failing := func(string, string, string) error { calls.Add(1); return errors.New("migration 0042 refused") }

	_, errA := migratedDatabase(t, HarnessOwner, key, failing)
	_, errB := migratedDatabase(t, HarnessOwner, key, failing)

	require.ErrorContains(t, errA, "migration 0042 refused")
	require.ErrorContains(t, errB, "migration 0042 refused")
	require.Equal(t, int32(1), calls.Load(), "a failed template is not rebuilt by the next caller")
}

func TestMigratedPostgresReportsABuildThatNeverReturned(t *testing.T) {
	t.Parallel()
	key := uniqueKey()
	require.Panics(t, func() {
		_, _ = migratedDatabase(t, HarnessOwner, key, func(string, string, string) error { panic("build blew up") })
	})
	_, err := migratedDatabase(t, HarnessOwner, key, func(string, string, string) error { return nil })
	require.ErrorContains(t, err, "did not finish")
}

func TestMigratedPostgresRefusesStateAClonesCannotInherit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, stmt, want string }{
		{"setting", `DO $$ BEGIN EXECUTE format('ALTER DATABASE %I SET work_mem = %L', current_database(), '8MB'); END $$`, "setting"},
		{"acl", `DO $$ BEGIN EXECUTE format('REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC', current_database()); END $$`, "privileges"},
		{"comment", `DO $$ BEGIN EXECUTE format('COMMENT ON DATABASE %I IS %L', current_database(), 'x'); END $$`, "comment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			_, err := migratedDatabase(t, HarnessOwner, uniqueKey(), execBuild(&calls, tc.stmt))
			require.ErrorContains(t, err, "a clone does not inherit")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMigratedPostgresProductionOwnerOwnsTheClone(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	_, admin, _ := StartMigratedPostgres(t, ProductionOwner, uniqueKey(), execBuild(&calls, `CREATE TABLE probe (id int)`))

	var got struct{ Who, DB, Schema, Tbl string }
	require.NoError(t, openDSN(t, admin).Raw(`SELECT current_user AS who,
		(SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()) AS db,
		(SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = 'public') AS schema,
		(SELECT tableowner FROM pg_tables WHERE tablename = 'probe') AS tbl`).Scan(&got).Error)
	require.Equal(t, struct{ Who, DB, Schema, Tbl string }{"helivanta", "helivanta", "helivanta", "helivanta"}, got)
}

func TestMigratedPostgresKeepsOwnersApart(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	key := uniqueKey()
	StartMigratedPostgres(t, HarnessOwner, key, execBuild(&calls, `CREATE TABLE probe (id int)`))
	StartMigratedPostgres(t, ProductionOwner, key, execBuild(&calls, `CREATE TABLE probe (id int)`))
	require.Equal(t, int32(2), calls.Load())
}

func templateName(t *testing.T, owner Owner, key string) string {
	t.Helper()
	v, ok := templates.Load(templateKey{owner, key})
	require.True(t, ok)
	return v.(*template).name
}
