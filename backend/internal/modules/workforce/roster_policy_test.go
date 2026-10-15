package workforce_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type rosterPolicyFixture struct {
	r        *gin.Engine
	db       *tenantdb.DB
	hospital string
	base     string
}

func newRosterPolicyFixture(t *testing.T) rosterPolicyFixture {
	t.Helper()
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermRosterManage}, "reader": {workforce.PermRosterManage}, "directory": {}, "foreign": {workforce.PermRosterManage}}, Modules: []platform.Module{workforce.New()}})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Policy Hospital"}`).Code)
	for user, level := range map[string]string{"admin": "manager", "reader": "reader", "directory": "manager"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`).Code)
	}
	return rosterPolicyFixture{r: r, db: db, hospital: hospital, base: base}
}

func (f rosterPolicyFixture) put(t *testing.T, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.Do(f.r, "PUT", f.base+path, user, body)
}

func revisionOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	var out struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.RevisionID
}

func (f rosterPolicyFixture) outbox(t *testing.T, subject string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject = ?", subject).Count(&n).Error
	}))
	return n
}

func (f rosterPolicyFixture) scopedExec(t *testing.T, sql string, args ...any) error {
	t.Helper()
	return f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_roster_policy','authorized',true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(sql, args...).Error
	})
}

func TestRosterSettingsAreReviewedMonotonicAndScoped(t *testing.T) {
	f := newRosterPolicyFixture(t)
	require.Equal(t, 404, testutil.Do(f.r, "GET", f.base+"/hr/roster-settings", "reader", "").Code, "unconfigured settings are absent, not defaulted")
	body := `{"expected_version":0,"timezone":"Asia/Kolkata","week_start":0,"auto_population":"fixed_shift","effective_on":"2026-10-19","reviewed":true}`
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = f.put(t, "/hr/roster-settings", "admin", body).Code })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	w := testutil.Do(f.r, "GET", f.base+"/hr/roster-settings", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"timezone":"Asia/Kolkata"`)
	require.Contains(t, w.Body.String(), `"version":1`)
	require.Contains(t, w.Body.String(), `"reviewed_by_subject":"user-admin"`)
	for _, tc := range []struct {
		user   string
		status int
	}{{"reader", 404}, {"directory", 403}, {"foreign", 404}, {"", 401}} {
		require.Equal(t, tc.status, f.put(t, "/hr/roster-settings", tc.user, body).Code, tc.user)
	}
	require.Equal(t, 404, testutil.Do(f.r, "GET", f.base+"/hr/roster-settings", "foreign", "").Code)
	for _, b := range []string{
		strings.Replace(body, "Asia/Kolkata", "Mars/Olympus", 1),
		strings.Replace(body, "Asia/Kolkata", "Local", 1),
		strings.Replace(body, "Asia/Kolkata", "asia/kolkata", 1),
		strings.Replace(body, `"week_start":0`, `"week_start":7`, 1),
		strings.Replace(body, "fixed_shift", "everyone", 1),
		strings.Replace(body, `"reviewed":true`, `"reviewed":false`, 1),
		strings.Replace(body, "2026-10-19", "2026-02-30", 1),
		strings.Replace(body, `"reviewed":true`, `"reviewed":true,"tenant_id":"injected"`, 1),
	} {
		w = f.put(t, "/hr/roster-settings", "admin", strings.Replace(b, `"expected_version":0`, `"expected_version":1`, 1))
		require.Equal(t, 400, w.Code, b+" "+w.Body.String())
	}
	changed := strings.Replace(strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), "2026-10-19", "2026-11-02", 1), `"week_start":0`, `"week_start":6`, 1)
	require.Equal(t, 409, f.put(t, "/hr/roster-settings", "admin", strings.Replace(changed, `"expected_version":1`, `"expected_version":0`, 1)).Code)
	w = f.put(t, "/hr/roster-settings", "admin", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.Equal(t, 200, f.put(t, "/hr/roster-settings", "admin", changed).Code, "unchanged retry after success")
	backdated := strings.Replace(strings.Replace(changed, `"expected_version":1`, `"expected_version":2`, 1), "2026-11-02", "2026-10-26", 1)
	w = f.put(t, "/hr/roster-settings", "admin", backdated)
	require.Equal(t, 400, w.Code, "a revision cannot predate the current one")
	require.Contains(t, w.Body.String(), "effective")
	w = testutil.Do(f.r, "GET", f.base+"/hr/roster-settings/history", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"effective_on":"2026-10-19"`)
	require.Contains(t, w.Body.String(), `"effective_on":"2026-11-02"`)
	require.EqualValues(t, 2, f.outbox(t, workforce.RosterSettingsChanged))
	require.ErrorContains(t, f.scopedExec(t, `UPDATE workforce_roster_settings SET timezone='Not/AZone', version=version+1 WHERE hospital_id=?`, f.hospital), "timezone")
	require.ErrorContains(t, f.scopedExec(t, `UPDATE workforce_roster_settings SET effective_on='2026-01-01', version=version+1 WHERE hospital_id=?`, f.hospital), "effective")
	require.ErrorContains(t, f.scopedExec(t, `UPDATE workforce_roster_setting_revisions SET week_start=3`), "append only")
	require.ErrorContains(t, f.scopedExec(t, `DELETE FROM workforce_roster_settings`), "not supported")
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_roster_setting_revisions").Count(&n).Error)
		require.Zero(t, n, "RLS denies rows without hospital scope")
		return nil
	}))
}

func TestStaffRosterAssignmentsPinTemplatesAndRejectStaleReferences(t *testing.T) {
	f := newRosterPolicyFixture(t)
	shiftRevision := revisionOf(t, f.put(t, "/hr/shifts/"+uuid.NewString(), "admin", `{"expected_version":0,"shift_type":"day","code":"SYN-DAY","name":"Synthetic Day","description":"","active":true,"start_time":"08:00","end_time":"16:00","break_minutes":30,"grace_minutes":0,"full_day_ot_minutes":0,"half_day_ot_minutes":0,"locum":false}`))
	areaRevision := revisionOf(t, f.put(t, "/hr/roster-configs/"+uuid.NewString(), "admin", `{"expected_version":0,"kind":"area","code":"SYN-WARD","name":"Synthetic Ward","description":"","active":true,"effective_on":"2026-10-01","reviewed":true,"shift_revision_ids":["`+shiftRevision+`"],"days":[]}`))
	days := make([]string, 7)
	for d := range days {
		if d >= 5 {
			days[d] = `{"weekday":` + string(rune('0'+d)) + `,"weekly_off":true,"allocations":[]}`
			continue
		}
		days[d] = `{"weekday":` + string(rune('0'+d)) + `,"weekly_off":false,"allocations":[{"shift_revision_id":"` + shiftRevision + `","area_revision_id":"` + areaRevision + `"}]}`
	}
	templateID := uuid.NewString()
	template := `{"expected_version":0,"kind":"weekly_template","code":"SYN-WEEKDAY","name":"Synthetic Weekdays","description":"","active":true,"effective_on":"2026-10-05","reviewed":true,"shift_revision_ids":[],"days":[` + strings.Join(days, ",") + `]}`
	templateV1 := revisionOf(t, f.put(t, "/hr/roster-configs/"+templateID, "admin", template))
	staffID := uuid.NewString()
	require.Equal(t, 200, f.put(t, "/staff/"+staffID, "admin", `{"employee_code":"SYN-N1","display_name":"Synthetic Nurse","job_category":"nurse","employment_status":"active"}`).Code)

	path := "/staff/" + staffID + "/roster-assignment"
	body := `{"expected_version":0,"weekly_template_revision_id":"` + templateV1 + `","fixed":true,"active":true,"effective_on":"2026-10-12","reviewed":true}`
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = f.put(t, path, "admin", body).Code })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	w := testutil.Do(f.r, "GET", f.base+"/hr/roster-assignments", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	for _, want := range []string{`"employee_code":"SYN-N1"`, `"template_code":"SYN-WEEKDAY"`, `"weekly_template_revision_id":"` + templateV1 + `"`, `"fixed":true`, `"version":1`} {
		require.Contains(t, w.Body.String(), want)
	}
	for _, tc := range []struct {
		user   string
		status int
	}{{"reader", 404}, {"directory", 403}, {"foreign", 404}, {"", 401}} {
		require.Equal(t, tc.status, f.put(t, path, tc.user, body).Code, tc.user)
	}
	require.Equal(t, 404, f.put(t, "/staff/"+uuid.NewString()+"/roster-assignment", "admin", body).Code, "unknown staff")
	require.Equal(t, 404, testutil.Do(f.r, "GET", f.base+"/hr/roster-assignments", "foreign", "").Code)

	other := uuid.NewString()
	require.Equal(t, 200, f.put(t, "/staff/"+other, "admin", `{"employee_code":"SYN-N2","display_name":"Synthetic Locum","job_category":"nurse","employment_status":"active"}`).Code)
	otherPath := "/staff/" + other + "/roster-assignment"
	for name, b := range map[string]string{
		"area revision is not a template": strings.Replace(body, templateV1, areaRevision, 1),
		"unknown revision":                strings.Replace(body, templateV1, uuid.NewString(), 1),
		"predates template":               strings.Replace(body, "2026-10-12", "2026-10-04", 1),
	} {
		w = f.put(t, otherPath, "admin", b)
		require.Equal(t, 400, w.Code, name+" "+w.Body.String())
		require.Contains(t, w.Body.String(), "invalid_reference", name)
	}
	for _, b := range []string{strings.Replace(body, `"reviewed":true`, `"reviewed":false`, 1), strings.Replace(body, templateV1, uuid.Nil.String(), 1), strings.Replace(body, `"fixed":true`, `"fixed":true,"staff_id":"injected"`, 1)} {
		require.Equal(t, 400, f.put(t, otherPath, "admin", b).Code, b)
	}

	changedTemplate := strings.Replace(strings.Replace(template, `"expected_version":0`, `"expected_version":1`, 1), "Synthetic Weekdays", "Synthetic Weekdays Revised", 1)
	templateV2 := revisionOf(t, f.put(t, "/hr/roster-configs/"+templateID, "admin", changedTemplate))
	require.NotEqual(t, templateV1, templateV2)
	require.Equal(t, 200, f.put(t, path, "admin", body).Code, "unchanged pin survives a template revision")
	w = f.put(t, otherPath, "admin", body)
	require.Equal(t, 400, w.Code, "a superseded revision cannot be newly pinned")
	require.Contains(t, w.Body.String(), "invalid_reference")

	moved := strings.Replace(strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), templateV1, templateV2, 1), "2026-10-12", "2026-10-19", 1)
	require.Equal(t, 409, f.put(t, path, "admin", strings.Replace(moved, `"expected_version":1`, `"expected_version":0`, 1)).Code)
	w = f.put(t, path, "admin", moved)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	backdated := strings.Replace(strings.Replace(moved, `"expected_version":1`, `"expected_version":2`, 1), "2026-10-19", "2026-10-13", 1)
	require.Equal(t, 400, f.put(t, path, "admin", backdated).Code)

	require.Equal(t, 200, f.put(t, "/staff/"+staffID, "admin", `{"employee_code":"SYN-N1","display_name":"Synthetic Nurse","job_category":"nurse","employment_status":"inactive"}`).Code)
	reactivatedTemplate := strings.Replace(strings.Replace(moved, `"expected_version":1`, `"expected_version":2`, 1), "2026-10-19", "2026-10-26", 1)
	w = f.put(t, path, "admin", strings.Replace(reactivatedTemplate, `"fixed":true`, `"fixed":false`, 1))
	require.Equal(t, 400, w.Code, "inactive staff cannot receive an active assignment")
	ended := strings.Replace(reactivatedTemplate, `"active":true`, `"active":false`, 1)
	w = f.put(t, path, "admin", ended)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"active":false`)

	w = testutil.Do(f.r, "GET", f.base+path+"/history", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, want := range []string{templateV1, templateV2, `"effective_on":"2026-10-12"`, `"effective_on":"2026-10-26"`} {
		require.Contains(t, w.Body.String(), want)
	}
	require.EqualValues(t, 3, f.outbox(t, workforce.StaffRosterAssignmentChanged))
	require.ErrorContains(t, f.scopedExec(t, `UPDATE workforce_staff_roster_assignment_revisions SET fixed=false`), "append only")
	require.Error(t, f.scopedExec(t, `UPDATE workforce_staff_roster_assignments SET weekly_template_revision_id=?, version=version+1, revision_id=revision_id WHERE staff_id=?`, areaRevision, staffID), "FK pins only weekly templates")
}
