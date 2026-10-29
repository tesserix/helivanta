package workforce_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external owner HTTP tests
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestBasicStaffRejectsChangedOpeningAccount(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic directory hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	path := base + "/staff/" + uuid.NewString()
	body := `{"employee_code":"SYN-DIRECTORY","display_name":"Synthetic directory worker","job_category":"other","employment_status":"active"}`
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", body).Code)
	for _, method := range []string{"GET", "PUT"} {
		for _, header := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
			t.Run(method+"/"+header, func(t *testing.T) {
				req := httptest.NewRequest(method, path, strings.NewReader(strings.ReplaceAll(body, "worker", "changed")))
				req.Header.Set("Authorization", "Bearer admin")
				req.Header.Set("Content-Type", "application/json")
				if header == "X-Helivanta-Expected-Tenant" {
					req.Header.Set(header, testutil.TenantB)
				} else {
					req.Header.Set(header, "another-account")
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "session_changed")
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			})
		}
	}
	w := testutil.Do(r, "GET", path, "admin", "")
	require.Equal(t, 200, w.Code)
	var current map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
	require.Equal(t, "Synthetic directory worker", current["display_name"])
}

func TestBasicStaffNegotiatesVersionedDirectory(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic versioned directory"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	path := base + "/staff/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", `{"employee_code":"SYN-VERSION","display_name":"Synthetic worker","job_category":"other","employment_status":"active"}`).Code)
	req := httptest.NewRequest("GET", path+"?directory_context=v1", nil)
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	req.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	require.Equal(t, "v1", snapshot["directory_context_version"])
	require.Equal(t, float64(1), snapshot["version"])
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func directoryRequest(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	req.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestBasicStaffVersionedSavesPreserveConcurrentChangesAndReplay(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic save hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	staffID := uuid.NewString()
	path := base + "/staff/" + staffID + "?directory_context=v1"
	body := `{"employee_code":"SYN-SAFE-SAVE","display_name":"Synthetic original","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`
	w := directoryRequest(r, "PUT", path, body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var first map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Equal(t, float64(1), first["version"])
	require.Equal(t, "v1", first["directory_context_version"])
	currentBody := strings.ReplaceAll(strings.ReplaceAll(body, "Synthetic original", "Synthetic newer"), `"expected_version":0`, `"expected_version":1`)
	w = directoryRequest(r, "PUT", path, currentBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	var newer map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &newer))
	require.Equal(t, float64(2), newer["version"])
	auditCount := func() int64 {
		var n int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Where("hospital_id=? AND resource_id=?", hospital, staffID).Count(&n).Error
		}))
		return n
	}
	before := auditCount()
	require.Positive(t, before, "the audit assertion must observe persisted Staff writes")
	w = directoryRequest(r, "PUT", path, strings.ReplaceAll(currentBody, "Synthetic newer", "Synthetic stale"))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "staff_version_conflict")
	require.Equal(t, before, auditCount())
	w = directoryRequest(r, "PUT", path, currentBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	var replay map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &replay))
	require.Equal(t, newer, replay)
	require.Equal(t, before, auditCount())
	w = directoryRequest(r, "GET", path, "")
	require.Equal(t, 200, w.Code)
	var read map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &read))
	require.Equal(t, newer, read)
}

func TestBasicStaffDirectoryProtocolRejectsMalformedAndMissingPreconditions(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic protocol hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	path := base + "/staff/" + uuid.NewString()
	legacy := `{"employee_code":"SYN-PROTOCOL","display_name":"Synthetic protocol worker","job_category":"other","employment_status":"active"}`
	w := testutil.Do(r, "PUT", path, "admin", legacy)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), `"version"`)
	require.NotContains(t, w.Body.String(), "directory_context_version")
	projected := strings.TrimSuffix(legacy, "}") + `,"expected_version":1,"directory_context_version":"v1"}`
	for _, tc := range []struct {
		name, method, query, body string
		headers                   map[string][]string
		status                    int
	}{
		{name: "missing tenant", method: "GET", query: "?directory_context=v1", headers: map[string][]string{"X-Helivanta-Expected-Tenant": nil}, status: 409},
		{name: "missing subject", method: "PUT", query: "?directory_context=v1", body: projected, headers: map[string][]string{"X-Helivanta-Expected-Subject": nil}, status: 409},
		{name: "duplicate subject", method: "GET", query: "?directory_context=v1", headers: map[string][]string{"X-Helivanta-Expected-Subject": {"user-admin", "user-admin"}}, status: 409},
		{name: "comma joined tenant", method: "GET", query: "?directory_context=v1", headers: map[string][]string{"X-Helivanta-Expected-Tenant": {testutil.TenantA + ", " + testutil.TenantA}}, status: 409},
		{name: "duplicate projection", method: "GET", query: "?directory_context=v1&directory_context=v1", status: 400},
		{name: "unsupported projection", method: "GET", query: "?directory_context=v2", status: 400},
		{name: "missing metadata", method: "PUT", query: "?directory_context=v1", body: legacy, status: 400},
		{name: "metadata without projection", method: "PUT", body: projected, status: 400},
		{name: "null metadata without projection", method: "PUT", body: strings.TrimSuffix(legacy, "}") + `,"expected_version":null}`, status: 400},
		{name: "negative expected version", method: "PUT", query: "?directory_context=v1", body: strings.ReplaceAll(projected, `"expected_version":1`, `"expected_version":-1`), status: 400},
		{name: "unsafe integer", method: "PUT", query: "?directory_context=v1", body: strings.ReplaceAll(projected, `"expected_version":1`, `"expected_version":9007199254740992`), status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, path+tc.query, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer admin")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
			req.Header.Set("X-Helivanta-Expected-Subject", "user-admin")
			for key, values := range tc.headers {
				req.Header.Del(key)
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		})
	}
	w = directoryRequest(r, "GET", path+"?directory_context=v1", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var value map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &value))
	require.Equal(t, float64(1), value["version"])
	require.Equal(t, "Synthetic protocol worker", value["display_name"])
}

func TestBasicStaffConcurrentCreatesNeverOverwriteDifferentContent(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic concurrent hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	path := base + "/staff/" + uuid.NewString() + "?directory_context=v1"
	body := `{"employee_code":"SYN-RACE","display_name":"Synthetic first","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, value := range []string{body, strings.ReplaceAll(body, "Synthetic first", "Synthetic second")} {
		go func(value string) { <-start; results <- directoryRequest(r, "PUT", path, value) }(value)
	}
	close(start)
	codes := []int{}
	var winner map[string]any
	for range 2 {
		w := <-results
		codes = append(codes, w.Code)
		if w.Code == 200 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &winner))
		}
	}
	require.ElementsMatch(t, []int{200, 409}, codes)
	w := directoryRequest(r, "GET", path, "")
	require.Equal(t, 200, w.Code)
	var saved map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	require.Equal(t, winner, saved)
	require.Equal(t, float64(1), saved["version"])
}

func TestVersionedStaffInactivationKeepsReviewedLinksRevokedOnReplayAndReactivation(t *testing.T) {
	f := newLoginLinkFixture(t)
	link := f.path + "/" + uuid.Must(uuid.NewV7()).String()
	f.call("PUT", link, "manager", `{"subject":"user-candidate","reason":"Verified synthetic lifecycle binding"}`, 200)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independently reviewed synthetic binding"}`, 200)
	path := f.base + "/staff/" + f.staff + "?directory_context=v1"
	call := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer manager")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-manager")
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		return w
	}
	w := call("GET", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	body := map[string]any{"employee_code": "LINK-01", "display_name": "Synthetic Link Staff", "job_category": "nurse", "employment_status": "inactive", "expected_version": snapshot["version"], "directory_context_version": "v1"}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	w = call("PUT", string(data))
	require.Equal(t, 200, w.Code, w.Body.String())
	var inactive map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inactive))
	w = call("PUT", string(data))
	require.Equal(t, 200, w.Code, w.Body.String())
	var replay map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &replay))
	require.Equal(t, inactive, replay)
	w = f.call("GET", link+"/history", "manager", "", 200)
	var history struct {
		Data []struct {
			Status  string
			Version int64
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history.Data, 3)
	require.Equal(t, "revoked", history.Data[0].Status)
	require.Equal(t, int64(3), history.Data[0].Version)
	body["expected_version"] = inactive["version"]
	body["employment_status"] = "active"
	data, err = json.Marshal(body)
	require.NoError(t, err)
	w = call("PUT", string(data))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = f.call("GET", f.path, "manager", "", 200)
	require.Contains(t, w.Body.String(), `"status":"revoked"`)
	f.call("POST", link+"/decision", "reviewer", `{"expected_version":3,"decision":"approve","reason":"Attempt to restore historical authority"}`, 409)
}

func TestVersionedStaffDirectoryRetainsHospitalAndTenantAuthority(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}, "reader": {workforce.PermEnrollmentManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic authority hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-reader", "admin", `{"level":"reader"}`).Code)
	path := base + "/staff/" + uuid.NewString()
	body := `{"employee_code":"SYN-ISOLATED","display_name":"Synthetic isolated worker","job_category":"other","employment_status":"active"}`
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", body).Code)
	for _, tc := range []struct {
		token, tenant, method string
		status                int
	}{
		{"reader", testutil.TenantA, "GET", 200}, {"reader", testutil.TenantA, "PUT", 404},
		{"other", testutil.TenantB, "GET", 404}, {"other", testutil.TenantB, "PUT", 404},
	} {
		t.Run(tc.token+"/"+tc.method, func(t *testing.T) {
			requestBody := strings.TrimSuffix(body, "}") + `,"expected_version":1,"directory_context_version":"v1"}`
			req := httptest.NewRequest(tc.method, path+"?directory_context=v1", strings.NewReader(requestBody))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Helivanta-Expected-Tenant", tc.tenant)
			req.Header.Set("X-Helivanta-Expected-Subject", "user-"+tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 404 {
				require.NotContains(t, w.Body.String(), "Synthetic isolated worker")
			}
		})
	}
}
