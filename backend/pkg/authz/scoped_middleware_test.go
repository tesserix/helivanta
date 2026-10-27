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
