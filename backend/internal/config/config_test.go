package config_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/pkg/logging"
)

// The default must be production. A guard that defaults to permissive
// protects nothing, because the deployment that forgets to set the
// variable is exactly the one that needed protecting.
func TestEnvDefaultsToProduction(t *testing.T) {
	t.Setenv("HELIVANTA_ENV", "")
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
		t.Setenv("HELIVANTA_ENV", tc.env)
		require.Equal(t, tc.isDev, config.Load().IsDev(), "HELIVANTA_ENV=%q", tc.env)
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
	t.Setenv("RATE_LIMIT_LOGIN_PER_MIN", "")

	cfg := config.Load()
	require.Equal(t, 600, cfg.RateLimitTenantPerMin)
	require.Equal(t, 120, cfg.RateLimitPrincipalPerMin)
	// 20/min, per
	// docs/superpowers/specs/2026-08-16-login-rate-limit-design.md D2:
	// refills a fully-drained 10-token burst (bootstrap.LoginRateLimitRule)
	// in 30s, well inside the 5-minute gap between #838 D4a's synchronized
	// multi-tab renewal clusters.
	require.Equal(t, 20, cfg.RateLimitLoginPerMin)
}

// TestRateLimitEnvOverridesAreHonoured proves getenvInt actually reads
// the environment rather than always returning the default — without
// this, TestRateLimitDefaultsMatchTheDesignSpec alone could not tell
// getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600) apart from a function that
// ignores its first argument and always returns 600.
func TestRateLimitEnvOverridesAreHonoured(t *testing.T) {
	t.Setenv("RATE_LIMIT_TENANT_PER_MIN", "100000")
	t.Setenv("RATE_LIMIT_PRINCIPAL_PER_MIN", "100000")
	t.Setenv("RATE_LIMIT_LOGIN_PER_MIN", "100000")

	cfg := config.Load()
	require.Equal(t, 100000, cfg.RateLimitTenantPerMin)
	require.Equal(t, 100000, cfg.RateLimitPrincipalPerMin)
	require.Equal(t, 100000, cfg.RateLimitLoginPerMin)
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
	require.Equal(t, "https://helivanta.local", config.Load().SessionIssuer)

	t.Setenv("SESSION_ISSUER", "https://hms.example.org")
	require.Equal(t, "https://hms.example.org", config.Load().SessionIssuer)
}

// TestTrustedProxyCIDRsDefaultsToNilNotEverything is Finding C1's
// fail-closed default (#867 Task 4 fix round 3): an unset
// TRUSTED_PROXY_CIDRS must resolve to an EMPTY list, which
// httpserver.New treats as "trust no proxy" — never gin's own
// trust-everyone default. A default of ["0.0.0.0/0"] here, or any
// non-empty fallback, would silently reopen the X-Forwarded-For
// spoofing hole this field exists to close.
func TestTrustedProxyCIDRsDefaultsToNilNotEverything(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	cfg := config.Load()
	require.Empty(t, cfg.TrustedProxyCIDRs)
}

// TestTrustedProxyCIDRsParsesCommaSeparatedList proves getenvCIDRList
// actually reads and splits the environment variable, trimming
// whitespace around each entry — without this,
// TestTrustedProxyCIDRsDefaultsToNilNotEverything alone could not tell
// a real parser apart from a function that always returns nil.
func TestTrustedProxyCIDRsParsesCommaSeparatedList(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.20.0.0/16, 10.30.0.0/20")
	cfg := config.Load()
	require.Equal(t, []string{"10.20.0.0/16", "10.30.0.0/20"}, cfg.TrustedProxyCIDRs)
}

// TestTrustedProxyCIDRsDropsOnlyTheMalformedEntry pins getenvCIDRList's
// per-entry fail-closed behaviour: one malformed CIDR in a list must not
// silently discard the operator's other, correctly-typed entries (which
// would widen the trust boundary to nothing when SOME configuration was
// clearly intended), and it must not be silently accepted either (which
// would trust a value that is not actually a CIDR at all).
func TestTrustedProxyCIDRsDropsOnlyTheMalformedEntry(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.20.0.0/16,not-a-cidr,10.30.0.0/20")
	cfg := config.Load()
	require.Equal(t, []string{"10.20.0.0/16", "10.30.0.0/20"}, cfg.TrustedProxyCIDRs)
}

// TestTrustedProxyCIDRsAllMalformedYieldsEmptyNotError proves that
// Load()'s PARSING of the all-garbage case still lands on the SAME
// fail-closed answer as unset — an empty list — rather than a partially
// or fully accepted bogus trust boundary. This is unchanged by #824 Task
// 1's boot guard: Load() itself still never fails. What DOES change is
// that "empty list" is no longer the end of the story — outside
// HELIVANTA_ENV=dev, RequireTrustedProxyCIDRs (trustedproxy.go) turns
// this exact case into a boot refusal, distinct from the unset case, by
// re-reading the raw env var Load() already discarded. See that
// function's doc comment for why parsing and boot-enforcement are
// deliberately split across two functions.
func TestTrustedProxyCIDRsAllMalformedYieldsEmptyNotError(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "not-a-cidr, also-not-one")
	cfg := config.Load()
	require.Empty(t, cfg.TrustedProxyCIDRs)
}

// TestDurationFallbackIsReadableInLogs asserts on the bytes actually
// emitted, not on the call being made.
//
// slog renders a time.Duration as its nanosecond int64, and
// pkg/logging's PHI matcher treats a long digit run as an identifier: 15m
// is 900000000000 — twelve digits — and is emitted as
// "[REDACTED:aadhaar]"; 24h is fourteen and becomes "[REDACTED:abha]".
// This line exists solely so a mistyped SESSION_TTL is visible, so a
// fallback the operator cannot read defeats the only reason to log it.
func TestDurationFallbackIsReadableInLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.NewWithWriter(&buf, "info"))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Setenv("SESSION_TTL", "not-a-duration")
	cfg := config.Load()

	out := buf.String()
	require.Contains(t, out, "15m0s",
		"the fallback duration must be readable; slog renders a bare time.Duration as nanoseconds")
	require.NotContains(t, out, "REDACTED",
		"a nanosecond int64 is a long digit run, which the PHI matcher masks as an identifier")
	require.Equal(t, 15*time.Minute, cfg.SessionTTL)
}
