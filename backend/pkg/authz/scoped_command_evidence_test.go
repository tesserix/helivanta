package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestScopedCommandEvidenceCannotEscapeTheIAMCallback(t *testing.T) {
	request, grant := commandEvidenceFixture()
	checks := []authz.ScopeCheck{{Request: request, Grant: grant}}
	var escaped context.Context
	err := authz.WithScopedCommandEvidence(t.Context(), checks, func(ctx context.Context) error {
		got, ok := authz.ScopedCommandGrant(ctx, request)
		require.True(t, ok)
		require.Equal(t, grant, got)
		escaped = context.WithoutCancel(ctx)
		return nil
	})
	require.NoError(t, err)
	got, ok := authz.ScopedCommandGrant(escaped, request)
	require.False(t, ok, "removing cancellation cannot revive evidence after the owner callback")
	require.Empty(t, got)
}

func TestScopedCommandEvidenceMatchesOnlyItsImmutableCheckedScope(t *testing.T) {
	request, grant := commandEvidenceFixture()
	checks := []authz.ScopeCheck{{Request: request, Grant: grant}}
	require.NoError(t, authz.WithScopedCommandEvidence(t.Context(), checks, func(ctx context.Context) error {
		checks[0].Request.Subject = "changed-after-issue"
		checks[0].Grant.Version++
		got, ok := authz.ScopedCommandGrant(ctx, request)
		require.True(t, ok)
		require.Equal(t, grant, got)
		for _, change := range []func(*authz.ScopeRequest){
			func(r *authz.ScopeRequest) { r.TenantID = "00000000-0000-4000-8000-000000000004" },
			func(r *authz.ScopeRequest) { r.HospitalID = "00000000-0000-4000-8000-000000000005" },
			func(r *authz.ScopeRequest) { r.Subject = checks[0].Request.Subject },
			func(r *authz.ScopeRequest) { r.Permission = "workforce.enrollment.manage" },
		} {
			other := request
			change(&other)
			got, ok := authz.ScopedCommandGrant(ctx, other)
			require.False(t, ok, "a different tenant, hospital, subject or action cannot borrow the checked grant")
			require.Empty(t, got)
		}
		return nil
	}))
}

func TestScopedCommandEvidenceCannotStripRootCancellation(t *testing.T) {
	request, grant := commandEvidenceFixture()
	root, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, authz.WithScopedCommandEvidence(root, []authz.ScopeCheck{{Request: request, Grant: grant}}, func(ctx context.Context) error {
		detached := context.WithoutCancel(ctx)
		cancel()
		got, ok := authz.ScopedCommandGrant(detached, request)
		require.False(t, ok)
		require.Empty(t, got)
		return nil
	}))
}

func commandEvidenceFixture() (authz.ScopeRequest, authz.ScopeGrant) {
	return authz.ScopeRequest{TenantID: "00000000-0000-4000-8000-000000000001", Subject: "synthetic-current-staff", HospitalID: "00000000-0000-4000-8000-000000000002", Permission: "workforce.identity_checks.manage"},
		authz.ScopeGrant{AssignmentID: "00000000-0000-4000-8000-000000000003", Version: 2, ScopeEpoch: 3, MembershipEpoch: 4}
}
