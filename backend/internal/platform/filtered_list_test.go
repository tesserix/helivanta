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

func TestSealedFilteredQueryListBindsHospitalAndFiltersBeforeScanning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	sealer, err := pagination.NewSealer([]byte("synthetic-private-cursor-key-32-bytes"))
	require.NoError(t, err)
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{}).WithCursorSealer(sealer)
	rows, scans := makeRows(t, 3), 0
	platform.SealedFilteredQueryListRoute(r, "/hospitals/:hospital/inbox", listRoutePerm, "hospital", []platform.QueryFilter{{Key: "mode", Default: "review"}, {Key: "status", Default: "pending"}}, 2, func(_ *gin.Context, page pagination.Params) ([]fakeRow, error) {
		scans++
		if page.After != nil {
			return rows[2:], nil
		}
		return rows, nil
	}, func(_ *gin.Context, rows []fakeRow) ([]fakeRow, error) { return rows, nil })
	call := func(path, subject string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/hospitals/"+path, nil)
		req.Header.Set("X-Test-Subject", subject)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	first := call("one/inbox", "alice", 200)
	cursor := url.QueryEscape(first["page"].(map[string]any)["next_cursor"].(string))
	call("two/inbox?cursor="+cursor, "alice", 400)
	call("one/inbox?status=approved&cursor="+cursor, "alice", 400)
	call("one/inbox?mode=prepare&cursor="+cursor, "alice", 400)
	call("one/inbox?cursor="+cursor, "bob", 400)
	require.Equal(t, 1, scans, "resource/filter/account mismatch cannot reach the scan")
	last := call("one/inbox?status=pending&mode=review&cursor="+cursor+"&limit=1", "alice", 200)
	require.Len(t, last["data"], 1)
	require.Equal(t, 2, scans)
}

func TestSealedFilteredQueryListBindsHospitalStaffAndBasisBeforeScanning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: testTenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	sealer, err := pagination.NewSealer([]byte("synthetic-private-cursor-key-32-bytes"))
	require.NoError(t, err)
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{}).WithCursorSealer(sealer)
	rows, scans := makeRows(t, 3), 0
	platform.SealedFilteredQueryListRoute(r, "/hospitals/:hospital/staff/:id/findings", listRoutePerm, "hospital", []platform.QueryFilter{{Key: "staff_version", Default: "1"}, {Key: "employment_epoch", Default: "1"}}, 2, func(_ *gin.Context, page pagination.Params) ([]fakeRow, error) {
		scans++
		if page.After != nil {
			return rows[2:], nil
		}
		return rows, nil
	}, func(_ *gin.Context, rows []fakeRow) ([]fakeRow, error) { return rows, nil }, "id")
	call := func(path, subject string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/hospitals/"+path, nil)
		req.Header.Set("X-Test-Subject", subject)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	first := call("one/staff/applicant/findings", "alice", 200)
	cursor := url.QueryEscape(first["page"].(map[string]any)["next_cursor"].(string))
	call("two/staff/applicant/findings?cursor="+cursor, "alice", 400)
	call("one/staff/applicant/findings?staff_version=2&cursor="+cursor, "alice", 400)
	call("one/staff/applicant/findings?employment_epoch=2&cursor="+cursor, "alice", 400)
	call("one/staff/applicant/findings?cursor="+cursor, "bob", 400)
	call("one/staff/other/findings?cursor="+cursor, "alice", 400)
	require.Equal(t, 1, scans, "resource/filter/account mismatch cannot reach the scan")
	last := call("one/staff/applicant/findings?staff_version=1&employment_epoch=1&cursor="+cursor+"&limit=1", "alice", 200)
	require.Len(t, last["data"], 1)
	require.Equal(t, 2, scans)
}

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

func TestSealedFilteredListHidesDeniedAnchorsAndRejectsReplays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		tenant := testTenant
		if supplied := c.GetHeader("X-Test-Tenant"); supplied != "" {
			tenant = supplied
		}
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: tenant})
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
	})
	sealer, err := pagination.NewSealer([]byte("synthetic-private-cursor-key-32-bytes"))
	require.NoError(t, err)
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{}).WithCursorSealer(sealer)
	rows := makeRows(t, 3)
	scans := 0
	scan := func(_ *gin.Context, p pagination.Params) ([]fakeRow, error) {
		scans++
		if p.After != nil {
			require.Equal(t, rows[1].ID, p.After.ID)
			return rows[2:], nil
		}
		return rows, nil
	}
	filter := func(_ *gin.Context, candidates []fakeRow) ([]fakeRow, error) {
		if len(candidates) == 2 {
			return nil, nil
		}
		return candidates, nil
	}
	platform.SealedFilteredListRoute(r, "/things", listRoutePerm, 2, scan, filter)
	platform.SealedFilteredListRoute(r, "/other", listRoutePerm, 2, scan, filter)
	call := func(path, subject, tenant string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1"+path, nil)
		req.Header.Set("X-Test-Subject", subject)
		req.Header.Set("X-Test-Tenant", tenant)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	first := call("/things", "alice", "", 200)
	require.Empty(t, first["data"])
	cursor := first["page"].(map[string]any)["next_cursor"].(string)
	_, err = pagination.Decode(cursor, testTenant)
	require.Error(t, err, "a denied hospital anchor cannot leave as a readable cursor")
	call("/things?cursor="+url.QueryEscape(cursor), "bob", "", 400)
	call("/other?cursor="+url.QueryEscape(cursor), "alice", "", 400)
	call("/things?cursor="+url.QueryEscape(cursor), "alice", "22222222-2222-4222-8222-222222222222", 400)
	call("/things?cursor="+url.QueryEscape(cursor+"tampered"), "alice", "", 400)
	require.Equal(t, 1, scans, "invalid cursors are rejected before candidate reads")
	next := call("/things?cursor="+url.QueryEscape(cursor), "alice", "", 200)
	require.Len(t, next["data"], 1)
	require.Equal(t, false, next["page"].(map[string]any)["has_more"])
	platform.SealedFilteredListRoute(platform.NewRouter(e.Group("/without-key"), alwaysMemberListRoute{}), "/things", listRoutePerm, 2, scan, filter)
	missing := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/without-key/things", nil)
	req.Header.Set("X-Test-Subject", "alice")
	e.ServeHTTP(missing, req)
	require.Equal(t, 503, missing.Code, missing.Body.String())
	require.Equal(t, 2, scans, "missing encryption cannot fall back to a readable anchor")
}
