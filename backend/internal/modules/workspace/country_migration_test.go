package workspace_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workspace" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

const pakistanMigration = "0003_workspace_india_only"

func insertPolicyScope(tx *gorm.DB, country, currency string) error {
	return tx.Exec(`INSERT INTO workspace_country_policy_scopes (tenant_id,id,country,jurisdiction,category,operation,currency,owner_subject) VALUES (?,?,?,?,?,?,?,?)`,
		testutil.TenantA, uuid.Must(uuid.NewV7()).String(), country, "TEST-REGION", "otc", "order", currency, "user-owner").Error
}

// #1287 D2: one tenant's Pakistan policy refuses the migration, which
// changes nothing; the owner sees the row despite forced RLS.
func TestCountryPolicyMigrationRefusesPakistanRows(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, workspace.New())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return insertPolicyScope(tx, "PK", "PKR") }))
	err := db.Migrate(ctx, rest)
	require.Error(t, err)
	// The count's own refusal, not the constraint check that would also
	// catch it: the count is what must see past forced RLS.
	require.Contains(t, err.Error(), "#1287: 1 non-India rows in workspace_country_policy_scopes")
	testutil.RequireForcedRLS(t, ctx, db, "workspace_country_policy_scopes")
	// Refused, not changed: the row is still there and still Pakistan.
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Raw(`SELECT count(*) FROM workspace_country_policy_scopes WHERE country='PK'`).Scan(&n).Error)
		require.Equal(t, int64(1), n)
		return nil
	}))
}

func TestCountryPolicyRefusesPakistan(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, workspace.New())
	require.NoError(t, db.Migrate(ctx, rest))
	testutil.RequireForcedRLS(t, ctx, db, "workspace_country_policy_scopes")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return insertPolicyScope(tx, "IN", "INR") }))
	require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return insertPolicyScope(tx, "PK", "PKR") }))
}
