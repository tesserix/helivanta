package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/authz"
)

// TestTenantOfObject pins the parse that every tenant-scoped delete
// depends on. The prefix cases matter most: "11111111-...-1111" must not
// be read as owning "11111111-...-1111-extra", or a reconcile of one
// tenant could delete another's tuples.
func TestTenantOfObject(t *testing.T) {
	cases := []struct {
		object string
		tenant string
		ok     bool
	}{
		{"role:tenant-a/doctor", "tenant-a", true},
		{"perm:tenant-a/x.thing.read", "tenant-a", true},
		{"role:tenant-a-extra/doctor", "tenant-a-extra", true},
		{"role:tenant-a/", "", false},
		{"role:/doctor", "", false},
		{"role:doctor", "", false},
		{"user:alice", "", false},
		{"", "", false},
		// A tenant object carries no /name segment. One appearing here
		// means the object is not the shape this parse believes, so it
		// must not parse at all — returning "tenant-a" would hand prune a
		// delete candidate bucketed under a tenant id derived from an
		// object nobody understands. Without this case the guard that
		// rejects it can be deleted with every test still green, which is
		// exactly what happened when it was first written.
		{"tenant:tenant-a/doctor", "", false},
		{"tenant:tenant-a/", "", false},
	}
	for _, c := range cases {
		tenant, ok := authz.TenantOfObject(c.object)
		require.Equal(t, c.ok, ok, c.object)
		require.Equal(t, c.tenant, tenant, c.object)
	}
}

// TestTenantOfObjectIsTypeAware pins the tenant type's shape: a tenant
// object IS its tenant id, with no /name segment, unlike role: and
// perm:. Making the parse lenient (any object without a "/" treated as a
// tenant) would be dangerous — see the "dangerous case" below.
func TestTenantOfObjectIsTypeAware(t *testing.T) {
	const tid = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

	got, ok := authz.TenantOfObject("tenant:" + tid)
	require.True(t, ok, "a tenant object carries its tenant id directly, with no /name segment")
	require.Equal(t, tid, got)

	got, ok = authz.TenantOfObject("perm:" + tid + "/medicore.visit.read")
	require.True(t, ok)
	require.Equal(t, tid, got)

	// The dangerous case: a non-tenant type with no /name segment must
	// NOT parse. If it did, prune would bucket it by a tenant id that
	// was never validated and could delete a tuple it does not
	// understand.
	_, ok = authz.TenantOfObject("perm:garbage")
	require.False(t, ok, "a non-tenant object without a /name segment must not parse")

	_, ok = authz.TenantOfObject("tenant:")
	require.False(t, ok, "an empty tenant id must not parse")
}

func TestReadTuplesByTenantBucketsByTenant(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantRole(ctx, tenantA, "alice", authz.RoleDoctor))
	require.NoError(t, c.GrantPermission(ctx, tenantA, "x.thing.read", authz.RoleDoctor))
	require.NoError(t, c.GrantRole(ctx, tenantB, "bob", authz.RoleNurse))

	byTenant, err := c.ReadTuplesByTenant(ctx)
	require.NoError(t, err)

	keys := func(tenantID string) []string {
		out := []string{}
		for _, tp := range byTenant[tenantID] {
			out = append(out, tp.Key())
		}
		return out
	}
	require.ElementsMatch(t, []string{
		authz.Tuple{User: "user:alice", Relation: "assignee", Object: authz.RoleObject(tenantA, authz.RoleDoctor)}.Key(),
		authz.Tuple{
			User:     authz.RoleObject(tenantA, authz.RoleDoctor),
			Relation: "granted_role",
			Object:   authz.PermObject(tenantA, "x.thing.read"),
		}.Key(),
	}, keys(tenantA))
	require.Equal(t, []string{
		authz.Tuple{User: "user:bob", Relation: "assignee", Object: authz.RoleObject(tenantB, authz.RoleNurse)}.Key(),
	}, keys(tenantB))
}

// TestDeleteTupleIsIdempotent keeps the delete half as safe to retry as
// every write helper: a reconcile that crashes after a delete and reruns
// must not fail on the tuple it already removed.
func TestDeleteTupleIsIdempotent(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	tuple := authz.Tuple{
		User: "user:alice", Relation: "assignee", Object: authz.RoleObject(tenantA, authz.RoleDoctor),
	}

	require.NoError(t, c.GrantRole(ctx, tenantA, "alice", authz.RoleDoctor))
	require.NoError(t, c.DeleteTuple(ctx, tuple))
	require.NoError(t, c.DeleteTuple(ctx, tuple))

	byTenant, err := c.ReadTuplesByTenant(ctx)
	require.NoError(t, err)
	require.Empty(t, byTenant[tenantA])
}
