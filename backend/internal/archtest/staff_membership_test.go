package archtest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestStaffMembershipGrantsNoBusinessPermissions(t *testing.T) {
	ctx := t.Context()
	role := authz.Role("staff_member")
	require.True(t, authz.KnownRole(role), "staff need membership without a clinical or administrative role")
	client, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, platform.ReconcileTenant(ctx, registry(t), client, tenantA))
	require.NoError(t, client.GrantTenantRole(ctx, tenantA, role))
	require.NoError(t, client.GrantRole(ctx, tenantA, "synthetic-staff", role))
	member, err := client.IsMember(ctx, "synthetic-staff", tenantA)
	require.NoError(t, err)
	require.True(t, member)
	permissions, err := client.Resolve(ctx, "synthetic-staff", tenantA)
	require.NoError(t, err)
	require.Empty(t, permissions, "membership must not grant any registered business permission")
	member, err = client.IsMember(ctx, "synthetic-staff", tenantB)
	require.NoError(t, err)
	require.False(t, member)
	require.NoError(t, client.RevokeRole(ctx, tenantA, "synthetic-staff", role))
	member, err = client.IsMember(ctx, "synthetic-staff", tenantA)
	require.NoError(t, err)
	require.False(t, member)
}
