package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPersonReviewGovernanceAuthorityBindsTheNonclinicalPurpose(t *testing.T) {
	hospital := uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-owner"}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 3, OwnerCount: 2, TemplateKey: "workforce.person_preparer.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	authority, err := newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksManage, source)
	require.NoError(t, err)
	values, err := authority.auditValues()
	require.NoError(t, err)
	require.Equal(t, source.GrantID, values["source_grant_id"])
	require.NotContains(t, values, "actor_staff_id")
	require.NotContains(t, values, "actor_link_id")
	require.Nil(t, authority.Staff)
	require.False(t, authority.isStaff(hospital))
	require.True(t, authority.same(authority))
	for _, test := range []struct {
		name   string
		change func(*authz.GovernanceSourceEvidence)
	}{
		{"wrong purpose", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.person_reviewer.v1" }},
		{"link reviewer", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.account_link_reviewer.v1" }},
		{"wrong employer", func(v *authz.GovernanceSourceEvidence) { id := uuid.Must(uuid.NewV7()); v.Scope.ID = &id }},
		{"wrong tenant", func(v *authz.GovernanceSourceEvidence) { v.Scope.TenantID = uuid.NewString() }},
		{"independent pharmacy", func(v *authz.GovernanceSourceEvidence) { v.Scope.Kind = "pharmacy" }},
		{"missing grant", func(v *authz.GovernanceSourceEvidence) { v.GrantID = uuid.Nil }},
		{"missing membership", func(v *authz.GovernanceSourceEvidence) { v.MembershipEpoch = 0 }},
		{"mixed provenance", func(v *authz.GovernanceSourceEvidence) { v.OwnerMembershipID = uuid.New() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := source
			test.change(&changed)
			_, err := newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksManage, changed)
			require.Error(t, err)
		})
	}
	_, err = newPersonReviewGovernanceAuthority(p, hospital, PermEmploymentActivate, source)
	require.Error(t, err)
	source.TemplateKey = "workforce.person_reviewer.v1"
	_, err = newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksReview, source)
	require.NoError(t, err)
	source.Kind = authz.GovernanceOwnerSource
	source.OwnerMembershipID = uuid.New()
	source.GrantID = uuid.Nil
	source.Version = 0
	source.TemplateKey = ""
	owner, err := newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksReview, source)
	require.NoError(t, err)
	require.Nil(t, owner.Staff)
	require.False(t, owner.same(authority))
}

func TestPersonReviewAuthoritySnapshotRetainsTheExactSource(t *testing.T) {
	hospital := uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-reviewer"}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 3, OwnerCount: 2, TemplateKey: "workforce.person_reviewer.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	original, err := newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksReview, source)
	require.NoError(t, err)
	copied, err := newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksReview, source)
	require.NoError(t, err)
	require.True(t, original.same(copied), "pointer allocation is not a source change")
	for _, test := range []struct {
		name   string
		change func(*authz.GovernanceSourceEvidence)
	}{
		{"version", func(v *authz.GovernanceSourceEvidence) { v.Version++ }},
		{"incarnation", func(v *authz.GovernanceSourceEvidence) { v.MembershipEpoch++ }},
		{"source", func(v *authz.GovernanceSourceEvidence) { v.GrantID = uuid.Must(uuid.NewV7()) }},
		{"expiry", func(v *authz.GovernanceSourceEvidence) { v.ExpiresAt = v.ExpiresAt.Add(time.Second) }},
		{"census", func(v *authz.GovernanceSourceEvidence) { v.OwnerCount++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := source
			test.change(&changed)
			current, err := newPersonReviewGovernanceAuthority(p, hospital, PermIdentityChecksReview, changed)
			require.NoError(t, err)
			require.False(t, original.same(current))
		})
	}
	require.False(t, (personReviewAuthority{}).same(personReviewAuthority{}))
}
