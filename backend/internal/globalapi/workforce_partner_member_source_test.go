package globalapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

type partnerMemberUseProbe func(context.Context, string, string, map[string]any, func(context.Context, authz.PartnerMemberReference) error) error

func newPartnerMemberUseProbe(authority authz.ScopedAuthorizer, source authz.CurrentPartnerMemberGuard, tenant string) partnerMemberUseProbe {
	return func(ctx context.Context, actor, member string, posting map[string]any, action func(context.Context, authz.PartnerMemberReference) error) error {
		scope := authz.PartnerMemberScope{MemberSubject: member, Posting: authz.StaffPostingSourceScope{TenantID: tenant, Subject: actor, HospitalID: uuid.MustParse(posting["hospital_id"].(string)), StaffID: uuid.MustParse(posting["staff_id"].(string)), PostingID: uuid.MustParse(posting["id"].(string)), RevisionID: uuid.MustParse(posting["revision_id"].(string)), Version: int64(posting["version"].(float64)), DepartmentID: uuid.MustParse(posting["department_id"].(string)), DepartmentRevisionID: uuid.MustParse(posting["department_revision_id"].(string))}}
		checks := []authz.ScopeCheck{}
		for _, permission := range []authz.Permission{authz.PartnerMemberSourceUse, authz.StaffPostingSourceUse, authz.StaffActorUse} {
			request := authz.ScopeRequest{TenantID: tenant, Subject: actor, HospitalID: scope.Posting.HospitalID.String(), Permission: permission}
			grant, err := authority.AuthorizeScope(ctx, request)
			if err != nil {
				return err
			}
			checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
		}
		return authority.(authz.CompoundScopedCommandGuard).WithScopeGrants(ctx, checks, time.Now().UTC(), func(command context.Context) error { return source.WithCurrentPartnerMember(command, scope, action) })
	}
}
func assertCurrentPartnerMemberSource(t *testing.T, actor, member string, posting map[string]any, use partnerMemberUseProbe) {
	t.Helper()
	called := false
	require.NoError(t, use(t.Context(), actor, member, posting, func(_ context.Context, ref authz.PartnerMemberReference) error {
		called = true
		require.Equal(t, member, ref.Subject)
		require.Equal(t, posting["staff_id"], ref.Member.StaffID.String())
		require.Equal(t, ref.Source.GenerationID, ref.Member.GenerationID)
		require.NotEqual(t, uuid.Nil, ref.UseID)
		require.NotEqual(t, uuid.Nil, ref.Member.LinkID)
		require.Greater(t, ref.Member.LinkVersion, int64(0))
		require.NotEmpty(t, ref.DisplayName)
		require.NotEqual(t, ref.Member.StaffID, ref.Source.Actor.StaffID, "current peer eligibility does not impersonate the requesting actor")
		return nil
	}))
	require.True(t, called)
	called = false
	err := use(t.Context(), actor, actor, posting, func(context.Context, authz.PartnerMemberReference) error { called = true; return nil })
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	require.False(t, called, "a subject cannot be paired with another person's canonical Staff posting")
}
