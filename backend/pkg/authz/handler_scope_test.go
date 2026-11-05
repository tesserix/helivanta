package authz_test

import (
	"context"
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
