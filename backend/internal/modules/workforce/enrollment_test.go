package workforce_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports the module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestEnrollmentPersistsPrivateFieldsWithoutDirectoryDisclosure(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, "workforce.enrollment.manage"}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Enrollment Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	path := base + "/staff/" + uuid.NewString()
	body := `{"expected_version":0,"staff":{"employee_code":"ENR-001","display_name":"Avery Synthetic","job_category":"nurse","employment_status":"active"},"profile":{"preferred_name":"Avery","date_of_birth":"1995-03-15","gender":"other","personal_email":"avery@example.invalid","mobile_phone":"+61400000001","blood_group":"AB-","engagement_type":"employee","department":"Emergency","designation":"Registered nurse","start_date":"2024-02-01"},"addresses":[{"id":"11111111-1111-4111-8111-111111111111","use":"current","line1":"1 Synthetic Avenue","city":"Melbourne","country":"AU"}],"emergency_contacts":[{"id":"22222222-2222-4222-8222-222222222222","name":"Morgan Synthetic","relationship":"partner","phone":"+61400000002","priority":1}],"credentials":[],"qualifications":[],"experiences":[]}`
	w := testutil.Do(r, "PUT", path+"/enrollment", "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var saved map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	profile := saved["profile"].(map[string]any)
	require.Equal(t, "avery@example.invalid", profile["personal_email"])
	require.Equal(t, "AB-", profile["blood_group"])
	require.Equal(t, "1995-03-15", profile["date_of_birth"])
	w = testutil.Do(r, "GET", path+"/enrollment", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.JSONEq(t, string(mustJSON(t, saved)), w.Body.String())
	for _, route := range []string{path, base + "/staff"} {
		w = testutil.Do(r, "GET", route, "admin", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.NotContains(t, w.Body.String(), "avery@example.invalid")
		require.NotContains(t, w.Body.String(), "blood_group")
		require.NotContains(t, w.Body.String(), "emergency_contacts")
	}
}

func TestEnrollmentAuthorizationIsolationRetryAndVersioning(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "directory": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin":     {workforce.PermHospitalManage, workforce.PermEnrollmentManage},
			"directory": {}, "reader": {workforce.PermEnrollmentManage},
			"outsider": {workforce.PermEnrollmentManage, workforce.PermHospitalManage},
		}, Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Isolation Hospital"}`).Code)
	for token, level := range map[string]string{"admin": "manager", "directory": "manager", "reader": "reader"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+token, "admin", `{"level":"`+level+`"}`).Code)
	}
	id := uuid.NewString()
	path := base + "/staff/" + id + "/enrollment"
	input := map[string]any{
		"expected_version": 0,
		"staff":            map[string]any{"employee_code": "ENR-002", "display_name": "Robin Synthetic", "job_category": "doctor", "employment_status": "active"},
		"profile":          map[string]any{"personal_email": "robin@example.invalid", "engagement_type": "consultant", "start_date": "2023-01-01"},
		"credentials":      []any{map[string]any{"id": uuid.NewString(), "type": "Medical registration", "number": "SYNTHETIC-001", "issuer": "Synthetic Council", "country": "AU", "valid_from": "2023-01-01", "valid_until": "2028-01-01"}},
		"qualifications":   []any{map[string]any{"id": uuid.NewString(), "qualification": "Synthetic MBBS", "institution": "Synthetic University", "completion_year": 2020}},
		"experiences":      []any{map[string]any{"id": uuid.NewString(), "employer": "Synthetic Clinic", "role": "Resident", "start_date": "2020-01-01", "end_date": "2022-12-31"}},
	}
	body := string(mustJSON(t, input))
	const concurrent = 8
	codes := make([]int, concurrent)
	bodies := make([]string, concurrent)
	var wg sync.WaitGroup
	for i := range concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := testutil.Do(r, "PUT", path, "admin", body)
			codes[i] = w.Code
			bodies[i] = w.Body.String()
		}()
	}
	wg.Wait()
	for i, code := range codes {
		require.Equal(t, 200, code, bodies[i])
		require.JSONEq(t, bodies[0], bodies[i])
	}
	duplicate := strings.Replace(body, "ENR-002", "enr-002", 1)
	wDuplicate := testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", duplicate)
	require.Equal(t, 409, wDuplicate.Code, "employee codes must be unique without case distinctions")
	for _, tc := range []struct {
		token string
		code  int
	}{{"directory", 403}, {"reader", 404}, {"outsider", 404}} {
		for _, method := range []string{"GET", "PUT"} {
			w := testutil.Do(r, method, path, tc.token, body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "robin@example.invalid")
		}
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", workforce.EnrollmentChanged).Count(&n).Error)
		require.EqualValues(t, 1, n)
		var payload string
		require.NoError(t, tx.Raw("SELECT payload::text FROM outbox_events WHERE subject = ?", workforce.EnrollmentChanged).Scan(&payload).Error)
		require.NotContains(t, payload, "robin")
		require.NotContains(t, payload, "SYNTHETIC-001")
		require.NoError(t, tx.Table("workforce_audit").Where("resource_id = ? AND action = 'workforce_staff.insert'", id).Count(&n).Error)
		require.EqualValues(t, 1, n)
		for _, table := range []string{"profile", "credentials", "qualifications", "experiences"} {
			require.NoError(t, tx.Table("workforce_enrollment_"+table).Count(&n).Error)
			require.Zero(t, n, "private RLS must deny transactions without enrollment scope")
		}
		return nil
	}))
	input["expected_version"] = 1
	input["profile"].(map[string]any)["department"] = "Cardiology"
	w := testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, input)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	input["profile"].(map[string]any)["department"] = "Emergency"
	w = testutil.Do(r, "PUT", path, "admin", string(mustJSON(t, input)))
	require.Equal(t, 409, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", path, "admin", "")
	require.Contains(t, w.Body.String(), "Cardiology")
	require.NotContains(t, w.Body.String(), "Emergency")
	require.Equal(t, 204, testutil.Do(r, "DELETE", base+"/access/user-admin", "admin", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "admin", "").Code)
}

func TestEnrollmentRejectsInvalidAndDuplicateRecordsAtomically(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Validation Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	for _, tc := range []struct {
		name  string
		extra string
	}{
		{"impossible birth date", `"profile":{"date_of_birth":"2024-02-30"}`},
		{"future birth date", `"profile":{"date_of_birth":"2199-01-01"}`},
		{"bad phone", `"profile":{"mobile_phone":"0400000000"}`},
		{"invalid blood group", `"profile":{"blood_group":"AB"}`},
		{"injected verification", `"profile":{"blood_group":"O+","verified":true}`},
		{"invalid period", `"profile":{"start_date":"2024-01-01","end_date":"2023-01-01"}`},
		{"end without start", `"profile":{"end_date":"2024-01-01"}`},
		{"missing emergency name", `"emergency_contacts":[{"id":"11111111-1111-4111-8111-111111111111","relationship":"partner","phone":"+61400000001","priority":1}]`},
		{"duplicate address use", `"addresses":[{"id":"11111111-1111-4111-8111-111111111111","use":"current","line1":"Synthetic One","country":"AU"},{"id":"22222222-2222-4222-8222-222222222222","use":"current","line1":"Synthetic Two","country":"AU"}]`},
		{"duplicate record ID", `"qualifications":[{"id":"11111111-1111-4111-8111-111111111111","qualification":"A","institution":"B"},{"id":"11111111-1111-4111-8111-111111111111","qualification":"C","institution":"D"}]`},
		{"unknown government field", `"profile":{"aadhaar":"SYNTHETIC"}`},
		{"nonexistent supervisor", `"profile":{"supervisor_id":"11111111-1111-4111-8111-111111111111"}`},
		{"missing credential issuer", `"credentials":[{"id":"11111111-1111-4111-8111-111111111111","type":"Nursing","number":"SYNTHETIC"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			path := base + "/staff/" + id
			body := fmt.Sprintf(`{"expected_version":0,"staff":{"employee_code":"%s","display_name":"Synthetic Validation","job_category":"other","employment_status":"active"},%s}`, id, tc.extra)
			w := testutil.Do(r, "PUT", path+"/enrollment", "admin", body)
			want := 400
			if tc.name == "nonexistent supervisor" {
				want = 404
			}
			require.Equal(t, want, w.Code, w.Body.String())
			require.Equal(t, 404, testutil.Do(r, "GET", path, "admin", "").Code)
		})
	}
	id := uuid.NewString()
	path := base + "/staff/" + id + "/enrollment"
	oversized := `{"expected_version":0,"staff":{"employee_code":"BIG","display_name":"Synthetic","job_category":"other","employment_status":"active"},"profile":{"exit_reason":"` + strings.Repeat("a", 33000) + `"}}`
	require.Equal(t, 400, testutil.Do(r, "PUT", path, "admin", oversized).Code)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	require.NoError(t, err)
	return b
}
