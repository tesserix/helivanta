package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPermissionSetHas(t *testing.T) {
	s := authz.NewPermissionSet("medicore.visit.create", "medicore.visit.read")

	require.True(t, s.Has("medicore.visit.create"))
	require.False(t, s.Has("pharmacy.dispense.fulfil"))
}

// TestHasNoLongerLiesAboutPublic is the regression test for #781 at the
// unit level: Has used to short-circuit true for Public regardless of
// what the set actually contained, which is what let a Public route
// serve a caller whose set was empty because their membership had been
// revoked. Has must now answer exactly what the set contains — Require
// is what skips the check for Public, not Has.
func TestHasNoLongerLiesAboutPublic(t *testing.T) {
	require.False(t, authz.NewPermissionSet().Has(authz.Public),
		"Has must consult the set honestly; Public is handled by Require, not by Has lying about the set")
	require.False(t, authz.NewPermissionSet().Has(authz.NoTenantMembership))
}

func TestSortedIsDeterministic(t *testing.T) {
	s := authz.NewPermissionSet("b.x.y", "a.x.y", "c.x.y")
	require.Equal(t, []string{"a.x.y", "b.x.y", "c.x.y"}, s.Sorted())
}

func TestSortedOnEmptySetIsEmptyNotNil(t *testing.T) {
	require.Equal(t, []string{}, authz.NewPermissionSet().Sorted())
}

// TestSystemRolesMatchesKnownRole pins the two exports to each other.
// SystemRoles exists so a caller can PRINT the valid roles (cmd/bootstrap
// rejecting a typo'd -role); if it ever disagreed with KnownRole, that
// message would tell an operator a role is valid which the validator then
// rejects, or omit one it accepts.
func TestSystemRolesMatchesKnownRole(t *testing.T) {
	roles := authz.SystemRoles()
	require.NotEmpty(t, roles)
	for _, r := range roles {
		require.True(t, authz.KnownRole(r), "SystemRoles listed %q but KnownRole rejects it", r)
	}
	require.False(t, authz.KnownRole("tenant_admn"))
}

// TestSystemRolesCannotBeMutatedByCallers: the returned slice is a copy,
// so a caller that sorts or appends to it cannot corrupt the list every
// KnownRole call reads.
func TestSystemRolesCannotBeMutatedByCallers(t *testing.T) {
	authz.SystemRoles()[0] = "not_a_role"

	require.True(t, authz.KnownRole(authz.RoleTenantAdmin))
	require.Equal(t, authz.RoleTenantAdmin, authz.SystemRoles()[0])
}

// TestReceptionistIsASystemRole pins the front-desk role into the
// registry. KnownRole is the gate every untrusted role_key passes
// through, so a role missing here cannot be granted at all.
func TestReceptionistIsASystemRole(t *testing.T) {
	require.True(t, authz.KnownRole(authz.RoleReceptionist))
	require.Contains(t, authz.SystemRoles(), authz.RoleReceptionist)
}
