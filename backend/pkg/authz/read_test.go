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
	}
	for _, c := range cases {
		tenant, ok := authz.TenantOfObject(c.object)
		require.Equal(t, c.ok, ok, c.object)
		require.Equal(t, c.tenant, tenant, c.object)
	}
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
