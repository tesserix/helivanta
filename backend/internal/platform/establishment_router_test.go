package platform_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

const establishmentPath = "/v1/workforce/pharmacies/0199e000-0000-7000-8000-000000000010"

type establishmentProbe struct{ calls []string }

func (p *establishmentProbe) admission(hide bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		p.calls = append(p.calls, "admission:"+c.Param("pharmacy"))
		if hide {
			c.AbortWithStatus(http.StatusNotFound)
		}
	}
}

func (p *establishmentProbe) handler(c *gin.Context) {
	p.calls = append(p.calls, "handler")
	c.Status(http.StatusOK)
}

func establishmentEngine(membership authz.MembershipChecker, granted ...authz.Permission) (*gin.Engine, *platform.Router) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: "0199e000-0000-7000-8000-000000000001"})
		c.Set("authz.permissions", authz.NewPermissionSet(granted...))
		c.Next()
	})
	return e, platform.NewRouter(e.Group("/v1"), membership)
}

func serve(e *gin.Engine, method string) int {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(method, establishmentPath, nil))
	return w.Code
}

func TestEstablishmentAdmissionRunsAfterPermissionAndBeforeTheHandler(t *testing.T) {
	for name, tc := range map[string]struct {
		membership authz.MembershipChecker
		granted    []authz.Permission
		hide       bool
		status     int
		calls      []string
	}{
		"admitted":           {alwaysMember{}, []authz.Permission{"workforce.hospital.manage"}, false, http.StatusOK, []string{"admission:0199e000-0000-7000-8000-000000000010", "handler"}},
		"hidden":             {alwaysMember{}, []authz.Permission{"workforce.hospital.manage"}, true, http.StatusNotFound, []string{"admission:0199e000-0000-7000-8000-000000000010"}},
		"missing permission": {alwaysMember{}, nil, false, http.StatusForbidden, nil},
		"not a member":       {neverMember{}, []authz.Permission{"workforce.hospital.manage"}, false, http.StatusForbidden, nil},
	} {
		probe := &establishmentProbe{}
		e, root := establishmentEngine(tc.membership, tc.granted...)
		root.Group("/workforce/pharmacies").Establishment("pharmacy", "pharmacy", probe.admission(tc.hide)).PUT("/:pharmacy", "workforce.hospital.manage", probe.handler)
		require.Equal(t, tc.status, serve(e, http.MethodPut), name)
		require.Equal(t, tc.calls, probe.calls, name)
	}
}

func TestEstablishmentAdmissionRunsAfterScopeAdmission(t *testing.T) {
	for name, tc := range map[string]struct {
		granted []authz.Permission
		status  int
		calls   []string
	}{
		"legacy grant":      {[]authz.Permission{"workforce.hospital.manage"}, http.StatusOK, []string{"admission:0199e000-0000-7000-8000-000000000010", "handler"}},
		"scope unavailable": {nil, http.StatusServiceUnavailable, nil},
	} {
		probe := &establishmentProbe{}
		e, root := establishmentEngine(alwaysMember{}, tc.granted...)
		root.Group("/workforce/pharmacies").Establishment("pharmacy", "pharmacy", probe.admission(false)).GETScopedHospital("/:pharmacy", "workforce.hospital.manage", "pharmacy", probe.handler)
		require.Equal(t, tc.status, serve(e, http.MethodGet), name)
		require.Equal(t, tc.calls, probe.calls, name)
	}
}

func TestEstablishmentRoutesRecordTheirKind(t *testing.T) {
	_, root := establishmentEngine(alwaysMember{})
	probe := &establishmentProbe{}
	root.GET("/workforce/hospitals", "workforce.hospital.manage", probe.handler)
	hospitals := root.Group("/workforce/hospitals").Establishment("hospital", "hospital", probe.admission(false))
	hospitals.PUT("/:hospital", "workforce.hospital.manage", probe.handler)
	hospitals.Group("/:hospital/staff").GET("", "workforce.staff.read", probe.handler)
	hospitals.POSTScopedHospital("/:hospital/decisions", "workforce.leave.approve", "hospital", probe.handler)

	kinds := map[string]string{}
	for _, route := range root.Declared() {
		kinds[route.Method+" "+route.Path] = route.EstablishmentKind
	}
	require.Equal(t, map[string]string{
		"GET /v1/workforce/hospitals":                      "",
		"PUT /v1/workforce/hospitals/:hospital":            "hospital",
		"GET /v1/workforce/hospitals/:hospital/staff":      "hospital",
		"POST /v1/workforce/hospitals/:hospital/decisions": "hospital",
	}, kinds)
}

func TestEstablishmentRouterRefusesAnIncompleteDeclaration(t *testing.T) {
	_, root := establishmentEngine(alwaysMember{})
	probe := &establishmentProbe{}
	admission := probe.admission(false)
	pharmacies := root.Group("/workforce/pharmacies").Establishment("pharmacy", "pharmacy", admission)

	require.Panics(t, func() { pharmacies.GET("", "workforce.hospital.manage", probe.handler) }, "a route without the parameter has no establishment to admit")
	require.Panics(t, func() { pharmacies.GET("/:pharmacy_id", "workforce.hospital.manage", probe.handler) }, "only the exact segment names the establishment")
	for _, permission := range []authz.Permission{"", authz.NoTenantMembership} {
		require.Panics(t, func() { pharmacies.GET("/:pharmacy", permission, probe.handler) }, "admission reads the kind only for a tenant member")
	}
	require.Panics(t, func() { pharmacies.Establishment("hospital", "hospital", admission) }, "a route admits one establishment kind")
	require.Panics(t, func() { root.Establishment("", "pharmacy", admission) })
	require.Panics(t, func() { root.Establishment("pharmacy", "", admission) })
	require.Panics(t, func() { root.Establishment("pharmacy", "pharmacy", nil) })
	require.Empty(t, root.Declared(), "a refused registration must not leave a declaration behind")
}
