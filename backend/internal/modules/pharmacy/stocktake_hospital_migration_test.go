package pharmacy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type pharmacyBeforeHospitalStocktake struct{ *pharmacy.Module }

func (m pharmacyBeforeHospitalStocktake) Migrations() []tenantdb.Migration {
	var prior []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID == "0066_pharmacy_stocktake_hospital" {
			break
		}
		prior = append(prior, migration)
	}
	return prior
}

func TestStocktakeHospitalMigrationPreservesSelectionAuthority(t *testing.T) {
	for _, scenario := range []struct{ name, tamper string }{
		{"unchanged authority", ""},
		{"altered selection source", `CREATE OR REPLACE FUNCTION pharmacy_operating_choice_context(t uuid,e uuid)RETURNS boolean LANGUAGE sql STABLE AS $$SELECT true$$;`},
		{"altered organization scope", `ALTER POLICY tenant_isolation ON pharmacy_organizations USING(true)WITH CHECK(true);`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			module := pharmacy.New()
			_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{pharmacyBeforeHospitalStocktake{module}}})
			var candidate tenantdb.Migration
			for _, migration := range module.Migrations() {
				if migration.ID == "0066_pharmacy_stocktake_hospital" {
					candidate = migration
				}
			}
			require.NotEmpty(t, candidate.SQL)
			if scenario.tamper != "" {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_hospital_stocktake_prior_drift", SQL: scenario.tamper}}))
				require.ErrorContains(t, db.Migrate(ctx, []tenantdb.Migration{candidate}), "requires unchanged pharmacy selection authority")
				var exists bool
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Raw(`SELECT to_regprocedure('pharmacy_stocktake_hospital_frame(uuid,uuid,text)')IS NOT NULL`).Scan(&exists).Error
				}))
				require.False(t, exists, "a refused migration must roll back all new hospital authority")
				return
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{candidate}))
			var intact bool
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT pharmacy_operating_choice_integrity()`).Scan(&intact).Error
			}))
			require.True(t, intact, "new evidence must retain restrictive selection policies on every pharmacy table")
		})
	}
}
