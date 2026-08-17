package platform_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

// alwaysMember is a fixed authz.MembershipChecker used by every router
// test in this file that is not itself about membership — these tests
// exercise Require's permission logic, so membership must be a settled
// "yes" to isolate what each test is actually about.
type alwaysMember struct{}

func (alwaysMember) IsMember(context.Context, string, string) (bool, error) { return true, nil }

// neverMember is the counterpart used by the membership-gate tests
// below.
type neverMember struct{}

func (neverMember) IsMember(context.Context, string, string) (bool, error) { return false, nil }

// principalStub stands in for authn.Middleware, which is already tested
// elsewhere — RequireMembership (mounted by platform.Router.handle)
// needs a principal on the context to know which subject/tenant to ask
// about.
func principalStub() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: "tenant-a"})
		c.Next()
	}
}

func TestNewRouterPanicsOnNilMembershipChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	require.Panics(t, func() { platform.NewRouter(e.Group("/v1"), nil) },
		"a nil MembershipChecker would silently skip the membership gate on every route (#781)")
}

func TestRouterAppliesRequireForDeclaredPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(principalStub())
	// Stand in for authz.Middleware with a set that lacks the permission.
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet("other.thing.read"))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMember{})
	r.GET("/thing", "medicore.visit.read", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/thing", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRouterPublicRouteSkipsThePermissionCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(principalStub())
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet())
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMember{})
	r.GET("/open", authz.Public, func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/open", nil))
	require.Equal(t, http.StatusOK, w.Code)
}

// TestRouterPublicRouteStillRequiresMembership is the router-level half
// of the #781 regression test: a route declaring authz.Public must still
// run RequireMembership, unlike authz.NoTenantMembership. See
// internal/modules/reference's TestPublicRouteRefusesANonMember for the
// full end-to-end version through a real module and harness.
func TestRouterPublicRouteStillRequiresMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(principalStub())
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet())
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), neverMember{})
	r.GET("/open", authz.Public, func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/open", nil))
	require.Equal(t, http.StatusForbidden, w.Code,
		"authz.Public declares no permission, but must still refuse a non-member")
}

// TestRouterNoTenantMembershipSkipsTheMembershipGate proves the narrower
// opt-out actually skips RequireMembership, unlike authz.Public: even a
// MembershipChecker that refuses everyone must not stop this route.
func TestRouterNoTenantMembershipSkipsTheMembershipGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(principalStub())
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet())
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), neverMember{})
	r.GET("/self-service", authz.NoTenantMembership, func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/self-service", nil))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestDeclaredCollectsPermissionsAcrossGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	r := platform.NewRouter(e.Group("/v1"), alwaysMember{})
	g := r.Group("/medicore")
	g.POST("/visits", "medicore.visit.create", func(c *gin.Context) {})
	g.GET("/visits", "medicore.visit.read", func(c *gin.Context) {})
	r.GET("/open", authz.Public, func(c *gin.Context) {})

	var perms []authz.Permission
	for _, dr := range r.Declared() {
		perms = append(perms, dr.Permission)
	}
	require.ElementsMatch(t,
		[]authz.Permission{"medicore.visit.create", "medicore.visit.read", authz.Public},
		perms)
}

// TestDeclaredCarriesMethodAndPath proves Declared() surfaces enough to
// answer "which route" a permission belongs to, not just "which
// permission" — what internal/archtest's NoTenantMembership allowlist
// test needs to pin routes rather than bare permission values.
func TestDeclaredCarriesMethodAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	r := platform.NewRouter(e.Group("/v1"), alwaysMember{})
	g := r.Group("/medicore")
	g.POST("/visits", "medicore.visit.create", func(c *gin.Context) {})

	require.Contains(t, r.Declared(), platform.DeclaredRoute{
		Method: http.MethodPost, Path: "/v1/medicore/visits", Permission: "medicore.visit.create",
	})
}
