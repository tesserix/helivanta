package platform_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authz"
)

func TestRouterAppliesRequireForDeclaredPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	// Stand in for authz.Middleware with a set that lacks the permission.
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet("other.thing.read"))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"))
	r.GET("/thing", "medicore.visit.read", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/thing", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRouterPublicRouteSkipsTheCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet())
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"))
	r.GET("/open", authz.Public, func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/open", nil))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestDeclaredCollectsPermissionsAcrossGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	r := platform.NewRouter(e.Group("/v1"))
	g := r.Group("/medicore")
	g.POST("/visits", "medicore.visit.create", func(c *gin.Context) {})
	g.GET("/visits", "medicore.visit.read", func(c *gin.Context) {})
	r.GET("/open", authz.Public, func(c *gin.Context) {})

	require.ElementsMatch(t,
		[]authz.Permission{"medicore.visit.create", "medicore.visit.read", authz.Public},
		r.Declared())
}
