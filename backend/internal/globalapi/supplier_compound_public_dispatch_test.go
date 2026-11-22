package globalapi

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
)

func (f supplierCompoundFixture) assertPublicPharmacyDispatch(t *testing.T, admin, operator operatingDutyFixture, order supplierorder.SupplierOrder, department uuid.UUID, receiver string) []fulfilment.Shipment {
	t.Helper()
	seller, buyer := order.Terms.Seller, order.Terms.Buyer
	root := "/v1/pharmacy/pharmacies/" + seller.OrganizationID.String() + "/outlets/" + seller.LocationID.String()
	product := order.Input.Lines[0].Product.ProductID
	productPath := root + "/catalogue/products/" + product.String()
	classification := uuid.Must(uuid.NewV7()).String()
	admin.call("PUT", productPath+"/schedule/"+classification, "owner", map[string]any{"regulatory_class": "H", "ndps": false, "source": "TEST pack identity fixture"}, 201)
	admin.call("POST", productPath+"/schedule/"+classification+"/review", "reviewer", map[string]any{"decision": "approved", "reason": "TEST independent classification review"}, 200)
	admin.call("PUT", productPath+"/tax-classifications/"+uuid.Must(uuid.NewV7()).String(), "owner", map[string]any{"classification_code": "30041010", "supply_nature": "taxable", "rate_percent": "5", "effective_from": time.Now().In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)}, 201)
	quarantine, shelf, batch := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	admin.call("PUT", root+"/stock/locations/"+quarantine.String(), "owner", map[string]any{"code": "TEST-COMPOUND-Q", "name": "TEST Compound Quarantine"}, 200)
	admin.call("PUT", root+"/stock/locations/"+shelf.String(), "owner", map[string]any{"code": "TEST-COMPOUND-SHELF", "name": "TEST Compound Shelf", "stock_status": "sellable"}, 200)
	admin.call("PUT", root+"/stock/batches/"+batch.String(), "owner", map[string]any{"product_id": product, "lot_number": "TEST-COMPOUND-LOT", "expires_on": "2030-12-31", "mrp_minor": 12500}, 200)
	admin.call("PUT", root+"/stock/movements/"+uuid.Must(uuid.NewV7()).String(), "owner", map[string]any{"location_id": quarantine, "batch_id": batch, "kind": "receipt", "quantity_units": 100, "reason": "TEST counted source stock", "source_reference": "TEST-COMPOUND-GRN:1"}, 200)
	admin.call("PUT", root+"/stock/releases/"+uuid.Must(uuid.NewV7()).String(), "reviewer", map[string]any{"batch_id": batch, "from_location_id": quarantine, "to_location_id": shelf, "quantity_units": 100, "reason": "TEST independent source physical release"}, 200)
	approveSyntheticTradingPolicyForReviewer(t, admin, "IN-MH", "prescription", "dispatch", "compound-source-reviewer")
	stockRoot := root + "/supplier-orders/" + order.ID.String()
	choicePath := stockRoot + "/lines/" + order.Input.Lines[0].ID.String() + "/stock-choices"
	assertAvailable := func(want int64) {
		t.Helper()
		response := f.sourceStaffRequest(t, operator.principal, "GET", choicePath, nil)
		require.Equal(t, 200, response.Code, response.Body.String())
		var page struct {
			Data []fulfilment.SupplierStockChoice `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		require.Equal(t, batch, page.Data[0].ID)
		require.Equal(t, want, page.Data[0].AvailableUnits)
	}
	assertAvailable(100)
	allocationID := uuid.Must(uuid.NewV7())
	allocationPath := stockRoot + "/allocations/" + allocationID.String()
	reserve := fulfilment.ReserveInput{ExpectedOrderVersion: order.Version, LineID: order.Input.Lines[0].ID, StockID: batch, StockLocationID: &shelf, QuantityUnits: 10, ExpiresAt: time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond), Reason: "TEST ten source-owned units for this accepted line"}
	created := f.sourceStaffRequest(t, operator.principal, "PUT", allocationPath, reserve)
	require.Equal(t, 200, created.Code, created.Body.String())
	var allocation fulfilment.Allocation
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &allocation))
	require.Equal(t, int64(10), allocation.ReservedUnits)
	require.Equal(t, operator.input.Identity.StaffID, allocation.Plan.ActorStaffID)
	replay := f.sourceStaffRequest(t, operator.principal, "PUT", allocationPath, reserve)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	assertAvailable(90)
	for index, quantity := range []int64{6, 4} {
		commandPath := allocationPath + "/commands/" + uuid.Must(uuid.NewV7()).String()
		input := fulfilment.AllocationDecisionInput{ExpectedVersion: int64(index + 1), Decision: "dispatch", QuantityUnits: quantity, HandoffReference: fmt.Sprintf("TEST-COMPOUND-HANDOFF-%d", index+1), Reason: "TEST physical counted handoff"}
		if index == 1 {
			over := input
			over.QuantityUnits++
			excess := f.sourceStaffRequest(t, operator.principal, "PUT", commandPath, over)
			require.Equal(t, 409, excess.Code, excess.Body.String())
		}
		dispatched := f.sourceStaffRequest(t, operator.principal, "PUT", commandPath, input)
		require.Equal(t, 200, dispatched.Code, dispatched.Body.String())
		var receipt fulfilment.AllocationCommandReceipt
		require.NoError(t, json.Unmarshal(dispatched.Body.Bytes(), &receipt))
		require.Equal(t, operator.principal.Subject, receipt.ActorSubject)
		require.Equal(t, operator.input.Identity.StaffID, receipt.ActorStaffID)
		require.Equal(t, int64(10), receipt.Allocation.ReservedUnits+receipt.Allocation.DispatchedUnits)
		replay := f.sourceStaffRequest(t, operator.principal, "PUT", commandPath, input)
		require.Equal(t, 200, replay.Code, replay.Body.String())
		require.JSONEq(t, dispatched.Body.String(), replay.Body.String())
		if index == 1 {
			require.Equal(t, "dispatched", receipt.Allocation.Status)
			require.Zero(t, receipt.Allocation.ReservedUnits)
			require.Equal(t, int64(10), receipt.Allocation.DispatchedUnits)
		}
	}
	assertAvailable(90)
	var shipments struct {
		Data []fulfilment.Shipment `json:"data"`
	}
	response := f.sourceStaffRequest(t, operator.principal, "GET", "/v1/supply/stock-owners/pharmacy/organizations/"+seller.OrganizationID.String()+"/locations/"+seller.LocationID.String()+"/supplier-orders/"+order.ID.String()+"/shipments", nil)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &shipments))
	require.Len(t, shipments.Data, 2)
	var total int64
	for _, shipment := range shipments.Data {
		require.Equal(t, seller.TenantID, shipment.SourceTenantID)
		require.Equal(t, buyer.TenantID, shipment.BuyerTenantID)
		require.Equal(t, order.ID, shipment.OrderID)
		require.Equal(t, allocationID, shipment.AllocationID)
		require.Equal(t, operator.principal.Subject, shipment.ActorSubject)
		total += shipment.QuantityUnits
	}
	require.Equal(t, int64(10), total)
	buyerList := "/v1/supply/hospitals/" + buyer.HospitalID.String() + "/departments/" + department.String() + "/outlets/" + buyer.LocationID.String() + "/supplier-shipments"
	require.Eventually(t, func() bool {
		page := f.hospitalCall(t, "GET", buyerList, receiver, nil, 200)
		rows := page["data"].([]any)
		if len(rows) != 2 {
			return false
		}
		for _, raw := range rows {
			require.Equal(t, order.ID.String(), raw.(map[string]any)["order_id"])
		}
		return true
	}, 20*time.Second, 100*time.Millisecond)
	return shipments.Data
}
