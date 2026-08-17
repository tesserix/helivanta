package authn_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authn"
)

type fakeVerifier struct {
	p   authn.Principal
	err error
}

func (f fakeVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if raw == "good" {
		return f.p, nil
	}
	return authn.Principal{}, errors.New("bad token")
}

// neverRevoked answers RevokedAfter with the zero time unconditionally.
// This file's tests are about token verification, not revocation, so
// they get the permissive default rather than each rolling their own.
type neverRevoked struct{}

func (neverRevoked) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

func router(v authn.TokenVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(v, neverRevoked{}), func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		c.JSON(http.StatusOK, p)
	})
	return r
}

func TestMiddlewareAcceptsBearer(t *testing.T) {
	r := router(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "t1", IdleDeadline: time.Now().Add(time.Hour)}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"t1"`)
}

func TestMiddlewareAcceptsSessionCookie(t *testing.T) {
	r := router(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "t1", IdleDeadline: time.Now().Add(time.Hour)}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: "good"})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareRejectsMissingAndBadTokens(t *testing.T) {
	r := router(fakeVerifier{})
	for _, tc := range []func(*http.Request){
		func(req *http.Request) {},
		func(req *http.Request) { req.Header.Set("Authorization", "Bearer evil") },
		func(req *http.Request) { req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: "evil"}) },
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/p", nil)
		tc(req)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
}

func TestTenantPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "11111111-1111-1111-1111-111111111111", IdleDeadline: time.Now().Add(time.Hour)}}, neverRevoked{}), func(c *gin.Context) {
		_, tenantID, ok := authn.TenantPrincipal(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, gin.H{"tenant": tenantID.String()})
	})
	r.GET("/bad", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "not-a-uuid", IdleDeadline: time.Now().Add(time.Hour)}}, neverRevoked{}), func(c *gin.Context) {
		if _, _, ok := authn.TenantPrincipal(c); !ok {
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "11111111")

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/bad", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "unauthenticated")
}

// TestTenantPrincipalRefusesEmptyTenant pins the specific shape a
// Zitadel-verified Principal takes today (pkg/authn/zitadel.go — no
// tenant claim exists on a Zitadel token, so TenantID is left empty
// rather than fabricated, see Principal.TenantID's doc comment): an
// empty TenantID must fail exactly like a malformed one, not be treated
// as "no tenant scoping required" anywhere downstream.
//
// IdleDeadline is set to a real future value deliberately: a genuine
// Zitadel-verified Principal also carries a zero IdleDeadline today, but
// that path never reaches Middleware in production (only the session
// verifier is mounted on V1Chain — see session_verifier.go), and this
// test's one concern is the empty-tenant refusal, not idle handling.
// Zero-deadline refusal has its own dedicated test.
func TestTenantPrincipalRefusesEmptyTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "", IdleDeadline: time.Now().Add(time.Hour)}}, neverRevoked{}), func(c *gin.Context) {
		if _, _, ok := authn.TenantPrincipal(c); !ok {
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "unauthenticated")
}

// principalWithIdleDeadline builds a Principal that is otherwise valid
// (real subject, real tenant, real auth_time) but carries deadline as
// its IdleDeadline, so the idle-timeout tests below exercise nothing
// but that one field.
func principalWithIdleDeadline(deadline time.Time) authn.Principal {
	return authn.Principal{
		Subject:      "u1",
		TenantID:     "t1",
		AuthTime:     time.Now(),
		IdleDeadline: deadline,
	}
}

// doRequest drives a single authenticated GET through Middleware with p
// as the Principal a "good" bearer token resolves to, and returns the
// recorded response.
func doRequest(t *testing.T, p authn.Principal) *httptest.ResponseRecorder {
	t.Helper()
	r := router(fakeVerifier{p: p})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	return w
}

// TestMiddlewareRefusesASessionPastItsIdleDeadline is the mandatory
// proof for #848/spec D2: a session whose idle deadline has already
// passed must never reach a handler, and the refusal must be
// distinguishable ("session_idle") from a generic credential failure —
// see spec D6 for why the frontend needs to tell the two apart.
func TestMiddlewareRefusesASessionPastItsIdleDeadline(t *testing.T) {
	rec := doRequest(t, principalWithIdleDeadline(time.Now().Add(-1*time.Second)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a session past its idle deadline", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "session_idle") {
		t.Errorf("body = %s, want a distinguishable idle reason", rec.Body.String())
	}
}

// TestMiddlewareAdmitsASessionInsideItsIdleDeadline proves the check
// above does not become a false positive for a session that is well
// within its idle window.
func TestMiddlewareAdmitsASessionInsideItsIdleDeadline(t *testing.T) {
	rec := doRequest(t, principalWithIdleDeadline(time.Now().Add(5*time.Minute)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 well inside the idle window", rec.Code)
	}
}

// TestMiddlewareRefusesASessionExactlyAtItsIdleDeadline pins the
// not-before boundary directly (code review minor #3): a deadline
// captured immediately before the request is refused, not admitted,
// because time.Now() inside Middleware can only be equal to or later
// than a timestamp captured before it — this is the case most likely to
// be "simplified" later by someone who misreads !Before(deadline) as
// After(deadline).
func TestMiddlewareRefusesASessionExactlyAtItsIdleDeadline(t *testing.T) {
	deadline := time.Now()
	rec := doRequest(t, principalWithIdleDeadline(deadline))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a session exactly at its idle deadline", rec.Code)
	}
}

// TestMiddlewareRefusesAZeroIdleDeadline pins code review Finding 2: a
// zero IdleDeadline is refused exactly like an already-past one, never
// read as "no limit". session.Verifier already refuses to hand back a
// Claims with no idle_deadline, so a Principal minted from a real HMS
// session can never carry a zero value here — this test is what keeps
// that true even if that guarantee is ever weakened or a different
// TokenVerifier is mounted without it: a zero IdleDeadline must never
// become a class of session exempt from this control.
func TestMiddlewareRefusesAZeroIdleDeadline(t *testing.T) {
	rec := doRequest(t, authn.Principal{Subject: "u1", TenantID: "t1", AuthTime: time.Now()})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a zero idle deadline", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "session_idle") {
		t.Errorf("body = %s, want a distinguishable idle reason", rec.Body.String())
	}
}

func TestPrincipalFromWhenKeyNotInContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		require.False(t, ok)
		require.Equal(t, authn.Principal{}, p)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestPrincipalFromWhenWrongType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", func(c *gin.Context) {
		c.Set("authn.principal", "not a principal")
		p, ok := authn.PrincipalFrom(c)
		require.False(t, ok)
		require.Equal(t, authn.Principal{}, p)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestTenantPrincipalWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", func(c *gin.Context) {
		_, _, ok := authn.TenantPrincipal(c)
		require.False(t, ok)
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "missing principal")
}
