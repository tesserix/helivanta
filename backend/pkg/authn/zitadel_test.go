package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// testOIDCProvider is a real OIDC discovery + JWKS server backed by a
// real RSA key, standing in for Zitadel: it serves the same two
// documents oidc.NewProvider fetches from a live Zitadel
// (docs/superpowers/spikes/2026-08-15-zitadel-spike.md P0-2/P0-4), and
// every token it signs is a genuine RS256 JWT. NewZitadelVerifier and
// go-oidc are exercised completely unmodified against it — nothing about
// the verifier under test is mocked, only the token issuer is a local
// stand-in for the network call to a running Zitadel instance the spike
// already proved works identically. A full testcontainers Zitadel would
// additionally need a browser-driven PKCE login (Task 0's recipe) to
// produce a token, which tests the SEEDING path, not the verifier; this
// is the narrower, faster, equally-real check for the OIDC/JWT boundary
// Task 3 owns.
type testOIDCProvider struct {
	issuer string
	key    *rsa.PrivateKey
	kid    string
}

func newTestOIDCProvider(t *testing.T) *testOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	p := &testOIDCProvider{key: key, kid: "test-kid-1"}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p.issuer = srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.issuer,
			"jwks_uri":                              p.issuer + "/oauth/v2/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"authorization_endpoint":                p.issuer + "/oauth/v2/authorize",
			"token_endpoint":                        p.issuer + "/oauth/v2/token",
			"subject_types_supported":               []string{"public"},
			"response_types_supported":              []string{"code"},
		})
	})
	mux.HandleFunc("/oauth/v2/keys", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{"kty": "RSA", "kid": p.kid, "use": "sig", "alg": "RS256", "n": n, "e": e},
			},
		})
	})
	return p
}

type testClaims struct {
	AuthTime int64 `json:"auth_time,omitempty"`
	jwt.RegisteredClaims
}

// sign mints a real RS256 JWT with the provider's key. issuer/audience
// default to the provider's own issuer and clientID when empty, so a
// test only overrides the one thing it means to break.
func (p *testOIDCProvider) sign(t *testing.T, claims testClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = p.kid
	signed, err := tok.SignedString(p.key)
	require.NoError(t, err)
	return signed
}

func validClaims(issuer, clientID, subject string) testClaims {
	now := time.Now().UTC()
	return testClaims{
		AuthTime: now.Add(-10 * time.Second).Unix(), // distinct from iat, like every token the spike observed
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{clientID},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
}

const testClientID = "test-client-id"

func TestZitadelVerifier_ValidTokenVerifies(t *testing.T) {
	p := newTestOIDCProvider(t)
	verifier, err := NewZitadelVerifier(context.Background(), p.issuer, testClientID)
	require.NoError(t, err)

	raw := p.sign(t, validClaims(p.issuer, testClientID, "386320787679739907"))

	principal, err := verifier.Verify(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "386320787679739907", principal.Subject)
	require.Equal(t, "", principal.TenantID, "Zitadel asserts no tenant — see Principal.TenantID's doc comment")
	require.WithinDuration(t, time.Now().Add(-10*time.Second), principal.AuthTime, 2*time.Second)
}

func TestZitadelVerifier_WrongIssuerRefused(t *testing.T) {
	p := newTestOIDCProvider(t)
	verifier, err := NewZitadelVerifier(context.Background(), p.issuer, testClientID)
	require.NoError(t, err)

	claims := validClaims(p.issuer, testClientID, "u1")
	claims.Issuer = "https://not-the-real-issuer.example"
	raw := p.sign(t, claims)

	_, err = verifier.Verify(context.Background(), raw)
	require.Error(t, err)
}

func TestZitadelVerifier_WrongAudienceRefused(t *testing.T) {
	p := newTestOIDCProvider(t)
	verifier, err := NewZitadelVerifier(context.Background(), p.issuer, testClientID)
	require.NoError(t, err)

	raw := p.sign(t, validClaims(p.issuer, "some-other-client-id", "u1"))

	_, err = verifier.Verify(context.Background(), raw)
	require.Error(t, err)
}

func TestZitadelVerifier_ExpiredTokenRefused(t *testing.T) {
	p := newTestOIDCProvider(t)
	verifier, err := NewZitadelVerifier(context.Background(), p.issuer, testClientID)
	require.NoError(t, err)

	claims := validClaims(p.issuer, testClientID, "u1")
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	raw := p.sign(t, claims)

	_, err = verifier.Verify(context.Background(), raw)
	require.Error(t, err)
}

// TestZitadelVerifier_TamperedTokenRefused flips a character in the
// MIDDLE of the payload segment, not the tail of the signature: a raw
// base64url signature's last character can carry unused padding bits
// (2048-bit RSA leaves 4 such bits on a 342-character signature), so
// flipping between 'A' (000000) and 'B' (000001) there can land entirely
// inside that padding — a no-op that decodes to the identical signature
// bytes and would make this test pass or fail depending on which
// character the freshly-signed token happened to end with. Flipping a
// character mid-payload always changes real, signed content, so the
// signature check fails deterministically regardless of what the raw
// token happens to look like.
func TestZitadelVerifier_TamperedTokenRefused(t *testing.T) {
	p := newTestOIDCProvider(t)
	verifier, err := NewZitadelVerifier(context.Background(), p.issuer, testClientID)
	require.NoError(t, err)

	raw := p.sign(t, validClaims(p.issuer, testClientID, "u1"))
	parts := strings.SplitN(raw, ".", 3)
	require.Len(t, parts, 3, "a JWT is header.payload.signature")
	payload := []byte(parts[1])
	mid := len(payload) / 2
	if payload[mid] == 'A' {
		payload[mid] = 'B'
	} else {
		payload[mid] = 'A'
	}
	parts[1] = string(payload)
	tampered := strings.Join(parts, ".")

	_, err = verifier.Verify(context.Background(), tampered)
	require.Error(t, err)
}

// TestZitadelVerifier_NoAuthTimeRefused pins the same contract GIP's
// principalFromToken used to enforce (gip_test.go, now removed with the
// verifier): a token that verifies cleanly but carries no auth_time
// cannot be evaluated against the #781 revocation watermark, so it must
// be refused rather than silently treated as unrevoked.
func TestZitadelVerifier_NoAuthTimeRefused(t *testing.T) {
	p := newTestOIDCProvider(t)
	verifier, err := NewZitadelVerifier(context.Background(), p.issuer, testClientID)
	require.NoError(t, err)

	claims := validClaims(p.issuer, testClientID, "u1")
	claims.AuthTime = 0
	raw := p.sign(t, claims)

	_, err = verifier.Verify(context.Background(), raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoAuthTime), "got %v", err)
}

func TestNewZitadelVerifier_RefusesEmptyIssuer(t *testing.T) {
	_, err := NewZitadelVerifier(context.Background(), "", testClientID)
	require.Error(t, err)
}

func TestNewZitadelVerifier_RefusesEmptyClientID(t *testing.T) {
	p := newTestOIDCProvider(t)
	_, err := NewZitadelVerifier(context.Background(), p.issuer, "")
	require.Error(t, err)
}
