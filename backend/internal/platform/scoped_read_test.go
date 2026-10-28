package platform_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

type readScopeAuthority struct{}

func (readScopeAuthority) AuthorizeScope(context.Context, authz.ScopeRequest) (authz.ScopeGrant, error) {
	return authz.ScopeGrant{AssignmentID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa", Version: 1, ScopeEpoch: 1, MembershipEpoch: 1}, nil
}

func TestScopedReadsRetainMembershipScopeAndPaginationMetadata(t *testing.T) {
	for _, member := range []bool{true, false} {
		e := gin.New()
		e.Use(func(c *gin.Context) {
			c.Set("authn.principal", authn.Principal{Subject: "synthetic-reader", TenantID: testTenant})
			c.Set("authz.permissions", authz.NewPermissionSet())
		})
		var membership authz.MembershipChecker = alwaysMember{}
		if !member {
			membership = neverMember{}
		}
		r := platform.NewRouter(e.Group("/v1"), membership).WithScopedAuthorizer(readScopeAuthority{})
		r.GETScopedHospital("/hospitals/:hospital/request", "workforce.leave.approve", "hospital", func(c *gin.Context) {
			_, ok := authz.HospitalScopeGrant(c, "workforce.leave.approve", c.Param("hospital"))
			require.True(t, ok)
			c.JSON(200, gin.H{"allowed": true})
		})
		platform.ScopedHospitalListRoute(r, "/hospitals/:hospital/requests", "workforce.leave.approve", "hospital", func(c *gin.Context, _ pagination.Params) ([]fakeRow, error) {
			_, ok := authz.HospitalScopeGrant(c, "workforce.leave.approve", c.Param("hospital"))
			require.True(t, ok)
			return makeRows(t, 1), nil
		})
		for _, path := range []string{"request", "requests"} {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/hospitals/11111111-1111-4111-8111-111111111111/"+path, nil))
			if member {
				require.Equal(t, 200, w.Code, w.Body.String())
			} else {
				require.Equal(t, 403, w.Code, w.Body.String())
			}
		}
		declared := r.Declared()
		require.Len(t, declared, 2)
		for _, route := range declared {
			require.Equal(t, "GET", route.Method)
			require.Equal(t, "hospital", route.ScopeKind)
			require.Equal(t, "hospital", route.ScopeParameter)
		}
		require.True(t, declared[1].Paginated)
		require.Panics(t, func() { r.GETScopedHospital("/:hospital/unsafe", authz.Public, "hospital", func(*gin.Context) {}) })
		require.Len(t, r.Declared(), 2)
	}
}
