package workforce_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

// attendanceFixture freezes a two-week roster that ended before today, so every shift in it can be judged.
type attendanceFixture struct {
	rosterFixture
	t      *testing.T
	n1, n2 string
	days   []string
}

var ist = time.FixedZone("IST", 5*3600+1800)

func newAttendanceFixture(t *testing.T) *attendanceFixture {
	t.Helper()
	f := &attendanceFixture{rosterFixture: newRosterFixture(t, "Asia/Kolkata", 0, "2026-01-01"), t: t}
	now := time.Now().In(ist)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -14)
	for start.Weekday() != time.Monday {
		start = start.AddDate(0, 0, -1)
	}
	for d := range 14 {
		f.days = append(f.days, start.AddDate(0, 0, d).Format(time.DateOnly))
	}
	f.n1, f.n2 = f.staff["SYN-N1"], f.staff["SYN-N2"]
	roster := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", `{"name":"Synthetic Attendance","start_date":"`+f.days[0]+`","end_date":"`+f.days[13]+`","reviewed":true}`).Code)
	w := f.write(t, roster, "populate", "planner", 1, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	w = f.write(t, roster, "freeze", "planner", 2, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	return f
}

func (f *attendanceFixture) call(method, path, user string, body any) (int, map[string]any) {
	f.t.Helper()
	data := ""
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(f.t, err)
		data = string(raw)
	}
	w := f.do(f.t, method, path, user, data)
	var out map[string]any
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return w.Code, out
}

func attendancePolicyBody(version int64, backdate int, effective string) map[string]any {
	return map[string]any{"expected_version": version, "early_arrival_minutes": 60, "late_departure_minutes": 120, "early_departure_grace_minutes": 10,
		"short_duty_minutes": 30, "duplicate_window_minutes": 5, "backdate_days": backdate, "effective_on": effective, "reviewed": true}
}

// istAt is a wall-clock time on a roster date in the hospital timezone.
func istAt(date, clock string) string {
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, ist)
	if err != nil {
		panic(err)
	}
	return t.UTC().Format(time.RFC3339)
}

func punchBody(staff, direction, at, reason string) map[string]any {
	return map[string]any{"staff_id": staff, "direction": direction, "punched_at": at, "reason": reason}
}

// punched records a punch that must succeed and returns its id.
func (f *attendanceFixture) punched(staff, direction, at string) string {
	f.t.Helper()
	id := uuid.NewString()
	code, out := f.call("PUT", "/hr/attendance/punches/"+id, "admin", punchBody(staff, direction, at, "Synthetic register entry"))
	require.Equal(f.t, 200, code, out)
	return id
}

func (f *attendanceFixture) sheet(date string) map[string]map[string]any {
	f.t.Helper()
	code, out := f.call("GET", "/hr/attendance?date="+date, "planner", nil)
	require.Equal(f.t, 200, code, out)
	rows := map[string]map[string]any{}
	for _, row := range out["data"].([]any) {
		r := row.(map[string]any)
		rows[r["staff_id"].(string)] = r
	}
	return rows
}

// workingDays drops the synthetic calendar's holidays from candidate dates.
func workingDays(dates ...string) []string {
	out := []string{}
	for _, date := range dates {
		if md := date[5:]; md != "10-02" && md != "10-20" && md != "11-09" {
			out = append(out, date)
		}
	}
	return out
}

func firstShift(day map[string]any) map[string]any {
	return day["shifts"].([]any)[0].(map[string]any)
}

func countOf(list any, code string) float64 {
	for _, c := range list.([]any) {
		if c.(map[string]any)["code"] == code {
			return c.(map[string]any)["count"].(float64)
		}
	}
	return -1
}

func TestAttendancePolicyIsVersionedAndReviewed(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("GET", "/hr/attendance-settings", "planner", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.n1, "in", istAt(f.days[0], "08:00"), "Synthetic"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "no_attendance_policy", out["error"], out)

	code, out = f.call("PUT", "/hr/attendance-settings", "planner", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 403, code, out)
	body := attendancePolicyBody(0, 30, "2026-01-01")
	body["reviewed"] = false
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", body)
	require.Equal(t, 400, code, out)
	body = attendancePolicyBody(0, 30, "2026-01-01")
	body["duplicate_window_minutes"] = 0
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", body)
	require.Equal(t, 400, code, out)

	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Equal(t, "user-admin", out["reviewed_by_subject"])
	first := out["revision_id"]
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	require.Equal(t, first, out["revision_id"], "an identical retry returns the stored revision")
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 20, "2026-01-01"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "stale_version", out["error"], out)
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(1, 30, "2025-12-31"))
	require.Equal(t, 400, code, out, "a revision cannot start before the current one")
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(1, 14, "2026-02-01"))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	require.EqualValues(t, 14, out["backdate_days"])

	code, out = f.call("GET", "/hr/attendance-settings/history", "planner", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 2)
	require.Equal(t, first, out["data"].([]any)[1].(map[string]any)["revision_id"])
	require.EqualValues(t, 2, f.count(t, "outbox_events", "subject=?", workforce.AttendanceSettingsChanged))
	require.EqualValues(t, 2, f.count(t, "workforce_audit", "action='workforce.attendance_settings.changed'"))
	code, out = f.call("GET", "/hr/attendance-settings", "foreign", nil)
	require.Equal(t, 404, code, out)
}

func TestAttendancePunchesAreIdempotentValidatedAndRedacted(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	policy := out["revision_id"]
	d := f.days

	id := uuid.NewString()
	body := punchBody(f.n1, "in", istAt(d[0], "08:05"), "  Biometric terminal offline  ")
	code, out = f.call("PUT", "/hr/attendance/punches/"+id, "admin", body)
	require.Equal(t, 200, code, out)
	require.Equal(t, d[0], out["local_date"])
	require.Equal(t, "Asia/Kolkata", out["timezone"])
	require.Equal(t, "manual", out["source"])
	require.Equal(t, policy, out["policy_revision_id"])
	require.Equal(t, "Biometric terminal offline", out["reason"])
	require.Equal(t, "SYN-N1", out["employee_code"])
	require.Equal(t, false, out["voided"])
	code, out = f.call("PUT", "/hr/attendance/punches/"+id, "admin", body)
	require.Equal(t, 200, code, out, "an identical retry returns the stored punch")
	require.EqualValues(t, 1, f.count(t, "workforce_attendance_punches", "id=?", id))
	code, out = f.call("PUT", "/hr/attendance/punches/"+id, "admin", punchBody(f.n1, "in", istAt(d[0], "08:06"), "Biometric terminal offline"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_reused", out["error"], out)

	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.n1, "in", istAt(d[0], "08:09"), "Synthetic"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "duplicate_punch", out["reason"], out)
	require.Equal(t, id, out["punch_id"])
	f.punched(f.n1, "in", istAt(d[0], "08:10"))

	future := time.Now().Add(time.Hour).Truncate(time.Minute).UTC().Format(time.RFC3339)
	for _, bad := range []map[string]any{
		punchBody(f.n1, "in", future, "Synthetic"),
		punchBody(f.n1, "in", strings.Replace(istAt(d[1], "08:00"), ":00Z", ":30Z", 1), "Synthetic"),
		punchBody(f.n1, "sideways", istAt(d[1], "08:00"), "Synthetic"),
		punchBody(f.n1, "in", istAt(d[1], "08:00"), "   "),
		punchBody(uuid.Nil.String(), "in", istAt(d[1], "08:00"), "Synthetic"),
	} {
		code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", bad)
		require.Equal(t, 400, code, "%v %v", bad, out)
	}
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.staff["SYN-N3"], "in", istAt(d[1], "08:00"), "Synthetic"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "staff_inactive", out["error"], out)
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(uuid.NewString(), "in", istAt(d[1], "08:00"), "Synthetic"))
	require.Equal(t, 404, code, out)
	old := time.Now().In(ist).AddDate(0, 0, -40).Format(time.DateOnly)
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.n1, "in", istAt(old, "08:00"), "Synthetic"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "outside_backdate_window", out["error"], out)
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "directory", punchBody(f.n1, "in", istAt(d[1], "08:00"), "Synthetic"))
	require.Equal(t, 403, code, out)
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "reader", punchBody(f.n1, "in", istAt(d[1], "08:00"), "Synthetic"))
	require.Equal(t, 403, code, out)

	code, out = f.call("GET", "/hr/attendance/punches/"+id, "planner", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["reason"], "reasons need the record permission")
	code, out = f.call("GET", "/hr/attendance/punches/"+id, "foreign", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", "/hr/attendance/punches/"+uuid.NewString(), "planner", nil)
	require.Equal(t, 404, code, out)

	code, out = f.call("POST", "/hr/attendance/punches/"+id+"/void", "admin", map[string]any{"reason": "Entered against the wrong person"})
	require.Equal(t, 200, code, out)
	require.Equal(t, true, out["voided"])
	require.Equal(t, "Entered against the wrong person", out["void_reason"])
	require.Equal(t, "user-admin", out["voided_by_subject"])
	code, out = f.call("POST", "/hr/attendance/punches/"+id+"/void", "admin", map[string]any{"reason": "Entered against the wrong person"})
	require.Equal(t, 200, code, out, "repeating a void returns the voided punch")
	code, out = f.call("POST", "/hr/attendance/punches/"+id+"/void", "admin", map[string]any{"reason": "Another reason"})
	require.Equal(t, 409, code, out)
	require.Equal(t, "punch_voided", out["error"], out)
	code, out = f.call("POST", "/hr/attendance/punches/"+id+"/void", "admin", map[string]any{"reason": " "})
	require.Equal(t, 400, code, out)
	code, out = f.call("POST", "/hr/attendance/punches/"+uuid.NewString()+"/void", "admin", map[string]any{"reason": "Synthetic"})
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", "/hr/attendance/punches/"+id, "planner", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "", out["void_reason"])
	f.punched(f.n1, "in", istAt(d[0], "08:01"))

	code, out = f.call("GET", "/hr/attendance/punches?staff_id="+f.n1+"&from="+d[0]+"&to="+d[0], "admin", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 3, "voided punches stay listed")
	code, out = f.call("GET", "/hr/attendance/punches?staff_id="+f.n1+"&from="+d[0]+"&to="+time.Now().AddDate(0, 0, 60).Format(time.DateOnly), "admin", nil)
	require.Equal(t, 400, code, out)

	require.EqualValues(t, 4, f.count(t, "outbox_events", "subject=?", workforce.AttendancePunchChanged))
	require.EqualValues(t, 0, f.count(t, "outbox_events", "subject=? AND payload::text LIKE '%Biometric%'", workforce.AttendancePunchChanged), "events carry identifiers only")
	require.EqualValues(t, 0, f.count(t, "workforce_audit", "action LIKE 'workforce.attendance.%' AND changes::text LIKE '%wrong person%'"), "audit rows carry identifiers only")
	require.EqualValues(t, 1, f.count(t, "workforce_audit", "action='workforce.attendance.punch_voided' AND resource_id=?", id))
}

func TestAttendanceSheetTotalsAndPersonRangeDeriveFromRosterPunchesAndLeave(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days

	f.punched(f.n1, "in", istAt(d[0], "08:05"))
	out1 := f.punched(f.n1, "out", istAt(d[0], "16:00"))
	f.punched(f.n1, "in", istAt(d[5], "10:00"))

	call := func(method, path, user string, body any) (int, map[string]any) {
		return f.call(method, path, user, body)
	}
	year, err := strconv.Atoi(d[0][:4])
	require.NoError(t, err)
	types := configureLeave(t, call, year)
	code, out = call("PUT", "/staff/"+f.n2+"/leave-assignment", "admin", map[string]any{"expected_version": 0, "structure_revision_id": types.structure, "active": true, "effective_on": d[0][:4] + "-01-01", "reviewed": true})
	require.Equal(t, 200, code, out)
	code, out = call("PUT", "/staff/"+f.n2+"/leave-corrections/"+uuid.NewString(), "admin", map[string]any{"leave_type_id": types.earned, "leave_year": year, "half_days": 4, "effective_on": d[0], "reason": "Synthetic opening balance"})
	require.Equal(t, 200, code, out)
	// Leave dates skip the synthetic calendar's holidays and keep the non-clubbable earned leave a week away from the duty leave.
	open := workingDays(d[1], d[2], d[8], d[9], d[10], d[11])
	away, halfDay, shortDay := open[0], open[len(open)-2], open[len(open)-1]
	require.Less(t, away, d[3])
	f.punched(f.n2, "in", istAt(halfDay, "12:00"))
	f.punched(f.n2, "out", istAt(halfDay, "16:00"))
	f.punched(f.n2, "in", istAt(shortDay, "09:00"))
	f.punched(f.n2, "out", istAt(shortDay, "16:00"))
	for _, body := range []map[string]any{full(types.duty, away, away), half(types.earned, halfDay, "first_half"), short(types.earned, shortDay, 60)} {
		leave := uuid.NewString()
		code, out = call("PUT", "/staff/"+f.n2+"/leave-requests/"+leave, "admin", body)
		require.Equal(t, 200, code, out)
		code, out = call("POST", "/staff/"+f.n2+"/leave-requests/"+leave+"/decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
		require.Equal(t, 200, code, out)
	}

	rows := f.sheet(d[0])
	require.Len(t, rows, 2)
	present := firstShift(rows[f.n1])
	require.Equal(t, "present", present["status"])
	require.EqualValues(t, 5, present["late_minutes"], "grace is zero on the synthetic day shift")
	require.EqualValues(t, 475, present["worked_minutes"])
	require.EqualValues(t, 445, present["payable_minutes"])
	require.Equal(t, []any{"late"}, present["exceptions"])
	require.Equal(t, "Asia/Kolkata", rows[f.n1]["timezone"])
	absent := firstShift(rows[f.n2])
	require.Equal(t, "absent", absent["status"])
	require.EqualValues(t, 2, absent["unpaid_half_days"])

	code, out = f.call("GET", "/hr/attendance-totals?date="+d[0], "planner", nil)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["people"])
	require.EqualValues(t, 2, out["shifts"])
	require.EqualValues(t, 445, out["payable_minutes"])
	require.EqualValues(t, 2, out["unpaid_half_days"])
	require.EqualValues(t, 1, countOf(out["statuses"], "present"))
	require.EqualValues(t, 1, countOf(out["statuses"], "absent"))
	require.EqualValues(t, 0, countOf(out["statuses"], "on_leave"), "every status is reported, zeros included")
	require.EqualValues(t, 1, countOf(out["exceptions"], "late"))

	code, out = f.call("POST", "/hr/attendance/punches/"+out1+"/void", "admin", map[string]any{"reason": "Synthetic correction"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "missing_out", firstShift(f.sheet(d[0])[f.n1])["status"], "a void changes derived attendance on the next read")

	require.Equal(t, "on_leave", firstShift(f.sheet(away)[f.n2])["status"])
	half := f.sheet(halfDay)[f.n2]
	require.Equal(t, "first_half", half["leave"])
	require.Equal(t, "present", firstShift(half)["status"])
	require.EqualValues(t, 240, firstShift(half)["payable_minutes"])
	require.Empty(t, firstShift(half)["exceptions"], "a first-half leave excuses the afternoon arrival")

	excused := f.sheet(shortDay)[f.n2]
	require.Equal(t, "short", excused["leave"])
	require.EqualValues(t, 60, excused["short_leave_minutes"])
	require.EqualValues(t, 390, firstShift(excused)["payable_minutes"])
	require.Equal(t, []any{}, firstShift(excused)["exceptions"], "short leave covers the hour-late arrival")

	weekend := f.sheet(d[5])
	require.Len(t, weekend, 1, "only someone with a stray punch appears on a day off")
	require.Len(t, weekend[f.n1]["stray_punch_ids"], 1)
	require.Equal(t, []any{"stray_punch"}, weekend[f.n1]["exceptions"])

	code, out = f.call("GET", "/staff/"+f.n2+"/attendance?from="+d[0]+"&to="+d[13], "planner", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["days"], 10, "weekdays only; weekends have no shift or punch")
	require.Equal(t, d[0], out["days"].([]any)[0].(map[string]any)["date"])
	require.EqualValues(t, 1, countOf(out["statuses"], "on_leave"))
	require.EqualValues(t, 2, countOf(out["statuses"], "present"))
	require.EqualValues(t, 7, countOf(out["statuses"], "absent"))
	require.EqualValues(t, 14, out["unpaid_half_days"])
	require.EqualValues(t, 630, out["payable_minutes"])

	for _, path := range []string{"/hr/attendance?date=2026-02-30", "/hr/attendance?date=", "/hr/attendance-totals?date=26-01-01", "/staff/" + f.n1 + "/attendance?from=" + d[6] + "&to=" + d[0]} {
		code, out = f.call("GET", path, "planner", nil)
		require.Equal(t, 400, code, "%s %v", path, out)
	}
	code, out = f.call("GET", "/staff/"+uuid.NewString()+"/attendance?from="+d[0]+"&to="+d[6], "planner", nil)
	require.Equal(t, 404, code, out)
	code, out = f.call("GET", "/hr/attendance?date="+d[0], "foreign", nil)
	require.Equal(t, 404, code, out)

	code, out = f.call("GET", "/hr/attendance?date="+d[0]+"&limit=1", "planner", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	seen := []any{out["data"].([]any)[0].(map[string]any)["staff_id"]}
	cursor, ok := out["page"].(map[string]any)["next_cursor"].(string)
	require.True(t, ok, out)
	code, out = f.call("GET", "/hr/attendance?date="+d[0]+"&limit=1&cursor="+cursor, "planner", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	seen = append(seen, out["data"].([]any)[0].(map[string]any)["staff_id"])
	require.ElementsMatch(t, []any{f.n1, f.n2}, seen, "keyset paging walks people without repeats")
}

// attendanceExec runs statements with hospital scope and, when flagged, the attendance write flag the handlers set.
func (f *attendanceFixture) attendanceExec(flagged bool, sql string, args ...any) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		flag := ""
		if flagged {
			flag = "authorized"
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_attendance',?,true)`, f.hospital, flag).Error; err != nil {
			return err
		}
		return tx.Exec(sql, args...).Error
	})
}

func TestAttendanceTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	id := f.punched(f.n1, "in", istAt(f.days[0], "08:00"))
	copyPunch := `INSERT INTO workforce_attendance_punches(tenant_id,hospital_id,id,staff_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,source,reason,recorded_by_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,'out',%s,%s,timezone,settings_revision_id,policy_revision_id,%s,'x','user-admin' FROM workforce_attendance_punches WHERE id=?`
	fails := func(contains, sql string, args ...any) {
		t.Helper()
		err := f.attendanceExec(true, sql, args...)
		require.Error(t, err, sql)
		require.Contains(t, err.Error(), contains)
	}
	valid := replace3(copyPunch, "punched_at+interval '8 hours'", "local_date", "'manual'")
	require.ErrorContains(t, f.attendanceExec(false, valid, id), "row-level security", "writes need the attendance flag")
	require.NoError(t, f.attendanceExec(true, valid, id), "the flagged copy is valid, so the failures below are the constraint under test")
	fails("append only", `UPDATE workforce_attendance_punches SET reason='changed' WHERE id=?`, id)
	fails("append only", `DELETE FROM workforce_attendance_punches WHERE id=?`, id)
	fails("check constraint", replace3(copyPunch, "punched_at+interval '30 seconds'", "local_date", "'manual'"), id)
	fails("check constraint", replace3(copyPunch, "punched_at", "local_date+1", "'manual'"), id)
	fails("check constraint", replace3(copyPunch, "punched_at+interval '1 hour'", "local_date", "'device'"), id)
	fails("back-dating window", replace3(copyPunch, "clock_timestamp()+interval '1 day'", "(clock_timestamp()+interval '1 day')::date", "'manual'"), id)
	fails("back-dating window", replace3(copyPunch, "punched_at-interval '60 days'", "local_date-60", "'manual'"), id)
	require.NoError(t, f.attendanceExec(true, `INSERT INTO workforce_attendance_punch_voids(tenant_id,hospital_id,punch_id,reason,voided_by_subject) SELECT tenant_id,hospital_id,id,'x','user-admin' FROM workforce_attendance_punches WHERE id=?`, id))
	fails("append only", `DELETE FROM workforce_attendance_punch_voids WHERE punch_id=?`, id)
	fails("", `DELETE FROM workforce_attendance_settings`)
	fails("append only", `UPDATE workforce_attendance_setting_revisions SET backdate_days=92`)
}

func replace3(sql, a, b, c string) string {
	for _, v := range []string{a, b, c} {
		sql = strings.Replace(sql, "%s", v, 1)
	}
	return sql
}
