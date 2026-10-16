package workforce_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (f *attendanceFixture) period(id, user, from, to, note string) (int, map[string]any) {
	f.t.Helper()
	return f.call("PUT", "/hr/attendance/periods/"+id, user, map[string]any{"from": from, "to": to, "note": note})
}

func (f *attendanceFixture) transition(id, step, user string, version int, note string) (int, map[string]any) {
	f.t.Helper()
	return f.call("POST", "/hr/attendance/periods/"+id+"/"+step, user, map[string]any{"expected_version": version, "note": note})
}

// list follows every page of a listing and returns its rows.
func (f *attendanceFixture) list(path, user string) []map[string]any {
	f.t.Helper()
	rows, cursor := []map[string]any{}, ""
	for {
		sep := "?"
		if u, err := url.Parse(path); err == nil && u.RawQuery != "" {
			sep = "&"
		}
		code, out := f.call("GET", path+sep+"limit=7"+cursor, user, nil)
		require.Equal(f.t, 200, code, out)
		for _, row := range out["data"].([]any) {
			rows = append(rows, row.(map[string]any))
		}
		page := out["page"].(map[string]any)
		if page["has_more"] != true {
			return rows
		}
		cursor = "&cursor=" + url.QueryEscape(page["next_cursor"].(string))
	}
}

func timesheetOf(period map[string]any) map[string]any { return period["timesheet"].(map[string]any) }

func TestAttendanceTimesheetsFreezeReconcileAndRecalculate(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days
	f.punched(f.n1, "in", istAt(d[0], "08:00"))
	n1Out := f.punched(f.n1, "out", istAt(d[0], "16:00"))
	// Saturday is a weekly off, so this punch is a stray day of its own.
	stray := f.punched(f.n1, "in", istAt(d[5], "12:00"))

	live := map[string]map[string]map[string]any{}
	var shifts, payable, unpaid float64
	for _, date := range d[:7] {
		live[date] = f.sheet(date)
		code, out := f.call("GET", "/hr/attendance-totals?date="+date, "planner", nil)
		require.Equal(t, 200, code, out)
		shifts += out["shifts"].(float64)
		payable += out["payable_minutes"].(float64)
		unpaid += out["unpaid_half_days"].(float64)
	}
	require.Positive(t, shifts)

	id := uuid.NewString()
	code, out = f.period(id, "planner", d[0], d[6], "")
	require.Equal(t, 403, code, out, "rostering does not grant finalising")
	code, out = f.period(id, "admin", d[0], d[6], "  Week one close  ")
	require.Equal(t, 200, code, out)
	require.Equal(t, "finalised", out["status"])
	require.EqualValues(t, 1, out["version"])
	first := timesheetOf(out)
	require.EqualValues(t, 1, first["number"])
	require.Equal(t, "Week one close", first["note"])
	require.EqualValues(t, shifts, first["shifts"], "the snapshot matches the live sheet it froze")
	require.EqualValues(t, payable, first["payable_minutes"])
	require.EqualValues(t, unpaid, first["unpaid_half_days"])
	require.EqualValues(t, 2, first["people"])
	require.EqualValues(t, first["days"], first["added_days"], "the first version adds every day")
	require.NotEmpty(t, first["policy_revision_ids"])

	code, again := f.period(id, "admin", d[0], d[6], "Week one close")
	require.Equal(t, 200, code, again, "an identical retry returns the period")
	require.Equal(t, first["id"], timesheetOf(again)["id"])
	code, out = f.period(id, "admin", d[0], d[5], "Week one close")
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_reused", out["error"])
	code, out = f.period(uuid.NewString(), "admin", d[6], d[8], "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_overlap", out["reason"])
	require.Equal(t, id, out["period_id"])

	// People and days reconcile to the totals, and each day is the live sheet's day exactly.
	base := "/hr/attendance/periods/" + id + "/timesheets/" + first["id"].(string)
	people := f.list(base+"/people", "planner")
	require.Len(t, people, 2)
	var personPayable float64
	for _, p := range people {
		personPayable += p["payable_minutes"].(float64)
	}
	require.Equal(t, payable, personPayable)
	days := f.list(base+"/days", "planner")
	require.EqualValues(t, first["days"], len(days))
	for i, day := range days {
		require.Equal(t, "added", day["change"])
		staff, date := day["staff_id"].(string), day["date"].(string)
		require.Equal(t, live[date][staff], day["day"], "%s %s", staff, date)
		if i > 0 {
			require.LessOrEqual(t, date, days[i-1]["date"].(string), "newest day first")
		}
	}

	// A finalised period refuses every attendance-owned write dated inside it.
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.n2, "in", istAt(d[1], "08:00"), "Late register"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_locked", out["error"])
	code, out = f.call("POST", "/hr/attendance/punches/"+n1Out+"/void", "admin", map[string]any{"reason": "Wrong time"})
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_locked", out["error"])
	code, out = f.fileCorrection(uuid.NewString(), "filer", addBody(f.n2, "in", istAt(d[2], "08:00"), "Forgot to punch in"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_locked", out["error"])
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(1, 30, d[3]))
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_locked", out["error"])
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.n2, "in", istAt(d[8], "08:00"), "After the period"))
	require.Equal(t, 200, code, out, "days after the period stay open")

	// Reopening is versioned, replayable and only allowed from finalised.
	code, out = f.transition(id, "reopening", "admin", 2, "Ward register arrived")
	require.Equal(t, 409, code, out)
	require.Equal(t, "stale_version", out["error"])
	code, out = f.transition(id, "reopening", "admin", 1, "  ")
	require.Equal(t, 400, code, out)
	code, out = f.transition(id, "reopening", "planner", 1, "Ward register arrived")
	require.Equal(t, 403, code, out)
	code, out = f.transition(id, "reopening", "admin", 1, "Ward register arrived")
	require.Equal(t, 200, code, out)
	require.Equal(t, "reopened", out["status"])
	require.EqualValues(t, 2, out["version"])
	require.Equal(t, first["id"], timesheetOf(out)["id"], "reopening keeps the finalised version")
	code, out = f.transition(id, "reopening", "admin", 1, "Ward register arrived")
	require.Equal(t, 200, code, out, "a replay returns the period")
	require.EqualValues(t, 2, out["version"])
	code, out = f.transition(id, "reopening", "admin", 2, "Again")
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_state", out["error"])

	// Reopened days accept writes again; finalising writes version two marked against version one.
	f.punched(f.n2, "in", istAt(d[1], "08:00"))
	f.punched(f.n2, "out", istAt(d[1], "16:00"))
	code, out = f.call("POST", "/hr/attendance/punches/"+n1Out+"/void", "admin", map[string]any{"reason": "Wrong time"})
	require.Equal(t, 200, code, out)
	code, out = f.call("POST", "/hr/attendance/punches/"+stray+"/void", "admin", map[string]any{"reason": "Not on duty"})
	require.Equal(t, 200, code, out)
	code, out = f.transition(id, "finalisation", "admin", 2, "Recalculated with the ward register")
	require.Equal(t, 200, code, out)
	require.Equal(t, "finalised", out["status"])
	require.EqualValues(t, 3, out["version"])
	second := timesheetOf(out)
	require.EqualValues(t, 2, second["number"])
	require.EqualValues(t, 2, second["changed_days"])
	require.EqualValues(t, 1, second["removed_days"])
	require.EqualValues(t, 0, second["added_days"])
	base = "/hr/attendance/periods/" + id + "/timesheets/" + second["id"].(string)
	changed := f.list(base+"/days?change=changed", "planner")
	require.Len(t, changed, 2)
	require.ElementsMatch(t, []string{f.n1 + d[0], f.n2 + d[1]}, []string{changed[0]["staff_id"].(string) + changed[0]["date"].(string), changed[1]["staff_id"].(string) + changed[1]["date"].(string)})
	require.Equal(t, "present", firstShift(f.sheet(d[1])[f.n2])["status"])
	removed := f.list(base+"/days?change=removed&staff_id="+f.n1, "planner")
	require.Len(t, removed, 1)
	require.Equal(t, d[5], removed[0]["date"])
	require.NotContains(t, removed[0], "day", "a removed day carries no derived day")
	require.EqualValues(t, 0, removed[0]["payable_minutes"])
	code, out = f.call("PUT", "/hr/attendance/punches/"+uuid.NewString(), "admin", punchBody(f.n2, "in", istAt(d[2], "08:00"), "Late register"))
	require.Equal(t, 409, code, out, "finalising again locks the days again")
	require.Equal(t, "period_locked", out["error"])

	// Recalculating an unchanged period writes a version with nothing marked.
	code, out = f.transition(id, "finalisation", "admin", 3, "Month-end check")
	require.Equal(t, 200, code, out)
	third := timesheetOf(out)
	require.EqualValues(t, 3, third["number"])
	require.EqualValues(t, 0, third["changed_days"].(float64)+third["added_days"].(float64)+third["removed_days"].(float64))
	require.Equal(t, second["payable_minutes"], third["payable_minutes"])

	versions := f.list("/hr/attendance/periods/"+id+"/timesheets", "admin")
	require.Len(t, versions, 3)
	require.EqualValues(t, 3, versions[0]["number"])
	history := f.list("/hr/attendance/periods/"+id+"/history", "admin")
	require.Len(t, history, 4)
	require.Equal(t, "Month-end check", history[0]["note"])
	require.Equal(t, "reopened", history[2]["status"])
	require.EqualValues(t, 4, f.count(t, "workforce_audit", "resource_id=? AND action LIKE 'workforce.attendance.period_%'", id))

	// Readers without an attendance permission see figures but never notes.
	code, out = f.call("GET", "/hr/attendance/periods/"+id, "reader", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "", timesheetOf(out)["note"])
	require.Equal(t, third["payable_minutes"], timesheetOf(out)["payable_minutes"])
	for _, event := range f.list("/hr/attendance/periods/"+id+"/history", "reader") {
		require.Equal(t, "", event["note"])
	}
	periods := f.list("/hr/attendance/periods", "reader")
	require.Len(t, periods, 1)
	require.EqualValues(t, 4, periods[0]["version"])
}

func TestAttendanceTimesheetFinalisationFailsClosed(t *testing.T) {
	f := newAttendanceFixture(t)
	d := f.days
	day := func(offset int) string {
		start, err := time.Parse(time.DateOnly, d[0])
		require.NoError(t, err)
		return start.AddDate(0, 0, offset).Format(time.DateOnly)
	}
	code, out := f.period(uuid.NewString(), "admin", d[0], d[6], "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_not_ready", out["reason"], "without an attendance policy every shift is unsettled")
	require.Positive(t, out["statuses"].(map[string]any)["no_policy"])
	code, out = f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)

	for name, body := range map[string][2]string{
		"reversed":  {d[6], d[0]},
		"too long":  {d[0], day(31)},
		"not a day": {"2026-02-30", d[6]},
		"loose":     {d[0][:8] + "1", d[6]},
	} {
		code, out := f.period(uuid.NewString(), "admin", body[0], body[1], "")
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	today := time.Now().In(ist).Format(time.DateOnly)
	code, out = f.period(uuid.NewString(), "admin", d[13], today, "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_not_closed", out["error"])

	f.punched(f.n1, "in", istAt(d[8], "08:00"))
	f.filedCorrection(addBody(f.n1, "out", istAt(d[8], "16:00"), "Forgot to punch out"))
	code, out = f.period(uuid.NewString(), "admin", d[7], d[9], "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "pending_corrections", out["reason"])
	require.EqualValues(t, 1, out["count"])

	// A draft roster's shifts are unpublished, and a guess is never frozen.
	draft := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+draft, "planner", `{"name":"Synthetic Draft","start_date":"`+day(-7)+`","end_date":"`+day(-1)+`","reviewed":true}`).Code)
	w := f.write(t, draft, "populate", "planner", 1, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	code, out = f.period(uuid.NewString(), "admin", day(-7), day(-1), "")
	require.Equal(t, 409, code, out)
	require.Equal(t, "period_not_ready", out["reason"])
	require.Positive(t, out["statuses"].(map[string]any)["unpublished"])
	require.EqualValues(t, 0, f.count(t, "workforce_attendance_periods", "true"), "refused finalisations leave nothing behind")

	id := uuid.NewString()
	code, out = f.period(id, "admin", d[0], d[6], "")
	require.Equal(t, 200, code, out)
	timesheet := timesheetOf(out)["id"].(string)
	code, out = f.call("GET", "/hr/attendance/periods/"+id, "foreign", nil)
	require.Equal(t, 404, code, out)
	code, out = f.transition(id, "reopening", "foreign", 1, "Not mine")
	require.Equal(t, 404, code, out)
	code, out = f.period(id, "foreign", d[0], d[6], "")
	require.Equal(t, 404, code, out)
	for _, path := range []string{"/hr/attendance/periods/" + uuid.NewString(), "/hr/attendance/periods/not-a-uuid", "/hr/attendance/periods/" + uuid.NewString() + "/history",
		"/hr/attendance/periods/" + uuid.NewString() + "/timesheets/" + timesheet + "/days", "/hr/attendance/periods/" + id + "/timesheets/" + uuid.NewString() + "/people"} {
		code, out := f.call("GET", path, "planner", nil)
		require.Equal(t, 404, code, "%s: %v", path, out)
	}
	code, out = f.call("GET", "/hr/attendance/periods/"+id+"/timesheets/"+timesheet+"/days?change=moved", "planner", nil)
	require.Equal(t, 400, code, out)
	code, out = f.call("GET", "/hr/attendance/periods/"+id+"/timesheets/"+timesheet+"/days?staff_id=nobody", "planner", nil)
	require.Equal(t, 400, code, out)
}

func TestAttendancePunchReportListsEveryPunchWithoutReasons(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days
	first := f.punched(f.n1, "in", istAt(d[0], "08:00"))
	f.punched(f.n2, "in", istAt(d[1], "08:00"))
	last := f.punched(f.n1, "out", istAt(d[0], "16:00"))
	f.punched(f.n1, "in", istAt(d[9], "08:00"))
	code, out = f.call("POST", "/hr/attendance/punches/"+first+"/void", "admin", map[string]any{"reason": "Confidential void reason"})
	require.Equal(t, 200, code, out)

	rows := f.list("/hr/attendance/punch-report?from="+d[0]+"&to="+d[6], "reader")
	require.Len(t, rows, 3, "the date range excludes later punches")
	require.Equal(t, last, rows[0]["id"], "most recently recorded first")
	require.Equal(t, first, rows[2]["id"])
	require.Equal(t, true, rows[2]["voided"])
	require.Equal(t, "user-admin", rows[2]["voided_by_subject"])
	require.Equal(t, false, rows[0]["voided"])
	require.Nil(t, rows[0]["voided_at"])
	for _, row := range rows {
		require.Equal(t, "nurse", row["job_category"])
		require.Equal(t, "manual", row["source"])
		require.Equal(t, "user-admin", row["recorded_by_subject"])
		require.NotContains(t, row, "reason")
		require.NotContains(t, row, "void_reason")
	}
	mine := f.list("/hr/attendance/punch-report?from="+d[0]+"&to="+d[13]+"&staff_id="+f.n1, "reader")
	require.Len(t, mine, 3)
	for _, row := range mine {
		require.Equal(t, f.n1, row["staff_id"])
	}
	for _, query := range []string{"from=" + d[6] + "&to=" + d[0], "from=" + d[0], "from=" + d[0] + "&to=" + d[13] + "&staff_id=x", "from=2026-01-01&to=2026-03-01"} {
		code, out := f.call("GET", "/hr/attendance/punch-report?"+query, "reader", nil)
		require.Equal(t, 400, code, "%s: %v", query, out)
	}
	code, out = f.call("GET", "/hr/attendance/punch-report?from="+d[0]+"&to="+d[6], "foreign", nil)
	require.Equal(t, 404, code, out)
}

func TestAttendanceTimesheetTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	f := newAttendanceFixture(t)
	code, out := f.call("PUT", "/hr/attendance-settings", "admin", attendancePolicyBody(0, 30, "2026-01-01"))
	require.Equal(t, 200, code, out)
	d := f.days
	punch := f.punched(f.n1, "in", istAt(d[0], "08:00"))
	id := uuid.NewString()
	code, out = f.period(id, "admin", d[0], d[6], "")
	require.Equal(t, 200, code, out)
	fails := func(contains, sql string, args ...any) {
		t.Helper()
		err := f.attendanceExec(true, sql, args...)
		require.Error(t, err, sql)
		require.Contains(t, err.Error(), contains)
	}
	copyPunch := `INSERT INTO workforce_attendance_punches(tenant_id,hospital_id,id,staff_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,source,reason,recorded_by_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,'out',punched_at+interval '8 hours',local_date,timezone,settings_revision_id,policy_revision_id,'manual','x','user-admin' FROM workforce_attendance_punches WHERE id=?`
	fails("finalised attendance period", copyPunch, punch)
	fails("finalised attendance period", `INSERT INTO workforce_attendance_punch_voids(tenant_id,hospital_id,punch_id,reason,voided_by_subject) SELECT tenant_id,hospital_id,id,'x','user-admin' FROM workforce_attendance_punches WHERE id=?`, punch)
	fails("workforce_attendance_period_days_pkey", `INSERT INTO workforce_attendance_period_days(tenant_id,hospital_id,day,period_id) SELECT tenant_id,hospital_id,?::date,id FROM workforce_attendance_periods WHERE id=?`, d[3], id)
	fails("keep their range", `UPDATE workforce_attendance_periods SET period_to=period_to-1,version=version+1,status='reopened' WHERE id=?`, id)
	fails("keep their range", `UPDATE workforce_attendance_periods SET status='reopened' WHERE id=?`, id)
	fails("not supported", `DELETE FROM workforce_attendance_periods WHERE id=?`, id)
	fails("append only", `UPDATE workforce_attendance_timesheet_days SET payable_minutes=payable_minutes+1`)
	fails("append only", `DELETE FROM workforce_attendance_period_days`)
	fails("append only", `UPDATE workforce_attendance_period_events SET note='changed'`)

	// A second version reconciles when its totals match its (empty) people and fails when they do not.
	version := `INSERT INTO workforce_attendance_timesheets(tenant_id,hospital_id,id,period_id,number,people,days,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,statuses,exceptions,policy_revision_ids,note,finalised_by_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),period_id,?,0,0,0,?,0,0,'[]','[]','{}','','user-admin' FROM workforce_attendance_timesheets WHERE period_id=? AND number=1`
	require.ErrorContains(t, f.attendanceExec(false, version, 2, 0, id), "row-level security", "writes need the attendance flag")
	require.NoError(t, f.attendanceExec(true, version, 2, 0, id), "the balanced version is valid, so the failure below is the reconciliation")
	fails("reconcile", version, 3, 60, id)
	fails("reconcile", version, 5, 0, id)
}
