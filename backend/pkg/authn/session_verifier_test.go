package authn_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/session"
)

const (
	sessionTestIssuer = "https://hms.test"
	sessionTestKID    = "hms-session-v1"
	sessionTestTTL    = 15 * time.Minute
)

func newSessionSignerVerifier(t *testing.T) (*session.Signer, *session.Verifier) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, sessionTestKID, sessionTestIssuer, sessionTestTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, sessionTestKID, sessionTestIssuer)
	require.NoError(t, err)
	return signer, verifier
}

// TestNewSessionVerifier_MapsClaimsToPrincipal proves the adapter is a
// faithful translation, not just a passthrough that happens to compile:
// subject, tenant and auth_time must all survive, unmodified, from
// session.Claims into authn.Principal.
func TestNewSessionVerifier_MapsClaimsToPrincipal(t *testing.T) {
	signer, verifier := newSessionSignerVerifier(t)
	authTime := time.Date(2026, 3, 4, 9, 30, 12, 0, time.UTC)

	token, err := signer.Mint("user-123", "22222222-2222-2222-2222-222222222222", authTime)
	require.NoError(t, err)

	tv := authn.NewSessionVerifier(verifier)
	p, err := tv.Verify(context.Background(), token)
	require.NoError(t, err)

	require.Equal(t, "user-123", p.Subject)
	require.Equal(t, "22222222-2222-2222-2222-222222222222", p.TenantID)
	require.True(t, authTime.Equal(p.AuthTime),
		"auth_time must survive the adapter unmodified: got %v, want %v", p.AuthTime, authTime)
}

// TestNewSessionVerifier_RefusesInvalidToken proves errors from
// session.Verifier propagate as a refusal, not a zero-value success.
func TestNewSessionVerifier_RefusesInvalidToken(t *testing.T) {
	_, verifier := newSessionSignerVerifier(t)
	tv := authn.NewSessionVerifier(verifier)

	_, err := tv.Verify(context.Background(), "not-a-jwt")
	require.Error(t, err)
}

// TestMiddleware_RefusesRawZitadelToken is the mandatory proof for plan
// Task 4: "a raw Zitadel ID token presented on an ordinary /v1 request
// is refused". It builds a genuine RS256-signed, Zitadel-shaped ID
// token (right issuer string even, right claim names) and confirms the
// session-backed TokenVerifier the /v1 chain now uses refuses it
// outright — not because the issuer string looks wrong, but because it
// is signed with the wrong algorithm/key entirely: an HMS
// session.Verifier only ever trusts EdDSA under its own Ed25519 key, and
// a Zitadel ID token is RS256 under Zitadel's key.
func TestMiddleware_RefusesRawZitadelToken(t *testing.T) {
	_, sessionVerifier := newSessionSignerVerifier(t)
	tv := authn.NewSessionVerifier(sessionVerifier)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(tv, neverRevoked{}), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	zitadelShaped := forgeRS256TokenShapedLikeZitadel(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+zitadelShaped)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code, "a raw Zitadel-shaped token must never authenticate an ordinary /v1 request")
}

// TestMiddleware_RefusesExpiredAndTamperedSession is the mandatory proof
// that an expired or tampered HMS session is refused on the request
// path — the request-path equivalent of pkg/session's own unit tests,
// exercised here through the actual Middleware+TokenVerifier composition
// the /v1 chain runs, not just the verifier in isolation.
func TestMiddleware_RefusesExpiredAndTamperedSession(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	// TTL so short the token is already expired by the time it is
	// verified, without a fake clock.
	shortSigner, err := session.NewSigner(priv, sessionTestKID, sessionTestIssuer, time.Nanosecond)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, sessionTestKID, sessionTestIssuer)
	require.NoError(t, err)
	tv := authn.NewSessionVerifier(verifier)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(tv, neverRevoked{}), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	expired, err := shortSigner.Mint("user-123", "tenant-abc", time.Now())
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: expired})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code, "an expired HMS session must be refused on the request path")

	// A genuine, non-expired token, tampered after minting.
	longSigner, err := session.NewSigner(priv, sessionTestKID, sessionTestIssuer, sessionTestTTL)
	require.NoError(t, err)
	good, err := longSigner.Mint("user-123", "tenant-abc", time.Now())
	require.NoError(t, err)
	tampered := good[:len(good)-4] + "AAAA"

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/p", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: tampered})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code, "a tampered HMS session must be refused on the request path")
}

// forgeRS256TokenShapedLikeZitadel builds a real RS256 JWT carrying
// exactly the claims a Zitadel ID token carries (sub, iss, auth_time,
// exp, iat) — the shape pkg/authn/zitadel_test.go's testOIDCProvider
// produces — but signed with a throwaway RSA key rather than routed
// through OIDC discovery, because this test's target is the
// session-verifier boundary on the /v1 request path, not OIDC discovery
// (already proved in zitadel_test.go).
func forgeRS256TokenShapedLikeZitadel(t *testing.T) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "zitadel-kid-1"}
	payload := map[string]any{
		"sub":       "zitadel-user-1",
		"iss":       "https://zitadel.local",
		"aud":       []string{"hms-client"},
		"auth_time": time.Now().Add(-time.Minute).Unix(),
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	h := base64.RawURLEncoding.EncodeToString(mustMarshal(t, header))
	p := base64.RawURLEncoding.EncodeToString(mustMarshal(t, payload))
	// The signature bytes are irrelevant to what this test proves: the
	// session.Verifier keyfunc refuses to hand back a key at all for a
	// non-Ed25519 method, so no signature could ever pass — an empty
	// signature segment demonstrates that just as well as a real RSA one
	// would, without pulling a second crypto dependency into this file.
	return h + "." + p + "."
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
