package testutil_test

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type migrationModule struct {
	logModule
	migs []tenantdb.Migration
}

func (m migrationModule) Migrations() []tenantdb.Migration { return m.migs }

func probeModule() migrationModule {
	return migrationModule{migs: []tenantdb.Migration{{ID: "9999_probe_" + rand.Text(), SQL: `SELECT 1`}}}
}

func templateCount(t *testing.T, db *tenantdb.DB) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pg_database WHERE datname LIKE 'hms\_test\_template\_%'`).Scan(&n).Error
	}))
	return n
}

func appliedAt(t *testing.T, db *tenantdb.DB, id string) time.Time {
	t.Helper()
	var at time.Time
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT applied_at FROM schema_migrations WHERE id = ?`, id).Scan(&at).Error
	}))
	require.False(t, at.IsZero(), "migration %s was not applied", id)
	return at
}

func TestHarnessesWithTheSameMigrationsShareATemplate(t *testing.T) {
	module := probeModule()
	_, first, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	before := templateCount(t, first)

	_, second, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	require.Equal(t, before, templateCount(t, second), "the same migrations built a second template")
	require.Equal(t, appliedAt(t, first, module.migs[0].ID), appliedAt(t, second, module.migs[0].ID),
		"the second harness migrated its own database instead of cloning the template")

	_, other, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{probeModule()}})
	require.Equal(t, before+1, templateCount(t, other), "different migrations must get their own template")
}
