package workforce_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type loginLinkFixture struct {
	t                 *testing.T
	r                 *gin.Engine
	db                *tenantdb.DB
	base, staff, path string
	fga               *authz.Client
	stopIdentity      func()
}

func newLoginLinkFixture(t *testing.T) *loginLinkFixture {
	t.Helper()
	endpoint, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(endpoint))
	t.Cleanup(proxy.Close)
	fga, err := authz.NewClient(t.Context(), proxy.URL, "links-"+uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-candidate", authz.RoleStaffMember))
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"manager": testutil.TenantA, "reviewer": testutil.TenantA, "candidate": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"manager":   {workforce.PermHospitalManage, workforce.PermEnrollmentManage, "workforce.login_links.manage", "workforce.login_links.review"},
			"reviewer":  {"workforce.login_links.review"},
			"candidate": {"workforce.login_links.manage", "workforce.login_links.review"},
			"foreign":   {"workforce.login_links.manage"},
		},
		Roles: fga, Modules: []platform.Module{workforce.New()},
	})
	f := &loginLinkFixture{t: t, r: r, db: db, base: "/v1/workforce/hospitals/" + uuid.NewString(), staff: uuid.NewString(), fga: fga, stopIdentity: proxy.Close}
	f.call("PUT", f.base, "manager", `{"name":"Synthetic Links Hospital"}`, 200)
	for _, subject := range []string{"manager", "reviewer", "candidate", "reader"} {
		f.call("PUT", f.base+"/access/user-"+subject, "manager", `{"level":"manager"}`, 200)
	}
	f.path = f.base + "/staff/" + f.staff + "/login-links"
	f.call("PUT", f.base+"/staff/"+f.staff, "manager", `{"employee_code":"LINK-01","display_name":"Synthetic Link Staff","job_category":"nurse","employment_status":"active"}`, 200)
	return f
}

func (f *loginLinkFixture) call(method, path, actor, body string, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	w := testutil.Do(f.r, method, path, actor, body)
	require.Equal(f.t, status, w.Code, w.Body.String())
	return w
}

func TestLoginLinkNeedsIndependentReview(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	w := f.call("PUT", link, "manager", `{"subject":"user-candidate","reason":"Verified synthetic account"}`, 200)
	var proposal map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &proposal))
	require.Equal(t, "pending", proposal["status"])
	require.Equal(t, float64(1), proposal["version"])
	f.call("POST", link+"/decision", "candidate", `{"expected_version":1,"decision":"approve","reason":"Self review"}`, 403)
	f.call("POST", link+"/decision", "manager", `{"expected_version":1,"decision":"approve","reason":"Self review"}`, 403)
	w = f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &proposal))
	require.Equal(t, "active", proposal["status"])
	require.Equal(t, float64(2), proposal["version"])
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestLoginLinkRetriesAndUniqueBindings(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	body := `{"subject":"user-candidate","reason":"Private link rationale"}`
	first := f.call("PUT", link, "manager", body, 200)
	repeat := f.call("PUT", link, "manager", body, 200)
	require.JSONEq(t, first.Body.String(), repeat.Body.String())
	f.call("PUT", link, "manager", strings.Replace(body, "rationale", "conflicting reason", 1), 409)
	w := f.call("PUT", f.path+"/"+uuid.Must(uuid.NewV7()).String(), "manager", body, 409)
	require.Contains(t, w.Body.String(), `"error":"link_conflict"`)
	secondStaff := f.base + "/staff/" + uuid.NewString()
	f.call("PUT", secondStaff, "manager", `{"employee_code":"LINK-02","display_name":"Second Synthetic Staff","job_category":"nurse","employment_status":"active"}`, 200)
	f.call("PUT", secondStaff+"/login-links/"+uuid.Must(uuid.NewV7()).String(), "manager", body, 409)
	decision := `{"expected_version":1,"decision":"approve","reason":"Independent private rationale"}`
	first = f.call("POST", link+"/decision", "reviewer", decision, 200)
	repeat = f.call("POST", link+"/decision", "reviewer", decision, 200)
	require.JSONEq(t, first.Body.String(), repeat.Body.String())
	f.call("POST", link+"/decision", "reviewer", strings.Replace(decision, "rationale", "changed rationale", 1), 409)
	f.call("POST", link+"/decision", "manager", `{"expected_version":2,"decision":"revoke","reason":"Wrong account selected"}`, 200)
	f.call("PUT", f.path+"/"+uuid.Must(uuid.NewV7()).String(), "manager", body, 200)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var payloads []string
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.LoginLinkChanged).Pluck("payload", &payloads).Error)
		require.Len(t, payloads, 4)
		for _, body := range payloads {
			require.NotContains(t, body, "rationale")
			require.NotContains(t, body, "user-candidate")
		}
		var audits []string
		require.NoError(t, tx.Table("workforce_audit").Where("action='workforce.login_link.changed'").Pluck("changes", &audits).Error)
		require.Len(t, audits, 4)
		for _, body := range audits {
			require.NotContains(t, body, "rationale")
			require.NotContains(t, body, "user-candidate")
		}
		return nil
	}))
}

func TestLoginLinkScopeAndMembershipFailures(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	body := `{"subject":"user-candidate","reason":"Private scope check"}`
	f.call("PUT", link, "reader", body, 403)
	f.call("PUT", link, "foreign", body, 404)
	f.call("PUT", link, "manager", strings.Replace(body, "user-candidate", "unknown-account", 1), 404)
	for _, subject := range []string{"*", "user:someone", "user#member", "user candidate", " user-candidate"} {
		f.call("PUT", link, "manager", string(mustJSON(t, map[string]string{"subject": subject, "reason": "Invalid account identifier"})), 400)
	}
	f.call("PUT", link, "manager", body, 200)
	f.call("GET", f.path, "reader", "", 403)
	f.call("GET", link+"/history", "foreign", "", 404)
	request := httptest.NewRequest("GET", f.path, nil)
	request.Header.Set("Authorization", "Bearer manager")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
	response := httptest.NewRecorder()
	f.r.ServeHTTP(response, request)
	require.Equal(t, 404, response.Code)
	require.NotContains(t, response.Body.String(), "user-candidate")
	require.NoError(t, f.fga.RevokeRole(t.Context(), testutil.TenantA, "user-candidate", authz.RoleStaffMember))
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Membership was removed"}`, 404)
	f.stopIdentity()
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Unavailable membership"}`, 503)
	w := f.call("GET", link+"/history", "reviewer", "", 200)
	require.NotContains(t, w.Body.String(), `"status":"active"`)
	f.call("DELETE", f.base+"/access/user-reviewer", "manager", "", 204)
	f.call("GET", link+"/history", "reviewer", "", 404)
}

func TestLoginLinkDatabaseGuardsAndPrivateReads(t *testing.T) {
	f := newLoginLinkFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	f.call("PUT", f.path+"/"+id, "manager", `{"subject":"user-candidate","reason":"Private database guard reason"}`, 200)
	f.call("GET", f.path, "manager", "", 200)
	f.call("GET", f.path+"/"+id+"/history", "reviewer", "", 200)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		for _, table := range []string{"workforce_login_links", "workforce_login_link_history"} {
			var count int64
			require.NoError(t, tx.Table(table).Count(&count).Error)
			require.Zero(t, count, "link data requires explicit scope")
		}
		var count int64
		require.NoError(t, tx.Table("workforce_audit").Where("action='workforce.login_link.read'").Count(&count).Error)
		require.EqualValues(t, 2, count)
		return nil
	}))
	for _, statement := range []string{
		`UPDATE workforce_login_links SET subject='another-user' WHERE id=?`,
		`UPDATE workforce_login_links SET status='active',version=2 WHERE id=?`,
		`DELETE FROM workforce_login_links WHERE id=?`,
		`UPDATE workforce_login_link_history SET reason='rewritten history' WHERE id=?`,
	} {
		err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','review',true)`, strings.TrimPrefix(f.base, "/v1/workforce/hospitals/")).Error; err != nil {
				return err
			}
			return tx.Exec(statement, id).Error
		})
		require.Error(t, err, statement)
	}
}

func TestStaffInactivationRevokesLoginLink(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	f.call("PUT", link, "manager", `{"subject":"user-candidate","reason":"Verified synthetic account"}`, 200)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`, 200)
	staffPath := f.base + "/staff/" + f.staff
	inactive := `{"employee_code":"LINK-01","display_name":"Synthetic Link Staff","job_category":"nurse","employment_status":"inactive"}`
	f.call("PUT", staffPath, "manager", inactive, 200)
	f.call("PUT", staffPath, "manager", inactive, 200)
	w := f.call("GET", link+"/history", "manager", "", 200)
	var history struct {
		Data []struct {
			Status  string `json:"status"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history.Data, 3)
	require.Equal(t, "revoked", history.Data[0].Status)
	require.Equal(t, int64(3), history.Data[0].Version)
	f.call("PUT", staffPath, "manager", `{"employee_code":"LINK-01","display_name":"Synthetic Link Staff","job_category":"nurse","employment_status":"active"}`, 200)
	w = f.call("GET", f.path, "manager", "", 200)
	require.Contains(t, w.Body.String(), `"status":"revoked"`)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":3,"decision":"approve","reason":"Restore old link"}`, 409)
}

func TestEnrollmentInactivationRevokesPendingLink(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	f.call("PUT", link, "manager", `{"subject":"user-candidate","reason":"Verified synthetic account"}`, 200)
	path := f.base + "/staff/" + f.staff + "/enrollment"
	w := f.call("GET", path, "manager", "", 200)
	var enrollment struct {
		Version int64 `json:"version"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &enrollment))
	body := string(mustJSON(t, map[string]any{"expected_version": enrollment.Version, "staff": map[string]string{"employee_code": "LINK-01", "display_name": "Synthetic Link Staff", "job_category": "nurse", "employment_status": "inactive"}}))
	f.call("PUT", path, "manager", body, 200)
	w = f.call("GET", link+"/history", "manager", "", 200)
	require.Contains(t, w.Body.String(), `"status":"revoked"`)
	require.Contains(t, w.Body.String(), `"version":2`)
}

func TestLoginLinkApprovalRacingInactivationCannotLeaveAccessLinkActive(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	f.call("PUT", link, "manager", `{"subject":"user-candidate","reason":"Verified synthetic account"}`, 200)
	start := make(chan struct{})
	approval := make(chan *httptest.ResponseRecorder, 1)
	inactivation := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		<-start
		approval <- testutil.Do(f.r, "POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independent review"}`)
	}()
	go func() {
		<-start
		inactivation <- testutil.Do(f.r, "PUT", f.base+"/staff/"+f.staff, "manager", `{"employee_code":"LINK-01","display_name":"Synthetic Link Staff","job_category":"nurse","employment_status":"inactive"}`)
	}()
	close(start)
	a, inactive := <-approval, <-inactivation
	require.Equal(t, 200, inactive.Code, inactive.Body.String())
	require.Contains(t, []int{200, 409}, a.Code, a.Body.String())
	w := f.call("GET", f.path, "reviewer", "", 200)
	var links struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &links))
	require.Len(t, links.Data, 1)
	require.Equal(t, "revoked", links.Data[0].Status)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independent review"}`, 409)
}
