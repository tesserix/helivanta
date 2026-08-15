package config_test

import (
	"testing"
	"time"

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

// TestRateLimitDefaultsMatchTheDesignSpec pins the production defaults
// documented in docs/superpowers/specs/2026-08-14-rate-limiting-design.md's
// D2 table (600/120/10 per minute) against an unset environment, so a
// change to those defaults is a deliberate edit to config.go rather than
// an accidental one.
func TestRateLimitDefaultsMatchTheDesignSpec(t *testing.T) {
	t.Setenv("RATE_LIMIT_TENANT_PER_MIN", "")
	t.Setenv("RATE_LIMIT_PRINCIPAL_PER_MIN", "")
	t.Setenv("RATE_LIMIT_MINT_PER_MIN", "")

	cfg := config.Load()
	require.Equal(t, 600, cfg.RateLimitTenantPerMin)
	require.Equal(t, 120, cfg.RateLimitPrincipalPerMin)
	require.Equal(t, 10, cfg.RateLimitMintPerMin)
}

// TestRateLimitEnvOverridesAreHonoured proves getenvInt actually reads
// the environment rather than always returning the default — without
// this, TestRateLimitDefaultsMatchTheDesignSpec alone could not tell
// getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600) apart from a function that
// ignores its first argument and always returns 600.
func TestRateLimitEnvOverridesAreHonoured(t *testing.T) {
	t.Setenv("RATE_LIMIT_TENANT_PER_MIN", "100000")
	t.Setenv("RATE_LIMIT_PRINCIPAL_PER_MIN", "100000")
	t.Setenv("RATE_LIMIT_MINT_PER_MIN", "1000")

	cfg := config.Load()
	require.Equal(t, 100000, cfg.RateLimitTenantPerMin)
	require.Equal(t, 100000, cfg.RateLimitPrincipalPerMin)
	require.Equal(t, 1000, cfg.RateLimitMintPerMin)
}

// TestRateLimitEnvFallsOpenOnUnparseableValue is the fail-open case
// getenvInt exists for (docs/standards/engineering-principles.md §3): a
// rate limit is a capacity control, so a mistyped env var must not stop
// a hospital's API from booting. It must fall back to the production
// default rather than to zero (which would deny every request) or a
// boot failure.
func TestRateLimitEnvFallsOpenOnUnparseableValue(t *testing.T) {
	t.Setenv("RATE_LIMIT_TENANT_PER_MIN", "not-a-number")
	cfg := config.Load()
	require.Equal(t, 600, cfg.RateLimitTenantPerMin,
		"an unparseable rate limit must fall back to the production default, not to zero or a boot failure")
}

// TestSessionTTLDefaultsAndFallsOpen pins SessionTTL's default and
// proves it is env-overridable, then proves the SAME fail-open
// direction as the rate limits: unlike SESSION_SIGNING_KEY, an
// unparseable SESSION_TTL is a capacity/latency-bound control (it
// trades renewal traffic against upstream-deactivation latency, spec
// D4), not the identity control the key is, so it must fall back to
// the default rather than block boot.
func TestSessionTTLDefaultsAndFallsOpen(t *testing.T) {
	t.Setenv("SESSION_TTL", "")
	require.Equal(t, 15*time.Minute, config.Load().SessionTTL)

	t.Setenv("SESSION_TTL", "5m")
	require.Equal(t, 5*time.Minute, config.Load().SessionTTL)

	t.Setenv("SESSION_TTL", "not-a-duration")
	require.Equal(t, 15*time.Minute, config.Load().SessionTTL,
		"an unparseable SESSION_TTL must fall back to the default, not block boot")
}

// TestSessionIssuerDefaultsAndOverrides pins SessionIssuer's default —
// a label the Verifier checks the session token's `iss` claim against,
// not a secret — and proves it is overridable.
func TestSessionIssuerDefaultsAndOverrides(t *testing.T) {
	t.Setenv("SESSION_ISSUER", "")
	require.Equal(t, "https://hms.local", config.Load().SessionIssuer)

	t.Setenv("SESSION_ISSUER", "https://hms.example.org")
	require.Equal(t, "https://hms.example.org", config.Load().SessionIssuer)
}
