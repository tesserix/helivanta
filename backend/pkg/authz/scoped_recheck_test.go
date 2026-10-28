package authz_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type withdrawingScopeAuthority struct {
	calls     int
	nextGrant authz.ScopeGrant
	nextError error
}

func (a *withdrawingScopeAuthority) AuthorizeScope(context.Context, authz.ScopeRequest) (authz.ScopeGrant, error) {
	a.calls++
	if a.calls > 1 {
		return a.nextGrant, a.nextError
	}
	return authz.ScopeGrant{AssignmentID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa", Version: 3, ScopeEpoch: 1, MembershipEpoch: 1}, nil
}

func TestScopedCommandRecheckReplacesEvidenceAndFailsClosed(t *testing.T) {
	valid := authz.ScopeGrant{AssignmentID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa", Version: 4, ScopeEpoch: 2, MembershipEpoch: 1}
	for _, tc := range []struct {
		name   string
		grant  authz.ScopeGrant
		err    error
		status int
	}{
		{"current evidence", valid, nil, 200},
		{"authority unavailable", authz.ScopeGrant{}, errors.New("dependency unavailable"), 503},
		{"malformed evidence", authz.ScopeGrant{AssignmentID: "not-an-id", Version: 4, ScopeEpoch: 2, MembershipEpoch: 1}, nil, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			e := gin.New()
			e.Use(func(c *gin.Context) {
				c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: "00000000-0000-0000-0000-000000000001"})
				c.Set("authz.permissions", authz.NewPermissionSet())
				c.Next()
			})
			authority := &withdrawingScopeAuthority{nextGrant: tc.grant, nextError: tc.err}
			r := platform.NewRouter(e.Group("/v1"), scopeMembership(true)).WithScopedAuthorizer(authority)
			r.POSTScopedHospital("/hospitals/:hospital/decision", "workforce.leave.approve", "hospital", func(c *gin.Context) {
				grant, err := authz.RecheckHospitalScope(c, "workforce.leave.approve", c.Param("hospital"))
				current, present := authz.HospitalScopeGrant(c, "workforce.leave.approve", c.Param("hospital"))
				if tc.status == 503 {
					require.Error(t, err)
					require.Empty(t, grant.AssignmentID)
					require.False(t, present)
					require.True(t, authz.HospitalScopeUnavailable(c))
					c.Status(503)
					return
				}
				require.NoError(t, err)
				require.True(t, present)
				require.Equal(t, valid, current)
				require.Equal(t, valid, grant)
				c.Status(200)
			})
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("POST", "/v1/hospitals/00000000-0000-0000-0000-000000000002/decision", nil))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, 2, authority.calls)
		})
	}
}

func TestScopedCommandRecheckDeniesAuthorityWithdrawnAfterMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: "00000000-0000-0000-0000-000000000001"})
		c.Set("authz.permissions", authz.NewPermissionSet())
		c.Next()
	})
	authority := &withdrawingScopeAuthority{}
	r := platform.NewRouter(e.Group("/v1"), scopeMembership(true)).WithScopedAuthorizer(authority)
	r.POSTScopedHospital("/hospitals/:hospital/decision", "workforce.leave.approve", "hospital", func(c *gin.Context) {
		grant, err := authz.RecheckHospitalScope(c, "workforce.leave.approve", c.Param("hospital"))
		require.NoError(t, err)
		if grant.AssignmentID == "" {
			_, present := authz.HospitalScopeGrant(c, "workforce.leave.approve", c.Param("hospital"))
			require.False(t, present, "withdrawal invalidates the earlier request evidence")
			c.Status(403)
			return
		}
		c.Status(200)
	})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("POST", "/v1/hospitals/00000000-0000-0000-0000-000000000002/decision", nil))
	require.Equal(t, 403, w.Code)
	require.Equal(t, 2, authority.calls)
}
