package pharmacy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // External test exercises the Pharmacy-owned choice migration boundary.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type pharmacyBeforeHospitalChoices struct{ *pharmacy.Module }

func (m pharmacyBeforeHospitalChoices) Migrations() []tenantdb.Migration {
	all := m.Module.Migrations()
	for i, migration := range all {
		if migration.ID == "0067_pharmacy_stocktake_hospital_choices" {
			return all[:i]
		}
	}
	panic("hospital stocktake choice migration missing")
}

func TestStocktakeHospitalChoiceMigrationPreservesAuthority(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "unchanged"
		if changed {
			name = "changed authority refuses migration"
		}
		t.Run(name, func(t *testing.T) {
			_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{pharmacyBeforeHospitalChoices{pharmacy.New()}}})
			ctx := tenantdb.WithActor(context.Background(), "hospital-choice-migration-test")
			if changed {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_hospital_choice_prior_drift", SQL: `ALTER TABLE pharmacy_stocktake_hospital_evidence DISABLE ROW LEVEL SECURITY;`}}))
			}
			var candidate tenantdb.Migration
			for _, migration := range pharmacy.New().Migrations() {
				if migration.ID == "0067_pharmacy_stocktake_hospital_choices" {
					candidate = migration
				}
			}
			require.NotEmpty(t, candidate.SQL)
			err := db.Migrate(ctx, []tenantdb.Migration{candidate})
			if changed {
				require.ErrorContains(t, err, "hospital outlet choices require unchanged owner authority")
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					var n int64
					require.NoError(t, tx.Raw(`SELECT count(*)FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=current_schema()AND p.proname='pharmacy_stocktake_hospital_choices_context'`).Scan(&n).Error)
					require.Zero(t, n)
					return nil
				}))
				return
			}
			require.NoError(t, err)
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var intact bool
				require.NoError(t, tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()AND pharmacy_operating_choice_integrity()AND pharmacy_stocktake_hospital_choice_integrity()`).Scan(&intact).Error)
				require.True(t, intact)
				return nil
			}))
		})
	}
}
