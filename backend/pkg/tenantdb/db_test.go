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

func TestWithTenantRejectsBadTenantID(t *testing.T) {
	db := openMigrated(t)
	err := db.WithTenant(context.Background(), "not-a-uuid", func(tx *gorm.DB) error { return nil })
	require.ErrorIs(t, err, tenantdb.ErrInvalidTenant)
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, db.Migrate(context.Background(), testMigrations)) // second run: no-op
}

func TestLintRLSFlagsUnprotectedTable(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID:  "0002_bad_table",
		SQL: `CREATE TABLE naughty (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"naughty"}, bad)
}
