package platform_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

func TestSealedSearchBindsNormalizedBodyCallerTenantAndHospital(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		tenant := testTenant
		if value := c.GetHeader("X-Test-Tenant"); value != "" {
			tenant = value
		}
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: tenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	sealer, err := pagination.NewSealer([]byte("synthetic-private-cursor-key-32-bytes"))
	require.NoError(t, err)
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{}).WithCursorSealer(sealer)
	type input struct {
		Code string `json:"code"`
	}
	scans := 0
	rows := makeRows(t, 3)
	prepare := func(c *gin.Context) (input, bool) {
		var in input
		if err := c.ShouldBindJSON(&in); err != nil {
			respond.BadRequest(c, err)
			return in, false
		}
		in.Code = strings.ToLower(in.Code)
		return in, true
	}
	scan := func(_ *gin.Context, in input, page pagination.Params) ([]fakeRow, error) {
		scans++
		require.Equal(t, "synthetic", in.Code)
		if page.After != nil {
			require.Equal(t, rows[1].ID, page.After.ID)
			return rows[2:], nil
		}
		return rows, nil
	}
	platform.SealedSearchRoute(r, "/hospitals/:hospital/targets/search", listRoutePerm, "hospital", 2, prepare, scan)
	platform.SealedSearchRoute(platform.NewRouter(e.Group("/unavailable"), alwaysMemberListRoute{}), "/hospitals/:hospital/targets/search", listRoutePerm, "hospital", 2, prepare, scan)
	call := func(prefix, hospital, query, body, subject, tenant string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest("POST", prefix+"/hospitals/"+hospital+"/targets/search"+query, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Subject", subject)
		req.Header.Set("X-Test-Tenant", tenant)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	first := call("/v1", "one", "", `{"code":"Synthetic"}`, "alice", "", 200)
	require.Len(t, first["data"], 2)
	cursor := url.QueryEscape(first["page"].(map[string]any)["next_cursor"].(string))
	for _, row := range []struct{ hospital, body, subject, tenant string }{
		{"two", `{"code":"synthetic"}`, "alice", ""},
		{"one", `{"code":"other"}`, "alice", ""},
		{"one", `{"code":"synthetic"}`, "bob", ""},
		{"one", `{"code":"synthetic"}`, "alice", "22222222-2222-2222-2222-222222222222"},
	} {
		call("/v1", row.hospital, "?cursor="+cursor, row.body, row.subject, row.tenant, 400)
	}
	for _, query := range []string{"?limit=3", "?limit=1&limit=2", "?cursor=x&cursor=y", "?code=private"} {
		call("/v1", "one", query, `{"code":"synthetic"}`, "alice", "", 400)
	}
	call("/v1", "one", "", `{`, "alice", "", 400)
	call("/unavailable", "one", "", `{"code":"synthetic"}`, "alice", "", 503)
	require.Equal(t, 1, scans, "invalid continuation, query, body or missing sealing cannot scan")
	last := call("/v1", "one", "?cursor="+cursor+"&limit=1", `{"code":"SYNTHETIC"}`, "alice", "", 200)
	require.Len(t, last["data"], 1)
	require.Equal(t, false, last["page"].(map[string]any)["has_more"])
	require.Equal(t, 2, scans)
}
