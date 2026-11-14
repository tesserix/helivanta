package workforce

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type activationBasisProbe struct{ calls int }

func (p *activationBasisProbe) ConsumeEmploymentBasis(context.Context, authz.EmploymentBasis) error {
	p.calls++
	return nil
}

func TestGovernanceActivationAuthorityOwnsItsExactIntent(t *testing.T) {
	hospital, staff, review := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-activator", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	scope := hospital
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 1, OwnerCount: 2, TemplateKey: "workforce.employment_activator.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &scope}, ExpiresAt: time.Now().Add(time.Hour)}
	intent := employmentActivationIntent{Generation: uuid.Must(uuid.NewV7()), Command: uuid.Must(uuid.NewV7()), StaffVersion: 1, Epoch: 1}
	a, err := newGovernanceActivationAuthority(p, hospital, staff, review, source, &intent)
	require.NoError(t, err)
	require.True(t, a.valid())
	require.Nil(t, a.Staff)
	require.False(t, a.isStaff(staff))
	proof, err := a.proof(p)
	require.NoError(t, err)
	require.Equal(t, "employment_activation", proof["purpose"])
	require.Equal(t, "activate", proof["operation"])
	require.Equal(t, intent.Generation, proof["generation_id"])
	require.Equal(t, source.GrantID, proof["source_grant_id"])
	require.NotContains(t, proof, "actor_staff_id")
	require.NotContains(t, proof, "actor_link_id")
	scope, intent.Generation = uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	require.Equal(t, hospital, *a.Governance.Scope.ID)
	require.NotEqual(t, intent.Generation, a.Intent.Generation)
	_, err = a.proof(authn.Principal{TenantID: p.TenantID, Subject: "another-account"})
	require.Error(t, err)
	mixed := *a
	mixed.Staff = &intakeAuthority{}
	require.False(t, mixed.valid())
	source.Scope.ID = &hospital
	source.TemplateKey = "workforce.employment_reviewer.v1"
	_, err = newGovernanceActivationAuthority(p, hospital, staff, review, source, nil)
	require.Error(t, err, "review authority is not activation authority")
}

func TestGovernanceActivationBasisRequiresTheBoundWriterAndTarget(t *testing.T) {
	hospital, staff, review := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-activator"}
	source := authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), MembershipEpoch: 1, OwnerCount: 1, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}, ExpiresAt: time.Now().Add(time.Hour)}
	intent := employmentActivationIntent{Generation: uuid.Must(uuid.NewV7()), Command: uuid.Must(uuid.NewV7()), StaffVersion: 1, Epoch: 1}
	a, err := newGovernanceActivationAuthority(p, hospital, staff, review, source, &intent)
	require.NoError(t, err)
	basis := authz.EmploymentBasis{TenantID: p.TenantID, HospitalID: hospital, StaffID: staff, Subject: "synthetic-ordinary", GenerationID: intent.Generation, EmploymentEpoch: 2, Until: time.Now().Add(time.Hour)}
	require.Error(t, a.consume(basis))
	probe := &activationBasisProbe{}
	a.writer, a.command = probe, context.Background()
	for _, test := range []struct {
		name   string
		change func(*authz.EmploymentBasis)
	}{
		{"tenant", func(v *authz.EmploymentBasis) { v.TenantID = uuid.NewString() }},
		{"hospital", func(v *authz.EmploymentBasis) { v.HospitalID = uuid.Must(uuid.NewV7()) }},
		{"staff", func(v *authz.EmploymentBasis) { v.StaffID = uuid.Must(uuid.NewV7()) }},
		{"generation", func(v *authz.EmploymentBasis) { v.GenerationID = uuid.Must(uuid.NewV7()) }},
		{"epoch", func(v *authz.EmploymentBasis) { v.EmploymentEpoch++ }},
		{"self", func(v *authz.EmploymentBasis) { v.Subject = p.Subject }},
	} {
		t.Run(test.name, func(t *testing.T) { changed := basis; test.change(&changed); require.Error(t, a.consume(changed)) })
	}
	require.Zero(t, probe.calls)
	require.NoError(t, a.consume(basis))
	require.Equal(t, 1, probe.calls)
	a.writer, a.command = nil, nil
	require.Error(t, a.consume(basis))
	read, err := newGovernanceActivationAuthority(p, hospital, staff, review, source, nil)
	require.NoError(t, err)
	proof, err := read.proof(p)
	require.NoError(t, err)
	require.Equal(t, "source", proof["operation"])
	require.NotContains(t, proof, "generation_id")
	require.Error(t, read.consume(basis))
}
