package authn_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/hms/pkg/authn"
)

// firebase-admin-go checks FIREBASE_AUTH_EMULATOR_HOST when the client is
// constructed; when it is set, VerifyIDToken skips signature verification
// entirely and trusts the decoded claims. A forged token would be
// accepted. Constructing a verifier must therefore refuse outside dev.
func TestNewGIPVerifierRefusesEmulatorOutsideDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := authn.NewGIPVerifier(context.Background(), "demo-hms", false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "FIREBASE_AUTH_EMULATOR_HOST")
}

func TestNewGIPMinterRefusesEmulatorOutsideDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := authn.NewGIPMinter(context.Background(), "demo-hms", false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "FIREBASE_AUTH_EMULATOR_HOST")
}

// With the emulator explicitly allowed the guard must not fire. The
// constructor may still fail for unrelated reasons in a sandbox, so this
// asserts only that the failure is not the guard.
func TestNewGIPVerifierAllowsEmulatorInDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := authn.NewGIPVerifier(context.Background(), "demo-hms", true)

	if err != nil {
		require.False(t, strings.Contains(err.Error(), "FIREBASE_AUTH_EMULATOR_HOST"),
			"guard fired despite allowEmulator=true: %v", err)
	}
}
