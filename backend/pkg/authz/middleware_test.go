package authz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
)

type fakeResolver struct {
	set authz.PermissionSet
	err error
}

func (f fakeResolver) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	return f.set, f.err
}

// principalStub stands in for authn.Middleware, which is already tested.
func principalStub(tenant string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: tenant})
		c.Next()
	}
}

func guarded(t *testing.T, r authz.Resolver, perm authz.Permission) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/v1", principalStub(tenantA), authz.Middleware(r))
	g.GET("/thing", authz.Require(perm), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return e
}

func get(e *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/thing", nil))
	return w
}

func TestRequireAllowsHeldPermission(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet("medicore.visit.read")}
	require.Equal(t, http.StatusOK, get(guarded(t, r, "medicore.visit.read")).Code)
}

func TestRequireDeniesMissingPermissionWith403(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet("medicore.visit.read")}
	w := get(guarded(t, r, "pharmacy.dispense.fulfil"))

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"forbidden"`)
}

func TestNonMemberResolvesEmptyAndIsDenied(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet()}
	require.Equal(t, http.StatusForbidden, get(guarded(t, r, "medicore.visit.read")).Code)
}

func TestPublicRouteAllowedWithEmptySet(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet()}
	require.Equal(t, http.StatusOK, get(guarded(t, r, authz.Public)).Code)
}

// Fail-closed is the single most important property of this package.
func TestResolverErrorFailsClosedWith503(t *testing.T) {
	r := fakeResolver{err: errors.New("openfga unreachable")}
	w := get(guarded(t, r, "medicore.visit.read"))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), `"authz_unavailable"`)
}

func TestResolverErrorFailsClosedEvenOnPublicRoutes(t *testing.T) {
	r := fakeResolver{err: errors.New("openfga unreachable")}
	require.Equal(t, http.StatusServiceUnavailable, get(guarded(t, r, authz.Public)).Code)
}

func TestHandlerNeverRunsWithoutResolvedSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	ran := false
	// No principal set: the middleware must abort before the handler.
	g := e.Group("/v1", authz.Middleware(fakeResolver{set: authz.NewPermissionSet()}))
	g.GET("/thing", authz.Require(authz.Public), func(c *gin.Context) { ran = true })

	require.Equal(t, http.StatusUnauthorized, get(e).Code)
	require.False(t, ran, "handler must not run without a resolved permission set")
}
