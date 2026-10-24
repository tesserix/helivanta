package globalapi

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEnvironmentRequiresExplicitProductionDestinations(t *testing.T) {
	_, err := FromEnv(func(string) string { return "" })
	require.Error(t, err)
	cfg, err := FromEnv(func(key string) string {
		return map[string]string{"HELIVANTA_ENV": "dev", "GLOBAL_API_UPSTREAM_URL": "http://localhost:8123"}[key]
	})
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 7)
	require.Equal(t, "http://localhost:8123", cfg.Upstreams["pharmacy"])
	_, err = FromEnv(func(key string) string {
		return map[string]string{"GLOBAL_API_UPSTREAMS": `{"platform":"https://platform.internal"}`}[key]
	})
	require.Error(t, err, "proxy trust must be explicit outside development")
}
