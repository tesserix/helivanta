package authz_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type guardedScopeAuthority struct {
	withdrawingScopeAuthority
	err    error
	called bool
}

type commandMarker struct{}

func (a *guardedScopeAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, g authz.ScopeGrant, authenticatedAt time.Time, action func(context.Context) error) error {
	a.called = r.Subject == "synthetic-reviewer" && g.Version == 3 && !authenticatedAt.IsZero()
	if a.err != nil {
		return a.err
	}
	return action(context.WithValue(ctx, commandMarker{}, true))
}

func TestHospitalCommandRequiresGuardAndPreservesCredentialDenial(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing bool
		failure error
		legacy  bool
	}{
		{name: "missing guard", missing: true, failure: authz.ErrScopedAuthorizationUnavailable},
		{name: "revoked credential cannot use legacy grant", failure: authz.ErrScopedCredentialRevoked, legacy: true},
		{name: "withdrawn assignment", failure: authz.ErrScopedCommandDenied},
		{name: "guarded context reaches product"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			e.Use(func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: "00000000-0000-0000-0000-000000000001", AuthTime: time.Now()})
				permissions := authz.NewPermissionSet()
				if tc.legacy {
					permissions = authz.NewPermissionSet("workforce.leave.approve")
				}
				c.Set("authz.permissions", permissions)
			})
			guard := &guardedScopeAuthority{err: tc.failure}
			var authority authz.ScopedAuthorizer = guard
			if tc.missing {
				authority = &withdrawingScopeAuthority{}
			}
			r := platform.NewRouter(e.Group("/v1"), scopeMembership(true)).WithScopedAuthorizer(authority)
			called := false
			r.POSTScopedHospital("/hospitals/:hospital/decision", "workforce.leave.approve", "hospital", func(c *gin.Context) {
				err := authz.WithHospitalScopeCommand(c, "workforce.leave.approve", c.Param("hospital"), func(ctx context.Context) error {
					called = true
					require.Equal(t, true, ctx.Value(commandMarker{}))
					return nil
				})
				if tc.failure != nil {
					require.ErrorIs(t, err, tc.failure)
					err = authz.WithHospitalScopeCommand(c, "workforce.leave.approve", c.Param("hospital"), func(context.Context) error {
						called = true
						return nil
					})
					require.ErrorIs(t, err, tc.failure, "a failed guard must not fall back within the request")
				} else {
					require.NoError(t, err)
				}
				c.Status(200)
			})
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("POST", "/v1/hospitals/00000000-0000-0000-0000-000000000002/decision", nil))
			require.Equal(t, 200, w.Code)
			require.Equal(t, tc.failure == nil, called)
			if !tc.missing {
				require.True(t, guard.called)
			}
		})
	}
}
