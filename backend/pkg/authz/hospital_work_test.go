package authz_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestHospitalWorkPurposesAreClosed(t *testing.T) {
	require.Equal(t, authz.Permission("pharmacy.stocktake.read"), authz.StocktakeWork.ReadPermission())
	require.Equal(t, authz.StocktakeActorUse, authz.StocktakeWork.ActorPermission())
	require.Equal(t, authz.Permission("pharmacy.indent.read"), authz.WardIndentWork.ReadPermission())
	require.Equal(t, authz.WardIndentActorUse, authz.WardIndentWork.ActorPermission())
	require.Equal(t, authz.Permission("workforce.ward_indent_actor.use"), authz.WardIndentActorUse)
	for _, unknown := range []authz.HospitalWorkPurpose{"", "governance", "Stocktake"} {
		require.False(t, unknown.Known())
		require.Empty(t, unknown.ReadPermission())
		require.Empty(t, unknown.ActorPermission())
		_, err := unknown.ReadScopeChecks(t.Context(), authz.StaffActorScope{})
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	}
}

func TestHospitalWorkNameReadsRequireExactHeldDepartmentProofs(t *testing.T) {
	scope := authz.StaffActorScope{TenantID: "0199e000-0000-7000-8000-000000000001", Subject: "TEST-hospital-reader", HospitalID: uuid.MustParse("0199e000-0000-4000-8000-000000000002"), DepartmentPath: []string{"0199e000-0000-7000-8000-000000000003", "0199e000-0000-7000-8000-000000000004"}}
	grant := authz.ScopeGrant{AssignmentID: "0199e000-0000-7000-8000-000000000005", Version: 2, ScopeEpoch: 3, MembershipEpoch: 4, ScopeKind: "department", DepartmentID: scope.DepartmentPath[0]}
	for _, work := range []struct {
		purpose authz.HospitalWorkPurpose
		other   authz.HospitalWorkPurpose
		write   authz.Permission
	}{
		{authz.StocktakeWork, authz.WardIndentWork, "pharmacy.stocktake.count"},
		{authz.WardIndentWork, authz.StocktakeWork, "pharmacy.indent.raise"},
	} {
		base := []authz.ScopeCheck{}
		for _, permission := range []authz.Permission{work.purpose.ReadPermission(), work.purpose.ActorPermission()} {
			base = append(base, authz.ScopeCheck{Request: authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), DepartmentPath: scope.DepartmentPath, Permission: permission}, Grant: grant})
		}
		_, err := work.purpose.ReadScopeChecks(t.Context(), scope)
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		for _, scenario := range []struct {
			name    string
			change  func([]authz.ScopeCheck) []authz.ScopeCheck
			allowed bool
		}{
			{"exact department read and source", func(v []authz.ScopeCheck) []authz.ScopeCheck { return v }, true},
			{"missing read", func(v []authz.ScopeCheck) []authz.ScopeCheck { return v[1:] }, false},
			{"missing source", func(v []authz.ScopeCheck) []authz.ScopeCheck { return v[:1] }, false},
			{"write cannot substitute for read", func(v []authz.ScopeCheck) []authz.ScopeCheck {
				v[0].Request.Permission = work.write
				return v
			}, false},
			{"another purpose read cannot substitute", func(v []authz.ScopeCheck) []authz.ScopeCheck {
				v[0].Request.Permission = work.other.ReadPermission()
				return v
			}, false},
			{"another purpose source cannot substitute", func(v []authz.ScopeCheck) []authz.ScopeCheck {
				v[1].Request.Permission = work.other.ActorPermission()
				return v
			}, false},
			{"another subject source", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Request.Subject = "TEST-other-reader"; return v }, false},
			{"hospital scope cannot substitute", func(v []authz.ScopeCheck) []authz.ScopeCheck {
				v[0].Grant.ScopeKind = "hospital"
				v[0].Grant.DepartmentID = ""
				return v
			}, false},
			{"missing authority epoch", func(v []authz.ScopeCheck) []authz.ScopeCheck { v[1].Grant.ScopeEpoch = 0; return v }, false},
			{"source outside candidate ancestry", func(v []authz.ScopeCheck) []authz.ScopeCheck {
				v[1].Grant.DepartmentID = "0199e000-0000-7000-8000-000000000006"
				return v
			}, false},
		} {
			t.Run(string(work.purpose)+"/"+scenario.name, func(t *testing.T) {
				checks := scenario.change(append([]authz.ScopeCheck(nil), base...))
				var escaped context.Context
				require.NoError(t, authz.WithScopedCommandEvidence(t.Context(), checks, func(ctx context.Context) error {
					escaped = ctx
					received, err := work.purpose.ReadScopeChecks(ctx, scope)
					if scenario.allowed {
						require.NoError(t, err)
						require.Equal(t, checks, received)
					} else {
						require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
						require.Empty(t, received)
					}
					return nil
				}))
				_, err := work.purpose.ReadScopeChecks(escaped, scope)
				require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, "checked references cannot outlive the owner callback")
			})
		}
	}
}
