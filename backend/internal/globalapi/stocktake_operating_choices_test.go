package globalapi

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
)

func TestStocktakeReviewedReadsChooseNamedStockWithoutReceivingAuthority(t *testing.T) {
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	receivingDuty := f.duty
	stock := f.stock(f.pharmacy, f.outlet, "")
	batch := v7()
	f.call("PUT", stock+"/batches/"+batch, "receiver", f.batch("TEST-CHOICE-LOT"), 200)
	f.call("PUT", stock+"/receipts/"+v7(), "receiver", f.receipt(batch, f.quarantine, "TEST physical stock remains countable"), 200)
	var product map[string]any
	require.NoError(t, json.Unmarshal([]byte(receiverProduct), &product))
	product["expected_version"], product["status"] = 1, "discontinued"
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/catalogue/products/"+f.product, "owner", product, 200)
	second := v7()
	f.call("PUT", "/v1/pharmacy/pharmacies/"+f.pharmacy+"/outlets/"+f.outlet+"/stock/locations/"+second, "owner", map[string]any{"code": "TEST-OTHER-Q", "name": "TEST Secondary Quarantine", "stock_status": "quarantine"}, 200)
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	f.call("PUT", receivingDuty+"/commands/"+v7(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 3, Decision: "revoke", Reason: "TEST worker has stocktake authority only"}, 201)
	f.call("GET", stock+"/products", "receiver", nil, 404)
	f.call("PUT", stock+"/batches/"+v7(), "receiver", f.batch("TEST-NO-RECEIVER"), 404)
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes"
	var products struct {
		Data []struct {
			ID        string `json:"id"`
			OutletID  string `json:"outlet_id"`
			BrandName string `json:"brand_name"`
			Version   int64  `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", root+"/product-choices", "receiver", nil, 200)), &products))
	require.Len(t, products.Data, 1)
	require.Equal(t, f.product, products.Data[0].ID)
	require.Equal(t, f.outlet, products.Data[0].OutletID)
	require.Equal(t, "TEST Amoxicillin", products.Data[0].BrandName)
	require.EqualValues(t, 2, products.Data[0].Version)
	type locations struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			StockStatus string `json:"stock_status"`
		} `json:"data"`
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		} `json:"page"`
	}
	var first, last locations
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", root+"/quarantine-choices?limit=1", "receiver", nil, 200)), &first))
	require.Len(t, first.Data, 1)
	require.True(t, first.Page.HasMore)
	require.NotNil(t, first.Page.NextCursor)
	continuation := root + "/quarantine-choices?limit=1&cursor=" + url.QueryEscape(*first.Page.NextCursor)
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", continuation, "receiver", nil, 200)), &last))
	require.Len(t, last.Data, 1)
	require.False(t, last.Page.HasMore)
	require.Nil(t, last.Page.NextCursor)
	require.ElementsMatch(t, []string{f.quarantine, second}, []string{first.Data[0].ID, last.Data[0].ID})
	require.ElementsMatch(t, []string{"TEST Quarantine", "TEST Secondary Quarantine"}, []string{first.Data[0].Name, last.Data[0].Name})
	require.Equal(t, "quarantine", first.Data[0].StockStatus)
	require.Equal(t, "quarantine", last.Data[0].StockStatus)
	f.call("GET", strings.Replace(continuation, f.outlet, f.sibling, 1), "receiver", nil, 400)
	f.call("GET", continuation, "owner", nil, 400)
	for _, suffix := range []string{"/product-choices", "/quarantine-choices"} {
		for _, actor := range []string{"owner", "foreign"} {
			f.call("GET", root+suffix, actor, nil, 404)
		}
		f.call("GET", strings.Replace(root, f.outlet, f.sibling, 1)+suffix, "receiver", nil, 404)
		f.call("GET", root+suffix+"?status=active", "receiver", nil, 400)
		f.checks.down.Store(true)
		unavailable := f.call("GET", root+suffix, "receiver", nil, 503)
		require.NotContains(t, unavailable, "TEST Amoxicillin")
		require.NotContains(t, unavailable, "TEST Quarantine")
		f.checks.down.Store(false)
	}
}
