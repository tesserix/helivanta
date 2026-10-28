package platform_test

import (
	"encoding/json"
	"errors"
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

func TestFilteredListPreservesDeniedPageContinuationAndBindsCaller(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	rows := makeRows(t, 3)
	scans, filtered := 0, 0
	filterFailure := false
	filterOverrun := false
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{})
	platform.FilteredListRoute(r, "/things", listRoutePerm, 2, func(c *gin.Context, p pagination.Params) ([]fakeRow, error) {
		scans++
		require.Equal(t, 2, p.Limit)
		if p.After != nil {
			require.Equal(t, rows[1].ID, p.After.ID)
			return rows[2:], nil
		}
		return rows, nil
	}, func(_ *gin.Context, candidates []fakeRow) ([]fakeRow, error) {
		filtered++
		if filterOverrun {
			return rows, nil
		}
		if filterFailure {
			return nil, errors.New("synthetic authority unavailable")
		}
		if len(candidates) == 2 {
			return nil, nil
		}
		return candidates, nil
	})
	call := func(query, subject string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/things"+query, nil)
		req.Header.Set("X-Test-Subject", subject)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	first := call("", "alice", 200)
	require.Empty(t, first["data"])
	page := first["page"].(map[string]any)
	require.Equal(t, true, page["has_more"])
	cursor := page["next_cursor"].(string)
	call("?cursor="+url.QueryEscape(cursor), "bob", 400)
	require.Equal(t, 1, scans, "another subject's cursor must be rejected before querying")
	second := call("?cursor="+url.QueryEscape(cursor), "alice", 200)
	require.Len(t, second["data"], 1)
	require.Equal(t, false, second["page"].(map[string]any)["has_more"])
	call("?limit=3", "alice", 400)
	require.Equal(t, 2, scans)
	require.Equal(t, 2, filtered)
	filterFailure = true
	failed := call("", "alice", 500)
	require.NotContains(t, failed, "data", "authority failure must not disclose a partial page")
	filterFailure = false
	filterOverrun = true
	failed = call("", "alice", 500)
	require.NotContains(t, failed, "data", "a filter must not exceed the bounded candidate page")
}
