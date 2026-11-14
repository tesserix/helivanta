package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceFindingProofRequiresExactPreparationSourceAndStaffBasis(t *testing.T) {
	hospital, staff := uuid.New(), uuid.New()
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-finding-preparer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	version, epoch := int64(3), int64(2)
	basis := EmploymentPersonFindingSearchInput{StaffVersion: &version, EmploymentEpoch: &epoch}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 2, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().Add(time.Hour)}
	proof, err := employmentFindingGovernanceProof(p, hospital, staff, basis, source)
	require.NoError(t, err)
	require.Equal(t, "employment_finding_selection", proof["purpose"])
	require.Equal(t, staff, proof["staff_id"])
	require.Equal(t, version, proof["staff_version"])
	require.Equal(t, epoch, proof["employment_epoch"])
	require.Equal(t, authz.GovernanceEmploymentPrepare, proof["capability"])
	for _, key := range []string{"review_id", "actor_staff_id", "actor_link_id", "declared_subject", "identity_hash"} {
		require.NotContains(t, proof, key)
	}
	_, err = employmentFindingGovernanceProof(p, hospital, uuid.Nil, basis, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = employmentFindingGovernanceProof(p, uuid.New(), staff, basis, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	_, err = employmentFindingGovernanceProof(p, hospital, staff, EmploymentPersonFindingSearchInput{}, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	source.Kind, source.OwnerMembershipID, source.GrantID, source.Version, source.TemplateKey = authz.GovernanceGrantSource, uuid.Nil, uuid.Must(uuid.NewV7()), 2, "workforce.employment_reviewer.v1"
	_, err = employmentFindingGovernanceProof(p, hospital, staff, basis, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	source.TemplateKey = "workforce.employment_preparer.v1"
	proof, err = employmentFindingGovernanceProof(p, hospital, staff, basis, source)
	require.NoError(t, err)
	require.Equal(t, source.GrantID, proof["source_grant_id"])
	require.NotContains(t, proof, "owner_membership_id")
	version = 9007199254740991
	_, err = employmentFindingGovernanceProof(p, hospital, staff, basis, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
}
