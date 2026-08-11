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

	"firebase.google.com/go/v4/auth"
	"github.com/stretchr/testify/require"
)

func TestPrincipalFromToken_StringTenantID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		},
	}
	p, err := principalFromToken(tok)
	require.NoError(t, err)
	require.Equal(t, "u1", p.Subject)
	require.Equal(t, "3fa85f64-5717-4562-b3fc-2c963f66afa6", p.TenantID)
}

func TestPrincipalFromToken_CanonicalizesMixedCaseUUID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": "AbC12345-5717-4562-B3fc-2C963f66aFA6",
		},
	}
	p, err := principalFromToken(tok)
	require.NoError(t, err)
	require.Equal(t, "abc12345-5717-4562-b3fc-2c963f66afa6", p.TenantID)
}

func TestPrincipalFromToken_NonUUIDTenantID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": "not-a-uuid",
		},
	}
	_, err := principalFromToken(tok)
	require.ErrorIs(t, err, ErrNoTenantClaim)
}

func TestPrincipalFromToken_MissingTenantID(t *testing.T) {
	tok := &auth.Token{
		UID:    "u1",
		Claims: map[string]interface{}{},
	}
	_, err := principalFromToken(tok)
	require.ErrorIs(t, err, ErrNoTenantClaim)
}

func TestPrincipalFromToken_NonStringTenantID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": 42,
		},
	}
	_, err := principalFromToken(tok)
	require.ErrorIs(t, err, ErrNoTenantClaim)
}

// TestGIPMinterMintsTokenCarryingTheTenantClaim exercises the real
// Firebase Admin SDK path end to end without any network or emulator:
// given a service-account credential, the SDK signs custom tokens
// locally, so a throwaway RSA key is enough to prove NewGIPMinter wires
// a working minter and that the tenant_id we pass survives into the
// token. That claim is the whole point — it is what a client's next ID
// token inherits, and therefore what decides which hospital the request
// runs in.
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

	minter, err := NewGIPMinter(context.Background(), "demo-hms")
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
