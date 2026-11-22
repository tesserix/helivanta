package globalapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
)

func assertSupplierReceiptFinancials(t *testing.T, admin operatingDutyFixture, shipment fulfilment.Shipment, receipt, product, batch uuid.UUID, accepted, discrepancy int64) {
	t.Helper()
	root := "/v1/pharmacy/pharmacies/" + shipment.BuyerOrganizationID.String() + "/outlets/" + shipment.BuyerLocationID.String() + "/accounts"
	acquisition := func() pharmacy.AcquisitionReceipt {
		t.Helper()
		var page struct {
			Data []pharmacy.AcquisitionReceipt `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(admin.call("GET", root+"/acquisition-receipts?limit=50", "owner", nil, 200)), &page))
		for _, row := range page.Data {
			if row.ID == receipt {
				return row
			}
		}
		t.Fatal("actual accepted-stock receipt missing from financial acquisition choices")
		return pharmacy.AcquisitionReceipt{}
	}
	original := acquisition()
	require.Equal(t, shipment.BuyerTenantID, original.OwnerTenantID)
	require.Equal(t, shipment.BuyerLocationID, original.OutletID)
	require.Equal(t, product, original.ProductID)
	require.Equal(t, batch, original.BatchID)
	require.Equal(t, accepted, original.QuantityUnits)
	require.Equal(t, "supplier-receipt:"+receipt.String(), original.SourceReference)
	require.Nil(t, original.PayableID)
	require.NotNil(t, original.Supplier)
	require.Equal(t, shipment.ID, original.Supplier.ShipmentID)
	require.Equal(t, shipment.OrderID, original.Supplier.OrderID)
	require.Equal(t, shipment.Product.Owner, original.Supplier.SourceProduct)
	require.Equal(t, accepted, original.Supplier.OriginalAcceptedUnits)
	require.Equal(t, accepted, original.Supplier.EffectiveAcceptedUnits)
	require.EqualValues(t, 1, original.Supplier.ReceiptVersion)
	require.EqualValues(t, 2, original.Supplier.SummaryVersion)
	require.True(t, original.Supplier.ReceivingClosed)
	require.Equal(t, discrepancy, original.Supplier.DiscrepancyUnits)
	today := time.Now().In(time.FixedZone("Asia/Kolkata", 19800))
	id := uuid.Must(uuid.NewV7())
	path := root + "/payables/" + id.String()
	total := accepted * 1000
	bill := map[string]any{
		"payee_reference": shipment.SourceOrganizationID.String(), "payee_name": "TEST independently owned source",
		"document_reference": "TEST-COMPOUND-" + receipt.String(), "issued_on": today.Format(time.DateOnly), "due_on": today.AddDate(0, 0, 30).Format(time.DateOnly), "currency": "INR",
		"lines": []map[string]any{{"category": "inventory", "receipt_id": receipt, "description": "TEST acquisition linked to actual accepted units", "net_minor": total, "tax_minor": 0, "recoverable_tax_minor": 0, "landed_minor": 0}},
	}
	first := admin.call("PUT", path, "owner", bill, 200)
	require.JSONEq(t, first, admin.call("PUT", path, "owner", bill, 200))
	bill["document_reference"] = "TEST-DUPLICATE-RECEIPT-" + receipt.String()
	admin.call("PUT", root+"/payables/"+uuid.Must(uuid.NewV7()).String(), "owner", bill, 409)
	linked := acquisition()
	require.NotNil(t, linked.PayableID)
	require.Equal(t, id, *linked.PayableID)
	require.Equal(t, original.QuantityUnits, linked.QuantityUnits)
	require.Equal(t, original.Supplier, linked.Supplier)
	balance := func(paid, credit, outstanding int64) {
		t.Helper()
		var row struct {
			Total       int64 `json:"total_minor"`
			Paid        int64 `json:"paid_minor"`
			Credit      int64 `json:"credit_minor"`
			Outstanding int64 `json:"outstanding_minor"`
		}
		require.NoError(t, json.Unmarshal([]byte(admin.call("GET", path, "owner", nil, 200)), &row))
		require.Equal(t, total, row.Total)
		require.Equal(t, paid, row.Paid)
		require.Equal(t, credit, row.Credit)
		require.Equal(t, outstanding, row.Outstanding)
	}
	balance(0, 0, total)
	payment := uuid.Must(uuid.NewV7())
	settlement := func(kind string, amount int64, reversal *uuid.UUID) map[string]any {
		return map[string]any{"kind": kind, "amount_minor": amount, "currency": "INR", "paid_by": "TEST buyer finance owner", "method": "bank", "source_reference": "TEST-" + kind + "-" + receipt.String(), "occurred_on": today.Format(time.DateOnly), "reason": "TEST retained financial evidence; no real bank transaction", "reversal_of": reversal}
	}
	paymentPath := path + "/payments/" + payment.String()
	input := settlement("payment", 1000, nil)
	paid := admin.call("PUT", paymentPath, "owner", input, 200)
	require.JSONEq(t, paid, admin.call("PUT", paymentPath, "owner", input, 200))
	balance(1000, 0, total-1000)
	input["amount_minor"] = int64(999)
	admin.call("PUT", paymentPath, "owner", input, 409)
	for _, kind := range []string{"credit", "payment_reversal"} {
		amount := int64(100)
		var reversal *uuid.UUID
		if kind == "payment_reversal" {
			amount = 1000
			reversal = &payment
		}
		command := path + "/adjustments/" + uuid.Must(uuid.NewV7()).String()
		input := settlement(kind, amount, reversal)
		recorded := admin.call("PUT", command, "owner", input, 200)
		require.JSONEq(t, recorded, admin.call("PUT", command, "owner", input, 200))
	}
	balance(0, 100, total-100)
}
