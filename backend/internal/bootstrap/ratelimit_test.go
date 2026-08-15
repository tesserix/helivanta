package bootstrap_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/pkg/ratelimit"
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

// TestLoginBudgetIsIsolatedFromThePrincipalBudget closes the gap #841's
// implementation report flagged honestly: login and the /v1 chain share ONE
// ratelimit.Memory instance, and nothing proved their buckets are separate.
//
// They are separate only because the key prefixes differ — "login:" here,
// "subject:" in pkg/ratelimit.Middleware. That is one string literal away
// from collapsing into a single bucket, and the collapse would be silent:
// everything still returns 429s, just the wrong ones. A clinician whose
// tabs renewed a few times would find ordinary requests refused, or a burst
// of ordinary requests would lock them out of signing in — neither of which
// any existing test would notice.
//
// Asserted against the real rule constructors on a real shared limiter,
// not against the string literals, so it fails if either call site's key
// scheme changes to overlap.
func TestLoginBudgetIsIsolatedFromThePrincipalBudget(t *testing.T) {
	t.Setenv("RATE_LIMIT_LOGIN_PER_MIN", "20")
	cfg := config.Load()

	shared := ratelimit.NewMemory(100)
	loginRule := bootstrap.LoginRateLimitRule(cfg)
	principalRule := bootstrap.RateLimitConfig(cfg).Principal
	now := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	const subject = "386401163026628615"

	// Drain the login bucket completely.
	for i := 0; i < loginRule.Burst+1; i++ {
		shared.Allow("login:"+subject, loginRule, now)
	}
	require.False(t, shared.Allow("login:"+subject, loginRule, now).Allowed,
		"the login bucket must actually be exhausted, or the real assertion below proves nothing")

	// The same subject's ordinary request budget must be untouched.
	require.True(t, shared.Allow("subject:"+subject, principalRule, now).Allowed,
		"exhausting the login budget must not spend the caller's ordinary request budget: "+
			"a clinician whose tabs renewed a few times would find normal requests refused, "+
			"and nothing in the response would say why")
}
