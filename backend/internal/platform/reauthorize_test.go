package platform_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http/httptest"
	"testing"
)

type currentAccess struct {
	revoked, membershipError, permissionError, lostPermission bool
	memberCalls, resolveCalls                                 int
}

func (a *currentAccess) IsMember(context.Context, string, string) (bool, error) {
	a.memberCalls++
	if a.membershipError {
		return false, errors.New("synthetic membership outage")
	}
	return !a.revoked, nil
}
func (a *currentAccess) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	a.resolveCalls++
	if a.permissionError {
		return nil, errors.New("synthetic resolver outage")
	}
	if a.lostPermission {
		return authz.NewPermissionSet(), nil
	}
	return authz.NewPermissionSet("private.evidence.read"), nil
}
func TestReauthorizationRefreshesMembershipAndPermissionsAfterExternalIO(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*currentAccess)
		want   int
	}{
		{"retained", func(*currentAccess) {}, 200}, {"membership_revoked", func(a *currentAccess) { a.revoked = true }, 403}, {"membership_unavailable", func(a *currentAccess) { a.membershipError = true }, 503}, {"permission_revoked", func(a *currentAccess) { a.lostPermission = true }, 403}, {"permission_unavailable", func(a *currentAccess) { a.permissionError = true }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			a := &currentAccess{}
			e.Use(principalStub(), authz.Middleware(a))
			r := platform.NewRouter(e.Group("/v1"), a)
			released := false
			r.GET("/private", "private.evidence.read", func(c *gin.Context) {
				tc.change(a)
				if !r.Reauthorize(c, "private.evidence.read") {
					return
				}
				released = true
				c.String(200, "private bytes")
			})
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/private", nil))
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Equal(t, tc.want == 200, released)
			require.Equal(t, 2, a.memberCalls)
			if tc.want != 200 {
				require.NotContains(t, w.Body.String(), "private bytes")
			}
			if !a.revoked && !a.membershipError {
				require.Equal(t, 2, a.resolveCalls)
			}
		})
	}
}
func TestReauthorizationWithoutPrincipalOrResolverCannotRelease(t *testing.T) {
	for _, withPrincipal := range []bool{false, true} {
		t.Run(fmt.Sprint(withPrincipal), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			if withPrincipal {
				e.Use(principalStub())
			}
			r := platform.NewRouter(e.Group("/v1"), alwaysMember{})
			released := false
			e.GET("/private", func(c *gin.Context) {
				if !r.Reauthorize(c, "private.evidence.read") {
					return
				}
				released = true
			})
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("GET", "/private", nil))
			want := 401
			if withPrincipal {
				want = 500
			}
			require.Equal(t, want, w.Code)
			require.False(t, released)
		})
	}
}
