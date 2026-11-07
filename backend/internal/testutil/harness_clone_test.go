package testutil

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestTemplateBuildRejectsATableWithoutForcedRLS(t *testing.T) {
	app, admin, system := testinfra.StartPostgres(t)
	migs := append(bootstrap.PlatformMigrations(), tenantdb.Migration{ID: "9999_lintprobe", SQL: `CREATE TABLE lint_probe (id int)`})

	err := migrateTemplate(migs)(app, admin, system)
	require.ErrorContains(t, err, "tenant tables must carry forced RLS")
	require.ErrorContains(t, err, "lint_probe")
}

func TestCloneLedgerMustMatchTheMigrations(t *testing.T) {
	_, db, _, ctx := NewHarness(t, HarnessOptions{})
	migs := bootstrap.PlatformMigrations()
	require.NoError(t, verifyCloneLedger(ctx, db, migs))

	extra := append(slices.Clone(migs), tenantdb.Migration{ID: "9999_not_in_template", SQL: `SELECT 1`})
	require.ErrorContains(t, verifyCloneLedger(ctx, db, extra), "missing [9999_not_in_template], unexpected []")

	last := migs[len(migs)-1].ID
	require.ErrorContains(t, verifyCloneLedger(ctx, db, migs[:len(migs)-1]), "missing [], unexpected ["+last+"]")
}
