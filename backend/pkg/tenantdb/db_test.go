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
		  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
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
			  FOR INSERT WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{
		"naughty_check_only",
		"naughty_none",
		"naughty_not_forced",
		"naughty_using_only",
	}, bad)
	require.NotContains(t, bad, "widgets")
}
