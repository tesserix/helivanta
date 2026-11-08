package supplierfulfilment

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestReceivingSummaryMustMatchActualDispatchedQuantity(t *testing.T) {
	order, in, actor, at := acceptedOrder(t, "supply")
	plan, err := Plan(uuid.Must(uuid.NewV7()), order, in, actor, Budget{}, at)
	require.NoError(t, err)
	allocation, err := Reserve(plan, StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2027-01-01", AvailableUnits: 100, Description: "TEST capsules", PackSize: 10}, at)
	require.NoError(t, err)
	receipt, err := Decide(allocation, AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 20, Decision: "dispatch", HandoffReference: "TEST dispatch", Reason: "TEST handoff"}, actor, at)
	require.NoError(t, err)
	shipment, err := ShipmentFor(AllocationCommandReceipt{CommandID: uuid.Must(uuid.NewV7()), Input: AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 20, Decision: "dispatch", HandoffReference: "TEST dispatch", Reason: "TEST handoff"}, Allocation: receipt, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, RecordedAt: at}, nil)
	require.NoError(t, err)
	valid := ReceivingSummary{ID: uuid.Must(uuid.NewV7()), ShipmentID: shipment.ID, OrderID: shipment.OrderID, OwnerTenantID: shipment.BuyerTenantID, Version: 1, CountedUnits: 18, AcceptedUnits: 16, RejectedUnits: 2, ShortUnits: 2, Closed: true, Reason: "TEST counted", ActorSubject: "TEST receiver", RecordedAt: at.Add(time.Minute)}
	require.NoError(t, ValidateReceivingSummary(valid, shipment))
	for _, tc := range []struct {
		name   string
		change func(*ReceivingSummary)
	}{
		{"over shipment", func(s *ReceivingSummary) { s.CountedUnits = 21; s.AcceptedUnits = 19; s.ShortUnits = 0 }},
		{"wrong shortage", func(s *ReceivingSummary) { s.ShortUnits = 1 }},
		{"open shortage", func(s *ReceivingSummary) { s.Closed = false }},
		{"wrong destination", func(s *ReceivingSummary) { s.OwnerTenantID = shipment.SourceTenantID }},
		{"wrong shipment", func(s *ReceivingSummary) { s.ShipmentID = uuid.Must(uuid.NewV7()) }},
		{"wrong order", func(s *ReceivingSummary) { s.OrderID = uuid.Must(uuid.NewV7()) }},
		{"overflow", func(s *ReceivingSummary) { s.AcceptedUnits = 1 << 62; s.RejectedUnits = 1 << 62 }},
		{"before dispatch", func(s *ReceivingSummary) { s.RecordedAt = at.Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.change(&s)
			require.ErrorIs(t, ValidateReceivingSummary(s, shipment), ErrInvalid)
		})
	}
}
