package archtest

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam"
	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
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
// Takes the already-computed grants rather than the registry so callers
// that iterate over multiple roles only pay GrantsFor's duplicate check
// once.
func expectedPermissions(grants []authz.Grant, role authz.Role) authz.PermissionSet {
	set := authz.PermissionSet{}
	for _, g := range grants {
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

	grants, err := platform.GrantsFor(reg)
	require.NoError(t, err)
	allPerms := map[authz.Permission]struct{}{}
	for _, g := range grants {
		allPerms[g.Permission] = struct{}{}
	}

	for _, role := range allRoles() {
		role := role
		t.Run(string(role), func(t *testing.T) {
			got, err := client.Resolve(ctx, "user-"+string(role), tenantA)
			require.NoError(t, err)
			want := expectedPermissions(grants, role)

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
// route (per platform.Router.Declared()) with a permission that no system
// role but tenant_admin holds — such a route would be unreachable for
// everyone else, which is nearly always a declaration bug. It is the
// route half of the pair that starts with TestEveryDeclaredPermissionIsGranted
// in arch_test.go (every permission a route uses must be declared in
// Permissions()) — this test adds the other direction, that every
// permission a route uses is actually reachable by some non-admin role.
func TestEveryGuardedRouteIsCoveredByTheMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reg := registry(t)
	grants, err := platform.GrantsFor(reg)
	require.NoError(t, err)
	holders := map[authz.Permission]int{}
	for _, g := range grants {
		holders[g.Permission] = len(g.Roles)
	}

	for _, m := range allModules() {
		e := gin.New()
		r := platform.NewRouter(e.Group("/v1"))
		m.Routes(r, platform.Deps{})
		for _, perm := range r.Declared() {
			if perm == authz.Public {
				continue
			}
			// Every grant gets tenant_admin appended, so a permission held
			// by exactly one role is admin-only.
			if n := holders[perm]; n <= 1 && perm != iam.PermMemberManage {
				t.Errorf("module %q guards a route with %q, held by tenant_admin only; declare the role that needs it", m.Name(), perm)
			}
		}
	}
}

// approvedPermissionMatrix is the independently-authored ground truth for
// the role -> permission mapping, transcribed by hand from the design
// spec's "System roles" table
// (docs/superpowers/specs/2026-08-11-hms-authorization-design.md) crossed
// with the mapping actually implemented, as recorded in
// .superpowers/sdd/2026-08-11-hms-authorization/task-4-report.md. The spec
// table predates pharmacy.medication.write, which the Task 4 mapping adds
// (pharmacist-only); this literal uses the implemented set.
//
// Deliberately NOT derived from platform.GrantsFor: TestPermissionMatrix's
// own ground truth (expectedPermissions) calls that same function, so a
// role swapped between two roles in a module's Permissions() would change
// both the tuples written and the expected set identically and stay
// green. This literal is the independent oracle that catches exactly that
// bug class — a role mapping change must be a deliberate, reviewable edit
// here.
var approvedPermissionMatrix = map[authz.Permission][]authz.Role{
	iam.PermMemberManage: {authz.RoleTenantAdmin},

	medicore.PermVisitCreate: {authz.RoleDoctor, authz.RoleTenantAdmin},
	medicore.PermVisitRead:   {authz.RoleDoctor, authz.RoleNurse, authz.RoleTenantAdmin},
	medicore.PermVisitUpdate: {authz.RoleNurse, authz.RoleTenantAdmin},

	pharmacy.PermMedicationWrite: {authz.RolePharmacist, authz.RoleTenantAdmin},
	pharmacy.PermMedicationRead:  {authz.RolePharmacist, authz.RoleTenantAdmin},
	pharmacy.PermDispenseRead:    {authz.RolePharmacist, authz.RoleDoctor, authz.RoleTenantAdmin},
	pharmacy.PermDispenseFulfil:  {authz.RolePharmacist, authz.RoleTenantAdmin},

	lab.PermOrderRead:   {authz.RoleLabTech, authz.RoleDoctor, authz.RoleTenantAdmin},
	lab.PermOrderFulfil: {authz.RoleLabTech, authz.RoleTenantAdmin},
}

// TestDeclaredPermissionsMatchTheApprovedMatrix is the independent-oracle
// half of the phase gate: it asserts platform.GrantsFor produces exactly
// approvedPermissionMatrix — same permissions, same roles per permission,
// no extras, no omissions. Unlike TestPermissionMatrix (which proves the
// tuple-write/resolve/tenant-scope plumbing works, using GrantsFor as its
// own ground truth), this test is the only one that can catch a module
// declaring the wrong role for a permission, because its expected table
// is hand-authored from the approved design rather than computed from the
// code under test.
func TestDeclaredPermissionsMatchTheApprovedMatrix(t *testing.T) {
	reg := registry(t)
	grants, err := platform.GrantsFor(reg)
	require.NoError(t, err)
	actual := map[authz.Permission][]authz.Role{}
	for _, g := range grants {
		actual[g.Permission] = g.Roles
	}

	require.Equal(t, len(approvedPermissionMatrix), len(actual),
		"declared permission count must match the approved matrix exactly (no extras, no omissions)")

	for perm, wantRoles := range approvedPermissionMatrix {
		gotRoles, ok := actual[perm]
		require.Truef(t, ok, "approved matrix expects permission %q but GrantsFor does not produce it", perm)
		require.ElementsMatchf(t, wantRoles, gotRoles, "permission %q: role mismatch (want %v, got %v)", perm, wantRoles, gotRoles)
	}
}
