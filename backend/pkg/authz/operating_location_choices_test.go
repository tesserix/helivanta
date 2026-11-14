package authz

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"testing"
	"time"
)

func TestOperatingLocationChoiceProofRequiresCurrentExactSource(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	employer := uuid.Must(uuid.NewV7())
	request := GovernedIndependentEmployerRequest{Principal: authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-preparer", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}, Capability: GovernanceOperatingManage}
	request.Scope = IndependentEmployerScope{TenantID: request.Principal.TenantID, EmployerKind: "pharmacy", EmployerID: employer}
	request.Source = GovernanceSourceEvidence{Kind: GovernanceGrantSource, Scope: GovernanceEstablishmentScope{TenantID: request.Principal.TenantID, Kind: "pharmacy", ID: &employer}, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 1, OwnerCount: 2, TemplateKey: "iam.operating_access_reviewer.v1", ExpiresAt: now.Add(time.Hour)}
	proof, err := OperatingLocationChoiceProof(request, now)
	require.NoError(t, err)
	require.Equal(t, "operating_location_choices", proof["purpose"])
	require.Equal(t, employer, proof["employer_id"])
	for name, alter := range map[string]func(*GovernedIndependentEmployerRequest){
		"foreign tenant":   func(r *GovernedIndependentEmployerRequest) { r.Scope.TenantID = uuid.NewString() },
		"foreign employer": func(r *GovernedIndependentEmployerRequest) { r.Scope.EmployerID = uuid.Must(uuid.NewV7()) },
		"hospital":         func(r *GovernedIndependentEmployerRequest) { r.Scope.EmployerKind = "hospital" },
		"wrong capability": func(r *GovernedIndependentEmployerRequest) { r.Capability = GovernanceOperatingReview },
		"wrong template": func(r *GovernedIndependentEmployerRequest) {
			r.Source.TemplateKey = "workforce.account_link_reviewer.v1"
		},
		"expired source":     func(r *GovernedIndependentEmployerRequest) { r.Source.ExpiresAt = now },
		"expired session":    func(r *GovernedIndependentEmployerRequest) { r.Principal.IdleDeadline = now },
		"future credential":  func(r *GovernedIndependentEmployerRequest) { r.Principal.AuthTime = now.Add(time.Minute) },
		"no membership":      func(r *GovernedIndependentEmployerRequest) { r.Source.MembershipEpoch = 0 },
		"unicode whitespace": func(r *GovernedIndependentEmployerRequest) { r.Principal.Subject = "synthetic\u00a0preparer" },
		"control subject":    func(r *GovernedIndependentEmployerRequest) { r.Principal.Subject = "synthetic\x01preparer" },
		"unknown source":     func(r *GovernedIndependentEmployerRequest) { r.Source.Kind = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			alter(&changed)
			proof, err := OperatingLocationChoiceProof(changed, now)
			require.ErrorIs(t, err, ErrScopedAuthorizationUnavailable)
			require.Nil(t, proof)
		})
	}
	owner := request
	owner.Source.Kind = GovernanceOwnerSource
	owner.Source.OwnerMembershipID = uuid.New()
	owner.Source.GrantID = uuid.Nil
	owner.Source.Version = 0
	owner.Source.TemplateKey = ""
	proof, err = OperatingLocationChoiceProof(owner, now)
	require.NoError(t, err)
	require.Equal(t, owner.Source.OwnerMembershipID, proof["owner_membership_id"])
	require.NotContains(t, proof, "source_grant_id")
}
