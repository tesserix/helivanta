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

func TestSealedFilteredResourceListRejectsOtherHospitalDepartmentAndAccountBeforeScan(t *testing.T) {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	sealer, err := pagination.NewSealer([]byte("synthetic-private-cursor-key-32-bytes"))
	require.NoError(t, err)
	router := platform.NewRouter(engine.Group("/v1"), alwaysMemberListRoute{}).WithCursorSealer(sealer)
	rows, scans := makeRows(t, 3), 0
	platform.SealedFilteredResourceListRoute(router, "/hospitals/:hospital/departments/:department/choices", listRoutePerm, 2, func(_ *gin.Context, page pagination.Params) ([]fakeRow, error) {
		scans++
		if page.After != nil {
			return rows[2:], nil
		}
		return rows, nil
	}, func(_ *gin.Context, rows []fakeRow) ([]fakeRow, error) { return rows, nil }, "hospital", "department")
	call := func(path, subject string, status int) map[string]any {
		t.Helper()
		request := httptest.NewRequest("GET", "/v1/hospitals/"+path, nil)
		request.Header.Set("X-Test-Subject", subject)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	first := call("one/departments/main/choices", "alice", 200)
	cursor := url.QueryEscape(first["page"].(map[string]any)["next_cursor"].(string))
	call("two/departments/main/choices?cursor="+cursor, "alice", 400)
	call("one/departments/other/choices?cursor="+cursor, "alice", 400)
	call("one/departments/main/choices?cursor="+cursor, "bob", 400)
	require.Equal(t, 1, scans)
	last := call("one/departments/main/choices?cursor="+cursor+"&limit=1", "alice", 200)
	require.Len(t, last["data"], 1)
	require.Equal(t, 2, scans)
}
