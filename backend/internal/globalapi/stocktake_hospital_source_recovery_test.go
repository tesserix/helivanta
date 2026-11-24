package globalapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalStocktakeSourceRecovery(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, rootDepartment, rootRevision, pharmacy, owner, counter, reviewer string) {
	t.Helper()
	for _, scenario := range []string{"end", "replace", "revision", "retire"} {
		t.Run("stocktake source recovery "+scenario, func(t *testing.T) {
			department, revision := v7(), v7()
			version, retired := 1, false
			orgRoot := "/v1/organization/hospitals/" + hospital + "/departments/" + department + "/revisions/"
			input := map[string]any{"expected_version": 0, "code": "TEST-RECOVERY-" + scenario, "name": "TEST Stocktake recovery " + scenario, "kind": "sub_unit", "parent_id": rootDepartment, "status": "active", "reason": "TEST source recovery department"}
			// Organization codes are canonical uppercase.
			input["code"] = "TEST-RECOVERY-" + strings.ToUpper(scenario)
			call(t, "PUT", orgRoot+revision, owner, input, 200)
			t.Cleanup(func() {
				if retired {
					return
				}
				input["expected_version"], input["status"] = version, "retired"
				call(t, "PUT", orgRoot+v7(), owner, input, 200)
			})
			outlet, product, quarantine, batch := v7(), v7(), v7(), v7()
			base := "/v1/pharmacy/pharmacies/" + pharmacy + "/outlets/" + outlet
			call(t, "PUT", base, owner, map[string]any{"code": "TEST-RECOVERY-" + strings.ToUpper(scenario), "name": "TEST Recovery outlet " + scenario, "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
			access := func(subject string, enabled bool) {
				if enabled {
					call(t, "PUT", base+"/access/"+subject, owner, map[string]any{"level": "editor"}, 200)
				} else {
					call(t, "DELETE", base+"/access/"+subject, owner, nil, 200)
				}
			}
			bindingRoot := base + "/hospitals/" + hospital + "/staff-department-bindings/"
			bind := func(department, revision string, replaces any, replacesVersion any) string {
				id := v7()
				access(counter, true)
				access(reviewer, true)
				call(t, "PUT", bindingRoot+id, counter, map[string]any{"department_id": department, "department_revision_id": revision, "replaces_binding_id": replaces, "replaces_binding_version": replacesVersion, "reason": "TEST current recovery stock owner"}, 200)
				call(t, "POST", bindingRoot+id+"/commands", reviewer, map[string]any{"id": v7(), "operation": "approve", "expected_version": 1, "reason": "TEST independently reviewed recovery binding", "attest_department_responsibility": true}, 200)
				access(counter, false)
				access(reviewer, false)
				return id
			}
			originalBinding := bind(department, revision, nil, nil)
			call(t, "PUT", base+"/catalogue/products/"+product, owner, json.RawMessage(receiverProduct), 200)
			call(t, "PUT", base+"/stock/locations/"+quarantine, owner, map[string]any{"code": "TEST-RECOVERY-Q", "name": "TEST Recovery quarantine", "stock_status": "quarantine"}, 200)
			call(t, "PUT", base+"/stock/batches/"+batch, owner, map[string]any{"product_id": product, "lot_number": "TEST-RECOVERY-LOT", "expires_on": "2030-10-31", "mrp_minor": 12500}, 200)
			call(t, "PUT", base+"/stock/movements/"+v7(), owner, map[string]any{"location_id": quarantine, "batch_id": batch, "kind": "receipt", "quantity_units": 10, "reason": "TEST recovery fixture receipt", "source_reference": "TEST-RECOVERY-RECEIPT"}, 200)
			workRoot := func(d string) string {
				return "/v1/pharmacy/hospitals/" + hospital + "/departments/" + d + "/pharmacies/" + pharmacy + "/outlets/" + outlet + "/stocktakes"
			}
			oldID := v7()
			old := workRoot(department) + "/" + oldID
			start := map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": quarantine, "reason": "TEST source-pinned original count"}
			call(t, "PUT", old, counter, start, 200)
			line := call(t, "GET", old+"/lines", counter, nil, 200)["data"].([]any)[0].(map[string]any)
			count := map[string]any{"expected_version": 0, "counts": []map[string]any{{"line_id": line["id"], "counted_units": 9}}, "reason": "TEST retained original observation"}
			countCommand := v7()
			counted := call(t, "PUT", old+"/counts/"+countCommand, counter, count, 200)
			oldVersion := 1
			if scenario == "replace" || scenario == "revision" {
				call(t, "PUT", old+"/decisions/"+v7(), counter, map[string]any{"expected_version": 1, "decision": "submit", "reason": "TEST original source submitted evidence"}, 200)
				oldVersion = 2
			}
			targetDepartment, targetRevision := department, revision
			if scenario == "end" {
				access(reviewer, true)
				call(t, "POST", bindingRoot+originalBinding+"/commands", reviewer, map[string]any{"id": v7(), "operation": "end", "expected_version": 2, "reason": "TEST binding withdrawn with unfinished evidence"}, 200)
				access(reviewer, false)
				call(t, "GET", old, counter, nil, 404)
			}
			if scenario == "revision" || scenario == "retire" {
				input["expected_version"], input["name"] = 1, "TEST Revised recovery department"
				if scenario == "retire" {
					input["status"] = "retired"
					retired = true
					targetDepartment = rootDepartment
					targetRevision = rootRevision
				}
				next := v7()
				call(t, "PUT", orgRoot+next, owner, input, 200)
				version = 2
				if scenario == "revision" {
					targetRevision = next
				}
				call(t, "GET", old, counter, nil, 409)
			}
			var replacement string
			if scenario == "end" {
				replacement = bind(targetDepartment, targetRevision, nil, nil)
			} else {
				replacement = bind(targetDepartment, targetRevision, originalBinding, 2)
			}
			oldUnderCurrent := workRoot(targetDepartment) + "/" + oldID
			if oldVersion == 2 {
				denied := call(t, "PUT", oldUnderCurrent+"/decisions/"+v7(), reviewer, map[string]any{"expected_version": oldVersion, "decision": "approve", "reason": "TEST replacement cannot adopt old submission", "extra_units_disposition": "no_extra_units"}, 409)
				require.Equal(t, "source_changed", denied["error"])
			} else {
				denied := call(t, "PUT", oldUnderCurrent+"/counts/"+v7(), counter, map[string]any{"expected_version": oldVersion, "counts": count["counts"], "reason": "TEST replacement cannot adopt old count"}, 409)
				require.Equal(t, "source_changed", denied["error"])
			}
			fresh := workRoot(targetDepartment) + "/" + v7()
			started := call(t, "PUT", fresh, counter, start, 200)
			require.Equal(t, started, call(t, "PUT", fresh, counter, start, 200))
			retained := call(t, "GET", oldUnderCurrent, counter, nil, 200)
			require.Equal(t, true, retained["source_invalidated"])
			require.EqualValues(t, oldVersion, retained["version"])
			require.Equal(t, counted, call(t, "GET", oldUnderCurrent+"/counts/"+countCommand, counter, nil, 200))
			call(t, "PUT", fresh+"/cancellations/"+v7(), counter, map[string]any{"expected_version": 0, "decision": "cancel", "reason": "TEST finish new-source recovery fixture"}, 200)
			require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_subject',?,true)`, pharmacy, outlet, owner).Error; err != nil {
					return err
				}
				var result struct{ Units, Corrections int64 }
				if err := tx.Raw(`SELECT COALESCE(sum(quantity_units),0)units,count(*)FILTER(WHERE kind='correction')corrections FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, outlet).Scan(&result).Error; err != nil {
					return err
				}
				require.EqualValues(t, 10, result.Units)
				require.Zero(t, result.Corrections)
				return nil
			}))
			access(reviewer, true)
			call(t, "POST", bindingRoot+replacement+"/commands", reviewer, map[string]any{"id": v7(), "operation": "end", "expected_version": 2, "reason": "TEST recovery fixture completed"}, 200)
			access(reviewer, false)
		})
	}
}
