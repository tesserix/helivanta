package authz_test

import (
	"context"
	"testing"

	fgaclient "github.com/openfga/go-sdk/client"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func newClient(t *testing.T) *authz.Client {
	t.Helper()
	c, err := authz.NewClient(context.Background(), testinfra.StartOpenFGA(t), t.Name())
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

// TestConcurrentBootConvergesOnSameStore is an integration smoke test
// for the split-brain scenario, not a discriminating test of the
// selection rule: it seeds two stores under the same name and then
// calls authz.NewClient twice sequentially, with no writes in between.
// OpenFGA's ListStores happens to return a stable order across both
// reads, and OpenFGA store IDs are ULIDs (monotonic with creation
// time), so the min-ID store here is also the first-created and
// first-listed store — meaning even the pre-fix "first name match in
// list order" logic would pass this test too. This test only proves
// that end-to-end, two clients booted against a pre-existing duplicate
// land on a usable, shared store; it does NOT prove min-ID selection is
// what got them there. That property is covered by the pure-function
// unit test TestSmallestStoreID in store_selection_test.go, which feeds
// input ordered so the first match is deliberately not the min-ID one.
func TestConcurrentBootConvergesOnSameStore(t *testing.T) {
	ctx := context.Background()
	url := testinfra.StartOpenFGA(t)
	storeName := t.Name()

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
