package globalapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestShipmentSafetyRetainsEightDistinctSourceShipments(t *testing.T) {
	w := newOutletWorld(t)
	lot := w.lot(t)
	store := supplierstock.Store{Owner: supplierstock.Outlet}
	order, input, actor := sourceOrder(t, "pharmacy", lot)
	allocation := uuid.Must(uuid.NewV7())
	var shipments []f.Shipment
	require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
		_, _, err := store.Create(tx, allocation, order, input, actor, evidence(lot))
		return err
	}))
	for index := range 8 {
		require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
			receipt, _, err := store.Command(tx, order.OwnerTenantID, allocation, uuid.Must(uuid.NewV7()), f.AllocationDecisionInput{ExpectedVersion: int64(index + 1), QuantityUnits: 1, Decision: "dispatch", HandoffReference: "TEST retained capacity", Reason: "TEST distinct immutable shipment"}, actor, rawDispatch{})
			if err != nil {
				return err
			}
			mrp := int64(12500)
			shipment, err := f.ShipmentFor(receipt, &mrp)
			shipments = append(shipments, shipment)
			return err
		}))
	}
	safety := pharmacy.New().ShipmentSafety(platform.Deps{DB: w.db})
	ctx, cancel := context.WithTimeout(tenantdb.WithActor(t.Context(), actor.Subject), 3*time.Second)
	defer cancel()
	checked := 0
	var run func(context.Context, int) error
	run = func(ctx context.Context, index int) error {
		if index == len(shipments) {
			checked++
			return nil
		}
		return safety.WithSafeShipment(ctx, shipments[index], func(next context.Context) error { return run(next, index+1) })
	}
	err := run(ctx, 0)
	require.NoError(t, err, "eight distinct retained source checks must not exhaust the five-connection pool")
	require.Equal(t, 1, checked, "the destination continuation runs once, after every current source check")
}
