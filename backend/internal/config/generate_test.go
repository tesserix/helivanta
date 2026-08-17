package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	// NOTE the module path: it is hms/internal/config, NOT
	// hms/backend/internal/config — the go.mod lives in backend/ and the
	// module is named without that segment. Match signingkey_test.go.
	"github.com/tesserix/hms/internal/config"
)

// The generator's output must be accepted by the REAL validator, not by
// a reimplementation of its rules. A test that independently re-derived
// "base64, 32 bytes" would keep passing after SessionSigningKeySeed
// diverged from it — testing a replica of the wiring instead of the
// wiring.
func TestGenerateSessionSigningKey_IsAcceptedByTheRealValidator(t *testing.T) {
	raw, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)

	cfg := config.Config{Env: "production", SessionSigningKey: raw}
	seed, err := cfg.SessionSigningKeySeed()
	require.NoError(t, err, "generated key must be accepted outside dev")
	require.Len(t, seed, 32)
}

// A generator that returned a constant would pass the test above.
func TestGenerateSessionSigningKey_IsNotConstant(t *testing.T) {
	a, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)
	b, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)
	require.NotEqual(t, a, b, "each call must produce fresh entropy")
}

// It must never emit the committed dev key, which SessionSigningKeySeed
// refuses outside dev — a generator that did would produce a value that
// works on a developer machine and refuses to boot in production.
func TestGenerateSessionSigningKey_IsNotTheDevKey(t *testing.T) {
	raw, err := config.GenerateSessionSigningKey()
	require.NoError(t, err)
	require.NotEqual(t, config.DevSessionSigningKey, raw)
}
