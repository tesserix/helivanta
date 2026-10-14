package workforce_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestHRMastersVersionHistoryIsolationAndRetries(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, "workforce.master.manage"}, "reader": {"workforce.master.manage"}, "directory": {}, "foreign": {"workforce.master.manage"}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic HR Hospital"}`).Code)
	for user, level := range map[string]string{"admin": "manager", "reader": "reader", "directory": "manager"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`).Code)
	}
	path := base + "/hr/masters/" + uuid.NewString()
	body := `{"expected_version":0,"kind":"department","code":"SYN-CARE","name":"Synthetic Care","description":"Synthetic example","active":true}`
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() { defer wg.Done(); codes[i] = testutil.Do(r, "PUT", path, "admin", body).Code }()
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	w := testutil.Do(r, "GET", base+"/hr/masters?kind=department", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var page struct {
		Data []struct {
			Version    int64
			RevisionID string `json:"revision_id"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.EqualValues(t, 1, page.Data[0].Version)
	for _, tc := range []struct {
		token string
		code  int
	}{{"reader", 404}, {"directory", 403}, {"foreign", 404}} {
		require.Equal(t, tc.code, testutil.Do(r, "PUT", path, tc.token, body).Code)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", base+"/hr/masters", "foreign", "").Code)
	require.Equal(t, 401, testutil.Do(r, "GET", base+"/hr/masters", "", "").Code)
	for _, duplicate := range []string{strings.Replace(body, "SYN-CARE", "syn-care", 1), strings.Replace(body, "SYN-CARE", "SYN-OTHER", 1)} {
		require.Equal(t, 409, testutil.Do(r, "PUT", base+"/hr/masters/"+uuid.NewString(), "admin", duplicate).Code)
	}
	changed := strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), "Synthetic Care", "Synthetic Care Revised", 1)
	w = testutil.Do(r, "PUT", path, "admin", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(changed, "Synthetic Care Revised", "Stale Change", 1)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(changed, "SYN-CARE", "SYN-EDITED", 1)).Code)
	w = testutil.Do(r, "GET", path+"/history", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"Synthetic Care"`)
	require.Contains(t, w.Body.String(), `"name":"Synthetic Care Revised"`)
	require.Equal(t, 400, testutil.Do(r, "GET", base+"/hr/masters?kind=religion", "admin", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/workforce/hospitals/"+uuid.NewString()+"/hr/masters", "admin", "").Code)
	for _, invalid := range []string{strings.Replace(body, `"department"`, `"religion"`, 1), strings.Replace(body, `"Synthetic Care"`, `"   "`, 1), strings.Replace(body, `"active":true`, `"active":true,"tenant_id":"injected"`, 1)} {
		require.Equal(t, 400, testutil.Do(r, "PUT", path, "admin", invalid).Code)
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_hr_master_revisions").Count(&n).Error)
		require.Zero(t, n, "RLS denies missing hospital scope")
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", "helivanta.in.workforce.master_changed.v1").Count(&n).Error)
		require.EqualValues(t, 2, n)
		return nil
	}))
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_master','authorized',true)`, strings.Split(base, "/")[4]).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE workforce_hr_master_revisions SET name = 'Forbidden' WHERE id = ?", page.Data[0].RevisionID).Error
	})
	require.ErrorContains(t, err, "append only")
}

func TestEnrollmentPinsHRRevisionAndKeepsHistoricalNames(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, "workforce.master.manage"}}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Reference Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	masterPath := base + "/hr/masters/" + uuid.NewString()
	body := `{"expected_version":0,"kind":"department","code":"SYN-DEPT","name":"Synthetic Department","description":"","active":true}`
	w := testutil.Do(r, "PUT", masterPath, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var saved struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
	staffPath := base + "/staff/" + uuid.NewString() + "/enrollment"
	in := map[string]any{"expected_version": 0, "staff": map[string]any{"employee_code": "SYN-REF", "display_name": "Synthetic Reference Staff", "job_category": "nurse", "employment_status": "active"}, "profile": map[string]any{"department_revision_id": saved.RevisionID, "department": "Forged Name"}}
	w = testutil.Do(r, "PUT", staffPath, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	// An older client that omits revision fields cannot erase a pinned assignment.
	delete(in["profile"].(map[string]any), "department_revision_id")
	in["expected_version"] = 1
	in["profile"].(map[string]any)["preferred_name"] = "Legacy Client Synthetic"
	w = testutil.Do(r, "PUT", staffPath, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"department_revision_id":"`+saved.RevisionID+`"`)
	in["profile"].(map[string]any)["department_revision_id"] = saved.RevisionID
	require.Contains(t, w.Body.String(), `"department":"Synthetic Department"`)
	require.NotContains(t, w.Body.String(), "Forged Name")
	deactivated := strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), `"active":true`, `"active":false`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", masterPath, "admin", deactivated).Code)
	// Existing staff can retain a withdrawn revision when changing other fields.
	in["expected_version"] = 2
	in["profile"].(map[string]any)["preferred_name"] = "Synthetic Preferred"
	w = testutil.Do(r, "PUT", staffPath, "admin", string(mustJSON(t, in)))
	require.Equal(t, 200, w.Code, w.Body.String())
	in["expected_version"] = 0
	in["staff"].(map[string]any)["employee_code"] = "SYN-NEW"
	require.Equal(t, 400, testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString()+"/enrollment", "admin", string(mustJSON(t, in))).Code)
	in["profile"].(map[string]any)["designation_revision_id"] = saved.RevisionID
	require.Equal(t, 404, testutil.Do(r, "PUT", staffPath, "admin", string(mustJSON(t, in))).Code)
}
