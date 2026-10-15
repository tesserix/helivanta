package workforce_test

import (
	"encoding/json"
	"fmt"
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

type rosterFixture struct {
	r                                *gin.Engine
	db                               *tenantdb.DB
	hospital, base                   string
	dayShift, nightShift, earlyShift string
	area                             string
	staff                            map[string]string
}

func newRosterFixture(t *testing.T, timezone string, weekStart int, effective string) rosterFixture {
	t.Helper()
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "planner": testutil.TenantA, "approver": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin":   {workforce.PermHospitalManage, workforce.PermRosterManage, workforce.PermRosterCorrect, workforce.PermLeaveConfigure, workforce.PermLeaveAllocate, workforce.PermLeaveRequest},
			"planner": {workforce.PermRosterManage}, "approver": {workforce.PermLeaveApprove}, "reader": {workforce.PermRosterManage}, "directory": {}, "foreign": {workforce.PermRosterManage, workforce.PermRosterCorrect},
		},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	f := rosterFixture{r: r, db: db, hospital: hospital, base: "/v1/workforce/hospitals/" + hospital, staff: map[string]string{}}
	require.Equal(t, 200, testutil.Do(r, "PUT", f.base, "admin", `{"name":"Synthetic Roster Hospital"}`).Code)
	for user, level := range map[string]string{"admin": "manager", "planner": "manager", "approver": "manager", "reader": "reader", "directory": "manager"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", f.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`).Code)
	}
	shift := func(code, start, end string) string {
		return revisionOf(t, f.do(t, "PUT", "/hr/shifts/"+uuid.NewString(), "admin", `{"expected_version":0,"shift_type":"rotating","code":"`+code+`","name":"Synthetic `+code+`","description":"","active":true,"start_time":"`+start+`","end_time":"`+end+`","break_minutes":30,"grace_minutes":0,"full_day_ot_minutes":0,"half_day_ot_minutes":0,"locum":false}`))
	}
	f.dayShift, f.nightShift, f.earlyShift = shift("SYN-DAY", "08:00", "16:00"), shift("SYN-NIGHT", "22:00", "06:00"), shift("SYN-EARLY", "02:00", "10:00")
	f.area = revisionOf(t, f.do(t, "PUT", "/hr/roster-configs/"+uuid.NewString(), "admin", `{"expected_version":0,"kind":"area","code":"SYN-WARD","name":"Synthetic Ward","description":"","active":true,"effective_on":"2026-01-01","reviewed":true,"shift_revision_ids":["`+f.dayShift+`","`+f.nightShift+`","`+f.earlyShift+`"],"days":[]}`))
	days := make([]string, 7)
	for d := range days {
		if d >= 5 {
			days[d] = fmt.Sprintf(`{"weekday":%d,"weekly_off":true,"allocations":[]}`, d)
			continue
		}
		days[d] = fmt.Sprintf(`{"weekday":%d,"weekly_off":false,"allocations":[{"shift_revision_id":"%s","area_revision_id":"%s"}]}`, d, f.dayShift, f.area)
	}
	template := revisionOf(t, f.do(t, "PUT", "/hr/roster-configs/"+uuid.NewString(), "admin", `{"expected_version":0,"kind":"weekly_template","code":"SYN-WEEKDAY","name":"Synthetic Weekdays","description":"","active":true,"effective_on":"2026-01-01","reviewed":true,"shift_revision_ids":[],"days":[`+strings.Join(days, ",")+`]}`))
	require.Equal(t, 200, f.do(t, "PUT", "/hr/roster-settings", "admin", fmt.Sprintf(`{"expected_version":0,"timezone":"%s","week_start":%d,"auto_population":"all_assigned","effective_on":"%s","reviewed":true}`, timezone, weekStart, effective)).Code)
	for _, person := range []struct {
		code  string
		fixed bool
	}{{"SYN-N1", true}, {"SYN-N2", false}, {"SYN-N3", true}} {
		id := uuid.NewString()
		f.staff[person.code] = id
		require.Equal(t, 200, f.do(t, "PUT", "/staff/"+id, "admin", `{"employee_code":"`+person.code+`","display_name":"Synthetic `+person.code+`","job_category":"nurse","employment_status":"active"}`).Code)
		require.Equal(t, 200, f.do(t, "PUT", "/staff/"+id+"/roster-assignment", "admin", fmt.Sprintf(`{"expected_version":0,"weekly_template_revision_id":"%s","fixed":%t,"active":true,"effective_on":"2026-01-01","reviewed":true}`, template, person.fixed)).Code)
	}
	require.Equal(t, 200, f.do(t, "PUT", "/staff/"+f.staff["SYN-N3"], "admin", `{"employee_code":"SYN-N3","display_name":"Synthetic SYN-N3","job_category":"nurse","employment_status":"inactive"}`).Code)
	return f
}

func (f rosterFixture) do(t *testing.T, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.Do(f.r, method, f.base+path, user, body)
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

type rosterResult struct {
	Roster struct {
		Status      string `json:"status"`
		Version     int64  `json:"version"`
		Allocations int    `json:"allocations"`
	} `json:"roster"`
	Revision struct {
		ID                string `json:"id"`
		Added             int    `json:"added"`
		Removed           int    `json:"removed"`
		SkippedExisting   int    `json:"skipped_existing"`
		SkippedUnassigned int    `json:"skipped_unassigned"`
		SkippedConflicts  int    `json:"skipped_conflicts"`
		SkippedLeave      int    `json:"skipped_leave"`
	} `json:"revision"`
}

func (f rosterFixture) write(t *testing.T, roster, action, user string, version int64, extra string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, "POST", "/hr/rosters/"+roster+"/"+action, user, fmt.Sprintf(`{"change_id":"%s","expected_version":%d,"reviewed":true%s}`, uuid.NewString(), version, extra))
}

func (f rosterFixture) allocation(staff, date, shift string) (string, string) {
	id := uuid.NewString()
	return id, fmt.Sprintf(`{"id":"%s","staff_id":"%s","roster_date":"%s","shift_revision_id":"%s","area_revision_id":"%s"}`, id, staff, date, shift, f.area)
}

func (f rosterFixture) change(t *testing.T, roster, user string, version int64, mode string, add []string, remove []string) *httptest.ResponseRecorder {
	t.Helper()
	removed := "[]"
	if len(remove) > 0 {
		removed = `["` + strings.Join(remove, `","`) + `"]`
	}
	return f.write(t, roster, "changes", user, version, fmt.Sprintf(`,"mode":"%s","add":[%s],"remove":%s`, mode, strings.Join(add, ","), removed))
}

func (f rosterFixture) allocations(t *testing.T, roster, query string) []map[string]any {
	t.Helper()
	var all []map[string]any
	cursor := ""
	for range 50 {
		w := f.do(t, "GET", "/hr/rosters/"+roster+"/allocations?limit=200"+query+cursor, "reader", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		page := decodeBody[struct {
			Data []map[string]any `json:"data"`
			Page struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			} `json:"page"`
		}](t, w)
		all = append(all, page.Data...)
		if !page.Page.HasMore {
			return all
		}
		cursor = "&cursor=" + page.Page.NextCursor
	}
	t.Fatal("allocation pages did not terminate")
	return nil
}

func (f rosterFixture) count(t *testing.T, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Table(table).Where(where, args...).Count(&n).Error
	}))
	return n
}

func (f rosterFixture) scopedExec(t *testing.T, flagged bool, sql string, args ...any) error {
	t.Helper()
	return f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		flag := ""
		if flagged {
			flag = "authorized"
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_rostering',?,true)`, f.hospital, flag).Error; err != nil {
			return err
		}
		return tx.Exec(sql, args...).Error
	})
}

func TestRosterDraftsArePinnedAndScoped(t *testing.T) {
	f := newRosterFixture(t, "Asia/Kolkata", 0, "2026-10-05")
	roster := uuid.NewString()
	body := `{"name":"Synthetic October","start_date":"2026-10-12","end_date":"2026-10-25","reviewed":true}`

	w := f.do(t, "PUT", "/hr/rosters/"+uuid.NewString(), "admin", strings.NewReplacer("2026-10-12", "2026-09-28", "2026-10-25", "2026-10-11").Replace(body))
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "roster_settings_missing", "a roster before reviewed settings cannot default a policy")

	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i] = f.do(t, "PUT", "/hr/rosters/"+roster, "planner", body).Code })
	}
	wg.Wait()
	require.Equal(t, []int{200, 200, 200, 200}, codes, "idempotent concurrent create")
	w = f.do(t, "GET", "/hr/rosters/"+roster, "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	for _, want := range []string{`"status":"draft"`, `"version":1`, `"timezone":"Asia/Kolkata"`, `"auto_population":"all_assigned"`, `"reviewed_by_subject":"user-planner"`, `"allocations":0`} {
		require.Contains(t, w.Body.String(), want)
	}
	require.EqualValues(t, 1, f.count(t, "workforce_roster_revisions", "roster_id=?", roster))
	require.EqualValues(t, 1, f.count(t, "outbox_events", "subject=?", workforce.RosterChanged))

	for name, tc := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"renamed existing id": {strings.Replace(body, "Synthetic October", "Renamed", 1), 409, "roster_exists"},
		"not on week start":   {strings.NewReplacer("2026-10-12", "2026-11-03", "2026-10-25", "2026-11-16").Replace(body), 400, "week start"},
		"partial week":        {strings.NewReplacer("2026-10-12", "2026-11-02", "2026-10-25", "2026-11-11").Replace(body), 400, "invalid_request"},
		"over six weeks":      {strings.NewReplacer("2026-10-12", "2026-11-02", "2026-10-25", "2026-12-20").Replace(body), 400, "invalid_request"},
		"unreviewed":          {strings.Replace(strings.Replace(body, `"reviewed":true`, `"reviewed":false`, 1), "2026-10-12", "2026-11-02", 1), 400, "invalid_request"},
		"overlapping period":  {strings.NewReplacer("2026-10-12", "2026-10-19", "2026-10-25", "2026-11-01").Replace(body), 409, "roster_overlap"},
		"injected field":      {strings.Replace(body, `"reviewed":true`, `"reviewed":true,"status":"frozen"`, 1), 400, "invalid_request"},
	} {
		id := roster
		if name != "renamed existing id" {
			id = uuid.NewString()
		}
		w = f.do(t, "PUT", "/hr/rosters/"+id, "planner", tc.body)
		require.Equal(t, tc.status, w.Code, name+" "+w.Body.String())
		require.Contains(t, w.Body.String(), tc.code, name)
	}
	for user, status := range map[string]int{"reader": 404, "directory": 403, "foreign": 404, "": 401} {
		require.Equal(t, status, f.do(t, "PUT", "/hr/rosters/"+uuid.NewString(), user, strings.NewReplacer("2026-10-12", "2026-11-02", "2026-10-25", "2026-11-08").Replace(body)).Code, user)
	}
	require.Equal(t, 404, f.do(t, "GET", "/hr/rosters/"+roster, "foreign", "").Code)
	require.Equal(t, 404, f.do(t, "GET", "/hr/rosters/"+uuid.NewString(), "reader", "").Code)
	w = f.do(t, "GET", "/hr/rosters", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), roster)
}

func TestRosterPopulationChangesAndHistory(t *testing.T) {
	f := newRosterFixture(t, "Asia/Kolkata", 0, "2026-10-05")
	roster := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", `{"name":"Synthetic October","start_date":"2026-10-12","end_date":"2026-10-25","reviewed":true}`).Code)
	n1, n2, n3 := f.staff["SYN-N1"], f.staff["SYN-N2"], f.staff["SYN-N3"]

	change := uuid.NewString()
	populate := `{"change_id":"` + change + `","expected_version":1,"reviewed":true}`
	w := f.do(t, "POST", "/hr/rosters/"+roster+"/populate", "planner", populate)
	require.Equal(t, 200, w.Code, w.Body.String())
	res := decodeBody[rosterResult](t, w)
	require.Equal(t, 20, res.Revision.Added, "two active assigned staff x ten weekdays")
	require.Equal(t, 10, res.Revision.SkippedUnassigned, "inactive staff are skipped on working days")
	require.EqualValues(t, 2, res.Roster.Version)
	replay := f.do(t, "POST", "/hr/rosters/"+roster+"/populate", "planner", populate)
	require.Equal(t, 200, replay.Code)
	require.Equal(t, change, decodeBody[rosterResult](t, replay).Revision.ID, "a retried change returns the original revision")
	require.EqualValues(t, 20, f.count(t, "workforce_roster_allocations", "roster_id=?", roster), "replay wrote nothing")
	w = f.do(t, "POST", "/hr/rosters/"+roster+"/freeze", "planner", `{"change_id":"`+change+`","expected_version":2,"reviewed":true}`)
	require.Equal(t, 409, w.Code, "change ID reused for another action")
	require.Contains(t, w.Body.String(), "change_reused")
	require.Equal(t, 409, f.write(t, roster, "populate", "planner", 1, "").Code, "stale version")

	w = f.write(t, roster, "populate", "planner", 2, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	res = decodeBody[rosterResult](t, w)
	require.Zero(t, res.Revision.Added)
	require.Equal(t, 20, res.Revision.SkippedExisting, "existing staff-dates are never overwritten")

	night, nightBody := f.allocation(n1, "2026-10-17", f.nightShift)
	w = f.change(t, roster, "planner", 3, "employee", []string{nightBody}, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"mode":"employee"`)
	allocs := f.allocations(t, roster, "")
	require.Len(t, allocs, 21)
	for _, a := range allocs {
		if a["id"] == night {
			require.Equal(t, "2026-10-17T16:30:00Z", a["starts_at"], "22:00 IST")
			require.Equal(t, "2026-10-18T00:30:00Z", a["ends_at"], "overnight ends the next morning")
			require.Equal(t, "SYN-NIGHT", a["shift_code"])
		}
	}

	extra, extraBody := f.allocation(n1, "2026-10-12", f.nightShift)
	clash, clashBody := f.allocation(n1, "2026-10-12", f.earlyShift)
	w = f.change(t, roster, "planner", 4, "additional", []string{extraBody, clashBody}, nil)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "allocation_overlap")
	require.Contains(t, w.Body.String(), clash, "the overlapping allocation is identified")
	require.NotContains(t, w.Body.String(), extra)
	w = f.change(t, roster, "planner", 4, "additional", []string{extraBody}, nil)
	require.Equal(t, 200, w.Code, w.Body.String())

	_, dupBody := f.allocation(n2, "2026-10-12", f.nightShift)
	w = f.change(t, roster, "planner", 5, "roster", []string{dupBody}, nil)
	require.Equal(t, 409, w.Code, "a second primary on one date")
	require.Contains(t, w.Body.String(), "allocation_overlap")
	_, earlyBody := f.allocation(n1, "2026-10-18", f.earlyShift)
	w = f.change(t, roster, "planner", 5, "roster", []string{earlyBody}, nil)
	require.Equal(t, 409, w.Code, "02:00 overlaps the previous night shift ending 06:00")

	primary := ""
	for _, a := range f.allocations(t, roster, "") {
		if a["staff_id"] == n1 && a["roster_date"] == "2026-10-12" && a["additional"] == false {
			primary = a["id"].(string)
		}
	}
	require.NotEmpty(t, primary)
	_, otherArea := f.allocation(n2, "2026-10-18", f.dayShift)
	_, unknownStaff := f.allocation(uuid.NewString(), "2026-10-18", f.dayShift)
	_, inactive := f.allocation(n3, "2026-10-18", f.dayShift)
	_, outside := f.allocation(n2, "2026-10-26", f.dayShift)
	_, foreignShift := f.allocation(n2, "2026-10-18", uuid.NewString())
	_, noPrimary := f.allocation(n2, "2026-10-18", f.nightShift)
	_, n1Sunday := f.allocation(n1, "2026-10-18", f.dayShift)
	for name, tc := range map[string]struct {
		mode   string
		add    []string
		remove []string
		code   string
	}{
		"orphaned additional":   {"roster", nil, []string{primary}, "additional allocations"},
		"unknown staff":         {"roster", []string{unknownStaff}, nil, "invalid_reference"},
		"inactive staff":        {"roster", []string{inactive}, nil, "invalid_reference"},
		"shift outside area":    {"roster", []string{foreignShift}, nil, "invalid_reference"},
		"date outside roster":   {"roster", []string{outside}, nil, "invalid_request"},
		"two people":            {"employee", []string{otherArea, n1Sunday}, nil, "invalid_request"},
		"additional no primary": {"additional", []string{noPrimary}, nil, "primary allocation"},
		"unknown removal":       {"roster", nil, []string{uuid.NewString()}, "invalid_reference"},
	} {
		w = f.change(t, roster, "planner", 5, tc.mode, tc.add, tc.remove)
		require.Equal(t, 400, w.Code, name+" "+w.Body.String())
		require.Contains(t, w.Body.String(), tc.code, name)
	}
	require.Equal(t, 400, f.do(t, "POST", "/hr/rosters/"+roster+"/changes", "planner", `{"change_id":"`+uuid.NewString()+`","expected_version":5,"mode":"roster","add":[],"remove":[],"reviewed":true}`).Code, "empty change")
	require.Equal(t, 400, f.do(t, "POST", "/hr/rosters/"+roster+"/changes", "planner", `{"change_id":"`+uuid.NewString()+`","expected_version":5,"mode":"roster","add":[`+otherArea+`],"reviewed":false}`).Code, "unreviewed")
	require.Equal(t, 404, f.change(t, roster, "reader", 5, "roster", []string{otherArea}, nil).Code, "reader grant cannot write")
	require.Equal(t, 403, f.change(t, roster, "directory", 5, "roster", []string{otherArea}, nil).Code)
	require.Equal(t, 404, f.change(t, roster, "foreign", 5, "roster", []string{otherArea}, nil).Code)

	w = f.change(t, roster, "planner", 5, "roster", nil, []string{primary, extra})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 2, decodeBody[rosterResult](t, w).Revision.Removed)
	require.Len(t, f.allocations(t, roster, ""), 20)
	require.Len(t, f.allocations(t, roster, "&version=2"), 20, "as-of snapshot after population")
	require.Len(t, f.allocations(t, roster, "&version=5"), 22, "as-of snapshot before the removal")
	require.Equal(t, 404, f.do(t, "GET", "/hr/rosters/"+roster+"/allocations?version=99", "reader", "").Code)
	require.Equal(t, 400, f.do(t, "GET", "/hr/rosters/"+roster+"/allocations?version=0", "reader", "").Code)

	require.Equal(t, 200, f.write(t, roster, "freeze", "planner", 6, "").Code)
	w = f.change(t, roster, "planner", 7, "roster", []string{otherArea}, nil)
	require.Equal(t, 409, w.Code, "frozen rosters are read-only")
	require.Contains(t, w.Body.String(), "roster_state")
	require.Equal(t, 409, f.write(t, roster, "populate", "planner", 7, "").Code)
	require.Equal(t, 403, f.write(t, roster, "unfreeze", "planner", 7, `,"reason":"Correct ward cover"`).Code, "publishing does not grant correction")
	w = f.write(t, roster, "unfreeze", "admin", 7, "")
	require.Equal(t, 400, w.Code, "correction needs a reason")
	require.Equal(t, 200, f.write(t, roster, "unfreeze", "admin", 7, `,"reason":"Correct ward cover"`).Code)
	require.Equal(t, 200, f.change(t, roster, "planner", 8, "roster", []string{otherArea}, nil).Code)
	require.Equal(t, 409, f.write(t, roster, "unfreeze", "admin", 9, `,"reason":"Twice"`).Code, "only frozen rosters unfreeze")

	require.Equal(t, 400, f.write(t, roster, "discard", "planner", 9, "").Code, "discard needs a reason")
	w = f.write(t, roster, "discard", "planner", 9, `,"reason":"Superseded by revised plan"`)
	require.Equal(t, 200, w.Code, w.Body.String())
	res = decodeBody[rosterResult](t, w)
	require.Equal(t, "discarded", res.Roster.Status)
	require.Zero(t, res.Roster.Allocations)
	require.Equal(t, 21, res.Revision.Removed)
	require.Len(t, f.allocations(t, roster, "&version=9"), 21, "history survives discard")
	require.Equal(t, 409, f.write(t, roster, "freeze", "planner", 10, "").Code, "discarded is terminal")
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+uuid.NewString(), "planner", `{"name":"Synthetic October v2","start_date":"2026-10-12","end_date":"2026-10-25","reviewed":true}`).Code, "a discarded period is free again")

	w = f.do(t, "GET", "/hr/rosters/"+roster+"/revisions?limit=20", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, action := range []string{"created", "populated", "changed", "frozen", "unfrozen", "discarded", "Correct ward cover", "user-admin"} {
		require.Contains(t, w.Body.String(), action)
	}
	require.EqualValues(t, 10, f.count(t, "workforce_roster_revisions", "roster_id=?", roster))
	require.EqualValues(t, 11, f.count(t, "outbox_events", "subject=?", workforce.RosterChanged), "one identifier-only event per committed write")
	require.EqualValues(t, 10, f.count(t, "workforce_audit", "resource_id=? AND action LIKE 'workforce.roster.%'", roster))
}

func TestRosterSettingsAsOfAndPopulationScope(t *testing.T) {
	f := newRosterFixture(t, "Asia/Kolkata", 0, "2026-10-05")
	require.Equal(t, 200, f.do(t, "PUT", "/hr/roster-settings", "admin", `{"expected_version":1,"timezone":"Asia/Kolkata","week_start":0,"auto_population":"fixed_shift","effective_on":"2026-10-26","reviewed":true}`).Code)
	require.Equal(t, 200, f.do(t, "PUT", "/hr/roster-settings", "admin", `{"expected_version":2,"timezone":"Asia/Kolkata","week_start":0,"auto_population":"disabled","effective_on":"2026-11-09","reviewed":true}`).Code)

	first, fixed, disabled := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+first, "planner", `{"name":"Synthetic A","start_date":"2026-10-12","end_date":"2026-10-25","reviewed":true}`).Code)
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+fixed, "planner", `{"name":"Synthetic B","start_date":"2026-10-26","end_date":"2026-11-08","reviewed":true}`).Code)
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+disabled, "planner", `{"name":"Synthetic C","start_date":"2026-11-09","end_date":"2026-11-15","reviewed":true}`).Code)

	w := f.write(t, fixed, "populate", "planner", 1, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	res := decodeBody[rosterResult](t, w)
	require.Equal(t, 10, res.Revision.Added, "fixed_shift populates only fixed assignments")
	require.Equal(t, 20, res.Revision.SkippedUnassigned)

	w = f.write(t, first, "populate", "planner", 1, `,"staff_ids":["`+f.staff["SYN-N2"]+`"]`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 10, decodeBody[rosterResult](t, w).Revision.Added, "earlier roster keeps its pinned all_assigned settings; staff filter applies")

	w = f.write(t, disabled, "populate", "planner", 1, "")
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "population_disabled")

	_, crossing := f.allocation(f.staff["SYN-N2"], "2026-10-25", f.nightShift)
	require.Equal(t, 200, f.change(t, first, "planner", 2, "roster", []string{crossing}, nil).Code)
	_, early := f.allocation(f.staff["SYN-N2"], "2026-10-26", f.earlyShift)
	require.Equal(t, 409, f.change(t, fixed, "planner", 2, "roster", []string{early}, nil).Code, "same-person overlap across adjacent rosters")
}

func TestRosterDSTTransitionsFailClosed(t *testing.T) {
	f := newRosterFixture(t, "America/New_York", 6, "2026-03-01")
	roster := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", `{"name":"Synthetic March","start_date":"2026-03-08","end_date":"2026-03-14","reviewed":true}`).Code)
	_, gap := f.allocation(f.staff["SYN-N1"], "2026-03-08", f.earlyShift)
	w := f.change(t, roster, "planner", 1, "roster", []string{gap}, nil)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "dst_unresolvable", "02:00 does not exist on the spring-forward date")
	_, before := f.allocation(f.staff["SYN-N1"], "2026-03-07", f.nightShift)
	require.Equal(t, 400, f.change(t, roster, "planner", 1, "roster", []string{before}, nil).Code, "date before roster start")
	night, nightBody := f.allocation(f.staff["SYN-N1"], "2026-03-08", f.nightShift)
	require.Equal(t, 200, f.change(t, roster, "planner", 1, "roster", []string{nightBody}, nil).Code)
	for _, a := range f.allocations(t, roster, "") {
		if a["id"] == night {
			require.Equal(t, "2026-03-09T02:00:00Z", a["starts_at"], "22:00 EDT")
			require.Equal(t, "2026-03-09T10:00:00Z", a["ends_at"], "06:00 EDT")
		}
	}
}

func TestRosterTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	f := newRosterFixture(t, "Asia/Kolkata", 0, "2026-10-05")
	roster := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", `{"name":"Synthetic October","start_date":"2026-10-12","end_date":"2026-10-25","reviewed":true}`).Code)
	require.Equal(t, 200, f.write(t, roster, "populate", "planner", 1, "").Code)

	require.ErrorContains(t, f.scopedExec(t, true, `UPDATE workforce_roster_allocations SET shift_revision_id=? WHERE roster_id=?`, f.nightShift, roster), "immutable")
	require.ErrorContains(t, f.scopedExec(t, true, `DELETE FROM workforce_roster_allocations WHERE roster_id=?`, roster), "not supported")
	require.ErrorContains(t, f.scopedExec(t, true, `UPDATE workforce_roster_revisions SET reason='rewritten'`), "append only")
	require.ErrorContains(t, f.scopedExec(t, true, `UPDATE workforce_rosters SET start_date=start_date+7,end_date=end_date+7,version=version+1 WHERE id=?`, roster), "immutable")
	require.ErrorContains(t, f.scopedExec(t, true, `UPDATE workforce_rosters SET status='discarded',version=version+1 WHERE id=?`, roster), "")
	require.Error(t, f.scopedExec(t, false, `UPDATE workforce_roster_allocations SET removed_version=3,removed_revision_id=gen_random_uuid() WHERE roster_id=?`, roster), "writes need the rostering flag")
	require.ErrorContains(t, f.scopedExec(t, true, `INSERT INTO workforce_roster_allocations(tenant_id,hospital_id,id,roster_id,staff_id,roster_date,shift_revision_id,area_revision_id,starts_at,ends_at,additional,added_version,added_revision_id)
 SELECT tenant_id,hospital_id,gen_random_uuid(),roster_id,staff_id,roster_date,shift_revision_id,area_revision_id,starts_at+interval '1 hour',ends_at+interval '1 hour',true,added_version,added_revision_id FROM workforce_roster_allocations WHERE roster_id=? LIMIT 1`, roster), "workforce_roster_allocation_overlap")
	require.Equal(t, 200, f.write(t, roster, "freeze", "planner", 2, "").Code)
	require.ErrorContains(t, f.scopedExec(t, true, `UPDATE workforce_roster_allocations SET removed_version=3,removed_revision_id=(SELECT revision_id FROM workforce_rosters WHERE id=?) WHERE roster_id=?`, roster, roster), "immutable")
	require.ErrorContains(t, f.scopedExec(t, true, `INSERT INTO workforce_roster_allocations(tenant_id,hospital_id,id,roster_id,staff_id,roster_date,shift_revision_id,area_revision_id,starts_at,ends_at,additional,added_version,added_revision_id)
 SELECT tenant_id,hospital_id,gen_random_uuid(),roster_id,staff_id,roster_date+7,shift_revision_id,area_revision_id,starts_at+interval '7 days',ends_at+interval '7 days',false,3,added_revision_id FROM workforce_roster_allocations WHERE roster_id=? AND roster_date='2026-10-12' LIMIT 1`, roster), "draft roster")
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_roster_allocations").Count(&n).Error)
		require.Zero(t, n, "RLS hides rows without a hospital grant in scope")
		return nil
	}))
}
