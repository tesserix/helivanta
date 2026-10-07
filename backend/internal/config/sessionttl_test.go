package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/config"
)

// TestRequireSessionTTL_BelowMinimum_Refuses is the boot refusal itself
// (#921). Every value here parses, so getenvDuration's mistyped-value
// fallback never sees it:
//
//   - "0s" and "-5m" were previously refused only by session.NewSigner,
//     late and under the signing key's name;
//   - "5s" and "20s" booted cleanly and expired every session before its
//     first renewal (renewAtFor's 30s floor);
//   - "89s" is the boundary: one second under the derived minimum.
func TestRequireSessionTTL_BelowMinimum_Refuses(t *testing.T) {
	for _, v := range []string{"0s", "-5m", "5s", "20s", "89s"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("SESSION_TTL", v)

			got, err := config.Load().RequireSessionTTL()

			require.ErrorIs(t, err, config.ErrInvalidSessionTTL)
			require.Zero(t, got, "a refusal must not also hand back a usable TTL")
			require.Contains(t, err.Error(), "SESSION_TTL",
				"the refusal must name the variable an operator has to fix")
			require.Contains(t, err.Error(), "got "+mustParse(t, v).String(),
				"the refusal must state the value it actually resolved")
		})
	}
}

// TestRequireSessionTTL_AtOrAboveMinimum_Accepted proves the refusal is
// a boundary rather than unconditional, and that the operator's value is
// returned unchanged — never clamped. "3m" is the e2e suite's
// SESSION_TTL_TEST_VALUE (Makefile), the shortest TTL any environment
// runs today; refusing it would break session-renewal.spec.ts at boot.
func TestRequireSessionTTL_AtOrAboveMinimum_Accepted(t *testing.T) {
	cases := map[string]time.Duration{
		config.MinSessionTTL.String(): config.MinSessionTTL,
		"3m":                          3 * time.Minute,
		"8h":                          8 * time.Hour,
	}
	for v, want := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv("SESSION_TTL", v)

			got, err := config.Load().RequireSessionTTL()

			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

// TestRequireSessionTTL_Unset_TakesTheDocumentedDefault pins the default
// through the same accessor cmd/api boots with.
func TestRequireSessionTTL_Unset_TakesTheDocumentedDefault(t *testing.T) {
	t.Setenv("SESSION_TTL", "")

	got, err := config.Load().RequireSessionTTL()

	require.NoError(t, err)
	require.Equal(t, config.DefaultSessionTTL, got)
	require.Equal(t, 15*time.Minute, config.DefaultSessionTTL,
		"#838 Task 5 fixed the session lifetime at 15 minutes; changing it is a policy decision, not a refactor")
}

// TestRequireSessionTTL_Unparseable_StillFallsBackToTheDefault pins the
// half of the behaviour this change deliberately leaves alone: a
// MISTYPED value substitutes the documented default (and is logged by
// getenvDuration), and that default then passes the guard. Only an
// explicit, parseable wrong number refuses.
func TestRequireSessionTTL_Unparseable_StillFallsBackToTheDefault(t *testing.T) {
	t.Setenv("SESSION_TTL", "fifteen minutes")

	got, err := config.Load().RequireSessionTTL()

	require.NoError(t, err)
	require.Equal(t, config.DefaultSessionTTL, got)
}

// TestMinSessionTTL_IsBelowTheDefault guards against the minimum ever
// being raised past the default, which would make an unset SESSION_TTL
// refuse to boot everywhere.
func TestMinSessionTTL_IsBelowTheDefault(t *testing.T) {
	require.Less(t, config.MinSessionTTL, config.DefaultSessionTTL)
}

func mustParse(t *testing.T, v string) time.Duration {
	t.Helper()
	d, err := time.ParseDuration(v)
	require.NoError(t, err)
	return d
}
