package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestEmploymentDiscoveryProofRequiresItsCurrentEmployerAndPurpose(t *testing.T) {
	hospital := uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-discovery-reviewer", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 1, OwnerCount: 2, TemplateKey: "workforce.employment_reviewer.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().Add(time.Hour)}
	proof, err := employmentDiscoveryProof(p, hospital, PermEmploymentReview, source)
	require.NoError(t, err)
	require.Equal(t, "employment_discovery", proof["purpose"])
	require.Equal(t, hospital, proof["hospital_id"])
	require.Equal(t, source.GrantID, proof["source_grant_id"])
	for _, field := range []string{"staff_id", "review_id", "actor_staff_id", "actor_link_id"} {
		require.NotContains(t, proof, field)
	}
	for _, test := range []struct {
		name   string
		change func(*authz.GovernanceSourceEvidence)
	}{
		{"wrong employer", func(v *authz.GovernanceSourceEvidence) { id := uuid.New(); v.Scope.ID = &id }},
		{"wrong tenant", func(v *authz.GovernanceSourceEvidence) { v.Scope.TenantID = uuid.NewString() }},
		{"wrong kind", func(v *authz.GovernanceSourceEvidence) { v.Scope.Kind = "pharmacy" }},
		{"preparation grant", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.employment_preparer.v1" }},
		{"activation grant", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.employment_activator.v1" }},
		{"expired", func(v *authz.GovernanceSourceEvidence) { v.ExpiresAt = time.Now().Add(-time.Second) }},
		{"mixed owner source", func(v *authz.GovernanceSourceEvidence) { v.OwnerMembershipID = uuid.New() }},
		{"missing membership", func(v *authz.GovernanceSourceEvidence) { v.MembershipEpoch = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := source
			test.change(&value)
			_, err := employmentDiscoveryProof(p, hospital, PermEmploymentReview, value)
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		})
	}
	_, err = employmentDiscoveryProof(p, hospital, PermEmploymentActivate, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	p.AuthTime = time.Time{}
	_, err = employmentDiscoveryProof(p, hospital, PermEmploymentReview, source)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
}
