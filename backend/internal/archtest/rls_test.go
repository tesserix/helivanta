package archtest

import (
	"context"
	"testing"
	"time"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// TestAllMigrationsPassRLSLint applies every migration to a fresh
// database and re-runs the boot-time RLS linter, so an unprotected
// tenant table fails in CI, not at deploy.
func TestAllMigrationsPassRLSLint(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	migs := bootstrap.PlatformMigrations()
	for _, mod := range allModules() {
		migs = append(migs, mod.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bad, err := db.LintRLS(ctx)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if len(bad) > 0 {
		t.Errorf("tables missing forced RLS: %v", bad)
	}
}
