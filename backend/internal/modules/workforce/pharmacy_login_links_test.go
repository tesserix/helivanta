package workforce_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type pharmacyLinkFixture struct {
	t                         *testing.T
	h                         *establishmentHarness
	pharmacy, staff, hospital string
	links                     string
}

// newPharmacyLinkFixture registers a pharmacy with one Staff record; "viewer" holds both link permissions but only reader access.
func newPharmacyLinkFixture(t *testing.T, options ...func(*testutil.HarnessOptions, *authz.Client)) *pharmacyLinkFixture {
	t.Helper()
	endpoint, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(endpoint))
	t.Cleanup(proxy.Close)
	fga, err := authz.NewClient(t.Context(), proxy.URL, "pharmacy-links-"+uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-candidate", authz.RoleStaffMember))
	both := []authz.Permission{workforce.PermLoginLinkManage, workforce.PermLoginLinkReview}
	module := workforce.New()
	opts := testutil.HarnessOptions{
		Tokens: map[string]string{"manager": testutil.TenantA, "reviewer": testutil.TenantA, "candidate": testutil.TenantA, "reader": testutil.TenantA, "viewer": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"manager":   append([]authz.Permission{workforce.PermHospitalManage}, both...),
			"reviewer":  {workforce.PermLoginLinkReview},
			"candidate": both,
			"viewer":    both,
			"outsider":  both,
			"foreign":   both,
		},
		Roles: fga, Modules: []platform.Module{module},
	}
	for _, option := range options {
		option(&opts, fga)
	}
	router, db, bus, _ := testutil.NewHarness(t, opts)
	h := &establishmentHarness{router: router, db: db, bus: bus, module: module, hospital: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "manager", `{"name":"Synthetic Links Hospital"}`).Code)
	f := &pharmacyLinkFixture{t: t, h: h, pharmacy: h.pharmacy(t, "independent"), staff: uuid.Must(uuid.NewV7()).String(), hospital: h.hospital}
	for subject, level := range map[string]string{"manager": "manager", "reviewer": "manager", "candidate": "manager", "reader": "manager", "viewer": "reader"} {
		f.call("PUT", f.pharmacy+"/access/user-"+subject, "manager", `{"level":"`+level+`"}`, 200)
	}
	f.call("PUT", f.pharmacy+"/staff/"+f.staff+"?directory_context=v1", "manager", strings.Replace(pharmacyStaffBody, "%d", "0", 1), 200)
	f.links = f.pharmacy + "/staff/" + f.staff + "/login-links"
	return f
}

func (f *pharmacyLinkFixture) call(method, path, actor, body string, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	w := pharmacyRequest(f.h, actor, method, path, body)
	require.Equal(f.t, status, w.Code, w.Body.String())
	return w
}

func (f *pharmacyLinkFixture) newLink() string {
	return f.links + "/" + uuid.Must(uuid.NewV7()).String()
}

const pharmacyLinkProposal = `{"subject":"user-candidate","reason":"Verified synthetic pharmacy account"}`

func TestPharmacyLoginLinkIsProposedReviewedAndRead(t *testing.T) {
	f := newPharmacyLinkFixture(t)
	link := f.newLink()
	w := f.call("PUT", link, "manager", pharmacyLinkProposal, 200)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var proposal map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &proposal))
	require.Equal(t, "pending", proposal["status"])
	require.Equal(t, float64(1), proposal["version"])
	require.Equal(t, f.staff, proposal["staff_id"])
	require.NotContains(t, proposal, "hospital_id")

	f.call("POST", link+"/decision", "candidate", `{"expected_version":1,"decision":"approve","reason":"Own account"}`, 403)
	f.call("POST", link+"/decision", "manager", `{"expected_version":1,"decision":"approve","reason":"Own proposal"}`, 403)
	w = f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &proposal))
	require.Equal(t, "active", proposal["status"])
	require.Equal(t, float64(2), proposal["version"])

	var page struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(f.call("GET", f.links, "reviewer", "", 200).Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, link[strings.LastIndex(link, "/")+1:], page.Data[0].ID)
	require.Equal(t, "active", page.Data[0].Status)
	var history struct {
		Data []struct {
			Status  string `json:"status"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(f.call("GET", link+"/history", "manager", "", 200).Body.Bytes(), &history))
	require.Len(t, history.Data, 2)
	require.Equal(t, "active", history.Data[0].Status)
	require.Equal(t, "pending", history.Data[1].Status)

	var stored struct {
		Establishment string
		Generation    *string
	}
	require.NoError(t, f.h.tx(t, f.pharmacy, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_login_link_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT hospital_id AS establishment, employment_generation_id AS generation FROM workforce_login_links WHERE tenant_id=? AND id=?`, testutil.TenantA, link[strings.LastIndex(link, "/")+1:]).Scan(&stored).Error
	}))
	require.Equal(t, f.pharmacy, stored.Establishment)
	require.Nil(t, stored.Generation)
}

func TestPharmacyLoginLinkRefusesCallersOutsideItsScope(t *testing.T) {
	f := newPharmacyLinkFixture(t)
	link := f.newLink()
	f.call("PUT", link, "manager", pharmacyLinkProposal, 200)
	id := link[strings.LastIndex(link, "/")+1:]
	decision := `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`
	hospitalLinks := "/v1/workforce/hospitals/" + f.pharmacy + "/staff/" + f.staff + "/login-links"
	wrongFamily := f.hospital + "/staff/" + f.staff + "/login-links"
	for _, request := range []struct{ method, suffix, body string }{
		{"PUT", "/" + uuid.Must(uuid.NewV7()).String(), pharmacyLinkProposal},
		{"POST", "/" + id + "/decision", decision},
		{"GET", "", ""},
		{"GET", "/" + id + "/history", ""},
	} {
		f.call(request.method, wrongFamily+request.suffix, "manager", request.body, 404)
		w := testutil.Do(f.h.router, request.method, hospitalLinks+request.suffix, "manager", request.body)
		require.Equal(t, 404, w.Code, "%s %s", request.method, request.suffix)
		f.call(request.method, f.links+request.suffix, "foreign", request.body, 404)
		f.call(request.method, f.links+request.suffix, "outsider", request.body, 404)
		f.call(request.method, f.links+request.suffix, "viewer", request.body, 404)
	}
	f.call("PUT", f.newLink(), "reviewer", pharmacyLinkProposal, 403)
	f.call("GET", f.links, "reader", "", 403)
	f.call("PUT", f.links+"/"+uuid.NewString(), "manager", pharmacyLinkProposal, 404)
	f.call("GET", strings.Replace(f.links, f.pharmacy, uuid.NewString(), 1), "manager", "", 404)
	f.call("PUT", f.pharmacy+"/staff/"+uuid.NewString()+"/login-links/"+uuid.Must(uuid.NewV7()).String(), "manager", pharmacyLinkProposal, 404)
	f.call("PUT", f.newLink(), "manager", strings.Replace(pharmacyLinkProposal, "user-candidate", "unknown-account", 1), 404)
}

func TestPharmacyLoginLinkReplaysAndRefusesConflicts(t *testing.T) {
	f := newPharmacyLinkFixture(t)
	link := f.newLink()
	first := f.call("PUT", link, "manager", pharmacyLinkProposal, 200)
	require.JSONEq(t, first.Body.String(), f.call("PUT", link, "manager", pharmacyLinkProposal, 200).Body.String())
	w := f.call("PUT", link, "manager", strings.Replace(pharmacyLinkProposal, "Verified", "Changed", 1), 409)
	require.Contains(t, w.Body.String(), `"error":"link_conflict"`)
	w = f.call("PUT", f.newLink(), "manager", pharmacyLinkProposal, 409)
	require.Contains(t, w.Body.String(), `"error":"link_conflict"`)
	decision := `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`
	first = f.call("POST", link+"/decision", "reviewer", decision, 200)
	require.JSONEq(t, first.Body.String(), f.call("POST", link+"/decision", "reviewer", decision, 200).Body.String())
	w = f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"reject","reason":"Stale review"}`, 409)
	require.Contains(t, w.Body.String(), `"error":"link_conflict"`)
}

func TestPharmacyLoginLinkChangesPublishThePharmacySubject(t *testing.T) {
	f := newPharmacyLinkFixture(t)
	link := f.newLink()
	f.call("PUT", link, "manager", pharmacyLinkProposal, 200)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`, 200)
	inactive := strings.Replace(strings.Replace(pharmacyStaffBody, "%d", "1", 1), `"employment_status":"active"`, `"employment_status":"inactive"`, 1)
	f.call("PUT", f.pharmacy+"/staff/"+f.staff+"?directory_context=v1", "manager", inactive, 200)
	w := f.call("GET", link+"/history", "manager", "", 200)
	require.Contains(t, w.Body.String(), `"status":"revoked"`)

	require.NoError(t, f.h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var hospitalRows int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.LoginLinkChanged).Count(&hospitalRows).Error)
		require.Zero(t, hospitalRows, "a pharmacy link never travels as a hospital link")
		var payloads []string
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.PharmacyLoginLinkChanged).Pluck("payload", &payloads).Error)
		byStatus := map[string]map[string]any{}
		for _, payload := range payloads {
			var envelope struct {
				Type string         `json:"type"`
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(payload), &envelope))
			require.Equal(t, workforce.PharmacyLoginLinkChanged, envelope.Type)
			byStatus[envelope.Data["status"].(string)] = envelope.Data
		}
		require.Len(t, payloads, 3)
		for version, status := range []string{"pending", "active", "revoked"} {
			require.Equal(t, map[string]any{"pharmacy_id": f.pharmacy, "staff_id": f.staff, "link_id": link[strings.LastIndex(link, "/")+1:], "version": float64(version + 1), "status": status}, byStatus[status])
		}
		return nil
	}))
}

func TestPharmacyLoginLinkIsNotAHospitalScopeChoice(t *testing.T) {
	authority := &leaveScopeAuthority{grant: authz.ScopeGrant{AssignmentID: uuid.Must(uuid.NewV7()).String(), Version: 1, ScopeEpoch: 1, MembershipEpoch: 1, ScopeKind: "hospital"}}
	f := newPharmacyLinkFixture(t, func(opts *testutil.HarnessOptions, fga *authz.Client) {
		require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-scoped", authz.RoleStaffMember))
		opts.Tokens["scoped"] = testutil.TenantA
		opts.ScopedAuthorizer = func(db *tenantdb.DB) authz.ScopedAuthorizer { authority.db = db; return authority }
	})
	link := f.newLink()
	f.call("PUT", link, "manager", strings.Replace(pharmacyLinkProposal, "user-candidate", "user-scoped", 1), 200)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independently verified"}`, 200)

	request := httptest.NewRequest("GET", "/v1/workforce/leave-review-scopes", nil)
	request.Header.Set("Authorization", "Bearer scoped")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	w := httptest.NewRecorder()
	f.h.router.ServeHTTP(w, request)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), f.pharmacy)
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Empty(t, page.Data)
}
