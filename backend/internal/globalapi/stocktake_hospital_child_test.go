package globalapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalStocktakeChildWorkflow(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, department, revision, pharmacy, owner, counter, reviewer string) {
	t.Helper()
	outlet, product, quarantine, batch := v7(), v7(), v7(), v7()
	base := "/v1/pharmacy/pharmacies/" + pharmacy + "/outlets/" + outlet
	call(t, "PUT", base, owner, map[string]any{"code": "TEST-CHILD-COUNT", "name": "TEST Child counting outlet", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
	for _, subject := range []string{owner, counter, reviewer} {
		call(t, "PUT", base+"/access/"+subject, owner, map[string]any{"level": "editor"}, 200)
	}
	binding := base + "/hospitals/" + hospital + "/staff-department-bindings/" + v7()
	call(t, "PUT", binding, counter, map[string]any{"department_id": department, "department_revision_id": revision, "replaces_binding_id": nil, "replaces_binding_version": nil, "reason": "TEST inherited counting outlet"}, 200)
	call(t, "POST", binding+"/commands", reviewer, map[string]any{"id": v7(), "operation": "approve", "expected_version": 1, "reason": "TEST independent child responsibility review", "attest_department_responsibility": true}, 200)
	call(t, "PUT", base+"/catalogue/products/"+product, owner, json.RawMessage(receiverProduct), 200)
	call(t, "PUT", base+"/stock/locations/"+quarantine, owner, map[string]any{"code": "TEST-CHILD-Q", "name": "TEST Child quarantine", "stock_status": "quarantine"}, 200)
	call(t, "PUT", base+"/stock/batches/"+batch, owner, map[string]any{"product_id": product, "lot_number": "TEST-CHILD-LOT", "expires_on": "2030-10-31", "mrp_minor": 12500}, 200)
	call(t, "PUT", base+"/stock/movements/"+v7(), owner, map[string]any{"location_id": quarantine, "batch_id": batch, "kind": "receipt", "quantity_units": 10, "reason": "TEST child receipt", "source_reference": "TEST-CHILD-RECEIPT"}, 200)
	for _, subject := range []string{counter, reviewer} {
		call(t, "DELETE", base+"/access/"+subject, owner, nil, 200)
	}
	root := "/v1/pharmacy/hospitals/" + hospital + "/departments/" + department
	choices := call(t, "GET", root+"/stocktake-outlet-choices", counter, nil, 200)["data"].([]any)
	require.Len(t, choices, 1)
	require.Equal(t, outlet, choices[0].(map[string]any)["outlet_id"])
	path := root + "/pharmacies/" + pharmacy + "/outlets/" + outlet + "/stocktakes/" + v7()
	call(t, "PUT", path, counter, map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": quarantine, "reason": "TEST child physical count under ancestor assignment"}, 200)
	lines := call(t, "GET", path+"/lines", counter, nil, 200)["data"].([]any)
	require.Len(t, lines, 1)
	line := lines[0].(map[string]any)
	require.EqualValues(t, 10, line["book_units"])
	call(t, "PUT", path+"/counts/"+v7(), counter, map[string]any{"expected_version": 0, "counts": []map[string]any{{"line_id": line["id"], "counted_units": 7}}, "reason": "TEST observed child shelf units"}, 200)
	call(t, "PUT", path+"/decisions/"+v7(), counter, map[string]any{"expected_version": 1, "decision": "submit", "reason": "TEST child count ready for independent review"}, 200)
	approval := map[string]any{"expected_version": 2, "decision": "approve", "reason": "TEST independent review of child shortage", "extra_units_disposition": "no_extra_units"}
	call(t, "PUT", path+"/decisions/"+v7(), counter, approval, 409)
	decision := path + "/decisions/" + v7()
	posted := call(t, "PUT", decision, reviewer, approval, 200)
	require.Equal(t, "posted", posted["state"])
	require.EqualValues(t, 1, posted["adjustment_count"])
	require.Equal(t, posted, call(t, "GET", decision, reviewer, nil, 200))
	require.Equal(t, posted, call(t, "PUT", decision, reviewer, approval, 200))
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_subject',?,true)`, pharmacy, outlet, owner).Error; err != nil {
			return err
		}
		var result struct{ Units, Corrections int64 }
		if err := tx.Raw(`SELECT COALESCE(sum(quantity_units),0)units,count(*)FILTER(WHERE kind='correction')corrections FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, outlet).Scan(&result).Error; err != nil {
			return err
		}
		require.EqualValues(t, 7, result.Units)
		require.EqualValues(t, 1, result.Corrections)
		return nil
	}))
}
