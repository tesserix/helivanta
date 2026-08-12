package tenantdb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/tenantdb"
)

var testMigrations = []tenantdb.Migration{{
	ID: "0001_widgets",
	SQL: `
		CREATE TABLE widgets (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  name text NOT NULL,
		  created_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE widgets ENABLE ROW LEVEL SECURITY;
		ALTER TABLE widgets FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON widgets
		  USING (hms_tenant_visible(tenant_id))
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		CREATE INDEX ON widgets (tenant_id, created_at DESC);`,
}}

type widget struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	TenantID uuid.UUID
	Name     string
}

func (widget) TableName() string { return "widgets" }

func openMigrated(t *testing.T) *tenantdb.DB {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	// hms_tenant_visible is a platform migration (owned by this package but
	// applied like any module's); widgets' policy calls it, so it must exist
	// before widgets is created.
	require.NoError(t, db.Migrate(context.Background(), tenantdb.Migrations()))
	require.NoError(t, db.Migrate(context.Background(), testMigrations))
	return db
}

func TestTenantIsolation(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenantA), Name: "a-widget"}).Error
	}))

	// Tenant B sees nothing of tenant A's data.
	var got []widget
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Find(&got).Error
	}))
	require.Empty(t, got)

	// Tenant B cannot forge a row claiming tenant A (WITH CHECK).
	err := db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenantA), Name: "forged"}).Error
	})
	require.Error(t, err)
}

// TestWithAdminBypassesRLSAcrossTenants proves WithAdmin (admin/
// BYPASSRLS pool) can see a tenant-scoped table's rows regardless of
// tenant, unlike WithTenant (which is scoped to one tenant's GUC) or
// WithSystem (whose whole contract is that a tenant-scoped read comes
// back empty — see its doc). This is what the permission reconciler
// relies on to enumerate every tenant's memberships in one boot-time
// pass, with no single tenant to scope the read by.
func TestWithAdminBypassesRLSAcrossTenants(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenantA), Name: "a-widget"}).Error
	}))
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenantB), Name: "b-widget"}).Error
	}))

	var viaAdmin []widget
	require.NoError(t, db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Order("name").Find(&viaAdmin).Error
	}))
	require.Len(t, viaAdmin, 2, "WithAdmin must bypass RLS to see every tenant's rows")
	require.Equal(t, "a-widget", viaAdmin[0].Name)
	require.Equal(t, "b-widget", viaAdmin[1].Name)
}

func TestWithTenantRejectsBadTenantID(t *testing.T) {
	db := openMigrated(t)
	err := db.WithTenant(context.Background(), "not-a-uuid", func(tx *gorm.DB) error { return nil })
	require.ErrorIs(t, err, tenantdb.ErrInvalidTenant)
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, db.Migrate(context.Background(), testMigrations)) // second run: no-op
}

// Tenant isolation rests entirely on APP_DATABASE_URL naming a role that
// cannot bypass RLS. Nothing asserted that at runtime, and the two
// default DSNs differ only by username — so pasting the admin URL into
// APP_DATABASE_URL silently disabled isolation with every test still
// green. Open must refuse.
func TestOpenRefusesAppPoolThatCanBypassRLS(t *testing.T) {
	_, adminDSN := testutil.StartPostgres(t)

	_, err := tenantdb.Open(adminDSN, adminDSN)

	require.Error(t, err)
	require.Contains(t, err.Error(), "bypass")
}

func TestLintRLSFlagsUnprotectedTable(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0002_bad_tables",
		SQL: `
			-- No RLS at all.
			CREATE TABLE naughty_none (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);

			-- Full policy (USING + WITH CHECK) but RLS is not forced.
			CREATE TABLE naughty_not_forced (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_not_forced ENABLE ROW LEVEL SECURITY;
			CREATE POLICY p ON naughty_not_forced
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

			-- Enabled + forced, but policy only carries USING (no WITH CHECK).
			CREATE TABLE naughty_using_only (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_using_only ENABLE ROW LEVEL SECURITY;
			ALTER TABLE naughty_using_only FORCE ROW LEVEL SECURITY;
			CREATE POLICY p ON naughty_using_only
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid);

			-- Enabled + forced, but policy only carries WITH CHECK (INSERT-only;
			-- pg_policies shows qual NULL for INSERT-only policies).
			CREATE TABLE naughty_check_only (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_check_only ENABLE ROW LEVEL SECURITY;
			ALTER TABLE naughty_check_only FORCE ROW LEVEL SECURITY;
			CREATE POLICY p ON naughty_check_only
			  FOR INSERT WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

			-- The failure a hurried module author actually makes: forget tenant_id
			-- entirely. The old lint could not see this — it only inspected tables
			-- that already had the column — so the table had no tenancy, no RLS and
			-- no policy, and every check passed green.
			CREATE TABLE naughty_no_tenant_column (id uuid PRIMARY KEY, note text);

			-- A policy that inlines the old predicate instead of calling the shared
			-- function. Allowed to exist, it would silently opt out of any future
			-- group-visibility change.
			CREATE TABLE naughty_inlined_predicate (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_inlined_predicate ENABLE ROW LEVEL SECURITY;
			ALTER TABLE naughty_inlined_predicate FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON naughty_inlined_predicate
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{
		"naughty_check_only: no policy with both USING and WITH CHECK",
		"naughty_inlined_predicate: USING does not call hms_tenant_visible",
		"naughty_no_tenant_column: no tenant_id column",
		"naughty_none: row-level security not enabled and forced",
		"naughty_not_forced: row-level security not enabled and forced",
		"naughty_using_only: no policy with both USING and WITH CHECK",
	}, bad)
	require.NotContains(t, bad, "widgets")
}

// TestLintRLSFlagsWideningWithCheck covers MEDIUM 1 from the final
// whole-branch review: the asymmetry between USING and WITH CHECK is
// deliberate — reads may widen to hospital-group visibility later, writes
// must always land in exactly one tenant — so a policy whose WITH CHECK
// also calls hms_tenant_visible has to be reported, not just one that lacks
// a WITH CHECK entirely.
func TestLintRLSFlagsWideningWithCheck(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0003_naughty_check_widens",
		SQL: `
			CREATE TABLE naughty_check_widens (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_check_widens ENABLE ROW LEVEL SECURITY;
			ALTER TABLE naughty_check_widens FORCE ROW LEVEL SECURITY;
			CREATE POLICY p ON naughty_check_widens
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (hms_tenant_visible(tenant_id));`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Contains(t, bad, "naughty_check_widens: WITH CHECK calls hms_tenant_visible instead of pinning to one tenant")
}

// TestLintRLSFlagsSecondPermissivePolicy covers MEDIUM 2: has_policy and
// uses_function used to be independent EXISTS clauses that could each be
// satisfied by a different policy row on the same table. RLS permissive
// policies OR together, so a second policy — here a wide-open USING (true)
// — defeats isolation entirely even while a first, correct policy still
// makes every per-clause EXISTS true. The lint now also fails outright on
// any table carrying other than exactly one policy.
func TestLintRLSFlagsSecondPermissivePolicy(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0003_naughty_extra_policy",
		SQL: `
			CREATE TABLE naughty_extra_policy (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_extra_policy ENABLE ROW LEVEL SECURITY;
			ALTER TABLE naughty_extra_policy FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON naughty_extra_policy
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE POLICY wide_open ON naughty_extra_policy
			  AS PERMISSIVE FOR SELECT USING (true);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Contains(t, bad, "naughty_extra_policy: expected exactly one policy, found a different count")
}

// TestLintRLSFlagsTableOutsidePublicSchema covers the first escape from
// MEDIUM 3: a tenant table living in a schema other than public used to
// never be enumerated at all, because the old query filtered on
// nspname = 'public'.
func TestLintRLSFlagsTableOutsidePublicSchema(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0003_naughty_wrong_schema",
		SQL: `
			CREATE SCHEMA naughty_schema;
			CREATE TABLE naughty_schema.naughty_wrong_schema (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Contains(t, bad, "naughty_wrong_schema: tenant table lives outside the public schema")
}

// TestLintRLSFlagsPartitionedParent covers the second escape from MEDIUM 3:
// a partitioned parent has relkind 'p', not 'r', so the old
// `c.relkind = 'r'` filter skipped it entirely regardless of RLS state.
func TestLintRLSFlagsPartitionedParent(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0003_naughty_partitioned",
		SQL: `
			CREATE TABLE naughty_partitioned_parent (id uuid, tenant_id uuid NOT NULL)
			  PARTITION BY LIST (tenant_id);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Contains(t, bad, "naughty_partitioned_parent: tenant table is a partitioned parent or foreign table, not a plain table")
}

// TestLintRLSFlagsForeignTable covers the same relkind escape as the
// partitioned-parent case, but for foreign tables (relkind 'f'), which the
// old `relkind = 'r'` filter also skipped.
func TestLintRLSFlagsForeignTable(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0003_naughty_foreign",
		SQL: `
			CREATE EXTENSION IF NOT EXISTS postgres_fdw;
			CREATE SERVER naughty_loopback FOREIGN DATA WRAPPER postgres_fdw
			  OPTIONS (host 'localhost', dbname 'postgres');
			CREATE USER MAPPING FOR CURRENT_USER SERVER naughty_loopback
			  OPTIONS (user 'postgres');
			CREATE FOREIGN TABLE naughty_foreign_table (id uuid, tenant_id uuid NOT NULL)
			  SERVER naughty_loopback OPTIONS (table_name 'irrelevant');`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Contains(t, bad, "naughty_foreign_table: tenant table is a partitioned parent or foreign table, not a plain table")
}

// TestLintRLSFlagsNonInvokerView covers the sharpest escape from MEDIUM 3:
// a view over a tenant table, created by the migration (owner) role
// without security_invoker = true, reads the underlying table with the
// owner's RLS-bypassing rights regardless of who queries the view.
func TestLintRLSFlagsNonInvokerView(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID: "0003_naughty_view",
		SQL: `
			CREATE TABLE naughty_view_base (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
			ALTER TABLE naughty_view_base ENABLE ROW LEVEL SECURITY;
			ALTER TABLE naughty_view_base FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON naughty_view_base
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE VIEW naughty_exposed_view AS SELECT * FROM naughty_view_base;
			CREATE VIEW compliant_invoker_view WITH (security_invoker = true)
			  AS SELECT * FROM naughty_view_base;`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	// bad entries are "<table>: <reason>", never the bare table name, so
	// require.NotContains(bad, "compliant_invoker_view") could never fail
	// regardless of what the lint reported — testify's slice Contains needs
	// exact element equality. Assert the exact, complete set instead: one
	// entry for the non-invoker view, nothing for the compliant view or the
	// (compliant) base table.
	require.Equal(t, []string{
		"naughty_exposed_view: view over a tenant table is not security_invoker",
	}, bad)
}
