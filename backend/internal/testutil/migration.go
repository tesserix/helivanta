package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// MigratedBefore returns a database migrated up to, but not including, the
// migration named before (platform first, then each module in order), and
// the migrations still to apply. A test seeds rows the later migration must
// see, then applies the rest. The owner role is NOSUPERUSER NOBYPASSRLS, as
// in production, so a migration that reads tenant rows is tested under
// forced RLS (#1287).
func MigratedBefore(t *testing.T, before string, modules ...platform.Module) (*tenantdb.DB, context.Context, []tenantdb.Migration) {
	t.Helper()
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	migs := bootstrap.PlatformMigrations()
	for _, m := range modules {
		migs = append(migs, m.Migrations()...)
	}
	for i, m := range migs {
		if m.ID == before {
			require.NoError(t, db.Migrate(ctx, migs[:i]))
			return db, ctx, migs[i:]
		}
	}
	t.Fatalf("no migration %s", before)
	return nil, nil, nil
}

// RequireForcedRLS fails unless every named table still forces row-level
// security: a migration that lifts it to count rows must put it back.
func RequireForcedRLS(t *testing.T, ctx context.Context, db *tenantdb.DB, tables ...string) {
	t.Helper()
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	for _, table := range tables {
		require.NotContains(t, bad, table, "row-level security no longer forced on %s", table)
	}
}
