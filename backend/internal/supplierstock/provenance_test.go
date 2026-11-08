package supplierstock

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"strings"
	"testing"
	"time"
)

func TestShipmentVerificationRejectsAnotherStockOwnerBeforeReading(t *testing.T) {
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	version := int64(1)
	shipment := f.Shipment{ID: id(), AllocationID: id(), OrderID: id(), LineID: id(), OrderInputSha256: strings.Repeat("a", 64), SourceTenantID: uuid.New(), BuyerTenantID: uuid.New(), HospitalID: uuid.New(), SourceOrganizationID: id(), SourceLocationID: id(), BuyerOrganizationID: id(), BuyerLocationID: id(), Product: supplierorder.SupplierOrderProduct{Owner: "pharmacy", ProductID: id(), Version: &version}, UnitCode: "capsule", QuantityUnits: 1, LotNumber: "TEST-LOT", ExpiresOn: "2030-12-31", HandoffReference: "TEST physical handoff", ActorSubject: "TEST stock actor", ProductDescription: "TEST capsules", PackSize: 10, DispatchedAt: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, f.ValidateShipment(shipment))
	require.ErrorIs(t, (Store{Owner: Warehouse}).VerifyShipment(nil, shipment), f.ErrInvalid)
	shipment.ID = uuid.Nil
	require.ErrorIs(t, (Store{Owner: Outlet}).VerifyShipment(nil, shipment), f.ErrInvalid)
}
