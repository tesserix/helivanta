package authn_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/authn"
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

func router(v authn.TokenVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(v), func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		c.JSON(http.StatusOK, p)
	})
	return r
}

func TestMiddlewareAcceptsBearer(t *testing.T) {
	r := router(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "t1"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"t1"`)
}

func TestMiddlewareAcceptsSessionCookie(t *testing.T) {
	r := router(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "t1"}})
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
	r.GET("/p", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "11111111-1111-1111-1111-111111111111"}}), func(c *gin.Context) {
		_, tenantID, ok := authn.TenantPrincipal(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, gin.H{"tenant": tenantID.String()})
	})
	r.GET("/bad", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "not-a-uuid"}}), func(c *gin.Context) {
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
