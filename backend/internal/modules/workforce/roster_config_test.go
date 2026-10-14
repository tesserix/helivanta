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

func TestRosterAreasAndGroupsPreserveMembershipAndScopedHistory(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermRosterManage}, "reader": {workforce.PermRosterManage}, "directory": {}, "foreign": {workforce.PermRosterManage}}, Modules: []platform.Module{workforce.New()}})
	hospital := uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Planning Hospital"}`).Code)
	for user, level := range map[string]string{"admin": "manager", "reader": "reader", "directory": "manager"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`).Code)
	}
	shiftID := uuid.NewString()
	body := `{"expected_version":0,"shift_type":"day","code":"SYN-DAY","name":"Synthetic Day","description":"","active":true,"start_time":"08:00","end_time":"16:00","break_minutes":30,"grace_minutes":0,"full_day_ot_minutes":0,"half_day_ot_minutes":0,"locum":false}`
	w := testutil.Do(r, "PUT", base+"/hr/shifts/"+shiftID, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var shift struct {
		RevisionID string `json:"revision_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shift))
	path := base + "/hr/roster-configs/" + uuid.NewString()
	cfg := `{"expected_version":0,"kind":"area","code":"SYN-WARD","name":"Synthetic Ward","description":"Synthetic policy only","active":true,"effective_on":"2026-10-14","reviewed":true,"shift_revision_ids":["` + shift.RevisionID + `"],"days":[]}`
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = testutil.Do(r, "PUT", path, "admin", cfg).Code })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	w = testutil.Do(r, "GET", base+"/hr/roster-configs?kind=area", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), `"code":"SYN-DAY"`)
	require.Contains(t, w.Body.String(), `"effective_on":"2026-10-14"`)
	var page struct {
		Data []struct {
			ID         string
			RevisionID string `json:"revision_id"`
			Version    int
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, 1, page.Data[0].Version)
	for _, tc := range []struct {
		user   string
		status int
	}{{"reader", 404}, {"directory", 403}, {"foreign", 404}, {"", 401}} {
		require.Equal(t, tc.status, testutil.Do(r, "PUT", path, tc.user, cfg).Code)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", base+"/hr/roster-configs", "foreign", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", "/v1/workforce/hospitals/"+uuid.NewString()+"/hr/roster-configs", "admin", "").Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", base+"/hr/roster-configs/"+uuid.NewString(), "admin", strings.Replace(cfg, "SYN-WARD", "syn-ward", 1)).Code)
	changed := strings.Replace(strings.Replace(cfg, `"expected_version":0`, `"expected_version":1`, 1), "Synthetic Ward", "Synthetic Ward Revised", 1)
	w = testutil.Do(r, "PUT", path, "admin", changed)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"version":2`)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(changed, "Revised", "Conflict", 1)).Code)
	require.Equal(t, 409, testutil.Do(r, "PUT", path, "admin", strings.Replace(changed, "SYN-WARD", "SYN-NEW", 1)).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", changed).Code)
	w = testutil.Do(r, "GET", path+"/history", "reader", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"name":"Synthetic Ward"`)
	require.Contains(t, w.Body.String(), `"name":"Synthetic Ward Revised"`)
	group := strings.Replace(strings.Replace(cfg, `"kind":"area"`, `"kind":"group"`, 1), "SYN-WARD", "SYN-GROUP", 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/hr/roster-configs/"+uuid.NewString(), "admin", group).Code)
	for _, invalid := range []string{strings.Replace(cfg, `"reviewed":true`, `"reviewed":false`, 1), strings.Replace(cfg, "2026-10-14", "2026-02-30", 1), strings.Replace(cfg, shift.RevisionID, uuid.NewString(), 1), strings.Replace(cfg, `"days":[]`, `"days":[],"tenant_id":"injected"`, 1), strings.Replace(cfg, "Synthetic Ward", `Synthetic\u0000Ward`, 1), strings.Replace(cfg, `"`+shift.RevisionID+`"`, `"`+shift.RevisionID+`","`+shift.RevisionID+`"`, 1)} {
		w = testutil.Do(r, "PUT", base+"/hr/roster-configs/"+uuid.NewString(), "admin", invalid)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	retire := strings.Replace(strings.Replace(body, `"expected_version":0`, `"expected_version":1`, 1), `"active":true`, `"active":false`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/hr/shifts/"+shiftID, "admin", retire).Code)
	require.Equal(t, 400, testutil.Do(r, "PUT", base+"/hr/roster-configs/"+uuid.NewString(), "admin", strings.Replace(cfg, "SYN-WARD", "SYN-OTHER", 1)).Code)
	historical := strings.Replace(strings.Replace(changed, `"expected_version":1`, `"expected_version":2`, 1), "Synthetic policy only", "Historical membership retained", 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "admin", historical).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_roster_config_members").Count(&n).Error)
		require.Zero(t, n, "forced RLS without hospital scope")
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", "helivanta.in.workforce.roster_config_changed.v1").Count(&n).Error)
		require.EqualValues(t, 4, n)
		return nil
	}))
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_roster','authorized',true)`, hospital).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE workforce_roster_config_revisions SET name = 'Forbidden' WHERE id = ?", page.Data[0].RevisionID).Error
	})
	require.ErrorContains(t, err, "append only")
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_roster','authorized',true)`, hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_roster_config_members(tenant_id,hospital_id,revision_id,kind,position,shift_revision_id) VALUES (?,?,?,'area',1,?)`, testutil.TenantA, hospital, page.Data[0].RevisionID, shift.RevisionID).Error
	})
	require.ErrorContains(t, err, "creating transaction")
}

func TestWeeklyTemplatesRejectOverlapsOffDaysAndForeignAreaRevisions(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermRosterManage}}, Modules: []platform.Module{workforce.New()}})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Weekly Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	send := func(path string, v any) (int, map[string]any) {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		w := testutil.Do(r, "PUT", path, "admin", string(data))
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return w.Code, out
	}
	shifts := make([]string, 4)
	for i, span := range [][2]string{{"08:00", "16:00"}, {"22:00", "06:00"}, {"14:00", "20:00"}, {"05:00", "09:00"}} {
		id := uuid.NewString()
		code, out := send(base+"/hr/shifts/"+id, map[string]any{"expected_version": 0, "shift_type": "day", "code": "SYN-" + string(rune('A'+i)), "name": "Synthetic Shift " + string(rune('A'+i)), "description": "", "active": true, "start_time": span[0], "end_time": span[1], "break_minutes": 0, "grace_minutes": 0, "full_day_ot_minutes": 0, "half_day_ot_minutes": 0, "locum": false})
		require.Equal(t, 200, code, out)
		shifts[i] = out["revision_id"].(string)
	}
	areaBody := map[string]any{"expected_version": 0, "kind": "area", "code": "SYN-AREA", "name": "Synthetic Weekly Area", "description": "", "active": true, "effective_on": "2026-10-14", "reviewed": true, "shift_revision_ids": shifts, "days": []any{}}
	code, area := send(base+"/hr/roster-configs/"+uuid.NewString(), areaBody)
	require.Equal(t, 200, code, area)
	areaID := area["revision_id"].(string)
	makeTemplate := func() map[string]any {
		days := make([]map[string]any, 7)
		for i := range days {
			allocations := []map[string]string{}
			if i < 5 {
				allocations = append(allocations, map[string]string{"shift_revision_id": shifts[0], "area_revision_id": areaID})
			}
			if i == 0 {
				allocations = append(allocations, map[string]string{"shift_revision_id": shifts[1], "area_revision_id": areaID})
			}
			days[i] = map[string]any{"weekday": i, "weekly_off": i >= 5, "allocations": allocations}
		}
		return map[string]any{"expected_version": 0, "kind": "weekly_template", "code": "SYN-WEEK", "name": "Synthetic Week", "description": "Synthetic policy only", "active": true, "effective_on": "2026-10-14", "reviewed": true, "shift_revision_ids": []string{}, "days": days}
	}
	path := base + "/hr/roster-configs/" + uuid.NewString()
	template := makeTemplate()
	code, out := send(path, template)
	require.Equal(t, 200, code, out)
	days := out["days"].([]any)
	require.Len(t, days, 7)
	monday := days[0].(map[string]any)
	require.Len(t, monday["allocations"], 2)
	require.Equal(t, true, days[6].(map[string]any)["weekly_off"])
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["effective_on"] = "2026-10-13" },
		func(v map[string]any) {
			d := v["days"].([]map[string]any)
			d[0]["allocations"].([]map[string]string)[1]["shift_revision_id"] = shifts[2]
		},
		func(v map[string]any) {
			d := v["days"].([]map[string]any)
			d[4]["allocations"] = append(d[4]["allocations"].([]map[string]string), map[string]string{"shift_revision_id": shifts[1], "area_revision_id": areaID})
		},
		func(v map[string]any) {
			d := v["days"].([]map[string]any)
			d[6]["weekly_off"] = false
			d[6]["allocations"] = []map[string]string{{"shift_revision_id": shifts[1], "area_revision_id": areaID}}
			d[0]["allocations"].([]map[string]string)[0]["shift_revision_id"] = shifts[3]
		},
		func(v map[string]any) { d := v["days"].([]map[string]any); d[0]["weekday"] = 1 },
		func(v map[string]any) { d := v["days"].([]map[string]any); d[0]["weekly_off"] = true },
		func(v map[string]any) { d := v["days"].([]map[string]any); d[1]["allocations"] = []map[string]string{} },
		func(v map[string]any) {
			d := v["days"].([]map[string]any)
			d[0]["allocations"].([]map[string]string)[0]["area_revision_id"] = uuid.NewString()
		},
		func(v map[string]any) { v["shift_revision_ids"] = []string{shifts[0]} },
	} {
		invalid := makeTemplate()
		invalid["code"] = "SYN-INVALID"
		invalid["name"] = "Synthetic Invalid"
		mutate(invalid)
		code, out = send(base+"/hr/roster-configs/"+uuid.NewString(), invalid)
		require.Equal(t, 400, code, out)
	}
	groupBody := map[string]any{}
	for k, v := range areaBody {
		groupBody[k] = v
	}
	groupBody["kind"] = "group"
	groupBody["code"] = "SYN-GROUP"
	groupBody["name"] = "Synthetic Group"
	code, group := send(base+"/hr/roster-configs/"+uuid.NewString(), groupBody)
	require.Equal(t, 200, code, group)
	invalid := makeTemplate()
	invalid["days"].([]map[string]any)[0]["allocations"].([]map[string]string)[0]["area_revision_id"] = group["revision_id"].(string)
	code, out = send(base+"/hr/roster-configs/"+uuid.NewString(), invalid)
	require.Equal(t, 400, code, out)
	template["expected_version"] = 1
	template["name"] = "Synthetic Week Revised"
	code, out = send(path, template)
	require.Equal(t, 200, code, out)
	history := testutil.Do(r, "GET", path+"/history", "admin", "")
	require.Equal(t, 200, history.Code)
	require.Contains(t, history.Body.String(), `"name":"Synthetic Week"`)
	require.Contains(t, history.Body.String(), `"name":"Synthetic Week Revised"`)
	require.Contains(t, history.Body.String(), `"area_name":"Synthetic Weekly Area"`)
	rawID, rawRevision := uuid.New(), uuid.New()
	hospital := strings.Split(base, "/")[4]
	scoped := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_roster','authorized',true)`, hospital).Error
	}
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := scoped(tx); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_roster_configs(tenant_id,hospital_id,id,kind,code,name,description,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,'weekly_template','SYN-DB-OVERLAP','Synthetic DB Overlap','',true,'2026-10-14','user-admin',1,?)`, testutil.TenantA, hospital, rawID, rawRevision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_roster_config_revisions(tenant_id,hospital_id,id,config_id,kind,code,name,description,active,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,'weekly_template','SYN-DB-OVERLAP','Synthetic DB Overlap','',true,'2026-10-14','user-admin',1)`, testutil.TenantA, hospital, rawRevision, rawID).Error; err != nil {
			return err
		}
		for weekday := range 7 {
			if err := tx.Exec(`INSERT INTO workforce_roster_config_days(tenant_id,hospital_id,revision_id,weekday,weekly_off) VALUES(?,?,?,?,?)`, testutil.TenantA, hospital, rawRevision, weekday, weekday >= 5).Error; err != nil {
				return err
			}
			if weekday < 5 {
				if err := tx.Exec(`INSERT INTO workforce_roster_config_members(tenant_id,hospital_id,revision_id,kind,position,weekday,shift_revision_id,area_revision_id) VALUES(?,?,?,'weekly_template',?,?,?,?)`, testutil.TenantA, hospital, rawRevision, weekday*2, weekday, shifts[0], areaID).Error; err != nil {
					return err
				}
			}
		}
		return tx.Exec(`INSERT INTO workforce_roster_config_members(tenant_id,hospital_id,revision_id,kind,position,weekday,shift_revision_id,area_revision_id) VALUES(?,?,?,'weekly_template',1,0,?,?)`, testutil.TenantA, hospital, rawRevision, shifts[2], areaID).Error
	})
	require.ErrorContains(t, err, "overlapping")
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := scoped(tx); err != nil {
			return err
		}
		var n int64
		if err := tx.Table("workforce_roster_configs").Where("id=?", rawID).Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n, "deferred failure rolled back the complete aggregate")
		return nil
	}))

}
