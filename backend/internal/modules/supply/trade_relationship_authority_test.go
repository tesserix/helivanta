package supply

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

type fabricatedRelationshipProof struct{}

func (fabricatedRelationshipProof) Snapshot() suppliertrade.Relationship {
	return suppliertrade.Relationship{Status: "active"}
}

func TestRelationshipProofRejectsForeignExpiredAndFabricatedProofsBeforeOwnerAccess(t *testing.T) {
	owner := &relationshipAuthority{}
	other := &relationshipAuthority{}
	for name, proof := range map[string]suppliertrade.VerifiedRelationship{
		"missing":      nil,
		"fabricated":   fabricatedRelationshipProof{},
		"other issuer": &verifiedRelationship{owner: other, checked: time.Now()},
		"expired":      &verifiedRelationship{owner: owner, checked: time.Now().Add(-6 * time.Second)},
	} {
		t.Run(name, func(t *testing.T) {
			err := owner.WithVerifiedRelationship(t.Context(), proof, func(context.Context, suppliertrade.Relationship) error {
				t.Fatal("invalid proofs must not invoke the consumer")
				return nil
			})
			require.ErrorIs(t, err, suppliertrade.ErrUnavailable)
		})
	}
	require.ErrorIs(t, owner.WithVerifiedRelationship(t.Context(), &verifiedRelationship{owner: owner, checked: time.Now()}, nil), suppliertrade.ErrUnavailable)
}

func TestRelationshipProofSnapshotCannotMutateVerifiedEndpointReferences(t *testing.T) {
	hospital, binding, registration := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	bindingVersion, registrationVersion := int64(2), int64(3)
	endpoint := suppliertrade.Endpoint{HospitalID: &hospital, BindingID: &binding, BindingVersion: &bindingVersion, RegistrationID: &registration, RegistrationVersion: &registrationVersion}
	proof := &verifiedRelationship{state: suppliertrade.Relationship{Terms: suppliertrade.Terms{Seller: endpoint, Buyer: endpoint}}}
	snapshot := proof.Snapshot()
	for _, ep := range []*suppliertrade.Endpoint{&snapshot.Terms.Seller, &snapshot.Terms.Buyer} {
		*ep.HospitalID, *ep.BindingID, *ep.RegistrationID = uuid.New(), uuid.New(), uuid.New()
		*ep.BindingVersion, *ep.RegistrationVersion = 100, 101
	}
	current := proof.Snapshot()
	for _, ep := range []suppliertrade.Endpoint{current.Terms.Seller, current.Terms.Buyer} {
		require.Equal(t, hospital, *ep.HospitalID)
		require.Equal(t, binding, *ep.BindingID)
		require.Equal(t, registration, *ep.RegistrationID)
		require.Equal(t, int64(2), *ep.BindingVersion)
		require.Equal(t, int64(3), *ep.RegistrationVersion)
	}
}
