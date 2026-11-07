package supply_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // external test exercises its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type fabricatedRelationshipProof struct{}

func (fabricatedRelationshipProof) Snapshot() suppliertrade.Relationship {
	return suppliertrade.Relationship{Status: "active"}
}

func verifyRelationshipProofBoundaries(t *testing.T, db *tenantdb.DB) {
	t.Helper()
	owner, proofs := supply.RelationshipProofFixtureForTest(db)
	proofs["fabricated"] = fabricatedRelationshipProof{}
	for name, proof := range proofs {
		t.Run(name, func(t *testing.T) {
			err := db.WithGuardedTenantCommand(t.Context(), testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
				require.True(t, db.GuardedCommandActive(ctx))
				err := owner.WithVerifiedRelationship(ctx, proof, func(context.Context, suppliertrade.Relationship) error {
					t.Fatal("invalid proofs must not invoke the consumer")
					return nil
				})
				require.ErrorIs(t, err, suppliertrade.ErrUnavailable)
				return nil
			})
			require.NoError(t, err)
		})
	}
}

func TestRelationshipProofSnapshotCannotMutateVerifiedEndpointReferences(t *testing.T) {
	hospital, binding, registration := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	bindingVersion, registrationVersion := int64(2), int64(3)
	endpoint := suppliertrade.Endpoint{HospitalID: &hospital, BindingID: &binding, BindingVersion: &bindingVersion, RegistrationID: &registration, RegistrationVersion: &registrationVersion}
	proof := supply.RelationshipSnapshotForTest(suppliertrade.Relationship{Terms: suppliertrade.Terms{Seller: endpoint, Buyer: endpoint}})
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
