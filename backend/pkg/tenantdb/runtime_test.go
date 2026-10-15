package tenantdb_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestRuntimePoolRejectsSchemaAndWholeTenantAuthority(t *testing.T) {
	app, owner, system := testutil.StartPostgres(t)
	runtime, err := tenantdb.OpenRuntime(t.Context(), app)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	require.NoError(t, runtime.WithTenant(t.Context(), uuid.NewString(), func(tx *gorm.DB) error {
		var tenant string
		require.NoError(t, tx.Raw(`SELECT current_setting('app.tenant_id')`).Scan(&tenant).Error)
		require.NotEmpty(t, tenant)
		return nil
	}))
	for _, dsn := range []string{owner, system} {
		_, err := tenantdb.OpenRuntime(t.Context(), dsn)
		require.Error(t, err, "a runtime-only pool must reject owner, inherited DDL and bypass identities")
	}
	require.Error(t, runtime.Migrate(t.Context(), testMigrations))
	require.Error(t, runtime.WithAdmin(t.Context(), func(*gorm.DB) error { t.Fatal("admin pool was exposed"); return nil }))
	require.Error(t, runtime.WithAllTenants(t.Context(), func(*gorm.DB) error { t.Fatal("system pool was exposed"); return nil }))
	_, err = runtime.LintRLS(t.Context())
	require.Error(t, err)
}
