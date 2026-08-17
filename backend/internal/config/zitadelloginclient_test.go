package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/config"
)

// TestRequireZitadelLoginClientToken_Unset_Refuses is the fail-fast boot
// refusal this file mirrors from SessionSigningKeySeed (review Finding
// 3): an unset ZITADEL_LOGIN_CLIENT_TOKEN must refuse, naming the
// variable, rather than let cmd/api boot with login silently unusable.
func TestRequireZitadelLoginClientToken_Unset_Refuses(t *testing.T) {
	t.Setenv("ZITADEL_LOGIN_CLIENT_TOKEN", "")

	cfg := config.Load()
	_, err := cfg.RequireZitadelLoginClientToken()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrNoZitadelLoginClientToken)
	require.Contains(t, err.Error(), "ZITADEL_LOGIN_CLIENT_TOKEN")
}

// TestRequireZitadelLoginClientToken_WhitespaceOnly_Refuses proves a
// whitespace-only value (a plausible copy-paste accident from a
// Makefile/compose env block, same failure mode
// TestSessionSigningKeySeed_BlankAfterTrimming_TreatedAsUnset covers for
// the signing key) is treated the same as fully unset, not accepted as a
// present-but-garbled token.
func TestRequireZitadelLoginClientToken_WhitespaceOnly_Refuses(t *testing.T) {
	t.Setenv("ZITADEL_LOGIN_CLIENT_TOKEN", "   ")

	cfg := config.Load()
	_, err := cfg.RequireZitadelLoginClientToken()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrNoZitadelLoginClientToken)
}

// TestRequireZitadelLoginClientToken_Present_Accepted proves the refusal
// is specific to empty/whitespace values, not an unconditional refusal —
// a real PAT must be accepted and returned trimmed.
func TestRequireZitadelLoginClientToken_Present_Accepted(t *testing.T) {
	t.Setenv("ZITADEL_LOGIN_CLIENT_TOKEN", "  a-real-looking-pat-value  ")

	cfg := config.Load()
	got, err := cfg.RequireZitadelLoginClientToken()

	require.NoError(t, err)
	require.Equal(t, "a-real-looking-pat-value", got)
}

// TestRequireZitadelLoginClientToken_ErrorNeverContainsTheTokenValue is
// the review's explicit ask: this is the most privileged credential HMS
// holds (ZitadelLoginClientToken's doc comment), and it must never
// appear in a log line or an error message. RequireZitadelLoginClientToken
// has exactly one refusal branch (empty-after-trim), so there is no
// scenario where a REAL secret value is both set and echoed into an
// error — this test pins that structurally: whatever value was actually
// set (including one that merely LOOKS like whitespace padding around a
// real secret) never appears verbatim in the returned error text, for
// every input this function refuses.
func TestRequireZitadelLoginClientToken_ErrorNeverContainsTheTokenValue(t *testing.T) {
	for name, value := range map[string]string{
		"empty":                    "",
		"whitespace only":          "   \t  ",
		"whitespace-padded secret": "   should-never-appear-in-error-text   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ZITADEL_LOGIN_CLIENT_TOKEN", value)
			cfg := config.Load()
			got, err := cfg.RequireZitadelLoginClientToken()
			if err == nil {
				// The trimmed, non-empty case is accepted (proven by
				// Present_Accepted above) — nothing to check for
				// leakage here since there is no error text at all.
				require.NotEmpty(t, got)
				return
			}
			if value == "" {
				// Nothing was ever set — an empty string trivially
				// "contains" no leak to check for; NotContains against ""
				// would fail on every non-empty string, not prove anything.
				return
			}
			require.NotContains(t, err.Error(), value)
		})
	}
}
