package workforce_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

// overtimeFixture works in the month after next, whose every day is inside the 92-day planning horizon.
type overtimeFixture struct {
	rosterFixture
	t          *testing.T
	n1, n2, n3 string
	days       []string
	month      string
	today      time.Time
}

func newOvertimeFixture(t *testing.T, weekStart int) *overtimeFixture {
	t.Helper()
	f := &overtimeFixture{rosterFixture: newRosterFixture(t, "Asia/Kolkata", weekStart, "2026-01-01"), t: t}
	now := time.Now().In(time.FixedZone("IST", 5*3600+1800))
	f.today = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	first := time.Date(now.Year(), now.Month()+2, 1, 0, 0, 0, 0, time.UTC)
	f.month = first.Format("2006-01")
	for first.Weekday() != time.Monday {
		first = first.AddDate(0, 0, 1)
	}
	for d := range 28 {
		f.days = append(f.days, first.AddDate(0, 0, d).Format(time.DateOnly))
	}
	f.n1, f.n2, f.n3 = f.staff["SYN-N1"], f.staff["SYN-N2"], f.staff["SYN-N3"]
	return f
}

func (f *overtimeFixture) call(method, path, user string, body any) (int, map[string]any) {
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

func overtimePolicyBody(version int64, daily, weekly, monthly, window int, effective string) map[string]any {
	return map[string]any{"expected_version": version, "daily_limit_minutes": daily, "weekly_limit_minutes": weekly, "monthly_limit_minutes": monthly, "filing_window_days": window, "effective_on": effective, "reviewed": true}
}

func overtimeBody(staff, day, start, end, reason string) map[string]any {
	return map[string]any{"staff_id": staff, "work_date": day, "start_time": start, "end_time": end, "reason": reason}
}

// configure sets limits of 240 a day, 600 a week and 900 a month with a seven-day filing window.
func (f *overtimeFixture) configure() string {
	code, out := f.call("PUT", "/hr/overtime-settings", "admin", overtimePolicyBody(0, 240, 600, 900, 7, "2026-01-01"))
	require.Equal(f.t, 200, code, out)
	return out["revision_id"].(string)
}

func (f *overtimeFixture) file(id, user string, body map[string]any) (int, map[string]any) {
	return f.call("PUT", "/hr/overtime/"+id, user, body)
}

func (f *overtimeFixture) act(id, action, user string, body map[string]any) (int, map[string]any) {
	return f.call("POST", "/hr/overtime/"+id+"/"+action, user, body)
}

// filed files a request that must succeed and returns its id.
func (f *overtimeFixture) filed(user string, body map[string]any) string {
	f.t.Helper()
	id := uuid.NewString()
	code, out := f.file(id, user, body)
	require.Equal(f.t, 200, code, out)
	require.Equal(f.t, "pending", out["status"])
	return id
}

func requireLimit(t *testing.T, code int, out map[string]any, period string, counted float64) {
	t.Helper()
	require.Equal(t, 409, code, out)
	require.Equal(t, "limit_exceeded", out["reason"], out)
	require.Contains(t, out["message"], map[string]string{"day": "daily", "week": "weekly", "month": "monthly"}[period])
	require.Equal(t, period, out["period"], out)
	require.EqualValues(t, counted, out["counted_minutes"], out)
}

func TestOvertimeFileDecideAndPay(t *testing.T) {
	f := newOvertimeFixture(t, 0)
	policy := f.configure()
	d := f.days

	a := uuid.NewString()
	body := overtimeBody(f.n1, d[1], "17:00", "20:00", "Synthetic surge cover")
	code, out := f.file(a, "filer", body)
	require.Equal(t, 200, code, out)
	require.Equal(t, "pending", out["status"])
	require.EqualValues(t, 180, out["requested_minutes"])
	require.EqualValues(t, 0, out["payable_minutes"])
	require.Nil(t, out["approved_minutes"])
	require.Equal(t, "Asia/Kolkata", out["timezone"])
	require.Equal(t, d[1]+"T11:30:00Z", out["starts_at"], "wall clock resolves in the hospital timezone")
	code, out = f.file(a, "filer", body)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"], "an identical retry returns the stored request")
	code, out = f.file(a, "filer", with(body, map[string]any{"reason": "Synthetic other"}))
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_reused", out["error"])

	code, out = f.call("GET", "/hr/overtime/"+a, "reader", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["reason"], "reasons stay with overtime permission holders")
	code, out = f.call("GET", "/hr/overtime/"+a, "decider2", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, "Synthetic surge cover", out["reason"])

	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[1], "19:00", "21:30", "Synthetic overlap"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "overtime_overlap", out["error"])
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[1], "21:00", "23:00", "Synthetic late"))
	requireLimit(t, code, out, "day", 180)
	a2 := f.filed("filer", overtimeBody(f.n1, d[1], "21:00", "22:00", "Synthetic late"))

	code, out = f.act(a, "decision", "filer", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 403, code, out)
	code, out = f.act(a, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve", "approved_minutes": 120})
	require.Equal(t, 400, code, "approving fewer minutes needs a note"+": %v", out)
	code, out = f.act(a, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve", "approved_minutes": 181, "note": "Synthetic"})
	require.Equal(t, 400, code, "approval never exceeds the request"+": %v", out)
	approve := map[string]any{"expected_version": 1, "decision": "approve", "approved_minutes": 120, "note": "Synthetic partial"}
	code, out = f.act(a, "decision", "decider", approve)
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", out["status"])
	require.EqualValues(t, 120, out["approved_minutes"])
	require.EqualValues(t, 120, out["payable_minutes"])
	require.Equal(t, policy, out["policy_revision_id"], "the decision pins the policy in force")
	require.Equal(t, "user-decider", out["decided_by_subject"])
	code, out = f.act(a, "decision", "decider", approve)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"], "replaying the decision returns it unchanged")
	code, out = f.act(a, "decision", "decider", with(approve, map[string]any{"approved_minutes": 90}))
	require.Equal(t, 409, code, "a different decision at the old version is stale"+": %v", out)
	require.Equal(t, "stale_version", out["error"])
	code, out = f.act(a, "decision", "decider", with(approve, map[string]any{"expected_version": 2}))
	require.Equal(t, 409, code, out)
	require.Equal(t, "request_state", out["error"])

	self := f.filed("decider", overtimeBody(f.n2, d[1], "17:00", "18:00", "Synthetic own"))
	code, out = f.act(self, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 403, code, "the filer never decides their own filing"+": %v", out)
	code, out = f.act(self, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "reject"})
	require.Equal(t, 400, code, "rejection needs a note"+": %v", out)
	code, out = f.act(self, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "reject", "approved_minutes": 30, "note": "Synthetic"})
	require.Equal(t, 400, code, "rejections carry no minutes"+": %v", out)
	code, out = f.act(self, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "reject", "note": "Synthetic not needed"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "rejected", out["status"])
	require.EqualValues(t, 0, out["payable_minutes"])

	// Day 1 counts 120 approved plus 60 pending; the week adds day 2 and day 3.
	b := f.filed("filer", overtimeBody(f.n1, d[2], "17:00", "21:00", "Synthetic surge"))
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[3], "17:00", "21:00", "Synthetic surge"))
	requireLimit(t, code, out, "week", 420)
	c := f.filed("filer", overtimeBody(f.n1, d[3], "17:00", "20:00", "Synthetic surge"))
	code, out = f.act(c, "decision", "decider2", map[string]any{"expected_version": 1, "decision": "reject", "note": "Synthetic"})
	require.Equal(t, 200, code, out)
	c2 := f.filed("filer", overtimeBody(f.n1, d[3], "17:00", "20:00", "Synthetic resubmitted"))
	code, out = f.act(c2, "cancellation", "filer", map[string]any{"expected_version": 1})
	require.Equal(t, 200, code, out)
	require.Equal(t, "cancelled", out["status"])
	code, out = f.act(c2, "cancellation", "filer", map[string]any{"expected_version": 1})
	require.Equal(t, 200, code, "replaying the cancellation returns it"+": %v", out)

	// The month holds 420 so far; week two adds 480 to reach exactly 900.
	f.filed("filer", overtimeBody(f.n1, d[8], "17:00", "21:00", "Synthetic surge"))
	f.filed("filer", overtimeBody(f.n1, d[9], "17:00", "21:00", "Synthetic surge"))
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[10], "17:00", "17:15", "Synthetic surge"))
	requireLimit(t, code, out, "month", 900)

	code, out = f.act(a, "revocation", "filer", map[string]any{"expected_version": 2, "note": "Synthetic"})
	require.Equal(t, 403, code, out)
	code, out = f.act(a, "revocation", "decider2", map[string]any{"expected_version": 2})
	require.Equal(t, 400, code, "revocation needs a note"+": %v", out)
	code, out = f.act(a, "revocation", "decider2", map[string]any{"expected_version": 2, "note": "Synthetic shift cancelled"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "revoked", out["status"])
	require.EqualValues(t, 120, out["approved_minutes"], "revocation keeps the decision on record")
	require.EqualValues(t, 0, out["payable_minutes"])
	require.Equal(t, "user-decider2", out["revoked_by_subject"])
	f.filed("filer", overtimeBody(f.n1, d[10], "17:00", "17:15", "Synthetic surge"))
	code, out = f.act(b, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 240, out["payable_minutes"])

	code, out = f.call("GET", "/hr/overtime-summary?month="+f.month, "reader", nil)
	require.Equal(t, 200, code, out)
	summary := map[string]map[string]any{}
	for _, row := range out["data"].([]any) {
		summary[row.(map[string]any)["staff_id"].(string)] = row.(map[string]any)
	}
	require.Len(t, summary, 2)
	require.EqualValues(t, 60+240+240+15, summary[f.n1]["pending_minutes"])
	require.EqualValues(t, 240, summary[f.n1]["approved_requested_minutes"])
	require.EqualValues(t, 240, summary[f.n1]["payable_minutes"], "rejected, cancelled and revoked minutes are never payable")
	require.EqualValues(t, 0, summary[f.n2]["payable_minutes"])
	require.Equal(t, "SYN-N1", summary[f.n1]["employee_code"])
	for _, query := range []string{"", "?month=2026-13", "?month=1999-12", "?month=" + f.month + "&limit=101"} {
		code, out = f.call("GET", "/hr/overtime-summary"+query, "reader", nil)
		require.Equal(t, 400, code, "%s %v", query, out)
	}

	code, out = f.call("GET", "/hr/overtime?status=approved", "reader", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.Equal(t, b, out["data"].([]any)[0].(map[string]any)["id"])
	require.Empty(t, out["data"].([]any)[0].(map[string]any)["reason"])
	code, out = f.call("GET", "/hr/overtime?staff_id="+f.n2, "filer", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.Equal(t, "Synthetic own", out["data"].([]any)[0].(map[string]any)["reason"])
	code, out = f.call("GET", "/hr/overtime?from="+d[2]+"&to="+d[3], "filer", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 3, "day 2 and both day-3 filings")
	code, out = f.call("GET", "/hr/overtime?limit=2", "filer", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 2)
	code, out = f.call("GET", "/hr/overtime?staff_id="+f.n1+"&from="+d[1]+"&to="+d[1]+"&status=pending", "filer", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
	require.Equal(t, a2, out["data"].([]any)[0].(map[string]any)["id"], "filters combine")
	for _, query := range []string{"status=open", "staff_id=x", "from=2026-02-30", "from=" + d[3] + "&to=" + d[2], "limit=101"} {
		code, out = f.call("GET", "/hr/overtime?"+query, "filer", nil)
		require.Equal(t, 400, code, "%s %v", query, out)
	}

	code, out = f.call("GET", "/hr/overtime/"+a+"/history", "filer", nil)
	require.Equal(t, 200, code, out)
	events := out["data"].([]any)
	require.Len(t, events, 3)
	require.Equal(t, "revoked", events[0].(map[string]any)["status"], "newest first")
	require.Equal(t, "Synthetic shift cancelled", events[0].(map[string]any)["note"])
	require.EqualValues(t, 120, events[1].(map[string]any)["approved_minutes"])
	code, out = f.call("GET", "/hr/overtime/"+a+"/history", "reader", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"].([]any)[0].(map[string]any)["note"], "notes follow reason redaction")
	code, _ = f.call("GET", "/hr/overtime/"+uuid.NewString()+"/history", "reader", nil)
	require.Equal(t, 404, code)

	require.EqualValues(t, 15, f.count(t, "outbox_events", "subject=?", workforce.OvertimeChanged), "one event per filing and transition")
	require.EqualValues(t, 1, f.count(t, "outbox_events", "subject=?", workforce.OvertimeSettingsChanged))
	var leaked []string
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject IN (?,?) UNION ALL SELECT changes::text FROM workforce_audit WHERE action LIKE 'workforce.overtime%'`,
			workforce.OvertimeChanged, workforce.OvertimeSettingsChanged).Scan(&leaked).Error
	}))
	require.Len(t, leaked, 32)
	for _, text := range leaked {
		require.NotContains(t, text, "Synthetic", "events and audit carry identifiers and minutes only")
	}
}

func TestOvertimeRulesFailClosed(t *testing.T) {
	f := newOvertimeFixture(t, 0)
	d := f.days
	code, out := f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[1], "17:00", "18:00", "Synthetic"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "no_overtime_policy", out["error"])
	code, _ = f.call("GET", "/hr/overtime-settings", "reader", nil)
	require.Equal(t, 404, code)

	code, out = f.call("PUT", "/hr/overtime-settings", "filer", overtimePolicyBody(0, 240, 600, 900, 7, "2026-01-01"))
	require.Equal(t, 403, code, out)
	for _, bad := range []map[string]any{
		overtimePolicyBody(0, 300, 240, 900, 7, "2026-01-01"),
		overtimePolicyBody(0, 240, 1000, 900, 7, "2026-01-01"),
		overtimePolicyBody(0, 240, 600, 900, 93, "2026-01-01"),
		overtimePolicyBody(0, 240, 600, 900, 7, "1999-12-31"),
		with(overtimePolicyBody(0, 240, 600, 900, 7, "2026-01-01"), map[string]any{"reviewed": false}),
	} {
		code, out = f.call("PUT", "/hr/overtime-settings", "admin", bad)
		require.Equal(t, 400, code, "%v %v", bad, out)
	}
	first := f.configure()
	code, out = f.call("PUT", "/hr/overtime-settings", "admin", overtimePolicyBody(0, 240, 600, 900, 7, "2026-01-01"))
	require.Equal(t, 200, code, out)
	require.Equal(t, first, out["revision_id"], "an identical write returns the current revision")
	code, out = f.call("PUT", "/hr/overtime-settings", "admin", overtimePolicyBody(0, 60, 600, 900, 7, d[7]))
	require.Equal(t, 409, code, "a changed policy needs the current version"+": %v", out)
	code, out = f.call("PUT", "/hr/overtime-settings", "admin", overtimePolicyBody(1, 60, 600, 900, 7, "2025-12-31"))
	require.Equal(t, 400, code, "the effective date never moves earlier"+": %v", out)
	code, out = f.call("PUT", "/hr/overtime-settings", "admin", overtimePolicyBody(1, 60, 600, 900, 7, d[7]))
	require.Equal(t, 200, code, out)
	second := out["revision_id"].(string)
	code, out = f.call("GET", "/hr/overtime-settings/history", "reader", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 2)
	require.Equal(t, second, out["data"].([]any)[0].(map[string]any)["revision_id"])
	code, _ = f.call("GET", "/hr/overtime-settings", "foreign", nil)
	require.Equal(t, 404, code, "another tenant never sees the hospital")

	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[8], "17:00", "19:00", "Synthetic"))
	requireLimit(t, code, out, "day", 0)
	require.EqualValues(t, 60, out["limit_minutes"], "the revision in force on the work date governs")
	early := f.filed("filer", overtimeBody(f.n1, d[1], "17:00", "19:00", "Synthetic"))
	code, out = f.act(early, "decision", "decider", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)
	require.Equal(t, first, out["policy_revision_id"])

	reject := func(want string, body map[string]any) {
		t.Helper()
		code, out := f.file(uuid.NewString(), "filer", body)
		require.Equal(t, 409, code, "%v %v", body, out)
		require.Equal(t, want, out["error"], out)
	}
	reject("staff_inactive", overtimeBody(f.n3, d[2], "17:00", "18:00", "Synthetic"))
	reject("outside_filing_window", overtimeBody(f.n1, f.today.AddDate(0, 0, -8).Format(time.DateOnly), "17:00", "18:00", "Synthetic"))
	reject("outside_filing_window", overtimeBody(f.n1, f.today.AddDate(0, 0, 93).Format(time.DateOnly), "17:00", "18:00", "Synthetic"))
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, f.today.AddDate(0, 0, -7).Format(time.DateOnly), "23:00", "23:30", "Synthetic"))
	require.Equal(t, 200, code, "the last day of the filing window is open"+": %v", out)
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(uuid.NewString(), d[2], "17:00", "18:00", "Synthetic"))
	require.Equal(t, 404, code, out)
	for _, body := range []map[string]any{
		overtimeBody(f.n1, d[2], "24:00", "02:00", "Synthetic"),
		overtimeBody(f.n1, d[2], "+7:00", "18:00", "Synthetic"),
		overtimeBody(f.n1, d[2], "17:00", "17:10", "Synthetic"),
		overtimeBody(f.n1, "2026-02-30", "17:00", "18:00", "Synthetic"),
		overtimeBody(f.n1, d[2], "17:00", "18:00", "   "),
	} {
		code, out = f.file(uuid.NewString(), "filer", body)
		require.Equal(t, 400, code, "%v %v", body, out)
	}
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n2, d[2], "23:00", "01:00", "Synthetic night"))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 120, out["requested_minutes"], "an end before the start is the next day")

	roster := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", `{"name":"Synthetic Overtime","start_date":"`+d[14]+`","end_date":"`+d[20]+`","reviewed":true}`).Code)
	w := f.write(t, roster, "populate", "planner", 1, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[14], "15:00", "16:00", "Synthetic"))
	require.Equal(t, 409, code, out)
	require.Equal(t, "overlaps_duty", out["reason"])
	require.Len(t, out["allocation_ids"], 1)
	f.filed("filer", overtimeBody(f.n1, d[14], "16:00", "17:00", "Synthetic after duty"))

	call := func(method, path, user string, body any) (int, map[string]any) {
		return f.call(method, path, user, body)
	}
	year := d[22][:4]
	y, err := strconv.Atoi(year)
	require.NoError(t, err)
	types := configureLeave(t, call, y)
	code, out = call("PUT", "/staff/"+f.n2+"/leave-assignment", "admin", map[string]any{"expected_version": 0, "structure_revision_id": types.structure, "active": true, "effective_on": year + "-01-01", "reviewed": true})
	require.Equal(t, 200, code, out)
	leave := uuid.NewString()
	code, out = call("PUT", "/staff/"+f.n2+"/leave-requests/"+leave, "admin", full(types.duty, d[22], d[22]))
	require.Equal(t, 200, code, out)
	f.filed("filer", overtimeBody(f.n2, d[22], "20:00", "21:00", "Synthetic before approval"))
	code, out = call("POST", "/staff/"+f.n2+"/leave-requests/"+leave+"/decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)
	reject("on_leave", overtimeBody(f.n2, d[22], "22:00", "23:00", "Synthetic"))

	code, _ = f.file(uuid.NewString(), "directory", overtimeBody(f.n1, d[2], "17:00", "18:00", "Synthetic"))
	require.Equal(t, 403, code, "filing needs the request permission")
	code, _ = f.call("GET", "/hr/overtime/"+early, "foreign", nil)
	require.Equal(t, 404, code)
	code, _ = f.act(early, "revocation", "foreign", map[string]any{"expected_version": 2, "note": "Synthetic"})
	require.Equal(t, 404, code, "another tenant's request is never found")
	code, _ = f.call("GET", "/hr/overtime/"+uuid.NewString(), "reader", nil)
	require.Equal(t, 404, code)
	code, _ = f.call("GET", "/hr/overtime/not-a-uuid", "reader", nil)
	require.Equal(t, 404, code)
	code, _ = f.call("GET", "/hr/overtime", "directory", nil)
	require.Equal(t, 200, code, "hospital managers read overtime without its reasons")
}

func TestOvertimeWeekFollowsHospitalWeekStart(t *testing.T) {
	f := newOvertimeFixture(t, 3)
	d := f.days
	code, out := f.call("PUT", "/hr/overtime-settings", "admin", overtimePolicyBody(0, 240, 300, 900, 7, "2026-01-01"))
	require.Equal(t, 200, code, out)
	// Weeks run Thursday to Wednesday: days 3 to 9, then 10 to 16.
	f.filed("filer", overtimeBody(f.n1, d[9], "17:00", "21:00", "Synthetic"))
	f.filed("filer", overtimeBody(f.n1, d[10], "17:00", "21:00", "Synthetic"))
	f.filed("filer", overtimeBody(f.n1, d[3], "17:00", "18:00", "Synthetic"))
	code, out = f.file(uuid.NewString(), "filer", overtimeBody(f.n1, d[4], "17:00", "17:15", "Synthetic"))
	requireLimit(t, code, out, "week", 300)
	require.Equal(t, d[3], out["period_from"])
	require.Equal(t, d[9], out["period_to"])
	f.filed("filer", overtimeBody(f.n1, d[2], "17:00", "18:00", "Synthetic previous week"))
}

func TestOvertimeSerialisesOnLimits(t *testing.T) {
	f := newOvertimeFixture(t, 0)
	f.configure()
	ids := []string{uuid.NewString(), uuid.NewString()}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, slot := range [][2]string{{"14:00", "17:00"}, {"18:00", "21:00"}} {
		wg.Go(func() {
			codes[i], _ = f.file(ids[i], "filer", overtimeBody(f.n1, f.days[2], slot[0], slot[1], "Synthetic race"))
		})
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, codes, "two 180-minute filings never share a 240-minute day")
	winner := ids[0]
	if codes[1] == 200 {
		winner = ids[1]
	}
	for i, user := range []string{"decider", "decider2"} {
		wg.Go(func() {
			codes[i], _ = f.act(winner, "decision", user, map[string]any{"expected_version": 1, "decision": "approve"})
		})
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, codes, "one approval decides the request")
	require.EqualValues(t, 2, f.count(t, "workforce_overtime_events", "request_id=?", winner))
}

// overtimeExec runs statements in one transaction with hospital scope and the overtime write flag, as the handlers do.
func (f *overtimeFixture) overtimeExec(flagged bool, sql string, args ...any) error {
	return f.db.WithTenant(f.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		flag := ""
		if flagged {
			flag = "authorized"
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_overtime',?,true)`, f.hospital, flag).Error; err != nil {
			return err
		}
		for statement := range strings.SplitSeq(sql, ";\n") {
			n := strings.Count(statement, "?")
			if err := tx.Exec(statement, args[:n]...).Error; err != nil {
				return err
			}
			args = args[n:]
		}
		return nil
	})
}

func TestOvertimeTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	f := newOvertimeFixture(t, 0)
	f.configure()
	id := f.filed("filer", overtimeBody(f.n1, f.days[2], "17:00", "19:00", "Synthetic"))
	fails := func(contains, sql string, args ...any) {
		t.Helper()
		err := f.overtimeExec(true, sql, args...)
		require.Error(t, err, sql)
		require.Contains(t, err.Error(), contains)
	}
	event := `;
 INSERT INTO workforce_overtime_events(tenant_id,hospital_id,id,request_id,version,status,approved_minutes,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,version,status,approved_minutes,'user-x' FROM workforce_overtime_requests WHERE id=?`
	require.ErrorContains(t, f.overtimeExec(false, `UPDATE workforce_overtime_requests SET status='cancelled',version=2 WHERE id=?`+event, id, id), "row-level security", "writes need the overtime flag")
	fails("one allowed transition", `UPDATE workforce_overtime_requests SET reason='changed',status='cancelled',version=2 WHERE id=?`, id)
	fails("one allowed transition", `UPDATE workforce_overtime_requests SET status='cancelled',version=3 WHERE id=?`, id)
	fails("one allowed transition", `UPDATE workforce_overtime_requests SET status='approved',version=2,approved_minutes=60,decided_by_subject='user-x',decided_at=now(),policy_revision_id=NULL WHERE id=?`, id)
	fails("workforce_overtime_separation", `UPDATE workforce_overtime_requests SET status='rejected',version=2,decided_by_subject=requested_by_subject,decided_at=now() WHERE id=?`, id)
	fails("check constraint", `UPDATE workforce_overtime_requests SET status='approved',version=2,approved_minutes=121,decided_by_subject='user-x',decided_at=now(),policy_revision_id=workforce_overtime_policy_on(tenant_id,hospital_id,work_date) WHERE id=?`, id)
	fails("check constraint", `UPDATE workforce_overtime_requests SET status='rejected',version=2,approved_minutes=60,decided_by_subject='user-x',decided_at=now() WHERE id=?`, id)
	fails("needs its event", `UPDATE workforce_overtime_requests SET status='cancelled',version=2 WHERE id=?`, id)
	fails("must match", `INSERT INTO workforce_overtime_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,1,'approved','user-x' FROM workforce_overtime_requests WHERE id=?`, id)
	fails("append only", `DELETE FROM workforce_overtime_events WHERE request_id=?`, id)
	fails("", `DELETE FROM workforce_overtime_requests WHERE id=?`, id)
	fails("workforce_overtime_overlap", `INSERT INTO workforce_overtime_requests(tenant_id,hospital_id,id,staff_id,work_date,start_time,end_time,settings_revision_id,timezone,starts_at,ends_at,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,work_date,'18:00','20:00',settings_revision_id,timezone,starts_at+interval '1 hour',ends_at+interval '1 hour','x','pending','user-y',1 FROM workforce_overtime_requests WHERE id=?`, id)
	fails("check constraint", `INSERT INTO workforce_overtime_requests(tenant_id,hospital_id,id,staff_id,work_date,start_time,end_time,settings_revision_id,timezone,starts_at,ends_at,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,work_date,'08:00','09:00',settings_revision_id,timezone,starts_at-interval '8 hours',ends_at-interval '9 hours','x','pending','user-y',1 FROM workforce_overtime_requests WHERE id=?`, id)
	fails("filed pending", `INSERT INTO workforce_overtime_requests(tenant_id,hospital_id,id,staff_id,work_date,start_time,end_time,settings_revision_id,timezone,starts_at,ends_at,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,work_date,'08:00','09:00',settings_revision_id,'UTC',(work_date+time '08:00') AT TIME ZONE 'UTC',(work_date+time '09:00') AT TIME ZONE 'UTC','x','pending','user-y',1 FROM workforce_overtime_requests WHERE id=?`, id)
	require.NoError(t, f.overtimeExec(true, `UPDATE workforce_overtime_requests SET status='cancelled',version=2 WHERE id=?`+event, id, id), "the guarded path itself is allowed")
	require.EqualValues(t, 1, f.count(t, "workforce_overtime_requests", "id=? AND status='cancelled' AND version=2", id))
}
