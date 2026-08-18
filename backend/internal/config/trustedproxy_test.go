package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/config"
)

// TestRequireTrustedProxyCIDRs pins the four-way behaviour table from #824
// Task 1: outside dev, an unset/empty TRUSTED_PROXY_CIDRS or one whose every
// entry is malformed must refuse boot, with DISTINCT messages so an
// operator who mistyped their only CIDR is not told the variable is
// missing. The "none" sentinel and a valid CIDR list are always allowed,
// and dev is exempt from the refusal entirely — see
// RequireTrustedProxyCIDRs' doc comment (trustedproxy.go) for why.
func TestRequireTrustedProxyCIDRs(t *testing.T) {
	for _, tc := range []struct {
		name, env, value string
		wantErr          bool
		wantMsgContains  string
	}{
		{"unset outside dev refuses", "production", "", true, "TRUSTED_PROXY_CIDRS is not set"},
		{"empty outside dev refuses", "production", "  ", true, "TRUSTED_PROXY_CIDRS is not set"},
		{"none outside dev is allowed", "production", "none", false, ""},
		{"valid list outside dev is allowed", "production", "10.20.0.0/16", false, ""},
		{"all-malformed outside dev refuses with a DISTINCT message", "production", "not-a-cidr, also-bad", true, "no valid CIDR"},
		{"unset in dev is allowed", "dev", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HELIVANTA_ENV", tc.env)
			t.Setenv("TRUSTED_PROXY_CIDRS", tc.value)
			err := config.Load().RequireTrustedProxyCIDRs()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantMsgContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRequireTrustedProxyCIDRsNoneIsCaseInsensitiveAndTrimmed proves the
// "none" sentinel tolerates surrounding whitespace and mixed case, per its
// doc comment — an operator typing "None" or " none " in a shell-quoted env
// value must not be told the variable is missing or malformed.
func TestRequireTrustedProxyCIDRsNoneIsCaseInsensitiveAndTrimmed(t *testing.T) {
	t.Setenv("HELIVANTA_ENV", "production")
	for _, value := range []string{"none", "None", "NONE", "  none  "} {
		t.Setenv("TRUSTED_PROXY_CIDRS", value)
		require.NoError(t, config.Load().RequireTrustedProxyCIDRs(), "value=%q", value)
	}
}

// TestRequireTrustedProxyCIDRsRejectsOtherSentinels proves "none" is the
// ONLY accepted opt-out — not "off", "false", or any other value that
// might look like one. A guard that accepts a family of near-miss values
// is not meaningfully distinguishable from one with no sentinel at all.
func TestRequireTrustedProxyCIDRsRejectsOtherSentinels(t *testing.T) {
	t.Setenv("HELIVANTA_ENV", "production")
	for _, value := range []string{"off", "false", "no", "disabled"} {
		t.Setenv("TRUSTED_PROXY_CIDRS", value)
		err := config.Load().RequireTrustedProxyCIDRs()
		require.Error(t, err, "value=%q must not be accepted as an opt-out", value)
		require.Contains(t, err.Error(), "no valid CIDR")
	}
}
