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
