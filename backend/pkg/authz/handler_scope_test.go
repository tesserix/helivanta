package authz_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
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

const (
	handlerScopeTenant   = "00000000-0000-0000-0000-000000000001"
	handlerScopeHospital = "00000000-0000-0000-0000-000000000002"
	handlerScopePerm     = authz.Permission("workforce.posting.proposal.read")
)

func handlerScopeRequest(permission authz.Permission) authz.ScopeRequest {
	return authz.ScopeRequest{TenantID: handlerScopeTenant, Subject: "synthetic-reviewer", HospitalID: handlerScopeHospital, Permission: permission, DepartmentPath: []string{"019a5c40-0000-7000-8000-000000000003"}}
}

func handlerScopeGrant() authz.ScopeGrant {
	return authz.ScopeGrant{AssignmentID: "019a5c40-0000-7000-8000-000000000004", Version: 1, ScopeEpoch: 1, MembershipEpoch: 1, ScopeKind: "department", DepartmentID: "019a5c40-0000-7000-8000-000000000003"}
}

func serveHandlerScope(t *testing.T, hospital string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: handlerScopeTenant})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	r := platform.NewRouter(e.Group("/v1"), scopeMembership(true))
	r.GETHandlerScoped("/hospitals/:hospital/review", handlerScopePerm, "hospital", handler)
	declared := r.Declared()
	require.Len(t, declared, 1)
	require.Equal(t, handlerScopePerm, declared[0].Permission, "the route keeps declaring the permission it needs")
	require.Equal(t, "department", declared[0].ScopeKind)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/hospitals/"+hospital+"/review", nil))
	return w
}

func admitWithEvidence(c *gin.Context, checked, admitted authz.ScopeRequest) bool {
	var ok bool
	_ = authz.WithScopedCommandEvidence(c.Request.Context(), []authz.ScopeCheck{{Request: checked, Grant: handlerScopeGrant()}}, func(ctx context.Context) error {
		ok = authz.AdmitHandlerScope(ctx, c, admitted)
		return nil
	})
	return ok
}

func TestHandlerScopedRouteWithholdsSuccessWithoutCurrentScopedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name  string
		admit func(*gin.Context) bool
	}{
		{"never admitted", func(*gin.Context) bool { return false }},
		{"admitted without IAM evidence", func(c *gin.Context) bool {
			return authz.AdmitHandlerScope(c.Request.Context(), c, handlerScopeRequest(handlerScopePerm))
		}},
		{"evidence for another permission", func(c *gin.Context) bool {
			other := handlerScopeRequest("workforce.posting.review")
			return admitWithEvidence(c, other, other)
		}},
		{"evidence for another hospital", func(c *gin.Context) bool {
			other := handlerScopeRequest(handlerScopePerm)
			other.HospitalID = "00000000-0000-0000-0000-000000000009"
			return admitWithEvidence(c, other, other)
		}},
		{"request differs from the evidence", func(c *gin.Context) bool {
			changed := handlerScopeRequest(handlerScopePerm)
			changed.DepartmentPath = nil
			return admitWithEvidence(c, handlerScopeRequest(handlerScopePerm), changed)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := serveHandlerScope(t, handlerScopeHospital, func(c *gin.Context) {
				require.False(t, tc.admit(c))
				c.Header("X-Synthetic-Disclosure", "withheld")
				c.JSON(200, gin.H{"staff": "synthetic protected row"})
			})
			require.Equal(t, 500, w.Code)
			require.NotContains(t, w.Body.String(), "synthetic protected row")
			require.Empty(t, w.Header().Get("X-Synthetic-Disclosure"))
		})
	}
}

func TestHandlerScopedRouteReleasesAdmittedAndErrorResponses(t *testing.T) {
	admitted := serveHandlerScope(t, handlerScopeHospital, func(c *gin.Context) {
		require.True(t, admitWithEvidence(c, handlerScopeRequest(handlerScopePerm), handlerScopeRequest(handlerScopePerm)))
		c.JSON(200, gin.H{"staff": "synthetic reviewed row"})
	})
	require.Equal(t, 200, admitted.Code)
	require.Contains(t, admitted.Body.String(), "synthetic reviewed row")

	empty := serveHandlerScope(t, handlerScopeHospital, func(c *gin.Context) {
		require.True(t, admitWithEvidence(c, handlerScopeRequest(handlerScopePerm), handlerScopeRequest(handlerScopePerm)))
		c.Status(204)
	})
	require.Equal(t, 204, empty.Code)

	for _, status := range []int{403, 404, 409, 503} {
		denied := serveHandlerScope(t, handlerScopeHospital, func(c *gin.Context) {
			c.AbortWithStatusJSON(status, gin.H{"error": "synthetic"})
		})
		require.Equal(t, status, denied.Code)
		require.Contains(t, denied.Body.String(), "synthetic")
	}

	invalid := serveHandlerScope(t, "not-a-hospital", func(*gin.Context) { t.Fatal("an invalid hospital must not reach the handler") })
	require.Equal(t, 400, invalid.Code)
}

func TestPOSTHandlerScopedRouteWithholdsUnadmittedSuccess(t *testing.T) {
	serve := func(t *testing.T, admit bool) *httptest.ResponseRecorder {
		t.Helper()
		gin.SetMode(gin.TestMode)
		e := gin.New()
		e.Use(func(c *gin.Context) {
			c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: handlerScopeTenant})
			c.Set("authz.permissions", authz.NewPermissionSet())
		})
		r := platform.NewRouter(e.Group("/v1"), scopeMembership(true))
		r.POSTHandlerScoped("/hospitals/:hospital/decision", handlerScopePerm, "hospital", func(c *gin.Context) {
			if admit {
				require.True(t, admitWithEvidence(c, handlerScopeRequest(handlerScopePerm), handlerScopeRequest(handlerScopePerm)))
			}
			c.JSON(200, gin.H{"staff": "synthetic decided row"})
		})
		declared := r.Declared()
		require.Len(t, declared, 1)
		require.Equal(t, "POST", declared[0].Method)
		require.Equal(t, handlerScopePerm, declared[0].Permission)
		require.Equal(t, "department", declared[0].ScopeKind)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest("POST", "/v1/hospitals/"+handlerScopeHospital+"/decision", nil))
		return w
	}
	withheld := serve(t, false)
	require.Equal(t, 500, withheld.Code)
	require.NotContains(t, withheld.Body.String(), "synthetic decided row")
	admitted := serve(t, true)
	require.Equal(t, 200, admitted.Code)
	require.Contains(t, admitted.Body.String(), "synthetic decided row")
	require.Panics(t, func() {
		platform.NewRouter(gin.New().Group("/v1"), scopeMembership(true)).POSTHandlerScoped("/decision", handlerScopePerm, "hospital", func(*gin.Context) {})
	})
}

func TestHandlerScopedRouteRefusesPublicPermissionAndMissingParameter(t *testing.T) {
	r := platform.NewRouter(gin.New().Group("/v1"), scopeMembership(true))
	require.Panics(t, func() {
		r.GETHandlerScoped("/hospitals/:hospital/review", authz.Public, "hospital", func(*gin.Context) {})
	})
	require.Panics(t, func() { r.GETHandlerScoped("/review", handlerScopePerm, "hospital", func(*gin.Context) {}) })
}

type handlerScopeRow struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func (r handlerScopeRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func TestHandlerScopedListCursorBindsCallerHospitalAndEveryFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: c.GetHeader("X-Test-Subject"), TenantID: handlerScopeTenant})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	r := platform.NewRouter(e.Group("/v1"), scopeMembership(true))
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	rows := []handlerScopeRow{{uuid.Must(uuid.NewV7()), base.Add(3 * time.Second)}, {uuid.Must(uuid.NewV7()), base.Add(2 * time.Second)}, {uuid.Must(uuid.NewV7()), base.Add(time.Second)}}
	scans := 0
	platform.HandlerScopedQueryListRoute(r, "/hospitals/:hospital/inbox", handlerScopePerm, "hospital", []platform.QueryFilter{{Key: "status", Default: "pending"}, {Key: "department"}}, 2, func(c *gin.Context, page pagination.Params) ([]handlerScopeRow, error) {
		scans++
		request := handlerScopeRequest(handlerScopePerm)
		request.Subject, request.HospitalID = c.GetHeader("X-Test-Subject"), c.Param("hospital")
		require.True(t, admitWithEvidence(c, request, request))
		if page.After == nil {
			return rows[:2], nil
		}
		return rows[1:], nil
	})
	declared := r.Declared()
	require.Len(t, declared, 1)
	require.True(t, declared[0].Paginated)
	require.Equal(t, handlerScopePerm, declared[0].Permission)
	require.Equal(t, "department", declared[0].ScopeKind)
	call := func(hospital, query, subject string, status int) map[string]any {
		t.Helper()
		request := httptest.NewRequest("GET", "/v1/hospitals/"+hospital+"/inbox?limit=1"+query, nil)
		request.Header.Set("X-Test-Subject", subject)
		response := httptest.NewRecorder()
		e.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	department := "019a5c40-0000-7000-8000-000000000003"
	first := call(handlerScopeHospital, "&department="+department, "alice", 200)
	query := "&cursor=" + url.QueryEscape(first["page"].(map[string]any)["next_cursor"].(string))
	second := call(handlerScopeHospital, query+"&status=pending&department="+department, "alice", 200)
	require.Equal(t, rows[1].ID.String(), second["data"].([]any)[0].(map[string]any)["id"])
	for _, change := range []struct{ hospital, filter, subject string }{
		{handlerScopeHospital, "&department=" + department + "&status=rejected", "alice"},
		{handlerScopeHospital, "", "alice"},
		{handlerScopeHospital, "&department=019a5c40-0000-7000-8000-000000000005", "alice"},
		{handlerScopeHospital, "&department=" + department, "bob"},
		{"00000000-0000-0000-0000-000000000009", "&department=" + department, "alice"},
	} {
		call(change.hospital, query+change.filter, change.subject, 400)
	}
	require.Equal(t, 2, scans, "foreign-filter positions must be refused before querying")
}

func TestHandlerScopedFilteredListAdvancesPastWithheldRowsWithoutServingThem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "synthetic-reviewer", TenantID: handlerScopeTenant})
		c.Set("authz.permissions", authz.NewPermissionSet())
	})
	sealer, err := pagination.NewSealer(bytes.Repeat([]byte{5}, 32))
	require.NoError(t, err)
	r := platform.NewRouter(e.Group("/v1"), scopeMembership(true)).WithCursorSealer(sealer)
	base := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	rows := make([]handlerScopeRow, 4)
	for i := range rows {
		rows[i] = handlerScopeRow{uuid.Must(uuid.NewV7()), base.Add(time.Duration(len(rows)-i) * time.Second)}
	}
	withheld, overrun := map[uuid.UUID]bool{rows[1].ID: true}, false
	platform.HandlerScopedFilteredQueryListRoute(r, "/hospitals/:hospital/queue", handlerScopePerm, "hospital", []platform.QueryFilter{{Key: "department"}}, 2, func(c *gin.Context, page pagination.Params) ([]platform.Filtered[handlerScopeRow], error) {
		request := handlerScopeRequest(handlerScopePerm)
		require.True(t, admitWithEvidence(c, request, request))
		start := 0
		for i, row := range rows {
			if page.After != nil && row.ID == page.After.ID {
				start = i + 1
			}
		}
		end := min(len(rows), start+page.Limit+1)
		if overrun {
			end = len(rows)
		}
		scanned := []platform.Filtered[handlerScopeRow]{}
		for _, row := range rows[start:end] {
			scanned = append(scanned, platform.Filtered[handlerScopeRow]{Row: row, Visible: !withheld[row.ID]})
		}
		return scanned, nil
	})
	declared := r.Declared()
	require.Len(t, declared, 1)
	require.True(t, declared[0].Paginated)
	require.Equal(t, "department", declared[0].ScopeKind)
	call := func(query string, status int) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest("GET", "/v1/hospitals/"+handlerScopeHospital+"/queue?"+query, nil))
		require.Equal(t, status, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	ids := func(body map[string]any) []string {
		out := []string{}
		for _, row := range body["data"].([]any) {
			out = append(out, row.(map[string]any)["id"].(string))
		}
		return out
	}
	const department = "department=019a5c40-0000-7000-8000-000000000003"
	first := call(department, 200)
	require.Equal(t, []string{rows[0].ID.String()}, ids(first), "the withheld row and the visible probe row are never served")
	require.Equal(t, true, first["page"].(map[string]any)["has_more"])
	next := first["page"].(map[string]any)["next_cursor"].(string)
	blob, err := base64.RawURLEncoding.DecodeString(next)
	require.NoError(t, err)
	require.NotContains(t, string(blob), rows[1].ID.String(), "the cursor anchored on a withheld row must not disclose it")
	_, err = pagination.Decode(next, handlerScopeTenant)
	require.ErrorIs(t, err, pagination.ErrInvalidCursor, "a filtered list never issues a readable cursor")
	call("department=019a5c40-0000-7000-8000-000000000004&cursor="+url.QueryEscape(next), 400)
	second := call(department+"&cursor="+url.QueryEscape(next), 200)
	require.Equal(t, []string{rows[2].ID.String(), rows[3].ID.String()}, ids(second), "the cursor anchors on the last scanned row, so nothing after a withheld row is skipped")
	require.Equal(t, false, second["page"].(map[string]any)["has_more"])
	plain := pagination.Cursor{TenantID: handlerScopeTenant, CreatedAt: rows[1].CreatedAt, ID: rows[1].ID}
	call(department+"&cursor="+plain.Encode(), 400)
	overrun = true
	failure := call(department, 500)
	require.NotContains(t, failure, "data")
}

func TestHandlerScopedFilteredListRequiresACursorSealer(t *testing.T) {
	r := platform.NewRouter(gin.New().Group("/v1"), scopeMembership(true))
	require.Panics(t, func() {
		platform.HandlerScopedFilteredQueryListRoute(r, "/hospitals/:hospital/queue", handlerScopePerm, "hospital", []platform.QueryFilter{{Key: "department"}}, 2, func(*gin.Context, pagination.Params) ([]platform.Filtered[handlerScopeRow], error) {
			return nil, nil
		})
	})
	require.Empty(t, r.Declared())
}
