package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const lowStockReadMigration = "0071_pharmacy_low_stock_scope_read"

type priorLowStockReadModule struct{ *pharmacy.Module }

func (m priorLowStockReadModule) Migrations() []tenantdb.Migration {
	migrations := m.Module.Migrations()
	return migrations[:lowStockReadMigrationIndex(migrations)]
}

func lowStockReadMigrationIndex(migrations []tenantdb.Migration) int {
	for i, migration := range migrations {
		if migration.ID == lowStockReadMigration {
			return i
		}
	}
	panic("low-stock migration missing from registered history")
}

type lowStockPolicy struct {
	TableName        string
	PolicyName       string
	Qual             string
	WriteCheck       string
	Roles            string
	Permissive       bool
	Command          string
	RowSecurity      bool
	ForceRowSecurity bool
}

func TestReorderReadPolicyMigrationPreservesVisibilityAndWrites(t *testing.T) {
	for _, ownership := range []string{"independent", "hospital_owned"} {
		t.Run(ownership, func(t *testing.T) {
			module := pharmacy.New()
			migrations := module.Migrations()
			migration := migrations[lowStockReadMigrationIndex(migrations)]
			require.Equal(t, lowStockReadMigration, migration.ID)
			r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
				Modules: []platform.Module{priorLowStockReadModule{module}},
				Tokens:  map[string]string{"editor": testutil.TenantA, "reader": testutil.TenantA},
				Perms:   map[string][]authz.Permission{"editor": releasePerms, "reader": {"pharmacy.catalogue.read", "pharmacy.stock.read"}},
			})
			root := "/v1/pharmacy/pharmacies/" + pharmaOrganization
			created := do(r, "PUT", root, "editor", fmt.Sprintf(`{"code":"TEST-PHARMACY","name":"TEST policy pharmacy","ownership_type":%q}`, ownership))
			require.Equal(t, 200, created.Code, created.Body.String())
			created = do(r, "PUT", root+"/outlets/"+pharmaOutlet, "editor", `{"code":"TEST-MAIN","name":"TEST policy outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
			require.Equal(t, 200, created.Code, created.Body.String())
			grantCatalogue(t, r, "user-editor", "editor")
			grantCatalogue(t, r, "user-reader", "reader")
			product, location := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
			created = do(r, "PUT", productPath(product), "editor", productBody(0, "TEST policy capsule"))
			require.Equal(t, 200, created.Code, created.Body.String())
			created = do(r, "PUT", stockPath("locations/"+location), "editor", `{"code":"TEST-Q","name":"TEST quarantine"}`)
			require.Equal(t, 200, created.Code, created.Body.String())
			receive(t, r, product, location, "2030-10-31", `,"mrp_minor":12500`)
			setReorder(t, r, product, "2", 0)

			targets := []string{"pharmacy_catalogue_products", "pharmacy_reorder_levels", "pharmacy_stock_batches", "pharmacy_stock_movements", "pharmacy_stock_locations"}
			policies := func() []lowStockPolicy {
				t.Helper()
				var result []lowStockPolicy
				require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Raw(`SELECT c.relname table_name,p.polname policy_name,COALESCE(pg_get_expr(p.polqual,p.polrelid),'') qual,
      COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),'') write_check,p.polroles::text roles,p.polpermissive permissive,p.polcmd::text command,c.relrowsecurity row_security,c.relforcerowsecurity force_row_security
      FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
      WHERE n.nspname=current_schema() AND (c.relname IN ? OR c.relname IN ('pharmacy_organizations','pharmacy_outlets')) ORDER BY c.relname,p.polname`, targets).Scan(&result).Error
				}))
				require.GreaterOrEqual(t, len(result), len(targets))
				return result
			}
			visibility := func() map[string][]int64 {
				t.Helper()
				result := map[string][]int64{}
				for _, scope := range []struct {
					name, tenant, subject, pharmacy, outlet, overview string
					allowed                                           bool
				}{
					{"editor", testutil.TenantA, "user-editor", pharmaOrganization, pharmaOutlet, "", true},
					{"reader", testutil.TenantA, "user-reader", pharmaOrganization, pharmaOutlet, "", true},
					{"no-grant", testutil.TenantA, "user-absent", pharmaOrganization, pharmaOutlet, "", false},
					{"foreign", testutil.TenantB, "user-editor", pharmaOrganization, pharmaOutlet, "", false},
					{"wrong-pharmacy", testutil.TenantA, "user-editor", uuid.Must(uuid.NewV7()).String(), pharmaOutlet, "", false},
					{"wrong-outlet", testutil.TenantA, "user-editor", pharmaOrganization, uuid.Must(uuid.NewV7()).String(), "", false},
					{"missing-actor", testutil.TenantA, "", pharmaOrganization, pharmaOutlet, "", false},
					{"missing-outlet", testutil.TenantA, "user-editor", pharmaOrganization, "", "", false},
					{"overview", testutil.TenantA, "user-reader", pharmaOrganization, "", pharmaOrganization, true},
				} {
					require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), scope.subject), scope.tenant, func(tx *gorm.DB) error {
						if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_stock_overview',?,true)`, scope.subject, scope.pharmacy, scope.outlet, scope.overview).Error; err != nil {
							return err
						}
						for _, table := range targets {
							var count int64
							if err := tx.Table(table).Count(&count).Error; err != nil {
								return err
							}
							expected := int64(0)
							if scope.allowed && (scope.name != "overview" || table != "pharmacy_reorder_levels") {
								expected = 1
							}
							require.Equal(t, expected, count, "%s on %s", scope.name, table)
							result[scope.name] = append(result[scope.name], count)
						}
						return nil
					}))
				}
				return result
			}
			beforePolicies, beforeVisibility := policies(), visibility()
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{migration}))
			afterPolicies := policies()
			require.Equal(t, len(beforePolicies), len(afterPolicies))
			changed := 0
			for i, before := range beforePolicies {
				require.True(t, before.RowSecurity)
				require.True(t, before.ForceRowSecurity)
				after := afterPolicies[i]
				isTarget := false
				for _, table := range targets {
					isTarget = isTarget || before.TableName == table
				}
				if isTarget && before.PolicyName == "tenant_isolation" {
					require.Contains(t, after.Qual, "SELECT pharmacy_outlet_allowed")
					require.NotEqual(t, before.Qual, after.Qual)
					after.Qual = before.Qual
					changed++
				}
				require.Equal(t, before, after, "every write, role and non-target/purpose clause stays unchanged")
			}
			require.Equal(t, 5, changed)
			require.Equal(t, beforeVisibility, visibility())
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var intact bool
				if err := tx.Raw("SELECT pharmacy_operating_choice_integrity()AND pharmacy_stocktake_source_recovery_integrity()AND pharmacy_stocktake_hospital_integrity()AND pharmacy_stocktake_hospital_choice_integrity()").Scan(&intact).Error; err != nil {
					return err
				}
				require.True(t, intact)
				return nil
			}))
			// Replaying different SQL against an already-rewritten predecessor must fail atomically.
			require.Error(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: lowStockReadMigration + "_unexpected_predecessor", SQL: migration.SQL}}))
			require.Equal(t, afterPolicies, policies())
			var level struct {
				Level json.RawMessage `json:"level"`
			}
			current := do(r, "GET", productPath(product)+"/reorder-level", "reader", "")
			require.Equal(t, 200, current.Code, current.Body.String())
			require.NoError(t, json.Unmarshal(current.Body.Bytes(), &level))
			require.True(t, strings.Contains(string(level.Level), `"minimum_units":2`))
			require.Equal(t, 200, do(r, "DELETE", root+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", "").Code)
			require.Equal(t, 404, do(r, "GET", productPath(product)+"/reorder-level", "reader", "").Code)
		})
	}
}

func TestReorderReadMigrationRefusesUnrelatedSourceDrift(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"hospital evidence isolation", "ALTER TABLE pharmacy_stocktake_hospital_evidence DISABLE ROW LEVEL SECURITY"},
		{"private choice restriction", "ALTER POLICY hospital_stocktake_choice_purpose ON pharmacy_catalogue_products USING(true)"},
		{"source recovery uniqueness", "DROP INDEX pharmacy_staff_binding_current"},
		{"source seal security", "ALTER FUNCTION pharmacy_stocktake_hospital_integrity() SECURITY DEFINER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := pharmacy.New()
			migrations := module.Migrations()
			migration := migrations[lowStockReadMigrationIndex(migrations)]
			_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{priorLowStockReadModule{module}}})
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_low_stock_source_drift", SQL: tc.sql}}))
			state := func() string {
				t.Helper()
				var state string
				require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
					return tx.Raw(`SELECT jsonb_build_object('hospital',pharmacy_stocktake_hospital_support_state(),'choices',pharmacy_stocktake_hospital_choice_state(),
      'qual',(SELECT pg_get_expr(polqual,polrelid)FROM pg_policy WHERE polrelid='pharmacy_catalogue_products'::regclass AND polname='tenant_isolation'),
      'bulk',to_regprocedure('pharmacy_sellable_product_buckets(uuid,uuid,uuid,date)')IS NOT NULL,
 'recovery',pharmacy_stocktake_source_recovery_integrity(),
 'guard',(SELECT jsonb_build_array(prosrc,prosecdef,provolatile,proconfig)FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_integrity()')))::text`).Scan(&state).Error
				}))
				return state
			}
			before := state()
			err := db.Migrate(t.Context(), []tenantdb.Migration{migration})
			require.Error(t, err, "a migration must never bless an unrelated predecessor change")
			require.Equal(t, before, state(), "refusal must not leave partial policy, function or seal updates")
		})
	}
}
