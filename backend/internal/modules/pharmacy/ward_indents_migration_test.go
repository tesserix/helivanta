package pharmacy_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const wardIndentMigration = "0077_pharmacy_ward_indents"

var wardIndentTables = []string{"pharmacy_ward_indents", "pharmacy_ward_indent_lines", "pharmacy_ward_indent_issues"}

type priorWardIndentModule struct{ *pharmacy.Module }

func (m priorWardIndentModule) Migrations() []tenantdb.Migration {
	migrations := m.Module.Migrations()
	return migrations[:wardIndentMigrationIndex(migrations)]
}

func wardIndentMigrationIndex(migrations []tenantdb.Migration) int {
	for i, migration := range migrations {
		if migration.ID == wardIndentMigration {
			return i
		}
	}
	panic("ward indent migration missing from registered history")
}

func TestWardIndentMigrationRefusesSealDrift(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"hospital evidence isolation", "ALTER TABLE pharmacy_stocktake_hospital_evidence DISABLE ROW LEVEL SECURITY"},
		{"private choice restriction", "ALTER POLICY hospital_stocktake_choice_purpose ON pharmacy_catalogue_products USING(true)"},
		{"operating choice restriction", "ALTER POLICY operating_choice_purpose ON pharmacy_stock_batches USING(true)"},
		{"source recovery uniqueness", "DROP INDEX pharmacy_staff_binding_current"},
		{"source seal security", "ALTER FUNCTION pharmacy_stocktake_hospital_integrity() SECURITY DEFINER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := pharmacy.New()
			migrations := module.Migrations()
			migration := migrations[wardIndentMigrationIndex(migrations)]
			_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{priorWardIndentModule{module}}})
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_ward_indent_seal_drift", SQL: tc.sql}}))
			state := func() string {
				t.Helper()
				var state string
				require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Raw(`SELECT jsonb_build_object('hospital',pharmacy_stocktake_hospital_support_state(),'choices',pharmacy_stocktake_hospital_choice_state(),
 'tables',(SELECT jsonb_agg(to_regclass(t)IS NULL)FROM unnest(?::text[])t),
 'catalogue',(SELECT jsonb_agg(polname ORDER BY polname)FROM pg_policy WHERE polrelid='pharmacy_catalogue_products'::regclass),
 'returns',(SELECT jsonb_agg(polname ORDER BY polname)FROM pg_policy WHERE polrelid='pharmacy_credit_note_lines'::regclass),
 'seals',(SELECT jsonb_agg(jsonb_build_array(proname,prosrc,prosecdef)ORDER BY proname)FROM pg_proc WHERE proname IN('pharmacy_operating_choice_integrity','pharmacy_stocktake_hospital_integrity','pharmacy_stocktake_hospital_choice_integrity')))::text`, "{"+wardIndentTables[0]+","+wardIndentTables[1]+","+wardIndentTables[2]+"}").Scan(&state).Error
				}))
				return state
			}
			before := state()
			err := db.Migrate(t.Context(), []tenantdb.Migration{migration})
			require.Error(t, err, "0076 must never bless a predecessor seal change")
			require.Equal(t, before, state(), "refusal must not leave tables, policies or re-pinned seals behind")
		})
	}
}

func TestWardIndentTablesCarryPurposeSeals(t *testing.T) {
	_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{pharmacy.New()}})
	type sealState struct{ Operating, Choice, Hospital, Recovery bool }
	read := func() sealState {
		t.Helper()
		var s sealState
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pharmacy_operating_choice_integrity() operating,pharmacy_stocktake_hospital_choice_integrity() choice,pharmacy_stocktake_hospital_integrity() hospital,pharmacy_stocktake_source_recovery_integrity() recovery`).Scan(&s).Error
		}))
		return s
	}
	require.Equal(t, sealState{true, true, true, true}, read(), "0076 re-pins every seal it extends")

	for _, table := range wardIndentTables {
		var forced bool
		var restrictive []string
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Raw(`SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid=?::regclass`, table).Scan(&forced).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT polname FROM pg_policy WHERE polrelid=?::regclass AND NOT polpermissive ORDER BY polname`, table).Scan(&restrictive).Error
		}))
		require.True(t, forced, table)
		require.Equal(t, []string{"hospital_stocktake_choice_delete", "hospital_stocktake_choice_purpose", "operating_choice_delete", "operating_choice_purpose"}, restrictive, table)
	}

	for index, scenario := range []struct {
		name, tamper, restorationQuery string
		broken                         sealState
	}{
		{
			name:             "broadened operating purpose on an indent",
			tamper:           `ALTER POLICY operating_choice_purpose ON pharmacy_ward_indents USING(true);`,
			restorationQuery: `SELECT format('ALTER POLICY operating_choice_purpose ON pharmacy_ward_indents USING(%s);',pg_get_expr(polqual,polrelid))FROM pg_policy WHERE polrelid='pharmacy_ward_indents'::regclass AND polname='operating_choice_purpose'`,
			broken:           sealState{false, false, false, true},
		},
		{
			name:             "missing private choice restriction on an issue",
			tamper:           `DROP POLICY hospital_stocktake_choice_purpose ON pharmacy_ward_indent_issues;`,
			restorationQuery: `SELECT format('CREATE POLICY hospital_stocktake_choice_purpose ON pharmacy_ward_indent_issues AS RESTRICTIVE USING(%s)WITH CHECK(%s);',pg_get_expr(polqual,polrelid),pg_get_expr(polwithcheck,polrelid))FROM pg_policy WHERE polrelid='pharmacy_ward_indent_issues'::regclass AND polname='hospital_stocktake_choice_purpose'`,
			broken:           sealState{true, false, true, true},
		},
		{
			name:             "missing current-binding uniqueness",
			tamper:           `DROP INDEX pharmacy_staff_binding_current;`,
			restorationQuery: `SELECT pg_get_indexdef('pharmacy_staff_binding_current'::regclass)||';'`,
			broken:           sealState{true, true, false, false},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var restore string
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Raw(scenario.restorationQuery).Scan(&restore).Error }))
			require.NotEmpty(t, restore)
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: fmt.Sprintf("test_ward_indent_seal_tamper_%d", index), SQL: scenario.tamper}}))
			require.Equal(t, scenario.broken, read(), "altered authority must not be admitted")
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: fmt.Sprintf("test_ward_indent_seal_restore_%d", index), SQL: restore}}))
			require.Equal(t, sealState{true, true, true, true}, read(), "exact restoration matches the reviewed schema")
		})
	}
}
