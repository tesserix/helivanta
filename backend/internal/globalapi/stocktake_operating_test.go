package globalapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStocktakeReviewedIndependentCounterFreezesReceivedStock(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	batch := v7()
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+batch), "receiver", f.batch("TEST-COUNT-LOT"), 200)
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/receipts/"+v7()), "receiver", f.receipt(batch, f.quarantine, "TEST-COUNT-RECEIPT"), 200)
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes"
	path := root + "/" + v7()
	input := map[string]any{
		"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine,
		"reason": "TEST reviewed outlet count",
	}
	f.call("PUT", path, "receiver", input, 404)
	f.call("PUT", path, "owner", input, 404)
	f.call("PUT", path, "foreign", input, 404)

	// The new duty uses the same reviewed identity, without a legacy inventory role or outlet editor grant.
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.input.Reason = "TEST reviewed physical counting duty"
	f.activate(t)
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/operating-locations/"+f.sibling+"/stocktakes/"+v7(), "receiver", input, 404)
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.hospitalPharmacy+"/operating-locations/"+f.hospitalOutlet+"/stocktakes/"+v7(), "receiver", input, 404)
	first := f.call("PUT", path, "receiver", input, 200)
	require.JSONEq(t, first, f.call("PUT", path, "receiver", input, 200))
	changed := map[string]any{
		"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine,
		"reason": "TEST changed command identity",
	}
	f.call("PUT", path, "receiver", changed, 409)
	f.call("PUT", root+"/"+v7(), "receiver", input, 409)
	var page struct {
		Data []struct {
			ID        string `json:"id"`
			BatchID   string `json:"batch_id"`
			BookUnits int64  `json:"book_units"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"/lines", "receiver", nil, 200)), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, batch, page.Data[0].BatchID)
	require.EqualValues(t, 30, page.Data[0].BookUnits)
	f.call("GET", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/stock/stocktakes", "receiver", nil, 403)
	f.checks.down.Store(true)
	unavailable := f.call("GET", path+"/lines", "receiver", nil, 503)
	require.NotContains(t, unavailable, "TEST-COUNT-LOT")
	f.checks.down.Store(false)
	f.call("GET", path+"/lines", "receiver", nil, 200)
	countPath := path + "/counts/" + v7()
	count := map[string]any{
		"expected_version": 0, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 27}},
		"reason": "TEST reviewed physical shelf count",
	}
	f.call("PUT", countPath, "owner", count, 404)
	f.call("PUT", countPath, "foreign", count, 404)
	recorded := f.call("PUT", countPath, "receiver", count, 200)
	require.JSONEq(t, recorded, f.call("PUT", countPath, "receiver", count, 200))
	require.JSONEq(t, recorded, f.call("GET", countPath, "receiver", nil, 200))
	changedCount := map[string]any{
		"expected_version": 0, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 26}},
		"reason": "TEST changed physical shelf count",
	}
	f.call("PUT", countPath, "receiver", changedCount, 409)
	stalePath := path + "/counts/" + v7()
	f.call("PUT", stalePath, "receiver", count, 409)
	f.call("GET", stalePath, "receiver", nil, 404)
	var receipt struct {
		Version    int64  `json:"version"`
		RecordedBy string `json:"recorded_by"`
		Counts     []struct {
			CountedUnits int64 `json:"counted_units"`
		} `json:"counts"`
	}
	require.NoError(t, json.Unmarshal([]byte(recorded), &receipt))
	require.EqualValues(t, 1, receipt.Version)
	require.Len(t, receipt.Counts, 1)
	require.EqualValues(t, 27, receipt.Counts[0].CountedUnits)
	f.checks.down.Store(true)
	f.call("GET", countPath, "receiver", nil, 503)
	f.checks.down.Store(false)
	require.Equal(t, "user-receiver", receipt.RecordedBy)
	legacyCount := map[string]any{
		"expected_version": 1, "counts": []map[string]any{{"line_id": page.Data[0].ID, "counted_units": 25}},
		"reason": "TEST legacy editor cannot enter reviewed history",
	}
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/stock/stocktakes/"+path[len(root)+1:]+"/counts/"+v7(), "owner", legacyCount, 409)
	f.call("PUT", f.duty+"/commands/"+v7(), "owner", map[string]any{
		"expected_version": 3, "decision": "revoke", "reason": "TEST end the reviewed counting duty",
	}, 201)
	f.call("GET", path+"/lines", "receiver", nil, 404)
	f.call("GET", countPath, "receiver", nil, 404)
	f.call("PUT", path+"/counts/"+v7(), "receiver", count, 404)
}
