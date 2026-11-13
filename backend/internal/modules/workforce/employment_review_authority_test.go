package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestEmploymentReviewGovernanceAuthorityBindsPurposeAndTarget(t *testing.T) {
	hospital, staff, review := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-employment-reviewer", AuthTime: time.Now().UTC().Add(-time.Minute), IdleDeadline: time.Now().UTC().Add(time.Hour)}
	scopeID := hospital
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 1, OwnerCount: 2, TemplateKey: "workforce.employment_reviewer.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &scopeID}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	authority, err := newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, PermEmploymentReview, source)
	require.NoError(t, err)
	require.True(t, authority.valid())
	require.Nil(t, authority.Staff)
	require.False(t, authority.isStaff(staff))
	proof, err := authority.governanceProof(p)
	require.NoError(t, err)
	require.Equal(t, "employment_review", proof["purpose"])
	require.Equal(t, staff, proof["staff_id"])
	require.Equal(t, review, proof["review_id"])
	require.Equal(t, source.GrantID, proof["source_grant_id"])
	for _, field := range []string{"actor_staff_id", "actor_link_id", "assignment_id"} {
		require.NotContains(t, proof, field)
	}
	for _, test := range []struct {
		name   string
		change func(*authz.GovernanceSourceEvidence)
	}{
		{"wrong capability", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.employment_preparer.v1" }},
		{"person review", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.person_reviewer.v1" }},
		{"wrong tenant", func(v *authz.GovernanceSourceEvidence) { v.Scope.TenantID = uuid.NewString() }},
		{"wrong hospital", func(v *authz.GovernanceSourceEvidence) { id := uuid.Must(uuid.NewV7()); v.Scope.ID = &id }},
		{"missing scope", func(v *authz.GovernanceSourceEvidence) { v.Scope.ID = nil }},
		{"independent pharmacy", func(v *authz.GovernanceSourceEvidence) { v.Scope.Kind = "pharmacy" }},
		{"missing membership", func(v *authz.GovernanceSourceEvidence) { v.MembershipEpoch = 0 }},
		{"no owners", func(v *authz.GovernanceSourceEvidence) { v.OwnerCount = 0 }},
		{"expired", func(v *authz.GovernanceSourceEvidence) { v.ExpiresAt = time.Now().Add(-time.Minute) }},
		{"missing grant", func(v *authz.GovernanceSourceEvidence) { v.GrantID = uuid.Nil }},
		{"unversioned grant", func(v *authz.GovernanceSourceEvidence) { v.Version = 0 }},
		{"mixed provenance", func(v *authz.GovernanceSourceEvidence) { v.OwnerMembershipID = uuid.New() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := source
			test.change(&changed)
			_, err := newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, PermEmploymentReview, changed)
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		})
	}
	_, err = newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, PermEmploymentActivate, source)
	require.Error(t, err, "activation is a different guarded command and writer")
	_, err = newEmploymentReviewGovernanceAuthority(p, hospital, uuid.Nil, review, PermEmploymentReview, source)
	require.Error(t, err)
	_, err = newEmploymentReviewGovernanceAuthority(p, hospital, staff, uuid.New(), PermEmploymentReview, source)
	require.Error(t, err)
	scopeID = uuid.Must(uuid.NewV7())
	require.Equal(t, hospital, *authority.Governance.Scope.ID, "a retained snapshot must own its scope pointer")
	mixed := *authority
	mixed.Staff = &intakeAuthority{}
	require.False(t, mixed.valid())
	_, err = mixed.governanceProof(p)
	require.Error(t, err)
	p.Subject = "another-account"
	_, err = authority.governanceProof(p)
	require.Error(t, err)
}

func TestEmploymentReviewAuthoritySnapshotRejectsSourceOrTargetChanges(t *testing.T) {
	hospital, staff, review := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-employment-owner"}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 1, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	initial, err := newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, PermEmploymentPrepare, source)
	require.NoError(t, err)
	current, err := newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, PermEmploymentPrepare, source)
	require.NoError(t, err)
	require.True(t, initial.same(current))
	for _, test := range []struct {
		name   string
		change func(*employmentReviewAuthority)
	}{
		{"target", func(v *employmentReviewAuthority) { v.Target = uuid.Must(uuid.NewV7()) }},
		{"review", func(v *employmentReviewAuthority) { v.Review = uuid.Must(uuid.NewV7()) }},
		{"subject", func(v *employmentReviewAuthority) { v.Subject = "another-account" }},
		{"capability", func(v *employmentReviewAuthority) { v.Capability = authz.GovernanceEmploymentReview }},
		{"owner census", func(v *employmentReviewAuthority) { v.Governance.OwnerCount++ }},
		{"owner membership", func(v *employmentReviewAuthority) { v.Governance.OwnerMembershipID = uuid.New() }},
		{"membership incarnation", func(v *employmentReviewAuthority) { v.Governance.MembershipEpoch++ }},
		{"expiry", func(v *employmentReviewAuthority) { v.Governance.ExpiresAt = v.Governance.ExpiresAt.Add(time.Second) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, PermEmploymentPrepare, source)
			require.NoError(t, err)
			test.change(value)
			require.False(t, initial.same(value))
		})
	}
	require.False(t, initial.same(nil))
	require.False(t, (*employmentReviewAuthority)(nil).same(nil))
}

func TestEmploymentReviewScopedAuthorityRetainsExactAssignments(t *testing.T) {
	tenant, subject, hospital := uuid.NewString(), "synthetic-scoped-reviewer", uuid.Must(uuid.NewV7())
	grant := authz.ScopeGrant{AssignmentID: uuid.Must(uuid.NewV7()).String(), Version: 1, ScopeEpoch: 1, MembershipEpoch: 1}
	actor := intakeActorReference{Staff: uuid.Must(uuid.NewV7()), Link: uuid.Must(uuid.NewV7()), LinkVersion: 1, EmploymentEpoch: 1}
	scoped := intakeAuthority{Tenant: tenant, Subject: subject, Hospital: hospital, Scoped: true, Actor: actor, Grants: map[authz.Permission]authz.ScopeGrant{PermEmploymentReview: grant}}
	authority := employmentReviewAuthority{Tenant: tenant, Subject: subject, Hospital: hospital, Target: uuid.Must(uuid.NewV7()), Review: uuid.Must(uuid.NewV7()), Capability: authz.GovernanceEmploymentReview, Staff: &scoped}
	require.True(t, authority.valid())
	require.True(t, authority.isStaff(actor.Staff))
	require.False(t, authority.isStaff(authority.Target))
	delete(scoped.Grants, PermEmploymentReview)
	require.False(t, authority.valid(), "a Staff actor without the exact current review assignment cannot confer authority")
}
