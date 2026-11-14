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

func TestResourceListCursorCannotMoveBetweenActorsDepartmentsOutletsOrFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	router := platform.NewRouter(engine.Group("/v1"), alwaysMemberListRoute{})
	rows := makeRows(t, 3)
	scans := 0
	platform.ResourceListRoute(router, "/hospitals/:hospital/departments/:department/outlets/:outlet/orders", listRoutePerm, 2, func(_ *gin.Context, page pagination.Params) ([]fakeRow, error) {
		scans++
		if page.After == nil {
			return rows[:2], nil
		}
		return rows[1:2], nil
	})
	call := func(path, subject, query string, expected int) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", path+query, nil)
		req.Header.Set("X-Test-Subject", subject)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		require.Equal(t, expected, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	path := "/v1/hospitals/one/departments/ward/outlets/main/orders"
	first := call(path, "alice", "?limit=1&status=open", 200)
	cursor := first["page"].(map[string]any)["next_cursor"].(string)
	continueQuery := "?limit=1&status=open&cursor=" + url.QueryEscape(cursor)
	call(path, "alice", continueQuery, 200)
	require.Equal(t, 2, scans)
	for _, tc := range []struct{ path, subject, query string }{
		{"/v1/hospitals/two/departments/ward/outlets/main/orders", "alice", continueQuery},
		{"/v1/hospitals/one/departments/other/outlets/main/orders", "alice", continueQuery},
		{"/v1/hospitals/one/departments/ward/outlets/other/orders", "alice", continueQuery},
		{path, "bob", continueQuery},
		{path, "alice", "?limit=1&status=closed&cursor=" + url.QueryEscape(cursor)},
		{path, "alice", "?limit=3"},
	} {
		call(tc.path, tc.subject, tc.query, 400)
	}
	require.Equal(t, 2, scans, "invalid continuation must not reach the owner query")
}
