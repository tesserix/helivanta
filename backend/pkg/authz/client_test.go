package authz_test

import (
	"context"
	"testing"

	fgaclient "github.com/openfga/go-sdk/client"
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

// TestConcurrentBootConvergesOnSameStore covers the split-brain finding
// from review: OpenFGA does not enforce store-name uniqueness, so two
// replicas racing to boot for the first time can each create a
// same-named store. Here we simulate that race directly by creating a
// duplicate store before either authz.Client boots, then assert both
// independently constructed clients resolve to the same store (a grant
// written through one is visible to Resolve through the other) rather
// than silently splitting into two stores that each look like "no
// permissions" from the other's point of view.
func TestConcurrentBootConvergesOnSameStore(t *testing.T) {
	ctx := context.Background()
	url := testinfra.StartOpenFGA(t)
	const storeName = "hms-test"

	// Simulate the race window: two stores already exist under the same
	// name before any authz.Client tries to reconcile it.
	raw, err := fgaclient.NewSdkClient(&fgaclient.ClientConfiguration{ApiUrl: url})
	require.NoError(t, err)
	_, err = raw.CreateStore(ctx).Body(fgaclient.ClientCreateStoreRequest{Name: storeName}).Execute()
	require.NoError(t, err)
	_, err = raw.CreateStore(ctx).Body(fgaclient.ClientCreateStoreRequest{Name: storeName}).Execute()
	require.NoError(t, err)

	// Two independently booted replicas must converge on the same store.
	c1, err := authz.NewClient(ctx, url, storeName)
	require.NoError(t, err)
	c2, err := authz.NewClient(ctx, url, storeName)
	require.NoError(t, err)

	// A grant written through one replica must be visible through the
	// other. If they had landed on different stores, this would be
	// indistinguishable from "user has no permissions."
	require.NoError(t, c1.GrantPermission(ctx, tenantA, "clinic.visit.read", authz.RoleDoctor))
	require.NoError(t, c1.GrantRole(ctx, tenantA, "dana", authz.RoleDoctor))

	set, err := c2.Resolve(ctx, "dana", tenantA)
	require.NoError(t, err)
	require.True(t, set.Has("clinic.visit.read"), "both replicas must resolve to the same store")
}
