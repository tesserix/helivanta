package globalapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalStocktakeWorkflow(t *testing.T, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, hospital, department, revision, owner, counter, reviewer string, allowCounterReview func()) {
	t.Helper()
	pharmacy, outlet := v7(), v7()
	pharmacyBase := "/v1/pharmacy/pharmacies/" + pharmacy
	call(t, "PUT", pharmacyBase, owner, map[string]any{"code": "SYN-HOSPITAL-STOCKTAKE", "name": "Synthetic hospital stocktake pharmacy", "ownership_type": "hospital_owned"}, 200)
	base := pharmacyBase + "/outlets/" + outlet
	call(t, "PUT", base, owner, map[string]any{"code": "SYN-HOSPITAL-COUNT", "name": "Synthetic hospital counting outlet", "country_code": "IN", "timezone": "Asia/Kolkata"}, 200)
	for _, subject := range []string{owner, counter, reviewer} {
		call(t, "PUT", base+"/access/"+subject, owner, map[string]any{"level": "editor"}, 200)
	}
	binding := base + "/hospitals/" + hospital + "/staff-department-bindings/" + v7()
	call(t, "PUT", binding, counter, map[string]any{"department_id": department, "department_revision_id": revision, "replaces_binding_id": nil, "replaces_binding_version": nil, "reason": "Synthetic physical stocktake department binding"}, 200)
	approved := call(t, "POST", binding+"/commands", reviewer, map[string]any{"id": v7(), "operation": "approve", "expected_version": 1, "reason": "Synthetic independently approved stock owner binding", "attest_department_responsibility": true}, 200)
	require.Equal(t, "approved", approved["binding"].(map[string]any)["status"])
	product, quarantine, batch := v7(), v7(), v7()
	call(t, "PUT", base+"/catalogue/products/"+product, owner, json.RawMessage(receiverProduct), 200)
	call(t, "PUT", base+"/stock/locations/"+quarantine, owner, map[string]any{"code": "SYN-HOSPITAL-Q", "name": "Synthetic hospital quarantine", "stock_status": "quarantine"}, 200)
	call(t, "PUT", base+"/stock/batches/"+batch, owner, map[string]any{"product_id": product, "lot_number": "SYN-HOSPITAL-COUNT-LOT", "expires_on": "2030-10-31", "mrp_minor": 12500}, 200)
	receipt := func(units int) {
		call(t, "PUT", base+"/stock/movements/"+v7(), owner, map[string]any{"location_id": quarantine, "batch_id": batch, "kind": "receipt", "quantity_units": units, "reason": "Synthetic hospital stock receipt", "source_reference": "SYN-HOSPITAL-COUNT-" + v7()}, 200)
	}
	receipt(30)
	for _, subject := range []string{counter, reviewer} {
		call(t, "DELETE", base+"/access/"+subject, owner, nil, 200)
	}
	assertHospitalStocktakeOutletDiscovery(t, call, hospital, department, revision, pharmacy, outlet, owner, counter, reviewer, approved["binding"].(map[string]any))
	root := "/v1/pharmacy/hospitals/" + hospital + "/departments/" + department + "/pharmacies/" + pharmacy + "/outlets/" + outlet + "/stocktakes"
	take := v7()
	path := root + "/" + take
	start := map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": quarantine, "reason": "Synthetic hospital physical count"}
	started := call(t, "PUT", path, counter, start, 200)
	require.Equal(t, float64(1), started["snapshot_rows"])
	require.Equal(t, started, call(t, "PUT", path, counter, start, 200))
	call(t, "PUT", strings.Replace(path, department, v7(), 1), counter, start, 404)
	call(t, "PUT", strings.Replace(path, outlet, v7(), 1), counter, start, 404)
	call(t, "PUT", "/v1/pharmacy/pharmacies/"+pharmacy+"/operating-locations/"+outlet+"/stocktakes/"+take, counter, start, 404)
	lines := call(t, "GET", path+"/lines", counter, nil, 200)["data"].([]any)
	require.Len(t, lines, 1)
	line := lines[0].(map[string]any)
	require.Equal(t, float64(30), line["book_units"], "the snapshot uses the opening book quantity")
	count := map[string]any{"expected_version": started["version"], "reason": "Synthetic shelf observation", "counts": []map[string]any{{"line_id": line["id"], "counted_units": 27}}}
	staleCount := map[string]any{"expected_version": started["version"].(float64) + 1, "counts": count["counts"], "reason": count["reason"]}
	staleCommand := path + "/counts/" + v7()
	call(t, "PUT", staleCommand, counter, staleCount, 409)
	call(t, "GET", staleCommand, counter, nil, 404)
	countPath := path + "/counts/" + v7()
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_count_evidence_failure", SQL: `ALTER TABLE pharmacy_stocktake_hospital_evidence ADD CONSTRAINT synthetic_hospital_count_evidence_failure CHECK(kind<>'count')NOT VALID;`}}))
	call(t, "PUT", countPath, counter, count, 503)
	call(t, "GET", countPath, counter, nil, 503)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_hospital_count_evidence_restore", SQL: `ALTER TABLE pharmacy_stocktake_hospital_evidence DROP CONSTRAINT synthetic_hospital_count_evidence_failure;`}}))
	call(t, "GET", countPath, counter, nil, 404)
	counted := call(t, "PUT", countPath, counter, count, 200)
	require.Equal(t, counted, call(t, "PUT", countPath, counter, count, 200))
	require.Equal(t, counted, call(t, "GET", countPath, counter, nil, 200))
	receipt(5)
	laterLines := call(t, "GET", path+"/lines", counter, nil, 200)["data"].([]any)
	require.Equal(t, float64(30), laterLines[0].(map[string]any)["book_units"], "a later receipt must not rewrite the frozen book quantity")
	movedCount := map[string]any{"expected_version": counted["version"], "reason": "Synthetic recount after a physical movement", "counts": count["counts"]}
	movedCommand := path + "/counts/" + v7()
	call(t, "PUT", movedCommand, counter, movedCount, 409)
	call(t, "GET", movedCommand, counter, nil, 404)

	legacyCount := map[string]any{"expected_version": 1, "reason": "Synthetic legacy authority cannot adopt a hospital count", "counts": []map[string]any{{"line_id": line["id"], "counted_units": 26}}}
	call(t, "PUT", base+"/stock/stocktakes/"+take+"/counts/"+v7(), owner, legacyCount, 409)
	submit := map[string]any{"expected_version": counted["version"], "decision": "submit", "reason": "Synthetic count ready for independent review"}
	submitPath := path + "/decisions/" + v7()
	submitted := call(t, "PUT", submitPath, counter, submit, 200)
	require.Equal(t, "submitted", submitted["state"])
	require.Equal(t, submitted, call(t, "PUT", submitPath, counter, submit, 200))
	allowCounterReview()
	approval := map[string]any{"expected_version": submitted["version"], "decision": "approve", "reason": "Synthetic independent shortage approval", "extra_units_disposition": "no_extra_units"}
	call(t, "PUT", path+"/decisions/"+v7(), counter, approval, 409)
	approvalPath := path + "/decisions/" + v7()
	posted := call(t, "PUT", approvalPath, reviewer, approval, 200)
	require.Equal(t, "posted", posted["state"])
	require.Equal(t, float64(1), posted["adjustment_count"])
	require.Equal(t, posted, call(t, "PUT", approvalPath, reviewer, approval, 200))
	require.Equal(t, posted, call(t, "GET", approvalPath, reviewer, nil, 200))
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), owner), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_subject',?,true)`, pharmacy, outlet, owner).Error; err != nil {
			return err
		}
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*)FILTER(WHERE kind='correction')corrections,COALESCE(sum(quantity_units),0)units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, uuid.MustParse(outlet)).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, ledger.Corrections)
		require.EqualValues(t, 32, ledger.Units, "the frozen shortage correction preserves later receipts")
		return nil
	}))
	call(t, "PUT", base+"/access/"+reviewer, owner, map[string]any{"level": "editor"}, 200)
	bindingRecord := approved["binding"].(map[string]any)
	call(t, "POST", binding+"/commands", reviewer, map[string]any{"id": v7(), "operation": "end", "expected_version": bindingRecord["version"], "reason": "Synthetic source withdrawal after completed physical count"}, 200)
	call(t, "DELETE", base+"/access/"+reviewer, owner, nil, 200)
	call(t, "GET", path, reviewer, nil, 404)
	call(t, "PUT", root+"/"+v7(), counter, start, 404)
	endedChoices := call(t, "GET", "/v1/pharmacy/hospitals/"+hospital+"/departments/"+department+"/stocktake-outlet-choices?limit=20", counter, nil, 200)
	require.Empty(t, endedChoices["data"], "ended bindings must immediately remove their named outlet choices")
	assertHospitalStocktakeScopeDiscovery(t, db, call, owner, counter, reviewer, hospital, department)
}
