package globalapi

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
)

func (f supplierCompoundFixture) assertHospitalSupplierReceiving(t *testing.T, admin operatingDutyFixture, shipments []fulfilment.Shipment, department uuid.UUID, receiver, reviewer string) {
	t.Helper()
	require.Len(t, shipments, 2)
	sort.Slice(shipments, func(i, j int) bool { return shipments[i].QuantityUnits > shipments[j].QuantityUnits })
	first := shipments[0]
	root := "/v1/pharmacy/pharmacies/" + first.BuyerOrganizationID.String() + "/outlets/" + first.BuyerLocationID.String()
	product, batch, quarantine, shelf := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	var target map[string]any
	require.NoError(t, json.Unmarshal([]byte(receiverProduct), &target))
	source := strings.ToUpper(first.Product.Owner)
	target["sku"] = "TEST-RECEIVING-" + source
	admin.call("PUT", root+"/catalogue/products/"+product.String(), "owner", target, 200)
	classification := uuid.Must(uuid.NewV7()).String()
	productPath := root + "/catalogue/products/" + product.String()
	admin.call("PUT", productPath+"/schedule/"+classification, "owner", map[string]any{"regulatory_class": "H", "ndps": false, "source": "TEST source-matched capsule label"}, 201)
	admin.call("POST", productPath+"/schedule/"+classification+"/review", "reviewer", map[string]any{"decision": "approved", "reason": "TEST independent destination classification review"}, 200)
	admin.call("PUT", productPath+"/tax-classifications/"+uuid.Must(uuid.NewV7()).String(), "owner", map[string]any{"classification_code": "30041010", "supply_nature": "taxable", "rate_percent": "5", "effective_from": time.Now().In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)}, 201)
	admin.call("PUT", root+"/stock/locations/"+quarantine.String(), "owner", map[string]any{"code": "TEST-RECEIVING-Q-" + source, "name": "TEST Receiving Quarantine"}, 200)
	admin.call("PUT", root+"/stock/locations/"+shelf.String(), "owner", map[string]any{"code": "TEST-RECEIVING-SHELF-" + source, "name": "TEST Receiving Shelf", "stock_status": "sellable"}, 200)
	stock := func(location uuid.UUID, want int64) {
		t.Helper()
		path := root + "/stock/positions?as_of=" + url.QueryEscape(time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)) + "&balance=on_hand&batch_id=" + batch.String() + "&location_id=" + location.String()
		var page struct {
			Data []struct {
				OnHandUnits int64 `json:"on_hand_units"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(admin.call("GET", path, "owner", nil, 200)), &page))
		var total int64
		for _, row := range page.Data {
			total += row.OnHandUnits
		}
		require.Equal(t, want, total)
	}
	var acceptedTotal int64
	for index, shipment := range shipments {
		require.Equal(t, first.BuyerTenantID, shipment.BuyerTenantID)
		require.Equal(t, first.BuyerLocationID, shipment.BuyerLocationID)
		require.Equal(t, first.LotNumber, shipment.LotNumber)
		mrp := shipment.MrpMinor
		if shipment.Product.Owner == "supply" {
			require.Nil(t, mrp, "retain original warehouse evidence without a pharmacy printed MRP")
			mrp = ptr(int64(12500))
		} else {
			require.NotNil(t, mrp)
		}
		base := "/v1/pharmacy/hospitals/" + shipment.HospitalID.String() + "/departments/" + department.String() + "/outlets/" + shipment.BuyerLocationID.String() + "/supplier-shipments/" + shipment.ID.String()
		choices := f.hospitalCall(t, "GET", base+"/receiving/product-choices", receiver, nil, 200)["data"].([]any)
		found := false
		for _, raw := range choices {
			if raw.(map[string]any)["id"] == product.String() {
				found = true
			}
		}
		require.True(t, found, "only the matching destination catalogue may receive this unit/pack")
		counted, accepted, rejected, short := shipment.QuantityUnits, shipment.QuantityUnits, int64(0), int64(0)
		if index == 1 {
			counted, accepted, rejected, short = shipment.QuantityUnits-1, shipment.QuantityUnits-2, 1, 1
		}
		receiptID := uuid.Must(uuid.NewV7())
		receiptPath := base + "/receipts/" + receiptID.String()
		input := fulfilment.ReceivingInput{DestinationProductID: &product, DestinationProductVersion: ptr(int64(1)), DestinationBatchID: &batch, QuarantineLocationID: &quarantine, MrpMinor: mrp, CountedUnits: counted, AcceptedUnits: accepted, RejectedUnits: rejected, ObservedLotNumber: shipment.LotNumber, ObservedExpiresOn: shipment.ExpiresOn, IdentityConfirmed: true, Reason: "TEST counted delivery with separate accepted rejected and short evidence"}
		if shipment.Product.Owner == "supply" {
			uninspected := input
			uninspected.MrpMinor = nil
			f.hospitalCall(t, "PUT", receiptPath, receiver, uninspected, 400)
		}
		received := f.hospitalCall(t, "PUT", receiptPath, receiver, input, 200)
		require.Equal(t, receiver, received["actor_subject"])
		require.Equal(t, float64(accepted), received["cumulative_accepted_units"])
		require.Equal(t, received, f.hospitalCall(t, "PUT", receiptPath, receiver, input, 200))
		changed := input
		changed.Reason = "TEST different receipt payload"
		f.hospitalCall(t, "PUT", receiptPath, receiver, changed, 409)
		stock(quarantine, accepted)
		stock(shelf, acceptedTotal)
		releasePath := receiptPath + "/releases/" + uuid.Must(uuid.NewV7()).String()
		release := fulfilment.ReceivingReleaseInput{ExpectedSummaryVersion: 1, ToLocationID: shelf, QuantityUnits: accepted, Reason: "TEST independently reviewed accepted stock only"}
		f.hospitalCall(t, "PUT", releasePath, reviewer, release, 409)
		closurePath := base + "/closures/" + uuid.Must(uuid.NewV7()).String()
		closureInput := fulfilment.ReceivingClosureInput{ExpectedCountedUnits: counted, ExpectedSummaryVersion: ptr(int64(1)), Reason: "TEST close physical count and retain shortage"}
		closed := f.hospitalCall(t, "PUT", closurePath, receiver, closureInput, 200)
		require.Equal(t, float64(short), closed["short_units"])
		require.Equal(t, float64(rejected), closed["rejected_units"])
		require.Equal(t, closed, f.hospitalCall(t, "PUT", closurePath, receiver, closureInput, 200))
		release.ExpectedSummaryVersion = 2
		f.hospitalCall(t, "PUT", releasePath, reviewer, release, 409)
		clearance := fulfilment.ReceivingClearanceInput{ExpectedSummaryVersion: 2, Reason: "TEST independent current shipment and destination quality review"}
		clearancePath := receiptPath + "/clearances/" + uuid.Must(uuid.NewV7()).String()
		f.hospitalCall(t, "PUT", clearancePath, receiver, clearance, 409)
		cleared := f.hospitalCall(t, "PUT", clearancePath, reviewer, clearance, 200)
		require.Equal(t, "review_recorded", cleared["state"])
		require.Equal(t, cleared, f.hospitalCall(t, "PUT", clearancePath, reviewer, clearance, 200))
		f.hospitalCall(t, "PUT", releasePath, receiver, release, 409)
		excess := release
		excess.QuantityUnits++
		f.hospitalCall(t, "PUT", releasePath, reviewer, excess, 409)
		released := f.hospitalCall(t, "PUT", releasePath, reviewer, release, 200)
		require.Equal(t, reviewer, released["actor_subject"])
		require.NotEqual(t, received["actor_staff_id"], released["actor_staff_id"])
		require.Equal(t, released, f.hospitalCall(t, "PUT", releasePath, reviewer, release, 200))
		require.Equal(t, released, f.hospitalCall(t, "GET", releasePath, reviewer, nil, 200))
		acceptedTotal += accepted
		stock(quarantine, 0)
		stock(shelf, acceptedTotal)
		receiptVersion, summaryVersion := int64(1), int64(2)
		if index == 1 {
			require.True(t, t.Run("independent recount preserves released stock and original receipt", func(t *testing.T) {
				correctionPath := receiptPath + "/corrections/" + uuid.Must(uuid.NewV7()).String()
				correction := fulfilment.ReceivingCorrectionInput{ExpectedSummaryVersion: 2, ExpectedReceiptVersion: 1, CountedUnits: 4, AcceptedUnits: 2, RejectedUnits: 2, Reason: "TEST recount identifies the previously short unit as rejected custody"}
				f.hospitalCall(t, "PUT", correctionPath, receiver, correction, 409)
				stale := correction
				stale.ExpectedSummaryVersion = 1
				f.hospitalCall(t, "PUT", correctionPath, reviewer, stale, 409)
				recount := f.hospitalCall(t, "PUT", correctionPath, reviewer, correction, 200)
				require.Equal(t, float64(2), recount["version"])
				require.Equal(t, float64(0), recount["accepted_delta_units"])
				require.Equal(t, float64(4), recount["cumulative_counted_units"])
				require.Equal(t, float64(2), recount["cumulative_rejected_units"])
				require.Equal(t, recount, f.hospitalCall(t, "PUT", correctionPath, reviewer, correction, 200))
				require.Equal(t, recount, f.hospitalCall(t, "GET", correctionPath, receiver, nil, 200))
				changed := correction
				changed.Reason = "TEST conflicting correction intent"
				f.hospitalCall(t, "PUT", correctionPath, reviewer, changed, 409)
				original := f.hospitalCall(t, "GET", receiptPath+"/creation-receipt", receiver, nil, 200)
				require.Equal(t, received, original, "recount cannot rewrite original receipt evidence")
				open := f.hospitalCall(t, "GET", base+"/receiving", receiver, nil, 200)["summary"].(map[string]any)
				require.Equal(t, float64(3), open["version"])
				require.Equal(t, false, open["closed"], "recount requires a new explicit closure")
				stock(quarantine, 0)
				stock(shelf, acceptedTotal)
				require.Equal(t, released, f.hospitalCall(t, "GET", releasePath, reviewer, nil, 200), "original release evidence survives a later recount")
				reclose := fulfilment.ReceivingClosureInput{ExpectedCountedUnits: 4, ExpectedSummaryVersion: ptr(int64(3)), Reason: "TEST independently recounted rejected unit explicitly closed"}
				reclosePath := base + "/closures/" + uuid.Must(uuid.NewV7()).String()
				closed := f.hospitalCall(t, "PUT", reclosePath, receiver, reclose, 200)
				require.Equal(t, float64(2), closed["rejected_units"])
				require.Equal(t, float64(0), closed["short_units"])
				require.Equal(t, closed, f.hospitalCall(t, "PUT", reclosePath, receiver, reclose, 200))
			}))
			receiptVersion, summaryVersion = 2, 4
			rejected, short = 2, 0
		}
		current := f.hospitalCall(t, "GET", base+"/receiving", receiver, nil, 200)
		require.Equal(t, "recorded", current["state"])
		head := current["summary"].(map[string]any)
		require.Equal(t, true, head["closed"])
		require.Equal(t, float64(short), head["short_units"])
		require.Equal(t, float64(rejected), head["rejected_units"])
		require.True(t, t.Run("actual acquisition bill payment credit and reversal", func(t *testing.T) {
			assertSupplierReceiptFinancials(t, admin, shipment, receiptID, product, batch, accepted, acceptedTotal, rejected+short, receiptVersion, summaryVersion)
		}))
		stock(quarantine, 0)
		stock(shelf, acceptedTotal)
	}
	require.Equal(t, int64(8), acceptedTotal)
}
