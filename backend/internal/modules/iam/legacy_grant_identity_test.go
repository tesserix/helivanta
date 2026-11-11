package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type grantIdentityFunc func(context.Context, string, string) (bool, error)

func (f grantIdentityFunc) MembershipEligible(ctx context.Context, tenant, subject string) (bool, error) {
	return f(ctx, tenant, subject)
}

func TestNewLegacyGrantRequiresCurrentTenantOwnedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		eligible bool
		failure  error
		expected error
	}{
		{name: "own active verified identity", eligible: true},
		{name: "foreign inactive or unverified identity", expected: errGrantIdentityIneligible},
		{name: "identity dependency unavailable", failure: errors.New("synthetic dependency failure"), expected: errMembershipAuthority},
		{name: "cancelled proof", failure: context.Canceled, expected: errMembershipAuthority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			reader := grantIdentityFunc(func(ctx context.Context, tenant, subject string) (bool, error) {
				calls++
				require.Equal(t, "11111111-1111-4111-8111-111111111111", tenant)
				require.Equal(t, "fresh-staff", subject)
				return tc.eligible, tc.failure
			})
			err := requireNewGrantIdentity(t.Context(), reader, "11111111-1111-4111-8111-111111111111", "fresh-staff")
			require.ErrorIs(t, err, tc.expected)
			require.Equal(t, 1, calls)
		})
	}
	require.ErrorIs(t, requireNewGrantIdentity(t.Context(), nil, "11111111-1111-4111-8111-111111111111", "fresh-staff"), errMembershipAuthority)
}
