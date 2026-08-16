package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/config"
)

// TestRequireDistinctHostedLoginOrigin_SameOrigin_Refuses is Finding 5's
// core claim: a hosted-login URL misconfigured to share an origin with
// HMS's own frontend must refuse to boot, because that origin collision
// is what turns every MFA handoff into an infinite loop with no error
// anywhere (see the function's doc comment for the mechanism). Different
// paths on the identical scheme+host still count as the same origin.
func TestRequireDistinctHostedLoginOrigin_SameOrigin_Refuses(t *testing.T) {
	t.Setenv("ZITADEL_HOSTED_LOGIN_URL", "http://localhost:4301/login")
	t.Setenv("HMS_WEB_ORIGIN", "http://localhost:4301")

	cfg := config.Load()
	err := cfg.RequireDistinctHostedLoginOrigin()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrHostedLoginOriginMatchesWebOrigin)
	require.Contains(t, err.Error(), "ZITADEL_HOSTED_LOGIN_URL")
	require.Contains(t, err.Error(), "HMS_WEB_ORIGIN")
}

// TestRequireDistinctHostedLoginOrigin_ExactSameURL_Refuses covers the
// most literal misconfiguration: ZITADEL_HOSTED_LOGIN_URL copy-pasted as
// HMS's own login page URL, path and all.
func TestRequireDistinctHostedLoginOrigin_ExactSameURL_Refuses(t *testing.T) {
	t.Setenv("ZITADEL_HOSTED_LOGIN_URL", "http://localhost:4301/login")
	t.Setenv("HMS_WEB_ORIGIN", "http://localhost:4301/login")

	cfg := config.Load()
	err := cfg.RequireDistinctHostedLoginOrigin()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrHostedLoginOriginMatchesWebOrigin)
}

// TestRequireDistinctHostedLoginOrigin_DevDefaults_Accepted proves the
// guard does not fight the dev stack's own defaults: Zitadel's hosted
// login (localhost:20080) and HMS's own frontend (localhost:4301) are
// different origins out of the box, with nothing overridden — but ONLY
// inside HMS_ENV=dev, which this test sets explicitly rather than relying
// on Load()'s own "unset defaults to production" behaviour (proven
// separately by TestRequireDistinctHostedLoginOrigin_UnsetOutsideDev_Refuses
// immediately below, which is the review finding this pair exists to
// pin both directions of).
func TestRequireDistinctHostedLoginOrigin_DevDefaults_Accepted(t *testing.T) {
	t.Setenv("HMS_ENV", "dev")

	cfg := config.Load()
	err := cfg.RequireDistinctHostedLoginOrigin()

	require.NoError(t, err)
}

// TestRequireDistinctHostedLoginOrigin_UnsetOutsideDev_Refuses is
// Finding 6's core claim: an EARLIER version of this file let
// HMS_WEB_ORIGIN default to DevHMSWebOrigin unconditionally, which made
// the guard inert in production — an unset HMS_WEB_ORIGIN there compared
// the real ZITADEL_HOSTED_LOGIN_URL against "http://localhost:4301",
// found no collision, and booted, leaving the redirect loop this guard
// exists to make unrepresentable fully possible with nothing anywhere
// reporting it. Covers every non-dev value HMS_ENV can plausibly hold,
// including simply being unset (Load()'s own default, per
// TestEnvDefaultsToProduction in config_test.go) — mirroring the same
// table SessionSigningKeySeed's dev-key guard already runs
// (signingkey_test.go) for the identical class of check.
func TestRequireDistinctHostedLoginOrigin_UnsetOutsideDev_Refuses(t *testing.T) {
	for _, env := range []string{"", "production", "staging"} {
		t.Run("HMS_ENV="+env, func(t *testing.T) {
			t.Setenv("HMS_ENV", env)
			t.Setenv("HMS_WEB_ORIGIN", "")
			// A real-looking hosted-login URL, so the ONLY thing this
			// test can be failing on is the missing HMS_WEB_ORIGIN, not
			// an incidental origin collision with some other default.
			t.Setenv("ZITADEL_HOSTED_LOGIN_URL", "https://auth.tesserix.app/ui/v2/login")

			cfg := config.Load()
			err := cfg.RequireDistinctHostedLoginOrigin()

			require.Error(t, err)
			require.ErrorIs(t, err, config.ErrNoHMSWebOrigin)
			require.Contains(t, err.Error(), "HMS_WEB_ORIGIN")
		})
	}
}

// TestRequireDistinctHostedLoginOrigin_DifferentHost_Accepted proves the
// guard is not simply refusing everything: two URLs that plainly differ
// (different host, same scheme, same port) must be accepted, since the
// point is to catch a genuine collision, not to reject valid
// configuration.
func TestRequireDistinctHostedLoginOrigin_DifferentHost_Accepted(t *testing.T) {
	t.Setenv("ZITADEL_HOSTED_LOGIN_URL", "https://auth.tesserix.app/ui/v2/login")
	t.Setenv("HMS_WEB_ORIGIN", "https://hms.tesserix.app")

	cfg := config.Load()
	err := cfg.RequireDistinctHostedLoginOrigin()

	require.NoError(t, err)
}

// TestRequireDistinctHostedLoginOrigin_SameHostDifferentScheme_Accepted
// proves the comparison is scheme-sensitive: http and https on the same
// host are not the same origin (a browser sent to one never lands on the
// other's server for this purpose), so this must not false-positive.
func TestRequireDistinctHostedLoginOrigin_SameHostDifferentScheme_Accepted(t *testing.T) {
	t.Setenv("ZITADEL_HOSTED_LOGIN_URL", "https://hms.tesserix.app/ui/v2/login")
	t.Setenv("HMS_WEB_ORIGIN", "http://hms.tesserix.app")

	cfg := config.Load()
	err := cfg.RequireDistinctHostedLoginOrigin()

	require.NoError(t, err)
}

// TestRequireDistinctHostedLoginOrigin_MalformedURL_Refuses proves an
// unparseable URL on either side is a refusal too, not a silent
// same-origin-not-detected pass-through — a malformed URL cannot be
// proven distinct from anything, so it must not be treated as safe.
func TestRequireDistinctHostedLoginOrigin_MalformedURL_Refuses(t *testing.T) {
	t.Setenv("ZITADEL_HOSTED_LOGIN_URL", "http://[::1]:namedport/login")
	t.Setenv("HMS_WEB_ORIGIN", "http://localhost:4301")

	cfg := config.Load()
	err := cfg.RequireDistinctHostedLoginOrigin()

	require.Error(t, err)
	require.NotErrorIs(t, err, config.ErrHostedLoginOriginMatchesWebOrigin)
	require.Contains(t, err.Error(), "ZITADEL_HOSTED_LOGIN_URL")
}
