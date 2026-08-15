package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGIPMinterMintsTokenCarryingTheTenantClaim exercises the real
// Firebase Admin SDK path end to end without any network or emulator:
// given a service-account credential, the SDK signs custom tokens
// locally, so a throwaway RSA key is enough to prove NewGIPMinter wires
// a working minter and that the tenant_id we pass survives into the
// token. That claim is the whole point — it is what a client's next ID
// token inherits, and therefore what decides which hospital the request
// runs in.
//
// This, NewGIPMinter and NewGIPRevoker are the surviving GIP surface
// (see gip.go's package doc): the verifier tests that used to live here
// (principalFromToken's tenant_id/auth_time handling, the emulator guard
// on NewGIPVerifier) were removed with the verifier itself in plan
// Task 3 — their replacements are in zitadel_test.go.
func TestGIPMinterMintsTokenCarryingTheTenantClaim(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	credPath := filepath.Join(t.TempDir(), "service-account.json")
	cred := map[string]string{
		"type":         "service_account",
		"project_id":   "demo-hms",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		"client_email": "hms-test@demo-hms.iam.gserviceaccount.com",
		"client_id":    "1",
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	blob, err := json.Marshal(cred)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, blob, 0o600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credPath)
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "")

	minter, err := NewGIPMinter(context.Background(), "demo-hms", false)
	require.NoError(t, err)

	tenantID := "3fa85f64-5717-4562-b3fc-2c963f66afa6"
	tok, err := minter.CustomTokenWithClaims(context.Background(), "u1",
		map[string]interface{}{"tenant_id": tenantID})
	require.NoError(t, err)

	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3, "a custom token is a signed JWT")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims struct {
		UID    string            `json:"uid"`
		Claims map[string]string `json:"claims"`
	}
	require.NoError(t, json.Unmarshal(payload, &claims))
	require.Equal(t, "u1", claims.UID)
	require.Equal(t, tenantID, claims.Claims["tenant_id"],
		"the target tenant must reach the token, or the switch changes nothing")
}

func TestNewGIPMinterRefusesEmulatorOutsideDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := NewGIPMinter(context.Background(), "demo-hms", false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "FIREBASE_AUTH_EMULATOR_HOST")
}

// TestNewGIPRevokerRefusesEmulatorOutsideDev mirrors the minter guard:
// NewGIPRevoker shares newAuthClient, so a production process with the
// emulator variable accidentally set must refuse to construct a revoker
// for the same reason it must refuse a minter — an unverified token
// could otherwise walk through the revocation watermark this feature
// exists to enforce.
func TestNewGIPRevokerRefusesEmulatorOutsideDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := NewGIPRevoker(context.Background(), "demo-hms", false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "FIREBASE_AUTH_EMULATOR_HOST")
}
