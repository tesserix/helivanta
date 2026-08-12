package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/hms/internal/config"
)

// The default must be production. A guard that defaults to permissive
// protects nothing, because the deployment that forgets to set the
// variable is exactly the one that needed protecting.
func TestEnvDefaultsToProduction(t *testing.T) {
	t.Setenv("HMS_ENV", "")
	cfg := config.Load()
	require.Equal(t, "production", cfg.Env)
	require.False(t, cfg.IsDev())
}

func TestIsDevOnlyForExactDev(t *testing.T) {
	for _, tc := range []struct {
		env   string
		isDev bool
	}{
		{"dev", true},
		{"development", false},
		{"Dev", false},
		{"production", false},
		{"staging", false},
	} {
		t.Setenv("HMS_ENV", tc.env)
		require.Equal(t, tc.isDev, config.Load().IsDev(), "HMS_ENV=%q", tc.env)
	}
}
