package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/authz"
)

func TestPermissionSetHas(t *testing.T) {
	s := authz.NewPermissionSet("medicore.visit.create", "medicore.visit.read")

	require.True(t, s.Has("medicore.visit.create"))
	require.False(t, s.Has("pharmacy.dispense.fulfil"))
}

func TestPublicIsAlwaysAllowedEvenOnEmptySet(t *testing.T) {
	require.True(t, authz.NewPermissionSet().Has(authz.Public))
}

func TestSortedIsDeterministic(t *testing.T) {
	s := authz.NewPermissionSet("b.x.y", "a.x.y", "c.x.y")
	require.Equal(t, []string{"a.x.y", "b.x.y", "c.x.y"}, s.Sorted())
}

func TestSortedOnEmptySetIsEmptyNotNil(t *testing.T) {
	require.Equal(t, []string{}, authz.NewPermissionSet().Sorted())
}
