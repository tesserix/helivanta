package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/authz"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func newClient(t *testing.T) *authz.Client {
	t.Helper()
	c, err := authz.NewClient(context.Background(), testinfra.StartOpenFGA(t), "hms-test")
	require.NoError(t, err)
	return c
}

func TestResolveReturnsPermissionsGrantedViaRole(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "pharmacy.dispense.fulfil", authz.RolePharmacist))
	require.NoError(t, c.GrantRole(ctx, tenantA, "alice", authz.RolePharmacist))

	set, err := c.Resolve(ctx, "alice", tenantA)
	require.NoError(t, err)
	require.True(t, set.Has("pharmacy.dispense.fulfil"))
}

func TestResolveIsTenantScoped(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "pharmacy.dispense.fulfil", authz.RolePharmacist))
	require.NoError(t, c.GrantRole(ctx, tenantA, "alice", authz.RolePharmacist))

	// Same user, different tenant: no membership, so no permissions.
	set, err := c.Resolve(ctx, "alice", tenantB)
	require.NoError(t, err)
	require.Empty(t, set.Sorted(), "permissions must not leak across tenants")
}

func TestResolveForNonMemberIsEmptyNotError(t *testing.T) {
	set, err := newClient(t).Resolve(context.Background(), "nobody", tenantA)
	require.NoError(t, err)
	require.Empty(t, set.Sorted())
}

func TestRevokeRoleRemovesPermissions(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "lab.order.fulfil", authz.RoleLabTech))
	require.NoError(t, c.GrantRole(ctx, tenantA, "bob", authz.RoleLabTech))
	require.NoError(t, c.RevokeRole(ctx, tenantA, "bob", authz.RoleLabTech))

	set, err := c.Resolve(ctx, "bob", tenantA)
	require.NoError(t, err)
	require.False(t, set.Has("lab.order.fulfil"))
}

func TestWritesAreIdempotent(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "medicore.visit.read", authz.RoleDoctor))
	require.NoError(t, c.GrantPermission(ctx, tenantA, "medicore.visit.read", authz.RoleDoctor))
	require.NoError(t, c.GrantRole(ctx, tenantA, "carol", authz.RoleDoctor))
	require.NoError(t, c.GrantRole(ctx, tenantA, "carol", authz.RoleDoctor))

	// Revoking twice must also be a no-op, since consumers retry.
	require.NoError(t, c.RevokeRole(ctx, tenantA, "carol", authz.RoleDoctor))
	require.NoError(t, c.RevokeRole(ctx, tenantA, "carol", authz.RoleDoctor))
}

func TestResolveFailsWhenStoreUnreachable(t *testing.T) {
	c, err := authz.NewClient(context.Background(), "http://127.0.0.1:1", "hms-test")
	if err == nil {
		_, err = c.Resolve(context.Background(), "alice", tenantA)
	}
	require.Error(t, err, "an unreachable store must surface an error so the middleware can fail closed")
}
