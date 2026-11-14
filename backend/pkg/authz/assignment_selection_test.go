package authz_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceAssignmentSelectionRequiresExactCurrentHospitalSource(t *testing.T) {
	now := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	hospital := uuid.MustParse("019a55c0-1111-7111-8111-111111111111")
	tenant := "11111111-1111-4111-8111-111111111111"
	base := authz.GovernedAssignmentSelectionRequest{
		Principal:  authn.Principal{TenantID: tenant, Subject: "TEST-hospital-owner", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Minute)},
		HospitalID: hospital, Capability: authz.GovernanceAssignmentManage,
		Source: authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.MustParse("019a55c0-1111-7111-8111-111111111112"), OwnerCount: 1, MembershipEpoch: 2, ExpiresAt: now.Add(5 * time.Second), Scope: authz.GovernanceEstablishmentScope{TenantID: tenant, Kind: "hospital", ID: &hospital}},
	}
	proofs := []struct {
		name, purpose string
		build         func(authz.GovernedAssignmentSelectionRequest, time.Time) (map[string]any, error)
	}{
		{"targets", "assignment_target_choices", authz.AssignmentTargetChoiceProof},
		{"departments", "assignment_department_choices", authz.AssignmentDepartmentChoiceProof},
	}
	for _, proof := range proofs {
		t.Run(proof.name, func(t *testing.T) {
			out, err := proof.build(base, now)
			require.NoError(t, err)
			require.Equal(t, proof.purpose, out["purpose"])
			require.Equal(t, hospital, out["hospital_id"])
			require.Equal(t, tenant, out["tenant_id"])
			require.Equal(t, base.Source.OwnerMembershipID, out["owner_membership_id"])
			require.NotContains(t, out, "source_grant_id")
			grant := base
			grant.Source.Kind = authz.GovernanceGrantSource
			grant.Source.OwnerMembershipID = uuid.Nil
			grant.Source.GrantID = uuid.MustParse("019a55c0-1111-7111-8111-111111111113")
			grant.Source.Version = 3
			grant.Source.TemplateKey = "iam.scoped_access_reviewer.v1"
			out, err = proof.build(grant, now)
			require.NoError(t, err)
			require.Equal(t, grant.Source.GrantID, out["source_grant_id"])
			require.Equal(t, int64(3), out["source_grant_version"])
			require.NotContains(t, out, "owner_membership_id")
			for name, change := range map[string]func(*authz.GovernedAssignmentSelectionRequest){
				"review capability": func(r *authz.GovernedAssignmentSelectionRequest) { r.Capability = authz.GovernanceAssignmentReview },
				"invalid tenant": func(r *authz.GovernedAssignmentSelectionRequest) {
					r.Principal.TenantID = "not-a-tenant"
					r.Source.Scope.TenantID = r.Principal.TenantID
				},
				"zero tenant": func(r *authz.GovernedAssignmentSelectionRequest) {
					r.Principal.TenantID = uuid.Nil.String()
					r.Source.Scope.TenantID = r.Principal.TenantID
				},
				"missing owner identity":    func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.OwnerMembershipID = uuid.Nil },
				"unexpected owner revision": func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.Version = 1 },
				"empty actor":               func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.Subject = "" },
				"oversized actor":           func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.Subject = strings.Repeat("a", 201) },
				"malformed actor":           func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.Subject = string([]byte{0xff}) },
				"ambiguous actor":           func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.Subject = "TEST:owner" },

				"wrong capability": func(r *authz.GovernedAssignmentSelectionRequest) { r.Capability = authz.GovernanceOperatingManage },
				"other tenant": func(r *authz.GovernedAssignmentSelectionRequest) {
					r.Source.Scope.TenantID = "22222222-2222-4222-8222-222222222222"
				},
				"other kind": func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.Scope.Kind = "pharmacy" },
				"other hospital": func(r *authz.GovernedAssignmentSelectionRequest) {
					id := uuid.Must(uuid.NewV7())
					r.Source.Scope.ID = &id
				},
				"no hospital":            func(r *authz.GovernedAssignmentSelectionRequest) { r.HospitalID = uuid.Nil },
				"no source scope":        func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.Scope.ID = nil },
				"expired source":         func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.ExpiresAt = now },
				"expired account":        func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.IdleDeadline = now },
				"future authentication":  func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.AuthTime = now.Add(time.Second) },
				"missing authentication": func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.AuthTime = time.Time{} },
				"missing membership":     func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.MembershipEpoch = 0 },
				"unsafe membership":      func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.MembershipEpoch = 9007199254740992 },
				"no owners":              func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.OwnerCount = 0 },
				"unbounded owners":       func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.OwnerCount = 1001 },
				"mixed source":           func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.GrantID = uuid.Must(uuid.NewV7()) },
				"owner template": func(r *authz.GovernedAssignmentSelectionRequest) {
					r.Source.TemplateKey = "iam.scoped_access_reviewer.v1"
				},
				"unknown source": func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.Kind = "legacy" },
				"invalid actor":  func(r *authz.GovernedAssignmentSelectionRequest) { r.Principal.Subject = "TEST other" },
			} {
				t.Run(name, func(t *testing.T) {
					r := base
					change(&r)
					out, err := proof.build(r, now)
					require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
					require.Nil(t, out)
				})
			}
			for name, change := range map[string]func(*authz.GovernedAssignmentSelectionRequest){
				"other template": func(r *authz.GovernedAssignmentSelectionRequest) {
					r.Source.TemplateKey = "iam.operating_access_reviewer.v1"
				},
				"missing grant revision": func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.Version = 0 },
				"unsafe grant revision":  func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.Version = 9007199254740992 },
				"legacy grant id":        func(r *authz.GovernedAssignmentSelectionRequest) { r.Source.GrantID = uuid.Must(uuid.NewRandom()) },
				"grant with owner": func(r *authz.GovernedAssignmentSelectionRequest) {
					r.Source.OwnerMembershipID = base.Source.OwnerMembershipID
				},
			} {
				t.Run(name, func(t *testing.T) {
					r := grant
					change(&r)
					out, err := proof.build(r, now)
					require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
					require.Nil(t, out)
				})
			}
		})
	}
}
