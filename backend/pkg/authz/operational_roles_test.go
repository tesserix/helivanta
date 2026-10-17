package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestOperationalRolesAreExplicitAssignableIdentities(t *testing.T) {
	for _, key := range []string{"supply_owner", "supply_inventory", "supply_picker", "supply_packer", "supply_dispatcher", "supply_driver", "supply_regulator", "supply_support", "pharmacy_owner", "pharmacy_purchasing", "pharmacy_inventory"} {
		t.Run(key, func(t *testing.T) {
			require.True(t, authz.KnownRole(authz.Role(key)))
			require.Contains(t, authz.SystemRoles(), authz.Role(key))
		})
	}
	require.False(t, authz.KnownRole(authz.Role("supply_superuser")))
}
