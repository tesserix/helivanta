package loginclient

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const identityProjectionTenant = "11111111-1111-4111-8111-111111111111"

type identityResolverFunc func(context.Context, string, string, IdentityReadPurpose) (IdentitySnapshot, error)

func (f identityResolverFunc) TenantIdentitySnapshot(ctx context.Context, tenant, subject string, purpose IdentityReadPurpose) (IdentitySnapshot, error) {
	return f(ctx, tenant, subject, purpose)
}

func TestIdentityReadBrokerKeepsNativeLoginTokenReadOnly(t *testing.T) {
	var nativeCalls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { nativeCalls.Add(1); w.WriteHeader(http.StatusForbidden) })
	client.SetTenantIdentityResolver(identityResolverFunc(func(_ context.Context, tenant, subject string, purpose IdentityReadPurpose) (IdentitySnapshot, error) {
		require.Equal(t, identityProjectionTenant, tenant)
		require.Equal(t, "fresh-user", subject)
		switch purpose {
		case IdentityReadStatus:
			return IdentitySnapshot{State: UserStateActive}, nil
		case IdentityReadMembership:
			return IdentitySnapshot{State: UserStateActive, MembershipEligible: true}, nil
		case IdentityReadProfile:
			return IdentitySnapshot{State: UserStateActive, Profile: &UserProfile{DisplayName: "Synthetic Staff", Email: "synthetic@example.test"}}, nil
		default:
			t.Fatal("unexpected identity purpose")
			return IdentitySnapshot{}, ErrUnavailable
		}
	}))
	eligible, err := client.MembershipEligible(t.Context(), identityProjectionTenant, "fresh-user")
	require.NoError(t, err)
	require.True(t, eligible)
	state, err := client.UserState(t.Context(), identityProjectionTenant, "fresh-user")
	require.NoError(t, err)
	require.Equal(t, UserStateActive, state)
	profile, err := client.UserProfile(t.Context(), identityProjectionTenant, "fresh-user")
	require.NoError(t, err)
	require.Equal(t, "Synthetic Staff", profile.DisplayName)
	require.Zero(t, nativeCalls.Load(), "login PAT must not perform user-management reads")
}

func TestIdentitySnapshotRejectsMissingCancelledAndAmbiguousAuthority(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected native user read") })
	_, err := client.UserState(t.Context(), identityProjectionTenant, "fresh-user")
	require.ErrorIs(t, err, ErrUnavailable)
	for _, tc := range []struct {
		name    string
		state   IdentitySnapshot
		failure error
	}{
		{name: "missing state"},
		{name: "broker outage", failure: errors.New("private-contact@example.test")},
		{name: "inactive eligible", state: IdentitySnapshot{State: UserStateInactive, MembershipEligible: true}},
		{name: "unsolicited profile", state: IdentitySnapshot{State: UserStateActive, Profile: &UserProfile{DisplayName: "Private", Email: "private@example.test"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client.SetTenantIdentityResolver(identityResolverFunc(func(context.Context, string, string, IdentityReadPurpose) (IdentitySnapshot, error) {
				return tc.state, tc.failure
			}))
			eligible, err := client.MembershipEligible(t.Context(), identityProjectionTenant, "fresh-user")
			require.False(t, eligible)
			require.ErrorIs(t, err, ErrUnavailable)
			require.NotContains(t, err.Error(), "private-contact")
		})
	}
	var calls atomic.Int32
	client.SetTenantIdentityResolver(identityResolverFunc(func(context.Context, string, string, IdentityReadPurpose) (IdentitySnapshot, error) {
		calls.Add(1)
		return IdentitySnapshot{State: UserStateActive}, nil
	}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.UserState(ctx, identityProjectionTenant, "fresh-user")
	require.ErrorIs(t, err, ErrUnavailable)
	require.Zero(t, calls.Load())
}
