package suppliertrade

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBilateralConsentNeverBecomesAuthorityFromAnOfferOrLateAcknowledgement(t *testing.T) {
	terms := testTerms()
	at := terms.ValidFrom.Add(time.Hour)
	seller, err := PrepareRelationship(uuid.Must(uuid.NewV7()), terms, "TEST-maker", at)
	require.NoError(t, err)
	require.False(t, RelationshipCurrent(seller, at))
	_, _, err = DecideRelationship(seller, 1, "approve", "TEST-maker", at)
	require.ErrorIs(t, err, ErrConflict)
	seller, offer, err := DecideRelationship(seller, 1, "approve", "TEST-source-reviewer", at)
	require.NoError(t, err)
	require.Equal(t, "offered", seller.Status)
	buyer, reply, err := ApplyRelationshipExchange(nil, *offer, terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Nil(t, reply)
	require.Equal(t, "incoming", buyer.Status)
	require.False(t, RelationshipCurrent(buyer, at))
	buyer, accepted, err := DecideRelationship(buyer, buyer.Version, "approve", "TEST-hospital-reviewer", at)
	require.NoError(t, err)
	require.Equal(t, "accepted_pending_ack", buyer.Status)
	seller, ack, err := ApplyRelationshipExchange(&seller, *accepted, terms.Seller.TenantID, at)
	require.NoError(t, err)
	require.True(t, RelationshipCurrent(seller, at))
	require.NotNil(t, ack)
	buyer, reply, err = ApplyRelationshipExchange(&buyer, *ack, terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Nil(t, reply)
	require.True(t, RelationshipCurrent(buyer, at))
	require.Equal(t, seller.AuthorityEpoch, buyer.RemoteEpoch)
	require.Equal(t, buyer.AuthorityEpoch, seller.RemoteEpoch)
	suspended, stop, err := DecideRelationship(seller, seller.Version, "suspend", "TEST-source-reviewer", at)
	require.NoError(t, err)
	require.False(t, RelationshipCurrent(suspended, at))
	buyer, _, err = ApplyRelationshipExchange(&buyer, *stop, terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.False(t, RelationshipCurrent(buyer, at))
	late, _, err := ApplyRelationshipExchange(&buyer, *ack, terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, buyer, late)
	_, _, err = DecideRelationship(suspended, suspended.Version, "approve", "TEST-source-reviewer", at)
	require.ErrorIs(t, err, ErrConflict)
}
func TestExchangeRejectsForeignDestinationsAndTermsAndRetainsEarlySuspension(t *testing.T) {
	terms := testTerms()
	at := terms.ValidFrom.Add(time.Hour)
	seller, err := PrepareRelationship(uuid.Must(uuid.NewV7()), terms, "TEST-maker", at)
	require.NoError(t, err)
	seller, offer, err := DecideRelationship(seller, 1, "approve", "TEST-source-reviewer", at)
	require.NoError(t, err)
	_, _, err = ApplyRelationshipExchange(nil, *offer, uuid.New(), at)
	require.ErrorIs(t, err, ErrInvalid)
	changed := *offer
	changed.Terms.Buyer.LocationID = uuid.Must(uuid.NewV7())
	_, _, err = ApplyRelationshipExchange(nil, changed, terms.Buyer.TenantID, at)
	require.ErrorIs(t, err, ErrInvalid)
	seller, stop, err := DecideRelationship(seller, seller.Version, "suspend", "TEST-source-reviewer", at)
	require.NoError(t, err)
	buyer, _, err := ApplyRelationshipExchange(nil, *stop, terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, "suspended", buyer.Status)
	late, _, err := ApplyRelationshipExchange(&buyer, *offer, terms.Buyer.TenantID, at)
	require.NoError(t, err)
	require.Equal(t, buyer, late)
	expired := seller
	expired.Status = "active"
	expired.SignedBy = ptr("TEST-reviewer")
	expired.RemoteSignedBy = ptr("TEST-peer")
	expired.RemoteEpoch = 2
	require.False(t, RelationshipCurrent(expired, terms.ExpiresAt))
	require.False(t, RelationshipCurrent(expired, time.Time{}))
}
