package supplierorder

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"testing"
	"time"
)

func orderTerms() suppliertrade.Terms {
	return suppliertrade.Terms{Seller: suppliertrade.Endpoint{TenantID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Product: "supply", OrganizationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111112"), LocationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111113"), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse("019a55c0-1111-7111-8111-111111111114"), LicenceVersion: 1, Jurisdiction: "IN-MH"}, Buyer: suppliertrade.Endpoint{TenantID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), Product: "pharmacy", OrganizationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111115"), LocationID: uuid.MustParse("019a55c0-1111-7111-8111-111111111116"), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.MustParse("019a55c0-1111-7111-8111-111111111117"), LicenceVersion: 1, Jurisdiction: "IN-OD", RegistrationID: ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111119")), RegistrationVersion: ptr(int64(2)), HospitalID: ptr(uuid.MustParse("33333333-3333-4333-8333-333333333333")), BindingID: ptr(uuid.MustParse("019a55c0-1111-7111-8111-111111111118")), BindingVersion: ptr(int64(2))}, Category: "prescription", Currency: "INR", ValidFrom: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 12, 7, 0, 0, 0, 0, time.UTC), TermsReference: "TEST commercial terms revision 1"}
}
func ptr[T any](v T) *T { return &v }

func orderFixture(t *testing.T) (suppliertrade.Relationship, SupplierOrderInput, time.Time) {
	t.Helper()
	terms := orderTerms()
	at := terms.ValidFrom.Add(time.Hour)
	buyer, seller := "TEST-relationship-buyer", "TEST-relationship-seller"
	r := suppliertrade.Relationship{ID: uuid.Must(uuid.NewV7()), OwnerTenantID: terms.Buyer.TenantID, Side: "buyer", Terms: terms, Status: "active", Version: 3, AuthorityEpoch: 2, RemoteEpoch: 2, SignedBy: &buyer, RemoteSignedBy: &seller}
	var err error
	r.TermsSha256, err = suppliertrade.TermsDigest(terms)
	require.NoError(t, err)
	version := uuid.New()
	in := SupplierOrderInput{RelationshipID: r.ID, TermsSha256: r.TermsSha256, PurchaseOrderReference: "TEST-PO-1", QuoteReference: "TEST agreed source quote 1", ExpiresAt: at.Add(24 * time.Hour), Reason: "TEST agreed hospital replenishment", Lines: []SupplierOrderLine{{ID: uuid.Must(uuid.NewV7()), Product: SupplierOrderProduct{Owner: "supply", ProductID: uuid.New(), VersionID: &version}, QuantityUnits: 100, UnitCode: "capsule"}}}
	return r, in, at
}
func TestOrderPreparationRetainsApprovedRelationshipAndExactOwnerUnits(t *testing.T) {
	r, in, at := orderFixture(t)
	id := uuid.Must(uuid.NewV7())
	order, err := Prepare(id, r, in, testActor("TEST-buyer-maker"), at)
	require.NoError(t, err)
	require.Equal(t, "pending_review", order.Status)
	require.Equal(t, "buyer", order.Side)
	require.Equal(t, r.Terms.Buyer.TenantID, order.OwnerTenantID)
	require.Equal(t, in, order.Input)
	require.Nil(t, order.SignedBy)
	require.Equal(t, id, order.ID)
	require.Equal(t, int64(1), order.Version)
	// Mutating the caller's input cannot alter frozen order lines or source versions.
	in.Lines[0].QuantityUnits = 1
	*in.Lines[0].Product.VersionID = uuid.New()
	require.Equal(t, int64(100), order.Input.Lines[0].QuantityUnits)
	require.NotEqual(t, *in.Lines[0].Product.VersionID, *order.Input.Lines[0].Product.VersionID)
}

func TestOrderPreparationRejectsUnapprovedScopeAndInvalidLineIntent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*suppliertrade.Relationship, *SupplierOrderInput)
	}{
		{"inactive relationship", func(r *suppliertrade.Relationship, _ *SupplierOrderInput) { r.Status = "suspended" }},
		{"supplier cannot prepare buyer order", func(r *suppliertrade.Relationship, _ *SupplierOrderInput) {
			r.Side = "seller"
			r.OwnerTenantID = r.Terms.Seller.TenantID
		}},
		{"other relationship", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) {
			in.RelationshipID = uuid.Must(uuid.NewV7())
		}},
		{"changed terms", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.TermsSha256 = string(make([]byte, 64)) }},
		{"expired order", func(r *suppliertrade.Relationship, in *SupplierOrderInput) { in.ExpiresAt = r.Terms.ValidFrom }},
		{"beyond relationship validity", func(r *suppliertrade.Relationship, in *SupplierOrderInput) {
			in.ExpiresAt = r.Terms.ExpiresAt.Add(time.Second)
		}},
		{"no lines", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines = nil }},
		{"duplicate line", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines = append(in.Lines, in.Lines[0]) }},
		{"unbounded lines", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) {
			line := in.Lines[0]
			in.Lines = nil
			for range 101 {
				line.ID = uuid.Must(uuid.NewV7())
				in.Lines = append(in.Lines, line)
			}
		}},
		{"negative count", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines[0].QuantityUnits = -1 }},
		{"owner count bound", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines[0].QuantityUnits = 1000000001 }},
		{"other owner", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines[0].Product.Owner = "pharmacy" }},
		{"missing source product", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines[0].Product.ProductID = uuid.Nil }},
		{"missing source version", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines[0].Product.VersionID = nil }},
		{"ambiguous source version", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) {
			in.Lines[0].Product.Version = ptr(int64(1))
		}},
		{"empty units", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Lines[0].UnitCode = "" }},
		{"missing purchase reference", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.PurchaseOrderReference = "" }},
		{"missing agreed quote", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.QuoteReference = "" }},
		{"private control text", func(_ *suppliertrade.Relationship, in *SupplierOrderInput) { in.Reason = "TEST\nunsafe" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, in, at := orderFixture(t)
			tc.change(&r, &in)
			_, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
			require.Error(t, err)
		})
	}
}

func TestBuyerAndSellerIndependentlyAcceptTheSameFrozenOrder(t *testing.T) {
	r, in, at := orderFixture(t)
	buyer, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-buyer-maker"), at)
	require.NoError(t, err)
	decision := SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independent procurement review"}
	_, _, err = Decide(buyer, decision, testActor("TEST-buyer-maker"), at)
	require.ErrorIs(t, err, ErrConflict)
	buyer, offer, err := Decide(buyer, decision, testActor("TEST-buyer-reviewer"), at)
	require.NoError(t, err)
	require.Equal(t, "offered", buyer.Status)
	require.NotNil(t, offer)
	seller, err := Apply(nil, *offer, r.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, "incoming", seller.Status)
	accepted := SupplierOrderDecisionInput{ExpectedVersion: seller.Version, Decision: "approve", Reason: "TEST agreed source order"}
	_, _, err = Decide(seller, accepted, testActor("TEST-buyer-reviewer"), at)
	require.ErrorIs(t, err, ErrConflict)
	seller, event, err := Decide(seller, accepted, testActor("TEST-supplier-reviewer"), at)
	require.NoError(t, err)
	require.Equal(t, "accepted", seller.Status)
	buyer, err = Apply(&buyer, *event, r.Terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, "accepted", buyer.Status)
	require.Equal(t, seller.InputSha256, buyer.InputSha256)
	require.Equal(t, seller.AuthorityEpoch, buyer.RemoteEpoch)
	replay, err := Apply(&buyer, *event, r.Terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, buyer, replay)
}

func TestCancellationArrivingBeforeOrderApprovalCannotResurrectTheOrder(t *testing.T) {
	r, in, at := orderFixture(t)
	buyer, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
	require.NoError(t, err)
	buyer, offer, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST reviewed"}, testActor("TEST-buyer-reviewer"), at)
	require.NoError(t, err)
	buyer, cancel, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: buyer.Version, Decision: "cancel", Reason: "TEST replenishment withdrawn"}, testActor("TEST-maker"), at)
	require.NoError(t, err)
	require.Equal(t, "cancellation_requested", buyer.Status)
	require.NotNil(t, cancel)
	seller, err := Apply(nil, *cancel, r.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, "cancelled", seller.Status)
	retained, err := Apply(&seller, *offer, r.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, seller, retained)
	_, _, err = Decide(seller, SupplierOrderDecisionInput{ExpectedVersion: seller.Version, Decision: "approve", Reason: "TEST stale approval"}, testActor("TEST-source-reviewer"), at)
	require.ErrorIs(t, err, ErrConflict)
}

func TestUnacceptedOrderRejectionIsIndependentAndTerminal(t *testing.T) {
	r, in, at := orderFixture(t)
	buyer, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
	require.NoError(t, err)
	_, _, err = Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "reject", Reason: "TEST rejected"}, testActor("TEST-maker"), at)
	require.ErrorIs(t, err, ErrConflict)
	rejected, event, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "reject", Reason: "TEST rejected"}, testActor("TEST-buyer-reviewer"), at)
	require.NoError(t, err)
	require.Equal(t, "rejected", rejected.Status)
	require.Nil(t, event)
	buyer, offer, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST approved"}, testActor("TEST-buyer-reviewer"), at)
	require.NoError(t, err)
	seller, err := Apply(nil, *offer, r.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	seller, event, err = Decide(seller, SupplierOrderDecisionInput{ExpectedVersion: seller.Version, Decision: "reject", Reason: "TEST cannot supply"}, testActor("TEST-source-reviewer"), at)
	require.NoError(t, err)
	require.Equal(t, "rejected", seller.Status)
	require.NotNil(t, event)
	buyer, err = Apply(&buyer, *event, r.Terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, "rejected", buyer.Status)
}

func TestCancellationRetainsIndependentApprovalEvidence(t *testing.T) {
	r, in, at := orderFixture(t)
	buyer, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
	require.NoError(t, err)
	buyer, _, err = Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST approved"}, testActor("TEST-buyer-reviewer"), at)
	require.NoError(t, err)
	buyer, event, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: buyer.Version, Decision: "cancel", Reason: "TEST cancellation after expiry"}, testActor("TEST-maker"), in.ExpiresAt.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, "TEST-buyer-reviewer", *buyer.SignedBy)
	require.Equal(t, "TEST-maker", event.SignatorySubject)
}

func TestOrderDecisionRefusesForgedOrCorruptedOwnerEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*SupplierOrder)
	}{
		{"missing order", func(o *SupplierOrder) { o.ID = uuid.Nil }},
		{"other owner", func(o *SupplierOrder) { o.OwnerTenantID = o.Terms.Seller.TenantID }},
		{"changed immutable lines", func(o *SupplierOrder) { o.Input.Lines[0].QuantityUnits++ }},
		{"changed digest", func(o *SupplierOrder) { o.InputSha256 = "wrong" }},
		{"zero version", func(o *SupplierOrder) { o.Version = 0 }},
		{"invalid chronology", func(o *SupplierOrder) { o.UpdatedAt = o.CreatedAt.Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, in, at := orderFixture(t)
			order, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
			require.NoError(t, err)
			tc.change(&order)
			_, _, err = Decide(order, SupplierOrderDecisionInput{ExpectedVersion: order.Version, Decision: "approve", Reason: "TEST review"}, testActor("TEST-reviewer"), at)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestOrderExchangeCannotMutateTheRetainedApproval(t *testing.T) {
	r, in, at := orderFixture(t)
	buyer, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
	require.NoError(t, err)
	next, event, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST approved"}, testActor("TEST-reviewer"), at)
	require.NoError(t, err)
	event.Input.Lines[0].QuantityUnits = 1
	require.Equal(t, int64(100), next.Input.Lines[0].QuantityUnits)
	require.NoError(t, ValidateRecord(next))
}

func TestPharmacyOrdersKeepThePrivateCatalogueVersionAndOwnerUnit(t *testing.T) {
	r, in, at := orderFixture(t)
	r.Terms.Seller.Product = "pharmacy"
	r.Terms.Seller.RegistrationID = ptr(uuid.Must(uuid.NewV7()))
	r.Terms.Seller.RegistrationVersion = ptr(int64(2))
	var err error
	r.TermsSha256, err = suppliertrade.TermsDigest(r.Terms)
	require.NoError(t, err)
	in.TermsSha256 = r.TermsSha256
	in.Lines[0].Product = SupplierOrderProduct{Owner: "pharmacy", ProductID: uuid.Must(uuid.NewV7()), Version: ptr(int64(3))}
	in.Lines[0].UnitCode = "tablet"
	order, err := Prepare(uuid.Must(uuid.NewV7()), r, in, testActor("TEST-maker"), at)
	require.NoError(t, err)
	require.Equal(t, int64(3), *order.Input.Lines[0].Product.Version)
	require.Equal(t, "tablet", order.Input.Lines[0].UnitCode)
	require.Nil(t, order.Input.Lines[0].Product.VersionID)
	require.NoError(t, ValidateRecord(order))
}

func TestAnAliasAccountForThePreparingStaffCannotApproveAnOrder(t *testing.T) {
	r, in, at := orderFixture(t)
	maker := testActor("TEST-maker")
	alias := testActor("TEST-other-account")
	alias.StaffID = maker.StaffID
	order, err := Prepare(uuid.Must(uuid.NewV7()), r, in, maker, at)
	require.NoError(t, err)
	_, _, err = Decide(order, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST alias must not self-review"}, alias, at)
	require.ErrorIs(t, err, ErrConflict)
}

func testActor(subject string) Actor {
	return Actor{Subject: subject, StaffID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(subject))}
}

func TestDirectedOrderExchangeOmitsPrivateLocalStaffReferences(t *testing.T) {
	r, in, at := orderFixture(t)
	maker := testActor("TEST-maker")
	reviewer := testActor("TEST-reviewer")
	order, err := Prepare(uuid.Must(uuid.NewV7()), r, in, maker, at)
	require.NoError(t, err)
	order, event, err := Decide(order, SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST reviewed"}, reviewer, at)
	require.NoError(t, err)
	require.Equal(t, maker.StaffID, *order.PreparedStaffID)
	require.Equal(t, reviewer.StaffID, *order.SignedStaffID)
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	require.NotContains(t, string(raw), maker.StaffID.String())
	require.NotContains(t, string(raw), reviewer.StaffID.String())
}
