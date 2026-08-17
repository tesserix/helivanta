package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// testTenant matches the TenantID the stubbed principal carries in
// runListRoute, so a cursor this package encodes always decodes
// successfully against it.
const testTenant = "11111111-1111-1111-1111-111111111111"

const listRoutePerm authz.Permission = "platform.thing.read"

type fakeRow struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (r fakeRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// makeRows returns n rows with strictly decreasing CreatedAt, matching
// the newest-first keyset order ListRoute assumes.
func makeRows(t *testing.T, n int) []fakeRow {
	t.Helper()
	base := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	rows := make([]fakeRow, n)
	for i := 0; i < n; i++ {
		rows[i] = fakeRow{
			ID:        uuid.New(),
			Name:      "row",
			CreatedAt: base.Add(-time.Duration(i) * time.Second),
		}
	}
	return rows
}

// alwaysMemberListRoute is a fixed authz.MembershipChecker — these tests
// are about ListRoute's pagination mechanics, not membership, so
// membership must be a settled "yes".
type alwaysMemberListRoute struct{}

func (alwaysMemberListRoute) IsMember(context.Context, string, string) (bool, error) {
	return true, nil
}

// runListRoute registers h through ListRoute behind a stubbed principal
// and permission set, and issues a GET against "/v1/things"+query.
func runListRoute(t *testing.T, query string, h platform.ListHandler[fakeRow]) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: testTenant})
		c.Next()
	})
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{})
	platform.ListRoute[fakeRow](r, "/things", listRoutePerm, h)

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/things"+query, nil))
	return w
}

// TestListRouteTrimsTheProbeRowAndReportsHasMore pins the contract's
// central mechanic: the handler returns limit+1 rows and the caller must
// never see the extra one, while has_more must be true because of it.
func TestListRouteTrimsTheProbeRowAndReportsHasMore(t *testing.T) {
	rows := makeRows(t, 4) // handler will return all 4 for limit=3
	w := runListRoute(t, "?limit=3", func(_ *gin.Context, p pagination.Params) ([]fakeRow, error) {
		require.Equal(t, 3, p.Limit)
		return rows, nil
	})

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data []fakeRow `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	require.Len(t, body.Data, 3, "the probe row must be trimmed before the client sees it")
	require.Equal(t, rows[2].ID, body.Data[2].ID, "the trim must drop the LAST row, not an arbitrary one")
	require.True(t, body.Page.HasMore)
	require.NotNil(t, body.Page.NextCursor)

	// The cursor must point at the last SHOWN row, not the probe row —
	// pointing at the probe skips it on the next page.
	cur, err := pagination.Decode(*body.Page.NextCursor, testTenant)
	require.NoError(t, err)
	require.Equal(t, rows[2].ID, cur.ID, "next_cursor must anchor on the last row the client was shown")
}

func TestListRouteLastPageHasNoCursor(t *testing.T) {
	rows := makeRows(t, 2)
	w := runListRoute(t, "?limit=3", func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
		return rows, nil
	})

	var body struct {
		Data []fakeRow `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)
	require.False(t, body.Page.HasMore)
	require.Nil(t, body.Page.NextCursor)
}

// TestListRouteExactBoundary is the case a naive implementation gets
// wrong: exactly limit rows exist, so the probe finds nothing extra and
// this IS the last page — even though the page is full.
func TestListRouteExactBoundary(t *testing.T) {
	rows := makeRows(t, 3)
	w := runListRoute(t, "?limit=3", func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
		return rows, nil
	})

	var body struct {
		Data []fakeRow `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 3)
	require.False(t, body.Page.HasMore, "a full page with nothing after it is still the last page")
	require.Nil(t, body.Page.NextCursor)
}

func TestListRouteEmptyCollectionIsAnArrayNotNull(t *testing.T) {
	w := runListRoute(t, "", func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
		return nil, nil
	})
	require.Contains(t, w.Body.String(), `"data":[]`,
		"an empty collection must marshal to [] — panels branch on the array, and null crashes them")
}

func TestListRouteRejectsBadLimitAndCursor(t *testing.T) {
	for _, q := range []string{"?limit=201", "?limit=0", "?limit=abc", "?cursor=not-a-cursor"} {
		w := runListRoute(t, q, func(_ *gin.Context, _ pagination.Params) ([]fakeRow, error) {
			t.Fatalf("handler must not run for invalid input %q", q)
			return nil, nil
		})
		require.Equal(t, http.StatusBadRequest, w.Code, q)
		require.Contains(t, w.Body.String(), "invalid_request", q)
	}
}

// runListRouteWithoutPrincipal is runListRoute with the principal
// middleware omitted, so ListRoute's own missing-principal branch is
// reachable. It cannot be expressed through runListRoute, which always
// stubs one.
func runListRouteWithoutPrincipal(t *testing.T, h platform.ListHandler[fakeRow]) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet(listRoutePerm))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"), alwaysMemberListRoute{})
	platform.ListRoute[fakeRow](r, "/things", listRoutePerm, h)

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/things", nil))
	return w
}

// TestListRouteWithoutAPrincipalIsUnauthenticated covers the branch the
// happy-path tests cannot reach, because they all stub a principal.
//
// It matters beyond coverage: the cursor is scoped to the caller's
// tenant, so a ListRoute that proceeded without a principal would have
// no tenant to validate a cursor against. Failing here rather than
// later is what keeps that impossible.
func TestListRouteWithoutAPrincipalIsUnauthenticated(t *testing.T) {
	w := runListRouteWithoutPrincipal(t, func(*gin.Context, pagination.Params) ([]fakeRow, error) {
		t.Fatal("the handler must not run without an authenticated principal")
		return nil, nil
	})

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "unauthenticated")
}

// TestListRouteHandlerErrorIsFiveHundredWithNoPartialPage pins what a
// failing handler must NOT do: emit 200 with whatever rows it managed to
// collect, or an envelope claiming has_more about a page that was never
// assembled. A half-page presented as a whole one is the same
// silently-incomplete-list defect #816 exists to remove, arriving from
// the error path instead of the query.
func TestListRouteHandlerErrorIsFiveHundredWithNoPartialPage(t *testing.T) {
	partial := makeRows(t, 2)
	w := runListRoute(t, "?limit=3", func(*gin.Context, pagination.Params) ([]fakeRow, error) {
		return partial, errors.New("database is on fire")
	})

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), `"data"`,
		"a failed page must not return rows it happened to have; the client cannot tell them from a complete page")
	require.NotContains(t, w.Body.String(), "database is on fire",
		"the underlying cause is logged against the request id, never sent to the client")
}
