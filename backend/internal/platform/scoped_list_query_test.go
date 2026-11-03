package platform_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestScopedListCursorBindsCallerHospitalAndNormalizedFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMember{}).WithScopedAuthorizer(readScopeAuthority{})
	rows := makeRows(t, 3)
	scans := 0
	platform.ScopedHospitalQueryListRoute(r, "/hospitals/:hospital/queue", listRoutePerm, "hospital", "status", "pending", 2, func(_ *gin.Context, page pagination.Params) ([]fakeRow, error) {
		scans++
		if page.After == nil {
			return rows[:2], nil
		}
		require.Equal(t, rows[0].ID, page.After.ID)
		return rows[1:], nil
	})
	call := func(hospital, query, subject string, status int) map[string]any {
		t.Helper()
		request := httptest.NewRequest("GET", "/v1/hospitals/"+hospital+"/queue?limit=1"+query, nil)
		request.Header.Set("X-Test-Subject", subject)
		response := httptest.NewRecorder()
		e.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	hospital := "11111111-1111-4111-8111-111111111111"
	first := call(hospital, "", "alice", 200)
	cursor := first["page"].(map[string]any)["next_cursor"].(string)
	query := "&cursor=" + url.QueryEscape(cursor)
	second := call(hospital, query+"&status=pending", "alice", 200)
	require.Equal(t, rows[1].ID.String(), second["data"].([]any)[0].(map[string]any)["id"])
	for _, change := range []struct{ hospital, filter, subject string }{
		{hospital, "&status=rejected", "alice"},
		{hospital, "", "bob"},
		{"22222222-2222-4222-8222-222222222222", "", "alice"},
	} {
		call(change.hospital, query+change.filter, change.subject, 400)
	}
	legacy := pagination.Cursor{TenantID: testTenant, CreatedAt: rows[0].CreatedAt, ID: rows[0].ID}.Encode()
	call(hospital, "&cursor="+url.QueryEscape(legacy), "alice", 400)
	require.Equal(t, 2, scans, "foreign-scope positions must be refused before querying")
	declared := r.Declared()
	require.Len(t, declared, 1)
	require.True(t, declared[0].Paginated)
	require.Equal(t, "hospital", declared[0].ScopeKind)
}

func TestScopedQueryListBoundsDefaultAndWithholdsOverrun(t *testing.T) {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMember{}).WithScopedAuthorizer(readScopeAuthority{})
	rows := makeRows(t, 4)
	scans, overrun := 0, false
	platform.ScopedHospitalQueryListRoute(r, "/hospitals/:hospital/bounded", listRoutePerm, "hospital", "status", "pending", 2, func(_ *gin.Context, page pagination.Params) ([]fakeRow, error) {
		scans++
		require.Equal(t, 2, page.Limit)
		if overrun {
			return rows, nil
		}
		return rows[:3], nil
	})
	call := func(query string, status int) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest("GET", "/v1/hospitals/11111111-1111-4111-8111-111111111111/bounded"+query, nil))
		require.Equal(t, status, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	first := call("", 200)
	require.Len(t, first["data"], 2)
	require.Equal(t, true, first["page"].(map[string]any)["has_more"])
	call("?limit=3", 400)
	require.Equal(t, 1, scans)
	overrun = true
	failure := call("", 500)
	require.NotContains(t, failure, "data")
	require.Equal(t, 2, scans)
}
