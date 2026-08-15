package bootstrap_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
)

// TestLoginRateLimitRuleMatchesDesign pins #841's Burst=10 (fixed, not
// derived from Rate/6 — see LoginRateLimitRule's doc comment and
// docs/superpowers/specs/2026-08-16-login-rate-limit-design.md D2 for
// why) and proves Rate is read from config rather than hardcoded
// alongside it.
func TestLoginRateLimitRuleMatchesDesign(t *testing.T) {
	rule := bootstrap.LoginRateLimitRule(config.Config{RateLimitLoginPerMin: 20})
	require.Equal(t, 20, rule.Rate)
	require.Equal(t, 10, rule.Burst)
	require.Equal(t, time.Minute, rule.Per)

	// Rate tracks the config value, not a copy hardcoded alongside the
	// default — an env override must reach the rule actually applied.
	rule2 := bootstrap.LoginRateLimitRule(config.Config{RateLimitLoginPerMin: 9999})
	require.Equal(t, 9999, rule2.Rate)
	require.Equal(t, 10, rule2.Burst, "burst is fixed regardless of the configured rate")
}
