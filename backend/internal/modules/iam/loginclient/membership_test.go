package loginclient

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestScopedIdentityInactiveAndForeignMembershipAreNotConflated(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected native user read") })
	client.SetTenantIdentityResolver(identityResolverFunc(func(_ context.Context, _, _ string, purpose IdentityReadPurpose) (IdentitySnapshot, error) {
		if purpose == IdentityReadStatus {
			return IdentitySnapshot{State: UserStateActive}, nil
		}
		return IdentitySnapshot{State: UserStateActive, MembershipEligible: false}, nil
	}))
	allowed, err := client.MembershipEligible(t.Context(), identityProjectionTenant, "legacy-user")
	require.NoError(t, err)
	require.False(t, allowed)
	state, err := client.UserState(t.Context(), identityProjectionTenant, "legacy-user")
	require.NoError(t, err)
	require.Equal(t, UserStateActive, state)
	client.SetTenantIdentityResolver(identityResolverFunc(func(context.Context, string, string, IdentityReadPurpose) (IdentitySnapshot, error) {
		return IdentitySnapshot{State: UserStateInactive}, nil
	}))
	state, err = client.UserState(t.Context(), identityProjectionTenant, "removed-user")
	require.NoError(t, err)
	require.Equal(t, UserStateInactive, state)
	_, err = client.UserProfile(t.Context(), identityProjectionTenant, "removed-user")
	require.ErrorIs(t, err, ErrUnavailable)
}
