package pharmacy_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStocktakeHospitalAuthorityIntegrityRejectsDrift(t *testing.T) {
	_, db := stocktakeHarness(t)
	read := func() bool {
		var intact bool
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()AND pharmacy_stocktake_hospital_choice_integrity()`).Scan(&intact).Error
		}))
		return intact
	}
	require.True(t, read())
	for index, scenario := range []struct{ name, tamper, restorationQuery, restore string }{
		{name: "disabled evidence isolation", tamper: `ALTER TABLE pharmacy_stocktake_hospital_evidence DISABLE ROW LEVEL SECURITY;`, restore: `ALTER TABLE pharmacy_stocktake_hospital_evidence ENABLE ROW LEVEL SECURITY;`},
		{name: "missing immutable evidence trigger", tamper: `DROP TRIGGER pharmacy_stocktake_hospital_evidence_immutable ON pharmacy_stocktake_hospital_evidence;`, restorationQuery: `SELECT pg_get_triggerdef(oid)||';'FROM pg_trigger WHERE tgrelid='pharmacy_stocktake_hospital_evidence'::regclass AND tgname='pharmacy_stocktake_hospital_evidence_immutable'`},
		{name: "broadened binding selection", tamper: `ALTER POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings USING(true);`, restorationQuery: `SELECT format('ALTER POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings USING(%s);',pg_get_expr(polqual,polrelid))FROM pg_policy WHERE polrelid='pharmacy_staff_department_bindings'::regclass AND polname='pharmacy_staff_binding_scope'`},
		{name: "replaced immutable source function", tamper: `CREATE OR REPLACE FUNCTION pharmacy_catalogue_immutable()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NULL;END$$;`, restorationQuery: `SELECT pg_get_functiondef('pharmacy_catalogue_immutable()'::regprocedure)`},
		{name: "replaced hospital source function", tamper: `CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$SELECT true$$;`, restorationQuery: `SELECT pg_get_functiondef('pharmacy_stocktake_hospital_scope(uuid,uuid,text)'::regprocedure)`},

		{name: "missing private choice restriction", tamper: `DROP POLICY hospital_stocktake_choice_purpose ON pharmacy_catalogue_products;`, restorationQuery: `SELECT format('CREATE POLICY hospital_stocktake_choice_purpose ON pharmacy_catalogue_products AS RESTRICTIVE USING(%s)WITH CHECK(%s);',pg_get_expr(polqual,polrelid),pg_get_expr(polwithcheck,polrelid))FROM pg_policy WHERE polrelid='pharmacy_catalogue_products'::regclass AND polname='hospital_stocktake_choice_purpose'`},
		{name: "missing choice delete restriction", tamper: `DROP POLICY hospital_stocktake_choice_delete ON pharmacy_outlets;`, restorationQuery: `SELECT format('CREATE POLICY hospital_stocktake_choice_delete ON pharmacy_outlets AS RESTRICTIVE FOR DELETE USING(%s);',pg_get_expr(polqual,polrelid))FROM pg_policy WHERE polrelid='pharmacy_outlets'::regclass AND polname='hospital_stocktake_choice_delete'`},
		{name: "replaced choice proof function", tamper: `CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_choices_context(t uuid,h uuid,d uuid)RETURNS boolean LANGUAGE sql STABLE AS $$SELECT true$$;`, restorationQuery: `SELECT pg_get_functiondef('pharmacy_stocktake_hospital_choices_context(uuid,uuid,uuid)'::regprocedure)`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			restore := scenario.restore
			if scenario.restorationQuery != "" {
				require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Raw(scenario.restorationQuery).Scan(&restore).Error }))
				require.NotEmpty(t, restore)
			}
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: fmt.Sprintf("test_stocktake_hospital_drift_%d", index), SQL: scenario.tamper}}))
			require.False(t, read(), "altered source authority must not be admitted")
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: fmt.Sprintf("test_stocktake_hospital_restore_%d", index), SQL: restore}}))
			require.True(t, read(), "exact restoration must match the original reviewed schema")
		})
	}
}
