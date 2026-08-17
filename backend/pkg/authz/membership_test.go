package authz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
)

type fakeMembershipChecker struct {
	member bool
	err    error
}

func (f fakeMembershipChecker) IsMember(context.Context, string, string) (bool, error) {
	return f.member, f.err
}

func membershipHarness(t *testing.T, m authz.MembershipChecker, withPrincipal bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/v1")
	if withPrincipal {
		g.Use(principalStub(tenantA))
	}
	g.GET("/thing", authz.RequireMembership(m), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return e
}

func TestRequireMembershipAllowsAMember(t *testing.T) {
	e := membershipHarness(t, fakeMembershipChecker{member: true}, true)
	require.Equal(t, http.StatusOK, get(e).Code)
}

func TestRequireMembershipDeniesANonMemberWith403(t *testing.T) {
	e := membershipHarness(t, fakeMembershipChecker{member: false}, true)
	w := get(e)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"forbidden"`)
}

// TestRequireMembershipFailsClosedOnCheckerError is the fail-closed
// assertion: a membership decision that cannot be made must deny, never
// admit — a false-from-error and a false-from-genuine-non-member must
// never be treated identically to a caller.
func TestRequireMembershipFailsClosedOnCheckerError(t *testing.T) {
	e := membershipHarness(t, fakeMembershipChecker{err: errors.New("openfga unreachable")}, true)
	w := get(e)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), `"authz_unavailable"`)
}

func TestRequireMembershipRequiresAPrincipal(t *testing.T) {
	e := membershipHarness(t, fakeMembershipChecker{member: true}, false)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/thing", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequireMembershipIsTenantScoped proves the checker is asked about
// the caller's own tenant claim, not some fixed value — the same subject
// can be a member of one tenant and not another (spec D3), and this
// fake exercises exactly the arguments RequireMembership passes through.
func TestRequireMembershipIsTenantScoped(t *testing.T) {
	var gotSubject, gotTenant string
	recorder := recordingMembershipChecker{
		fn: func(_ context.Context, subject, tenantID string) (bool, error) {
			gotSubject, gotTenant = subject, tenantID
			return true, nil
		},
	}
	e := membershipHarness(t, recorder, true)
	require.Equal(t, http.StatusOK, get(e).Code)
	require.Equal(t, "alice", gotSubject)
	require.Equal(t, tenantA, gotTenant)
}

type recordingMembershipChecker struct {
	fn func(ctx context.Context, subject, tenantID string) (bool, error)
}

func (r recordingMembershipChecker) IsMember(ctx context.Context, subject, tenantID string) (bool, error) {
	return r.fn(ctx, subject, tenantID)
}
