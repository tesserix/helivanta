package workforce_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"
)

func TestShiftDefinitionsHistoryIsolationAndOvernightValidation(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, "workforce.roster.manage"}, "reader": {"workforce.roster.manage"}, "directory": {}, "foreign": {"workforce.roster.manage"}}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Shift Hospital"}`).Code)
	for user, level := range map[string]string{"admin": "manager", "reader": "reader", "directory": "manager"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`).Code)
	}
	path := base + "/hr/shifts/" + uuid.NewString()
	body := `{"expected_version":0,"shift_type":"night","code":"SYN-NIGHT","name":"Synthetic Night","description":"Synthetic policy only","active":true,"start_time":"22:00","end_time":"06:00","break_minutes":30,"grace_minutes":10,"full_day_ot_minutes":120,"half_day_ot_minutes":60,"locum":false}`
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Go(func() { codes[i] = testutil.Do(r, "PUT", path, "admin", body).Code })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	w := testutil.Do(r, "GET", base+"/hr/shifts", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var page struct {
		Data []struct {
			RevisionID      string `json:"revision_id"`
			Version         int
			DurationMinutes int `json:"duration_minutes"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, 1, page.Data[0].Version)
	require.Equal(t, 480, page.Data[0].DurationMinutes)
	for _, tc := range []struct {
		user   string
		status int
	}{{"reader", 404}, {"directory", 403}, {"foreign", 404}, {"", 401}} {
		require.Equal(t, tc.status, testutil.Do(r, "PUT", path, tc.user, body).Code)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", base+"/hr/shifts", "foreign", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/workforce/hospitals/"+uuid.NewString()+"/hr/shifts", "admin", "").Code)
	for _, b := range []string{strings.Replace(body, "SYN-NIGHT", "syn-night", 1), strings.Replace(body, "SYN-NIGHT", "SYN-OTHER", 1)} {
		require.Equal(t, 409, testutil.Do(r, "PUT", base+"/hr/shifts/"+uuid.NewString(), "admin", b).Code)
	}
	changed := strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), `"06:00"`, `"07:00"`, 1)
	w = testutil.Do(r, "PUT", path, "admin", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.Contains(t, w.Body.String(), `"duration_minutes":540`)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(changed, `"07:00"`, `"08:00"`, 1)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(changed, "SYN-NIGHT", "SYN-EDITED", 1)).Code)
	w = testutil.Do(r, "GET", path+"/history", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"end_time":"06:00"`)
	require.Contains(t, w.Body.String(), `"end_time":"07:00"`)
	for _, b := range []string{strings.Replace(body, `"22:00"`, `"25:00"`, 1), strings.Replace(body, `"06:00"`, `"22:00"`, 1), strings.Replace(body, `"break_minutes":30`, `"break_minutes":480`, 1), strings.Replace(body, `"grace_minutes":10`, `"grace_minutes":-1`, 1), strings.Replace(body, `"full_day_ot_minutes":120`, `"full_day_ot_minutes":40`, 1), strings.Replace(body, `"night"`, `"unknown"`, 1), strings.Replace(body, `"Synthetic Night"`, `"Synthetic\u0000Night"`, 1), strings.Replace(body, `"locum":false`, `"locum":false,"tenant_id":"injected"`, 1)} {
		w = testutil.Do(r, "PUT", base+"/hr/shifts/"+uuid.NewString(), "admin", b)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	retired := strings.Replace(strings.Replace(changed, `"expected_version":1`, `"expected_version":2`, 1), `"active":true`, `"active":false`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", retired).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_shift_revisions").Count(&n).Error)
		require.Zero(t, n, "RLS denies missing hospital scope")
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", "helivanta.in.workforce.shift_changed.v1").Count(&n).Error)
		require.EqualValues(t, 3, n)
		return nil
	}))
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_shift','authorized',true)`, strings.Split(base, "/")[4]).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE workforce_shift_revisions SET name = 'Forbidden' WHERE id = ?", page.Data[0].RevisionID).Error
	})
	require.ErrorContains(t, err, "append only")
}
