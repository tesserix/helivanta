package pharmacy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // External test verifies Pharmacy's source recovery migration boundary.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type pharmacyBeforeSourceRecovery struct{ *pharmacy.Module }

func (m pharmacyBeforeSourceRecovery) Migrations() []tenantdb.Migration {
	all := m.Module.Migrations()
	for i, migration := range all {
		if migration.ID == "0070_pharmacy_stocktake_source_recovery" {
			return all[:i]
		}
	}
	panic("stocktake source recovery migration missing")
}
func TestStocktakeHospitalSourceRecoveryMigrationPreservesAuthority(t *testing.T) {
	cases := []struct{ name, change string }{
		{"unchanged", ""},
		{"source drift", `ALTER TABLE pharmacy_stocktake_hospital_evidence DISABLE ROW LEVEL SECURITY;`},
		{"private choice drift", `DROP POLICY hospital_stocktake_choice_purpose ON pharmacy_catalogue_products;`},
		{"unique source index missing", `DROP INDEX pharmacy_staff_binding_current;`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{pharmacyBeforeSourceRecovery{pharmacy.New()}}})
			if test.change != "" {
				require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_recovery_source_drift", SQL: test.change}}))
			}
			var candidate tenantdb.Migration
			for _, migration := range pharmacy.New().Migrations() {
				if migration.ID == "0070_pharmacy_stocktake_source_recovery" {
					candidate = migration
				}
			}
			require.NotEmpty(t, candidate.SQL)
			err := db.Migrate(t.Context(), []tenantdb.Migration{candidate})
			if test.change != "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var intact bool
				require.NoError(t, tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()AND pharmacy_stocktake_hospital_choice_integrity()AND pharmacy_operating_choice_integrity()AND pharmacy_stocktake_source_recovery_integrity()`).Scan(&intact).Error)
				require.True(t, intact)
				return nil
			}))
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_recovery_index_drift", SQL: `DROP INDEX pharmacy_staff_binding_current;`}}))
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var intact bool
				require.NoError(t, tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()OR pharmacy_stocktake_source_recovery_integrity()`).Scan(&intact).Error)
				require.False(t, intact)
				return nil
			}))
		})
	}
}
