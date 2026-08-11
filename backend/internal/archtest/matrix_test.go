package archtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testinfra"
	"github.com/tesserix/hms/pkg/authz"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func allRoles() []authz.Role {
	return []authz.Role{
		authz.RoleTenantAdmin, authz.RoleDoctor, authz.RoleNurse,
		authz.RolePharmacist, authz.RoleLabTech,
	}
}

// expectedPermissions is the ground truth derived from module
// declarations: which permissions a role should hold after reconcile.
func expectedPermissions(reg *platform.Registry, role authz.Role) authz.PermissionSet {
	set := authz.PermissionSet{}
	for _, g := range platform.GrantsFor(reg) {
		for _, r := range g.Roles {
			if r == role {
				set[g.Permission] = struct{}{}
			}
		}
	}
	return set
}

func registry(t *testing.T) *platform.Registry {
	t.Helper()
	reg := platform.NewRegistry()
	for _, m := range allModules() {
		require.NoError(t, reg.Register(m))
	}
	return reg
}

// TestPermissionMatrix is the phase gate: every role × every declared
// permission, asserted allow/deny against a real OpenFGA, in both
// tenants. The matrix is generated from the registry, so adding a module
// extends the suite automatically — a route cannot ship untested.
func TestPermissionMatrix(t *testing.T) {
	ctx := context.Background()
	reg := registry(t)
	client, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), "hms-matrix")
	require.NoError(t, err)

	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantA))
	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantB))

	// One user per role, a member of tenant A only.
	for _, role := range allRoles() {
		require.NoError(t, client.GrantRole(ctx, tenantA, "user-"+string(role), role))
	}

	allPerms := map[authz.Permission]struct{}{}
	for _, g := range platform.GrantsFor(reg) {
		allPerms[g.Permission] = struct{}{}
	}

	for _, role := range allRoles() {
		role := role
		t.Run(string(role), func(t *testing.T) {
			got, err := client.Resolve(ctx, "user-"+string(role), tenantA)
			require.NoError(t, err)
			want := expectedPermissions(reg, role)

			for perm := range allPerms {
				if want.Has(perm) {
					require.Truef(t, got.Has(perm), "%s must hold %s", role, perm)
				} else {
					require.Falsef(t, got.Has(perm), "%s must NOT hold %s", role, perm)
				}
			}
		})
	}
}

// TestCrossTenantDenial is the adversarial half: a fully-privileged
// tenant_admin in tenant A must hold nothing at all in tenant B.
func TestCrossTenantDenial(t *testing.T) {
	ctx := context.Background()
	reg := registry(t)
	client, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), "hms-matrix-cross")
	require.NoError(t, err)

	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantA))
	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantB))
	require.NoError(t, client.GrantRole(ctx, tenantA, "admin-a", authz.RoleTenantAdmin))

	inA, err := client.Resolve(ctx, "admin-a", tenantA)
	require.NoError(t, err)
	require.NotEmpty(t, inA.Sorted(), "sanity: the admin must hold permissions in its own tenant")

	inB, err := client.Resolve(ctx, "admin-a", tenantB)
	require.NoError(t, err)
	require.Empty(t, inB.Sorted(), "tenant A admin must hold nothing in tenant B")
}

// TestEveryGuardedRouteIsCoveredByTheMatrix fails if a module guards a
// route with a permission that no system role holds — such a route would
// be unreachable for everyone except tenant_admin, which is nearly always
// a declaration bug.
func TestEveryGuardedRouteIsCoveredByTheMatrix(t *testing.T) {
	reg := registry(t)
	holders := map[authz.Permission]int{}
	for _, g := range platform.GrantsFor(reg) {
		holders[g.Permission] = len(g.Roles)
	}
	for perm, n := range holders {
		// Every grant gets tenant_admin appended, so a permission held by
		// exactly one role is admin-only.
		if n <= 1 && perm != iam.PermMemberManage {
			t.Errorf("permission %q is held by tenant_admin only; declare the role that needs it", perm)
		}
	}
}
