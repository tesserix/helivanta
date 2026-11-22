package pharmacy

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
)

func TestSupplierBatchSourcesPreserveReceiptScopeWithoutDuplicateSafety(t *testing.T) {
	tenant, outlet := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	first := f.Shipment{ID: uuid.Must(uuid.NewV7()), BuyerTenantID: tenant, BuyerLocationID: outlet, QuantityUnits: 10}
	second := first
	second.ID = uuid.Must(uuid.NewV7())
	row := func(s f.Shipment) supplierBatchSource {
		body, err := json.Marshal(s)
		require.NoError(t, err)
		return supplierBatchSource{ID: uuid.Must(uuid.NewV7()), ShipmentID: s.ID, Body: body}
	}
	rows := []supplierBatchSource{row(first), row(second), row(first), row(second), row(first), row(first)}
	before := append([]supplierBatchSource(nil), rows...)
	got, err := supplierBatchShipments(rows, tenant, outlet)
	require.NoError(t, err)
	require.Equal(t, []f.Shipment{first, second}, got, "partial receipts from one immutable shipment need one retained current safety proof")
	require.Equal(t, before, rows, "deduplication must preserve every receipt ID for the destination write scope")
}

func TestSupplierBatchSourcesRejectCorruptOrForeignProofs(t *testing.T) {
	tenant, outlet := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	shipment := f.Shipment{ID: uuid.Must(uuid.NewV7()), BuyerTenantID: tenant, BuyerLocationID: outlet, QuantityUnits: 10}
	body, err := json.Marshal(shipment)
	require.NoError(t, err)
	good := supplierBatchSource{ID: uuid.Must(uuid.NewV7()), ShipmentID: shipment.ID, Body: body}
	badID := good
	badID.ShipmentID = uuid.Must(uuid.NewV7())
	badReceipt := good
	badReceipt.ID = uuid.Nil
	badJSON := good
	badJSON.Body = json.RawMessage(`{`)
	changed := shipment
	changed.QuantityUnits++
	changedBody, err := json.Marshal(changed)
	require.NoError(t, err)
	conflict := supplierBatchSource{ID: uuid.Must(uuid.NewV7()), ShipmentID: shipment.ID, Body: changedBody}
	for _, tc := range []struct {
		name           string
		rows           []supplierBatchSource
		tenant, outlet uuid.UUID
	}{
		{"shipment identity", []supplierBatchSource{badID}, tenant, outlet},
		{"receipt identity", []supplierBatchSource{badReceipt}, tenant, outlet},
		{"invalid body", []supplierBatchSource{badJSON}, tenant, outlet},
		{"buyer tenant", []supplierBatchSource{good}, uuid.Must(uuid.NewV7()), outlet},
		{"buyer outlet", []supplierBatchSource{good}, tenant, uuid.Must(uuid.NewV7())},
		{"conflicting duplicate", []supplierBatchSource{good, conflict}, tenant, outlet},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := supplierBatchShipments(tc.rows, tc.tenant, tc.outlet)
			require.ErrorIs(t, err, f.ErrUnavailable)
			require.Nil(t, got)
		})
	}
}
