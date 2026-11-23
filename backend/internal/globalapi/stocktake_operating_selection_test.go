package globalapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStocktakeReviewedReadsFreezeSelectedProductsBeforeLaterReceipts(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	legacy := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/outlets/" + f.outlet
	other := v7()
	f.call("PUT", legacy+"/catalogue/products/"+other, "owner", json.RawMessage(strings.Replace(receiverProduct, "TEST-AMOX-500", "TEST-OTHER-500", 1)), 200)
	batch := v7()
	second := v7()
	for _, stock := range []struct{ product, batch, lot string }{{f.product, batch, "TEST-SELECTED"}, {other, second, "TEST-UNSELECTED"}} {
		input := f.batch(stock.lot)
		input["product_id"] = stock.product
		f.call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+stock.batch), "receiver", input, 200)
		f.call("PUT", f.stock(f.pharmacy, f.outlet, "/receipts/"+v7()), "receiver", f.receipt(stock.batch, f.quarantine, "TEST-"+stock.lot+"-RECEIPT"), 200)
	}
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.input.Reason = "TEST selected-product physical count"
	f.activate(t)
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes"
	selected := root + "/" + v7()
	f.call("PUT", selected, "receiver", map[string]any{"selection": "selected", "product_ids": []string{f.product}, "quarantine_location_id": f.quarantine, "reason": "TEST freeze selected product only"}, 200)
	type line struct {
		ProductID string `json:"product_id"`
		BatchID   string `json:"batch_id"`
		BookUnits int64  `json:"book_units"`
	}
	read := func(path string) []line {
		t.Helper()
		var result struct {
			Data []line `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"/lines", "receiver", nil, 200)), &result))
		return result.Data
	}
	frozen := read(selected)
	require.Equal(t, []line{{ProductID: f.product, BatchID: batch, BookUnits: 30}}, frozen)
	f.call("PUT", legacy+"/stock/movements/"+v7(), "owner", f.receipt(batch, f.quarantine, "TEST-AFTER-FREEZE-RECEIPT"), 200)
	require.Equal(t, frozen, read(selected), "a later physical receipt must not rewrite the frozen book")
	f.call("PUT", selected+"/cancellations/"+v7(), "receiver", map[string]any{"decision": "cancel", "expected_version": 0, "reason": "TEST finish selection-only verification"}, 200)
	all := root + "/" + v7()
	f.call("PUT", all, "receiver", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST subsequent full count sees current stock"}, 200)
	current := read(all)
	require.Len(t, current, 2)
	totals := map[string]int64{}
	for _, row := range current {
		totals[row.ProductID] = row.BookUnits
	}
	require.Equal(t, map[string]int64{f.product: 60, other: 30}, totals)
	require.Equal(t, frozen, read(selected), "retained selected-count history stays independent of subsequent counts")
}
