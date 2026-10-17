package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/config"
)

func TestTransitRequiresEveryOpenBaoSettingOrNone(t *testing.T) {
	valid := config.Config{OpenBaoAddr: "http://openbao:8200", OpenBaoTransitMount: "transit", OpenBaoAuthRole: "helivanta-api-transit", OpenBaoTokenPath: "/var/run/secrets/openbao/token"}
	for _, tc := range []struct {
		name             string
		mutate           func(*config.Config)
		enabled, invalid bool
	}{
		{"complete", func(*config.Config) {}, true, false},
		{"unset", func(c *config.Config) { *c = config.Config{} }, false, false},
		{"no_addr", func(c *config.Config) { c.OpenBaoAddr = "" }, false, true},
		{"no_mount", func(c *config.Config) { c.OpenBaoTransitMount = " " }, false, true},
		{"no_role", func(c *config.Config) { c.OpenBaoAuthRole = "" }, false, true},
		{"no_token", func(c *config.Config) { c.OpenBaoTokenPath = "" }, false, true},
		{"relative_token", func(c *config.Config) { c.OpenBaoTokenPath = "token" }, false, true},
		{"addr_only", func(c *config.Config) { *c = config.Config{OpenBaoAddr: valid.OpenBaoAddr} }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			enabled, err := cfg.RequireTransit()
			require.Equal(t, tc.enabled, enabled)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadReadsOpenBaoSettingsWithoutDefaults(t *testing.T) {
	for _, k := range []string{"OPENBAO_ADDR", "OPENBAO_TRANSIT_MOUNT", "OPENBAO_AUTH_ROLE", "OPENBAO_TOKEN_PATH"} {
		t.Setenv(k, "")
	}
	enabled, err := config.Load().RequireTransit()
	require.NoError(t, err)
	require.False(t, enabled, "an unconfigured process never invents a vault")
	t.Setenv("OPENBAO_ADDR", "http://openbao:8200")
	t.Setenv("OPENBAO_TRANSIT_MOUNT", "transit")
	t.Setenv("OPENBAO_AUTH_ROLE", "helivanta-api-transit")
	t.Setenv("OPENBAO_TOKEN_PATH", "/var/run/secrets/openbao/token")
	cfg := config.Load()
	require.Equal(t, []string{"http://openbao:8200", "transit", "helivanta-api-transit", "/var/run/secrets/openbao/token"}, []string{cfg.OpenBaoAddr, cfg.OpenBaoTransitMount, cfg.OpenBaoAuthRole, cfg.OpenBaoTokenPath})
}
