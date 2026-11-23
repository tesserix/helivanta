package workforce_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type workforceBeforeStocktakeHistory struct{ *workforce.Module }

func (m workforceBeforeStocktakeHistory) Migrations() []tenantdb.Migration {
	var prior []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID == "0122_workforce_stocktake_actor_history" {
			break
		}
		prior = append(prior, migration)
	}
	return prior
}

func TestHospitalStocktakeActorHistoryMigrationPreservesAuthority(t *testing.T) {
	for _, scenario := range []struct{ name, tamper string }{
		{"unchanged authority", ""},
		{"changed unrelated selection guard", `CREATE OR REPLACE FUNCTION workforce_assignment_selection_context(t uuid,h uuid) RETURNS boolean LANGUAGE sql STABLE AS $$SELECT true$$;`},
		{"changed history owner policy", `ALTER POLICY login_link_isolation ON workforce_login_link_history USING(true) WITH CHECK(true);`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			module := workforce.New()
			_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{workforceBeforeStocktakeHistory{module}}})
			read := func(query string, out any) {
				t.Helper()
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Raw(query).Scan(out).Error }))
			}
			var original, after struct{ Assignment, Operating string }
			definitions := `SELECT pg_get_functiondef('workforce_assignment_selection_integrity()'::regprocedure) assignment,pg_get_functiondef('workforce_operating_selection_integrity()'::regprocedure) operating`
			read(definitions, &original)
			var migration tenantdb.Migration
			for _, next := range module.Migrations() {
				if next.ID == "0122_workforce_stocktake_actor_history" {
					migration = next
				}
			}
			require.NotEmpty(t, migration.SQL)
			if scenario.tamper != "" {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_hospital_stocktake_history_tamper", SQL: scenario.tamper}}))
				require.ErrorContains(t, db.Migrate(ctx, []tenantdb.Migration{migration}), "requires unchanged prior Workforce authority")
				read(definitions, &after)
				require.Equal(t, original, after)
				var exists bool
				read(`SELECT to_regprocedure('workforce_stocktake_history_support()') IS NOT NULL`, &exists)
				require.False(t, exists, "refused migration must roll back new definitions")
				return
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{migration}))
			var intact struct{ History, Assignment, Operating bool }
			read(`SELECT workforce_stocktake_history_support() history,workforce_assignment_selection_integrity() assignment,workforce_operating_selection_integrity() operating`, &intact)
			require.True(t, intact.History)
			require.True(t, intact.Assignment)
			require.True(t, intact.Operating)
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_hospital_stocktake_history_trigger_tamper", SQL: `ALTER TABLE workforce_login_link_history DISABLE TRIGGER workforce_login_link_history_immutable;`}}))
			read(`SELECT workforce_stocktake_history_support() history`, &intact)
			require.False(t, intact.History, "missing canonical immutability must disable history review")
		})
	}
}
