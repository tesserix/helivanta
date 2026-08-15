package config_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/config"
)

// TestSessionSigningKeySeed_Unset_Refuses is the no-key boot refusal
// (plan Task 2, spec D5): an unset SESSION_SIGNING_KEY must refuse
// construction, naming the variable, never fall back to a generated or
// default key.
func TestSessionSigningKeySeed_Unset_Refuses(t *testing.T) {
	t.Setenv("SESSION_SIGNING_KEY", "")
	t.Setenv("HMS_ENV", "production")

	cfg := config.Load()
	_, err := cfg.SessionSigningKeySeed()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrNoSessionSigningKey)
	require.Contains(t, err.Error(), "SESSION_SIGNING_KEY")
}

// TestSessionSigningKeySeed_DevKeyOutsideDev_Refuses mirrors
// NewGIPVerifier's FIREBASE_AUTH_EMULATOR_HOST guard (pkg/authn/gip.go):
// the well-known dev key must be refused unless HMS_ENV=dev, whether
// HMS_ENV is unset (defaults to production, per TestEnvDefaultsToProduction)
// or set to "production" explicitly.
func TestSessionSigningKeySeed_DevKeyOutsideDev_Refuses(t *testing.T) {
	for _, env := range []string{"", "production", "staging"} {
		t.Run("HMS_ENV="+env, func(t *testing.T) {
			t.Setenv("SESSION_SIGNING_KEY", config.DevSessionSigningKey)
			t.Setenv("HMS_ENV", env)

			cfg := config.Load()
			_, err := cfg.SessionSigningKeySeed()

			require.Error(t, err)
			require.ErrorIs(t, err, config.ErrDevSessionSigningKeyOutsideDev)
			require.Contains(t, err.Error(), "SESSION_SIGNING_KEY")
		})
	}
}

// TestSessionSigningKeySeed_DevKeyInDev_Accepted proves the guard above
// is a guard and not an unconditional refusal: the same well-known dev
// key must decode successfully to a real 32-byte Ed25519 seed when
// HMS_ENV=dev.
func TestSessionSigningKeySeed_DevKeyInDev_Accepted(t *testing.T) {
	t.Setenv("SESSION_SIGNING_KEY", config.DevSessionSigningKey)
	t.Setenv("HMS_ENV", "dev")

	cfg := config.Load()
	seed, err := cfg.SessionSigningKeySeed()

	require.NoError(t, err)
	require.Len(t, seed, ed25519.SeedSize)

	// The seed must be usable to derive a real signing key, not just the
	// right number of bytes — proves this is a genuine Ed25519 seed, not
	// 32 arbitrary bytes that happen to pass a length check.
	priv := ed25519.NewKeyFromSeed(seed)
	require.Len(t, priv, ed25519.PrivateKeySize)
}

// TestSessionSigningKeySeed_Malformed_Refuses covers every way a
// SESSION_SIGNING_KEY value can be present but not a real Ed25519 seed:
// not base64 at all, and base64 that decodes to the wrong length. Both
// must refuse outright — a half-parsed key must never yield a
// working-looking Signer.
func TestSessionSigningKeySeed_Malformed_Refuses(t *testing.T) {
	tooShort := base64.StdEncoding.EncodeToString(make([]byte, 16))
	tooLong := base64.StdEncoding.EncodeToString(make([]byte, 64))

	for name, value := range map[string]string{
		"not base64 at all": "not-valid-base64!!!",
		"decodes too short": tooShort,
		"decodes too long":  tooLong,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SESSION_SIGNING_KEY", value)
			t.Setenv("HMS_ENV", "dev")

			cfg := config.Load()
			_, err := cfg.SessionSigningKeySeed()

			require.Error(t, err)
			require.ErrorIs(t, err, config.ErrMalformedSessionSigningKey)
		})
	}
}

// TestSessionSigningKeySeed_BlankAfterTrimming_TreatedAsUnset proves a
// whitespace-only value (a plausible copy-paste accident from a
// Makefile/compose env block) is treated the same as fully unset —
// ErrNoSessionSigningKey, not ErrMalformedSessionSigningKey — rather
// than surviving TrimSpace into an empty-but-"present" value that base64
// happily decodes to zero bytes (which would then fail the length check
// with a more confusing message).
func TestSessionSigningKeySeed_BlankAfterTrimming_TreatedAsUnset(t *testing.T) {
	t.Setenv("SESSION_SIGNING_KEY", "   ")
	t.Setenv("HMS_ENV", "production")

	cfg := config.Load()
	_, err := cfg.SessionSigningKeySeed()

	require.Error(t, err)
	require.ErrorIs(t, err, config.ErrNoSessionSigningKey)
}

// TestSessionSigningKeySeed_RealKeyInProduction_Accepted proves the
// refusal is specific to the dev key and unset values, not to running
// outside dev in general — a real, properly-provisioned production key
// must be accepted.
func TestSessionSigningKeySeed_RealKeyInProduction_Accepted(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	seedValue := base64.StdEncoding.EncodeToString(priv.Seed())

	t.Setenv("SESSION_SIGNING_KEY", seedValue)
	t.Setenv("HMS_ENV", "production")

	cfg := config.Load()
	seed, err := cfg.SessionSigningKeySeed()

	require.NoError(t, err)
	require.Equal(t, priv.Seed(), ed25519.NewKeyFromSeed(seed).Seed())
}
