package globalapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func (f supplierCompoundFixture) assertPublicSupplierRelationship(t *testing.T, sellerAdmin, buyerAdmin operatingDutyFixture, seller, buyer suppliertrade.Endpoint) suppliertrade.Relationship {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.Must(uuid.NewV7())
	path := "/v1/supply/relationships/" + id.String()
	input := suppliertrade.SupplierRelationshipInput{Terms: suppliertrade.Terms{Seller: seller, Buyer: buyer, Category: "prescription", Currency: "INR", ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), TermsReference: "TEST public bilateral supplier agreement"}, Reason: "TEST source administrator proposes exact independently owned endpoints"}
	first := sellerAdmin.call("PUT", path, "owner", input, 201)
	require.JSONEq(t, first, sellerAdmin.call("PUT", path, "owner", input, 200))
	var proposed suppliertrade.Relationship
	require.NoError(t, json.Unmarshal([]byte(first), &proposed))
	require.Equal(t, "pending_review", proposed.Status)
	require.False(t, suppliertrade.RelationshipCurrent(proposed, now))
	command := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	approve := suppliertrade.SupplierRelationshipCommand{ExpectedVersion: proposed.Version, Decision: "approve", Reason: "TEST independent source signatory approves proposed terms"}
	sellerAdmin.call("PUT", command, "owner", approve, 409)
	offered := sellerAdmin.call("PUT", command, "reviewer", approve, 201)
	require.JSONEq(t, offered, sellerAdmin.call("PUT", command, "reviewer", approve, 200))
	require.JSONEq(t, offered, sellerAdmin.call("GET", command, "reviewer", nil, 200))
	wait := func(admin operatingDutyFixture, want string) suppliertrade.Relationship {
		t.Helper()
		var result suppliertrade.Relationship
		require.Eventually(t, func() bool {
			response := f.sourceStaffRequest(t, admin.principal, "GET", path, nil)
			require.Contains(t, []int{200, 404}, response.Code, response.Body.String())
			if response.Code != 200 {
				return false
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			return result.Status == want
		}, 20*time.Second, 100*time.Millisecond)
		return result
	}
	incoming := wait(buyerAdmin, "incoming")
	require.Equal(t, proposed.TermsSha256, incoming.TermsSha256)
	require.False(t, suppliertrade.RelationshipCurrent(incoming, time.Now()))
	buyerCommand := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	accept := suppliertrade.SupplierRelationshipCommand{ExpectedVersion: incoming.Version, Decision: "approve", Reason: "TEST distinct hospital signatory accepts current source and exact terms"}
	buyerAdmin.call("PUT", buyerCommand, "reviewer", accept, 409)
	accepted := buyerAdmin.call("PUT", buyerCommand, "owner", accept, 201)
	require.JSONEq(t, accepted, buyerAdmin.call("PUT", buyerCommand, "owner", accept, 200))
	source := wait(sellerAdmin, "active")
	hospital := wait(buyerAdmin, "active")
	require.True(t, suppliertrade.RelationshipCurrent(source, time.Now()))
	require.True(t, suppliertrade.RelationshipCurrent(hospital, time.Now()))
	require.Equal(t, source.TermsSha256, hospital.TermsSha256)
	require.Equal(t, source.AuthorityEpoch, hospital.RemoteEpoch)
	require.Equal(t, hospital.AuthorityEpoch, source.RemoteEpoch)
	require.NotNil(t, source.SignedBy)
	require.NotNil(t, source.RemoteSignedBy)
	require.NotEqual(t, *source.SignedBy, *source.RemoteSignedBy)
	changed := accept
	changed.Reason = "TEST changed buyer approval intent"
	buyerAdmin.call("PUT", buyerCommand, "owner", changed, 409)
	return source
}
