package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/config"
)

// TestRequireIdleTimeout_Zero_Refuses is the boot refusal itself: "0"
// parses as a valid duration, so getenvDuration's mistyped-value
// fallback never sees it, and the process would otherwise boot into a
// state where every request 401s with session_idle.
func TestRequireIdleTimeout_Zero_Refuses(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT", "0s")

	_, err := config.Load().RequireIdleTimeout()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrInvalidIdleTimeout)
	require.Contains(t, err.Error(), "IDLE_TIMEOUT",
		"the refusal must name the variable an operator has to fix")
}

// TestRequireIdleTimeout_Negative_Refuses covers the other side of
// "non-positive": a negative duration parses just as cleanly as zero and
// is just as total an outage.
func TestRequireIdleTimeout_Negative_Refuses(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT", "-5m")

	_, err := config.Load().RequireIdleTimeout()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrInvalidIdleTimeout)
}

// TestRequireIdleTimeout_Positive_Accepted proves the refusal is
// specific to non-positive values rather than unconditional, and that
// the operator's value is returned unchanged — never clamped to the
// default, which would leave the deployed policy differing from the
// declared one.
func TestRequireIdleTimeout_Positive_Accepted(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT", "7m")

	got, err := config.Load().RequireIdleTimeout()

	require.NoError(t, err)
	require.Equal(t, 7*time.Minute, got)
}

// TestRequireIdleTimeout_Unset_TakesTheDocumentedDefault pins the
// default at the value spec D1 argues for, through the same accessor
// cmd/api boots with — so an accidental change to either the constant or
// Load()'s call site fails here rather than silently shortening or
// lengthening every clinician's session.
func TestRequireIdleTimeout_Unset_TakesTheDocumentedDefault(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT", "")

	got, err := config.Load().RequireIdleTimeout()

	require.NoError(t, err)
	require.Equal(t, config.DefaultIdleTimeout, got)
	require.Equal(t, 15*time.Minute, config.DefaultIdleTimeout,
		"spec D1 fixes the clinical window at 15 minutes; changing it is a policy decision, not a refactor")
}

// TestRequireIdleTimeout_IsIndependentOfSessionTTL pins the one coupling
// spec D1/D3 forbids. The two happen to share a default, which is
// exactly what would make a derivation ("idle = ttl") invisible: setting
// only SESSION_TTL must move the token lifetime and leave the idle
// window alone, because renewal moves `exp` every five minutes and must
// never move the idle deadline with it.
func TestRequireIdleTimeout_IsIndependentOfSessionTTL(t *testing.T) {
	t.Setenv("SESSION_TTL", "3m")
	t.Setenv("IDLE_TIMEOUT", "")

	cfg := config.Load()
	idle, err := cfg.RequireIdleTimeout()

	require.NoError(t, err)
	require.Equal(t, 3*time.Minute, cfg.SessionTTL, "precondition: SESSION_TTL was applied")
	require.Equal(t, config.DefaultIdleTimeout, idle,
		"IDLE_TIMEOUT must come from its own variable, never be derived from SESSION_TTL")
}
