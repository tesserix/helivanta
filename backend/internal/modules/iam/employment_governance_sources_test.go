package iam

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceEmploymentSourcesCannotAliasOtherCapabilitiesOrEmployers(t *testing.T) {
	for capability, key := range map[authz.GovernanceCapability]string{
		authz.GovernanceEmploymentPrepare:  "workforce.employment_preparer.v1",
		authz.GovernanceEmploymentReview:   "workforce.employment_reviewer.v1",
		authz.GovernanceEmploymentActivate: "workforce.employment_activator.v1",
	} {
		require.Equal(t, key, governanceSourceTemplate(capability, "hospital"))
		for _, scope := range []string{"tenant", "pharmacy", "supplier", "department", ""} {
			require.Empty(t, governanceSourceTemplate(capability, scope))
		}
	}
	require.Empty(t, governanceSourceTemplate(authz.GovernanceCapability("workforce.employment.*"), "hospital"))
}

func TestGovernanceEmploymentBasisWriterBindsItsContextLifetimeAndExactTarget(t *testing.T) {
	tenant := uuid.NewString()
	hospital, staff := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	w := &employmentBasisWriter{request: authz.ScopeRequest{TenantID: tenant, Subject: "synthetic-reviewer", HospitalID: hospital.String()}, target: staff}
	w.active.Store(true)
	ctx := context.WithValue(t.Context(), employmentBasisProofKey{}, w)
	valid := authz.EmploymentBasis{TenantID: tenant, Subject: "synthetic-target", HospitalID: hospital, StaffID: staff, GenerationID: uuid.Must(uuid.NewV7()), EmploymentEpoch: 2, Until: time.Now().Add(time.Hour)}
	require.True(t, w.accepts(ctx, valid), "the valid case must reach the boundary being exercised")
	for name, mutate := range map[string]func(*authz.EmploymentBasis){
		"foreign tenant":   func(b *authz.EmploymentBasis) { b.TenantID = uuid.NewString() },
		"sibling hospital": func(b *authz.EmploymentBasis) { b.HospitalID = uuid.Must(uuid.NewV7()) },
		"other staff":      func(b *authz.EmploymentBasis) { b.StaffID = uuid.Must(uuid.NewV7()) },
		"nil staff":        func(b *authz.EmploymentBasis) { b.StaffID = uuid.Nil },
		"self activation":  func(b *authz.EmploymentBasis) { b.Subject = w.request.Subject },
		"unsafe subject":   func(b *authz.EmploymentBasis) { b.Subject = "user:forged" },
		"old generation":   func(b *authz.EmploymentBasis) { b.GenerationID = uuid.New() },
		"missing epoch":    func(b *authz.EmploymentBasis) { b.EmploymentEpoch = 1 },
		"missing end":      func(b *authz.EmploymentBasis) { b.Until = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) { b := valid; mutate(&b); require.False(t, w.accepts(ctx, b)) })
	}
	require.False(t, w.accepts(context.Background(), valid))
	other := &employmentBasisWriter{}
	require.False(t, w.accepts(context.WithValue(t.Context(), employmentBasisProofKey{}, other), valid))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.False(t, w.accepts(cancelled, valid))
	w.active.Store(false)
	require.False(t, w.accepts(ctx, valid), "retained writers lose authority on callback exit")
}

func TestGovernanceEmploymentMembershipNeverSubstitutesForOrdinaryMembership(t *testing.T) {
	for name, test := range map[string]struct {
		roles []authz.Role
		want  bool
	}{
		"ordinary":                          {[]authz.Role{authz.RoleStaffMember}, true},
		"ordinary and nonclinical delegate": {[]authz.Role{authz.RoleStaffMember, authz.RoleGovernanceDelegate}, true},
		"delegate alone":                    {[]authz.Role{authz.RoleGovernanceDelegate}, false},
		"missing":                           {nil, false},
		"clinical":                          {[]authz.Role{authz.RoleStaffMember, authz.RoleDoctor}, false},
		"legacy owner":                      {[]authz.Role{authz.RoleStaffMember, authz.RoleTenantAdmin}, false},
		"delegate and clinical":             {[]authz.Role{authz.RoleStaffMember, authz.RoleGovernanceDelegate, authz.RolePharmacist}, false},
		"unknown":                           {[]authz.Role{authz.RoleStaffMember, "synthetic_unknown"}, false},
		"duplicate":                         {[]authz.Role{authz.RoleStaffMember, authz.RoleStaffMember}, false},
	} {
		t.Run(name, func(t *testing.T) { require.Equal(t, test.want, employmentMembershipRoles(test.roles)) })
	}
}
