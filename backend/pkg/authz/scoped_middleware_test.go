package authz_test

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http/httptest"
	"testing"
)

type scopeMembership bool

func (m scopeMembership) IsMember(context.Context, string, string) (bool, error) { return bool(m), nil }

func TestScopedRouteFailsClosedWithoutAuthority(t *testing.T) {
	for _, tc := range []struct {
		name        string
		member      authz.MembershipChecker
		legacy      bool
		initialized bool
		status      int
	}{
		{"nonmember", scopeMembership(false), false, true, 403},
		{"missing dependency", scopeMembership(true), false, true, 503},
		{"legacy independent gate", scopeMembership(true), true, true, 200},
		{"missing permission context", scopeMembership(true), false, false, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			e.Use(func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: "00000000-0000-0000-0000-000000000001"})
				if tc.initialized {
					set := authz.NewPermissionSet()
					if tc.legacy {
						set = authz.NewPermissionSet("workforce.leave.approve")
					}
					c.Set("authz.permissions", set)
				}
				c.Next()
			})
			r := platform.NewRouter(e.Group("/v1"), tc.member)
			r.POSTScopedHospital("/hospitals/:hospital/decision", "workforce.leave.approve", "hospital", func(c *gin.Context) {
				_, ok := authz.HospitalScopeGrant(c, "workforce.leave.approve", c.Param("hospital"))
				require.False(t, ok)
				require.True(t, authz.HospitalScopeUnavailable(c))
				c.Status(200)
			})
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("POST", "/v1/hospitals/00000000-0000-0000-0000-000000000002/decision", nil))
			require.Equal(t, tc.status, w.Code)
		})
	}
}

func TestScopedRoutesRejectChangedWorkspaceBeforeAuthorityLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{"GET", "POST"} {
		for _, headers := range [][]string{{"00000000-0000-0000-0000-000000000003"}, {"invalid"}, {"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000001"}} {
			e := gin.New()
			e.Use(func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: "00000000-0000-0000-0000-000000000001"})
				c.Set("authz.permissions", authz.NewPermissionSet())
			})
			authority := &withdrawingScopeAuthority{}
			r := platform.NewRouter(e.Group("/v1"), scopeMembership(true)).WithScopedAuthorizer(authority)
			called := false
			handler := func(c *gin.Context) { called = true; c.Status(200) }
			r.GETScopedHospital("/hospitals/:hospital/requests", "workforce.leave.approve", "hospital", handler)
			r.POSTScopedHospital("/hospitals/:hospital/requests", "workforce.leave.approve", "hospital", handler)
			req := httptest.NewRequest(method, "/v1/hospitals/00000000-0000-0000-0000-000000000002/requests", nil)
			for _, header := range headers {
				req.Header.Add("X-Helivanta-Expected-Tenant", header)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, req)
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Zero(t, authority.calls)
			require.False(t, called)
		}
	}
}
