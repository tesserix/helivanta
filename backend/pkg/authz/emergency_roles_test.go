package authz_test

import (
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"testing"
)

func TestEmergencyOperationalRolesAreSeededAndDistinct(t *testing.T) {
	for _, key := range []string{"emergency_dispatcher", "emergency_supervisor"} {
		require.True(t, authz.KnownRole(authz.Role(key)), "emergency role must be grantable through IAM")
	}
	require.False(t, authz.KnownRole("emergency_admin_bypass"))
}
