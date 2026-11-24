package pharmacy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // External test verifies the Pharmacy-owned migration boundary.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type pharmacyBeforeLatestObservations struct{ *pharmacy.Module }

func (m pharmacyBeforeLatestObservations) Migrations() []tenantdb.Migration {
	all := m.Module.Migrations()
	for i, migration := range all {
		if migration.ID == "0069_pharmacy_stocktake_latest_observations" {
			return all[:i]
		}
	}
	panic("stocktake observation migration missing")
}
func TestStocktakeHospitalDifferencesMigrationPreservesAuthority(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "unchanged"
		if changed {
			name = "changed authority refuses migration"
		}
		t.Run(name, func(t *testing.T) {
			_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{pharmacyBeforeLatestObservations{pharmacy.New()}}})
			if changed {
				require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_latest_observations_drift", SQL: `ALTER TABLE pharmacy_stocktake_hospital_evidence DISABLE ROW LEVEL SECURITY;`}}))
			}
			var candidate tenantdb.Migration
			for _, migration := range pharmacy.New().Migrations() {
				if migration.ID == "0069_pharmacy_stocktake_latest_observations" {
					candidate = migration
				}
			}
			require.NotEmpty(t, candidate.SQL)
			err := db.Migrate(t.Context(), []tenantdb.Migration{candidate})
			if changed {
				require.ErrorContains(t, err, "stocktake observation planning requires unchanged owner authority")
				return
			}
			require.NoError(t, err)
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var intact bool
				require.NoError(t, tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()AND pharmacy_stocktake_hospital_choice_integrity()AND pharmacy_operating_choice_integrity()`).Scan(&intact).Error)
				require.True(t, intact)
				return nil
			}))
		})
	}
}
