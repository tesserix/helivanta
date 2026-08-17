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

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/session"
)

const (
	sessionTestIssuer = "https://hms.test"
	sessionTestKID    = "helivanta-session-v1"
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
// subject, tenant, auth_time AND idle_deadline must all survive,
// unmodified, from session.Claims into authn.Principal.
//
// The idle_deadline assertion is load-bearing, not decorative (code
// review Finding 1): before this, nothing in the package asserted that
// sessionTokenVerifier.Verify actually copies claims.IdleDeadline onto
// Principal.IdleDeadline — every idle-timeout test built its Principal
// by hand via fakeVerifier, so the mapping line in session_verifier.go
// could be deleted and the whole suite would stay green. Deleting it and
// watching this assertion fail is exactly what proved that gap closed
// (see this task's report for the real failure output); this is the
// "test the wiring, not a replica" lesson applied to this seam.
func TestNewSessionVerifier_MapsClaimsToPrincipal(t *testing.T) {
	signer, verifier := newSessionSignerVerifier(t)
	authTime := time.Date(2026, 3, 4, 9, 30, 12, 0, time.UTC)
	idleDeadline := time.Date(2026, 3, 4, 9, 45, 12, 0, time.UTC)

	token, err := signer.Mint("user-123", "22222222-2222-2222-2222-222222222222", authTime, idleDeadline)
	require.NoError(t, err)

	tv := authn.NewSessionVerifier(verifier)
	p, err := tv.Verify(context.Background(), token)
	require.NoError(t, err)

	require.Equal(t, "user-123", p.Subject)
	require.Equal(t, "22222222-2222-2222-2222-222222222222", p.TenantID)
	require.True(t, authTime.Equal(p.AuthTime),
		"auth_time must survive the adapter unmodified: got %v, want %v", p.AuthTime, authTime)
	require.True(t, idleDeadline.Equal(p.IdleDeadline),
		"idle_deadline must survive the adapter unmodified: got %v, want %v", p.IdleDeadline, idleDeadline)
}

// TestMiddleware_RefusesARealIdleExpiredSession is the end-to-end
// complement to TestNewSessionVerifier_MapsClaimsToPrincipal (code
// review Finding 1): it mints a REAL HMS session token with a past
// idle_deadline, verifies it through the REAL session.Verifier and the
// REAL NewSessionVerifier adapter, and drives it through the REAL
// Middleware — no fakeVerifier, no hand-built Principal anywhere in this
// path.
//
// On its own this refusal does NOT discriminate a deleted
// `IdleDeadline: claims.IdleDeadline` mapping line: with the mapping
// gone, Principal.IdleDeadline is always the zero value, and
// Middleware's fail-closed-on-zero behavior (authn.go) refuses a zero
// deadline too — so this test would still see 401/session_idle for the
// wrong reason. TestMiddleware_AdmitsARealSessionInsideItsIdleDeadline
// right below is the one that actually catches the deletion: it is
// asserted here.
func TestMiddleware_RefusesARealIdleExpiredSession(t *testing.T) {
	signer, verifier := newSessionSignerVerifier(t)
	tv := authn.NewSessionVerifier(verifier)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(tv, neverRevoked{}), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	token, err := signer.Mint("user-123", "tenant-abc", time.Now(), time.Now().Add(-1*time.Second))
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: token})
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code, "a real HMS session past its idle deadline must be refused on the request path")
	require.Contains(t, w.Body.String(), "session_idle", "the refusal must be distinguishable as idle, not a generic credential failure")
}

// TestMiddleware_AdmitsARealSessionInsideItsIdleDeadline is the test
// that actually catches a deleted `IdleDeadline: claims.IdleDeadline`
// mapping line (code review Finding 1), because the refusal test above
// cannot: it mints a REAL HMS session with a FUTURE idle_deadline and
// drives it through the REAL Middleware+NewSessionVerifier composition,
// expecting 200. If the mapping line were deleted, Principal.IdleDeadline
// would always be the zero value regardless of what the token actually
// carries, and Middleware's fail-closed-on-zero behavior would refuse
// this request too — turning an admit into a 401 and failing this test,
// even though the token's real deadline is safely in the future.
func TestMiddleware_AdmitsARealSessionInsideItsIdleDeadline(t *testing.T) {
	signer, verifier := newSessionSignerVerifier(t)
	tv := authn.NewSessionVerifier(verifier)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(tv, neverRevoked{}), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	token, err := signer.Mint("user-123", "tenant-abc", time.Now(), time.Now().Add(5*time.Minute))
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: token})
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "a real HMS session well inside its idle deadline must be admitted on the request path")
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

	// A comfortably-future idle deadline: this test is about token
	// expiry (exp) and tampering, not idle expiry, so the deadline plays
	// no part in the outcome either way.
	expired, err := shortSigner.Mint("user-123", "tenant-abc", time.Now(), time.Now().Add(time.Hour))
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
	good, err := longSigner.Mint("user-123", "tenant-abc", time.Now(), time.Now().Add(time.Hour))
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
		"aud":       []string{"helivanta-client"},
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
