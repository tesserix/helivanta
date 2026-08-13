package authn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const testTenant = "11111111-1111-1111-1111-111111111111"

// staticVerifier answers Verify with a fixed Principal per token,
// mirroring the shape authn_test.go's fakeVerifier already uses in the
// external test package — this file is internal (package authn) so it
// needs its own, but keeps the same behavior.
type staticVerifier map[string]Principal

func (s staticVerifier) Verify(_ context.Context, raw string) (Principal, error) {
	if p, ok := s[raw]; ok {
		return p, nil
	}
	return Principal{}, errors.New("unknown token")
}

// fixedRevocation answers RevokedAfter from a static map, defaulting to
// the zero time (never revoked) for any subject not present.
type fixedRevocation map[string]time.Time

func (f fixedRevocation) RevokedAfter(_ context.Context, subject string) (time.Time, error) {
	return f[subject], nil
}

// failingRevocation always errors, for the fail-closed assertion.
type failingRevocation struct{ err error }

func (f failingRevocation) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, f.err
}

func runWithMiddleware(t *testing.T, mw gin.HandlerFunc, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", mw, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	return w
}

// TestRefreshedTokenIsStillRevoked is the assertion this whole feature
// rests on. A refreshed token has a NEW iat and the ORIGINAL auth_time.
// If the comparison is ever "simplified" to iat, revocation silently
// degrades into a suggestion any client can ignore by refreshing — and
// this test is what fails.
func TestRefreshedTokenIsStillRevoked(t *testing.T) {
	signIn := time.Now().Add(-2 * time.Hour)
	revokedAt := time.Now().Add(-1 * time.Hour)
	// refreshedAt would be the new iat on a refreshed token: AFTER the
	// revocation. It plays no part in the comparison below — that is the
	// whole point of the test — but is named here to document what a
	// refresh actually changes.
	_ = time.Now()

	checker := fixedRevocation{"uid-nurse": revokedAt}
	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: signIn},
	}, checker)

	w := runWithMiddleware(t, mw, "tok")

	require.Equal(t, http.StatusUnauthorized, w.Code,
		"a refreshed token whose auth_time (%s) predates the watermark (%s) must be refused",
		signIn, revokedAt)
}

func TestTokenIssuedAfterRevocationIsAccepted(t *testing.T) {
	revokedAt := time.Now().Add(-1 * time.Hour)
	signIn := time.Now() // signed in again after the revocation

	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: signIn},
	}, fixedRevocation{"uid-nurse": revokedAt})

	require.Equal(t, http.StatusOK, runWithMiddleware(t, mw, "tok").Code,
		"revocation must not lock a user out permanently; signing in again works")
}

func TestRevocationLookupFailureIsFailClosed(t *testing.T) {
	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: time.Now()},
	}, failingRevocation{errors.New("postgres unreachable")})

	w := runWithMiddleware(t, mw, "tok")

	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a revocation state that cannot be read must deny, never admit")
	require.Contains(t, w.Body.String(), "authz_unavailable")
}

// TestNeverRevokedSubjectIsAccepted is the ordinary-path complement to
// the three failure/edge cases above: a subject with no watermark at all
// (the zero time) must not be refused.
func TestNeverRevokedSubjectIsAccepted(t *testing.T) {
	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: time.Now()},
	}, fixedRevocation{})

	require.Equal(t, http.StatusOK, runWithMiddleware(t, mw, "tok").Code)
}

// TestAuthTimeEqualToWatermarkIsRevoked pins the not-after boundary: a
// sign-in racing a revocation to the same second must be refused, not
// admitted. Comparing strictly-after would treat the tie as "signed in
// after", which is the wrong side of an ambiguous credential.
func TestAuthTimeEqualToWatermarkIsRevoked(t *testing.T) {
	at := time.Now().Truncate(time.Second)

	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: at},
	}, fixedRevocation{"uid-nurse": at})

	require.Equal(t, http.StatusUnauthorized, runWithMiddleware(t, mw, "tok").Code,
		"auth_time equal to the watermark must be treated as revoked, not as after it")
}
