package supplierorder

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCancellationAcknowledgementPreservesConsentAndClosesBothSides(t *testing.T) {
	relationship, input, at := orderFixture(t)
	buyer, err := Prepare(uuid.Must(uuid.NewV7()), relationship, input, testActor("TEST-buyer-maker"), at)
	require.NoError(t, err)
	buyer, offer, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: buyer.Version, Decision: "approve", Reason: "TEST buyer review"}, testActor("TEST-buyer-reviewer"), at)
	require.NoError(t, err)
	source, err := Apply(nil, *offer, relationship.Terms.Seller.TenantID, at)
	require.NoError(t, err)
	source, accepted, err := Decide(source, SupplierOrderDecisionInput{ExpectedVersion: source.Version, Decision: "approve", Reason: "TEST source review"}, testActor("TEST-source-reviewer"), at)
	require.NoError(t, err)
	_, _, err = CompleteCancellation(source, at)
	require.ErrorIs(t, err, ErrConflict)
	buyer, err = Apply(&buyer, *accepted, buyer.OwnerTenantID, at)
	require.NoError(t, err)
	buyer, request, err := Decide(buyer, SupplierOrderDecisionInput{ExpectedVersion: buyer.Version, Decision: "cancel", Reason: "TEST no further dispatch"}, testActor("TEST-buyer-maker"), at)
	require.NoError(t, err)
	source, err = Apply(&source, *request, source.OwnerTenantID, at)
	require.NoError(t, err)
	require.Equal(t, "cancellation_requested", source.Status)
	complete, ack, err := CompleteCancellation(source, at)
	require.NoError(t, err)
	require.Equal(t, "cancelled", complete.Status)
	require.Equal(t, source.Version+1, complete.Version)
	require.Equal(t, source.AuthorityEpoch+1, complete.AuthorityEpoch)
	require.Equal(t, source.SignedBy, complete.SignedBy)
	require.Equal(t, source.SignedStaffID, complete.SignedStaffID)
	require.Equal(t, source.Input, complete.Input)
	require.Equal(t, "system:supplier-fulfilment", ack.SignatorySubject)
	buyer, err = Apply(&buyer, ack, buyer.OwnerTenantID, at)
	require.NoError(t, err)
	require.Equal(t, "cancelled", buyer.Status)
	replay, replayed, err := CompleteCancellation(complete, at)
	require.NoError(t, err)
	require.Equal(t, complete, replay)
	require.Equal(t, ack, replayed)
}
