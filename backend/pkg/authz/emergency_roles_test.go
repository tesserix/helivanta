package authz_test

import (
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"testing"
)

func TestEmergencyOperationalRolesAreSeededAndDistinct(t *testing.T) {
	for _, key := range []string{"emergency_dispatcher", "emergency_supervisor", "emergency_crew"} {
		require.True(t, authz.KnownRole(authz.Role(key)), "emergency role must be grantable through IAM")
	}
	require.False(t, authz.KnownRole("emergency_admin_bypass"))
}

func TestReceivingRoleIsGrantableThroughIAM(t *testing.T) {
	require.True(t, authz.KnownRole("emergency_receiver"))
	require.False(t, authz.KnownRole("emergency_receiving_admin_bypass"))
}
