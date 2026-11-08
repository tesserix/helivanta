package supplierfulfilment

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
	"time"
)

func orderTerms() suppliertrade.Terms {
	return suppliertrade.Terms{Seller: suppliertrade.Endpoint{TenantID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Product: "supply", OrganizationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111112"), LocationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111113"), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse("019a55c0-1111-7111-8111-111111111114"), LicenceVersion: 1, Jurisdiction: "IN-MH"}, Buyer: suppliertrade.Endpoint{TenantID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), Product: "pharmacy", OrganizationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111115"), LocationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111116"), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse("019a55c0-1111-7111-8111-111111111117"), LicenceVersion: 1, Jurisdiction: "IN-OD", RegistrationID: ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111119")), RegistrationVersion: ptr(int64(2)), HospitalID: ptr(uuid.MustParse("33333333-3333-4333-8333-333333333333")), BindingID: ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111118")), BindingVersion: ptr(int64(2))}, Category: "prescription", Currency: "INR", ValidFrom: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 12, 7, 0, 0, 0, 0, time.UTC), TermsReference: "TEST commercial terms revision 1"}
}
func ptr[T any](v T) *T { return &v }

func orderFixture(t *testing.T) (suppliertrade.Relationship, supplierorder.SupplierOrderInput, time.Time) {
	t.Helper()
	terms := orderTerms()
	at := terms.ValidFrom.Add(time.Hour)
	buyer, seller := "TEST-relationship-buyer", "TEST-relationship-seller"
	r := suppliertrade.Relationship{ID: uuid.Must(uuid.NewV7()), OwnerTenantID: terms.Buyer.TenantID, Side: "buyer", Terms: terms, Status: "active", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, SignedBy: &buyer, RemoteSignedBy: &seller}
	var err error
	r.TermsSha256, err = suppliertrade.TermsDigest(terms)
	require.NoError(t, err)
	version := uuid.New()
	in := supplierorder.SupplierOrderInput{RelationshipID: r.ID, TermsSha256: r.TermsSha256, PurchaseOrderReference: "TEST-PO-1", QuoteReference: "TEST agreed source quote 1", ExpiresAt: at.Add(24 * time.Hour), Reason: "TEST agreed hospital replenishment", Lines: []supplierorder.SupplierOrderLine{{ID: uuid.Must(uuid.NewV7()), Product: supplierorder.SupplierOrderProduct{Owner: "supply", ProductID: uuid.New(), VersionID: &version}, QuantityUnits: 100, UnitCode: "capsule"}}}
	return r, in, at
}

func acceptedOrder(t *testing.T, owner string) (supplierorder.SupplierOrder, ReserveInput, supplierorder.Actor, time.Time) {
	t.Helper()
	r, input, at := orderFixture(t)
	if owner == "pharmacy" {
		r.Terms.Seller.Product = "pharmacy"
		r.Terms.Seller.RegistrationID = ptr(uuid.Must(uuid.NewV7()))
		r.Terms.Seller.RegistrationVersion = ptr(int64(1))
		var err error
		r.TermsSha256, err = suppliertrade.TermsDigest(r.Terms)
		require.NoError(t, err)
		input.TermsSha256 = r.TermsSha256
		v := int64(1)
		input.Lines[0].Product = supplierorder.SupplierOrderProduct{Owner: owner, ProductID: uuid.Must(uuid.NewV7()), Version: &v}
	}
	actor := supplierorder.Actor{Subject: "TEST-source-stock-operator", StaffID: uuid.New()}
	order, err := supplierorder.Prepare(uuid.Must(uuid.NewV7()), r, input, supplierorder.Actor{Subject: "TEST-hospital-maker", StaffID: uuid.New()}, at)
	require.NoError(t, err)
	_, event, err := supplierorder.Decide(order, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST hospital independent review"}, supplierorder.Actor{Subject: "TEST-hospital-reviewer", StaffID: uuid.New()}, at)
	require.NoError(t, err)
	source, err := supplierorder.Apply(nil, *event, r.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	source, _, err = supplierorder.Decide(source, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST source independent review"}, supplierorder.Actor{Subject: "TEST-source-reviewer", StaffID: uuid.New()}, at)
	require.NoError(t, err)
	in := ReserveInput{ExpectedOrderVersion: source.Version, LineID: source.Input.Lines[0].ID, StockID: uuid.Must(uuid.NewV7()), QuantityUnits: 50, ExpiresAt: at.Add(time.Hour), Reason: "TEST exact stock allocation"}
	if owner == "pharmacy" {
		v := uuid.Must(uuid.NewV7())
		in.StockLocationID = &v
	}
	return source, in, actor, at
}
func TestPlanRejectsCommitmentBeyondRemainingOrderLine(t *testing.T) {
	for _, owner := range []string{"supply", "pharmacy"} {
		t.Run(owner, func(t *testing.T) {
			order, in, actor, at := acceptedOrder(t, owner)
			_, err := Plan(uuid.Must(uuid.NewV7()), order, in, actor, Budget{ReservedUnits: 40, DispatchedUnits: 20}, at)
			require.ErrorIs(t, err, ErrConflict)
		})
	}
}

func TestReserveRejectsExpiryAtTheIndiaPremiseMidnight(t *testing.T) {
	order, in, actor, at := acceptedOrder(t, "supply")
	at = at.Add(17 * time.Hour)
	in.ExpiresAt = at.Add(time.Hour)
	plan, err := Plan(uuid.Must(uuid.NewV7()), order, in, actor, Budget{}, at)
	require.NoError(t, err)
	_, err = Reserve(plan, StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2026-11-08", AvailableUnits: 50, Description: "TEST capsule 500 mg; pack 10", PackSize: 10}, at.Add(31*time.Minute))
	require.ErrorIs(t, err, ErrConflict)
}

func TestDispatchCannotUseAnExpiredLease(t *testing.T) {
	order, in, actor, at := acceptedOrder(t, "pharmacy")
	plan, err := Plan(uuid.Must(uuid.NewV7()), order, in, actor, Budget{}, at)
	require.NoError(t, err)
	a, err := Reserve(plan, StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2027-01-01", AvailableUnits: 100, Description: "TEST capsule 500 mg; pack 10", PackSize: 10}, at)
	require.NoError(t, err)
	_, err = Decide(a, AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 10, Decision: "dispatch", HandoffReference: "TEST transport challan 1", Reason: "TEST confirmed physical handoff"}, actor, in.ExpiresAt)
	require.ErrorIs(t, err, ErrConflict)
}

func shipmentFixture(t *testing.T) (Shipment, ReceivingInput, supplierorder.Actor, time.Time) {
	t.Helper()
	o, in, a, at := acceptedOrder(t, "supply")
	p, err := Plan(uuid.Must(uuid.NewV7()), o, in, a, Budget{}, at)
	require.NoError(t, err)
	stock, err := Reserve(p, StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2027-01-01", AvailableUnits: 100, Description: "TEST capsule 500 mg; pack 10", PackSize: 10}, at)
	require.NoError(t, err)
	cmd := AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 50, Decision: "dispatch", HandoffReference: "TEST-CHALLAN-1", Reason: "TEST physical courier handoff"}
	next, err := Decide(stock, cmd, a, at)
	require.NoError(t, err)
	ship, err := ShipmentFor(AllocationCommandReceipt{CommandID: uuid.Must(uuid.NewV7()), Input: cmd, Allocation: next, ActorSubject: a.Subject, ActorStaffID: a.StaffID, RecordedAt: next.UpdatedAt}, ptr(int64(12345)))
	require.NoError(t, err)
	input := ReceivingInput{DestinationProductID: ptr(uuid.Must(uuid.NewV7())), DestinationProductVersion: ptr(int64(1)), DestinationBatchID: ptr(uuid.Must(uuid.NewV7())), QuarantineLocationID: ptr(uuid.Must(uuid.NewV7())), MrpMinor: ptr(int64(12345)), CountedUnits: 30, AcceptedUnits: 20, RejectedUnits: 10, ObservedLotNumber: ship.LotNumber, ObservedExpiresOn: ship.ExpiresOn, IdentityConfirmed: true, Reason: "TEST physical count with damaged units in retained custody"}
	return ship, input, supplierorder.Actor{Subject: "TEST-hospital-receiver", StaffID: uuid.New()}, at.Add(time.Minute)
}
func TestReceivingRejectsCumulativeCountsAboveDispatchedUnits(t *testing.T) {
	s, in, a, at := shipmentFixture(t)
	_, err := Receive(uuid.Must(uuid.NewV7()), s, in, ReceivingTotals{Counted: 30, Accepted: 20, Rejected: 10}, a, at)
	require.ErrorIs(t, err, ErrConflict)
}

func TestPartialDispatchAndReleasePreservePhysicalQuantities(t *testing.T) {
	for _, owner := range []string{"supply", "pharmacy"} {
		t.Run(owner, func(t *testing.T) {
			order, in, actor, at := acceptedOrder(t, owner)
			p, err := Plan(uuid.Must(uuid.NewV7()), order, in, actor, Budget{ReservedUnits: 30, DispatchedUnits: 20}, at)
			require.NoError(t, err)
			a, err := Reserve(p, StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2027-01-01", AvailableUnits: 60, Description: "TEST capsule 500 mg; pack 10", PackSize: 10}, at)
			require.NoError(t, err)
			a, err = Decide(a, AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 20, Decision: "dispatch", HandoffReference: "TEST-CHALLAN", Reason: "TEST partial physical handoff"}, actor, at)
			require.NoError(t, err)
			require.Equal(t, int64(30), a.ReservedUnits)
			require.Equal(t, int64(20), a.DispatchedUnits)
			require.Equal(t, "reserved", a.Status)
			v, err := Observe(a, in.ExpiresAt)
			require.NoError(t, err)
			require.Equal(t, "expired", v.EffectiveStatus)
			require.Zero(t, v.EffectiveReservedUnits)
			require.Equal(t, int64(20), v.Allocation.DispatchedUnits)
			a, err = Decide(a, AllocationDecisionInput{ExpectedVersion: 2, QuantityUnits: 30, Decision: "release", Reason: "TEST release only undispatched remainder"}, actor, in.ExpiresAt)
			require.NoError(t, err)
			require.Zero(t, a.ReservedUnits)
			require.Equal(t, int64(20), a.DispatchedUnits)
			require.Equal(t, int64(30), a.ReleasedUnits)
			require.NoError(t, ValidateAllocation(a))
			_, err = Decide(a, AllocationDecisionInput{ExpectedVersion: 3, QuantityUnits: 1, Decision: "release", Reason: "TEST cannot release physical dispatch"}, actor, in.ExpiresAt)
			require.ErrorIs(t, err, ErrConflict)
		})
	}
}
func TestReceivingKeepsAcceptedQuarantineAndRejectedCustodySeparate(t *testing.T) {
	s, in, a, at := shipmentFixture(t)
	r, err := Receive(uuid.Must(uuid.NewV7()), s, in, ReceivingTotals{}, a, at)
	require.NoError(t, err)
	require.Equal(t, int64(30), r.CumulativeCountedUnits)
	require.Equal(t, int64(20), r.CumulativeAcceptedUnits)
	require.Equal(t, int64(10), r.CumulativeRejectedUnits)
	require.NotNil(t, r.MovementID)
	c, err := CloseReceiving(uuid.Must(uuid.NewV7()), s, ReceivingTotals{Counted: 30, Accepted: 20, Rejected: 10}, ReceivingClosureInput{ExpectedCountedUnits: 30, Reason: "TEST remaining twenty units missing"}, a, at)
	require.NoError(t, err)
	require.Equal(t, int64(20), c.ShortUnits)
	require.Equal(t, int64(10), c.RejectedUnits)
}
func TestShipmentDoesNotAliasOwnerProductAndMRPEvidence(t *testing.T) {
	order, in, actor, at := acceptedOrder(t, "pharmacy")
	p, err := Plan(uuid.Must(uuid.NewV7()), order, in, actor, Budget{}, at)
	require.NoError(t, err)
	a, err := Reserve(p, StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2027-01-01", AvailableUnits: 100, Description: "TEST capsule 500 mg; pack 10", PackSize: 10}, at)
	require.NoError(t, err)
	cmd := AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 20, Decision: "dispatch", HandoffReference: "TEST-CHALLAN", Reason: "TEST partial dispatch"}
	a, err = Decide(a, cmd, actor, at)
	require.NoError(t, err)
	mrp := int64(100)
	s, err := ShipmentFor(AllocationCommandReceipt{CommandID: uuid.Must(uuid.NewV7()), Input: cmd, Allocation: a, ActorSubject: actor.Subject, ActorStaffID: actor.StaffID, RecordedAt: a.UpdatedAt}, &mrp)
	require.NoError(t, err)
	mrp = 999
	*a.Plan.Product.Version = 999
	require.Equal(t, int64(100), *s.MrpMinor)
	require.Equal(t, int64(1), *s.Product.Version)
}

func TestRejectedCustodyDoesNotInventDestinationStock(t *testing.T) {
	s, in, a, at := shipmentFixture(t)
	in.AcceptedUnits = 0
	in.RejectedUnits = in.CountedUnits
	in.IdentityConfirmed = false
	in.DestinationProductID = nil
	in.DestinationProductVersion = nil
	in.DestinationBatchID = nil
	in.QuarantineLocationID = nil
	in.MrpMinor = nil
	in.ObservedLotNumber = "TEST wrong lot"
	in.ObservedExpiresOn = "2025-01-01"
	r, err := Receive(uuid.Must(uuid.NewV7()), s, in, ReceivingTotals{}, a, at)
	require.NoError(t, err)
	require.Nil(t, r.MovementID)
	require.Zero(t, r.CumulativeAcceptedUnits)
	require.Equal(t, int64(30), r.CumulativeRejectedUnits)
	in.MrpMinor = ptr(int64(-1))
	_, err = Receive(uuid.Must(uuid.NewV7()), s, in, ReceivingTotals{}, a, at)
	require.ErrorIs(t, err, ErrInvalid)
}
