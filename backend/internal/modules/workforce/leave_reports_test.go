package workforce_test

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type reportFixture struct {
	h                                 *requestHarness
	types                             leaveTypes
	alpha, beta, department           string
	pending, rejected, short, revoked string
}

// newReportFixture files approved, pending and rejected leave for an enrolled staff member and one without a profile.
func newReportFixture(t *testing.T) reportFixture {
	h := newRequestHarness(t)
	f := reportFixture{h: h, types: configureLeave(t, h.call, 2026)}
	code, out := h.call("PUT", "/hr/masters/"+uuid.NewString(), "admin", map[string]any{"expected_version": 0, "kind": "department", "code": "SYN-WARD", "name": "Synthetic Ward", "description": "", "active": true})
	require.Equal(t, 200, code, out)
	f.department = out["id"].(string)
	f.alpha = uuid.NewString()
	code, out = h.call("PUT", "/staff/"+f.alpha+"/enrollment", "admin", map[string]any{"expected_version": 0,
		"staff":   map[string]any{"employee_code": "SYN-RA", "display_name": "Synthetic Alpha", "job_category": "nurse", "employment_status": "active"},
		"profile": map[string]any{"department_revision_id": out["revision_id"], "start_date": "2026-01-01"}})
	require.Equal(t, 200, code, out)
	code, out = h.assign(f.alpha, f.types.structure, "2026-01-01", 0)
	require.Equal(t, 200, code, out)
	f.beta = h.enrolled("SYN_RB", f.types, "2026-01-01")
	code, out = h.post("2026", 1)
	require.Equal(t, 200, code, out)

	approve := func(staff string, body map[string]any) string {
		id := uuid.NewString()
		code, out := h.file(staff, id, "admin", body)
		require.Equal(t, 200, code, out)
		code, out = h.act(staff, id, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve", "note": ""})
		require.Equal(t, 200, code, out)
		return id
	}
	approve(f.alpha, full(f.types.earned, "2026-09-29", "2026-10-03"))
	approve(f.alpha, half(f.types.earned, "2026-10-06", "first_half"))
	approve(f.alpha, half(f.types.earned, "2026-10-06", "second_half"))
	f.short = approve(f.alpha, short(f.types.earned, "2026-10-07", 60))
	f.pending = uuid.NewString()
	code, out = h.file(f.alpha, f.pending, "admin", full(f.types.duty, "2026-10-14", "2026-10-14"))
	require.Equal(t, 200, code, out)
	approve(f.beta, full(f.types.duty, "2026-10-31", "2026-11-01"))
	f.rejected = uuid.NewString()
	code, out = h.file(f.beta, f.rejected, "admin", full(f.types.duty, "2026-10-12", "2026-10-12"))
	require.Equal(t, 200, code, out)
	code, out = h.act(f.beta, f.rejected, "decision", "approver", map[string]any{"expected_version": 1, "decision": "reject", "note": "Synthetic cover"})
	require.Equal(t, 200, code, out)
	f.revoked = approve(f.beta, full(f.types.duty, "2026-10-21", "2026-10-21"))
	code, out = h.act(f.beta, f.revoked, "revocation", "approver", map[string]any{"expected_version": 2, "note": "Synthetic cover restored"})
	require.Equal(t, 200, code, out)
	return f
}

func reportRows(t *testing.T, h *requestHarness, path, user string) []map[string]any {
	t.Helper()
	code, out := h.call("GET", path, user, nil)
	require.Equal(t, 200, code, out)
	rows := []map[string]any{}
	for _, r := range out["data"].([]any) {
		rows = append(rows, r.(map[string]any))
	}
	return rows
}

func byStaff(rows []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, r := range rows {
		out[r["staff_id"].(string)] = r
	}
	return out
}

func month(t *testing.T, r map[string]any, i int) (string, []any, map[string][2]float64) {
	t.Helper()
	m := r["months"].([]any)[i].(map[string]any)
	types := map[string][2]float64{}
	for _, x := range m["types"].([]any) {
		e := x.(map[string]any)
		types[e["code"].(string)] = [2]float64{e["half_days"].(float64), e["minutes"].(float64)}
	}
	return m["month"].(string), m["leave_dates"].([]any), types
}

func TestLeaveMonthlyReportSplitsPeriodsAndCountsApprovedLeave(t *testing.T) {
	f := newReportFixture(t)
	h := f.h
	audits := func() int64 { return h.count("workforce_audit", "action='workforce.enrollment.report'") }
	rows := byStaff(reportRows(t, h, "/hr/leave-reports/monthly?from=2026-09-30&to=2026-10-31", "admin"))
	require.Len(t, rows, 2)
	alpha := rows[f.alpha]
	require.Equal(t, "Synthetic Ward", alpha["department"])
	require.Equal(t, "2026-01-01", alpha["joined_on"])
	require.Len(t, alpha["months"], 2)
	name, dates, types := month(t, alpha, 0)
	require.Equal(t, "2026-09", name)
	require.Equal(t, []any{"2026-09-30"}, dates, "29 September is outside the period")
	require.Equal(t, map[string][2]float64{"SYN-EARNED": {2, 0}}, types)
	name, dates, types = month(t, alpha, 1)
	require.Equal(t, "2026-10", name)
	require.Equal(t, []any{"2026-10-01", "2026-10-03", "2026-10-06", "2026-10-07"}, dates, "holidays and pending leave are not leave dates, and a date with two halves appears once")
	require.Equal(t, map[string][2]float64{"SYN-EARNED": {6, 60}}, types, "short leave adds minutes, not half-days")
	beta := rows[f.beta]
	require.Equal(t, "", beta["department"], "a holder sees an empty department for staff without a profile")
	require.NotContains(t, beta, "joined_on")
	require.Len(t, beta["months"], 1, "1 November is outside the period")
	_, dates, types = month(t, beta, 0)
	require.Equal(t, []any{"2026-10-31"}, dates)
	require.Equal(t, map[string][2]float64{"SYN-DUTY": {2, 0}}, types, "rejected leave is not counted")
	require.EqualValues(t, 1, audits(), "disclosed profiles are audited")

	// Over a period covering every request, report half-days equal the consumption ledger.
	whole := byStaff(reportRows(t, h, "/hr/leave-reports/monthly?from=2026-09-01&to=2026-10-31", "admin"))[f.alpha]
	total := 0.0
	for i := range whole["months"].([]any) {
		_, _, types := month(t, whole, i)
		total += types["SYN-EARNED"][0]
	}
	var consumed int64
	require.NoError(t, h.scoped(false, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT -sum(half_days) FROM workforce_leave_ledger WHERE staff_id=? AND kind='consumption'`, f.alpha).Scan(&consumed).Error
	}))
	require.EqualValues(t, consumed, total)
	require.EqualValues(t, 10, total)

	for _, user := range []string{"viewer", "profiler"} {
		before := audits()
		for _, r := range reportRows(t, h, "/hr/leave-reports/monthly?from=2026-09-30&to=2026-10-31", user) {
			require.NotContains(t, r, "department", user)
			require.NotContains(t, r, "joined_on", user)
		}
		require.Equal(t, before, audits(), "nothing disclosed, nothing audited")
	}
	require.Empty(t, reportRows(t, h, "/hr/leave-reports/monthly?from=2026-03-01&to=2026-03-31", "admin"))
	require.Empty(t, reportRows(t, h, "/hr/leave-reports/monthly?from=2026-10-02&to=2026-10-02", "admin"), "a holiday inside approved leave is not leave")
	require.Empty(t, reportRows(t, h, "/hr/leave-reports/monthly?from=2026-10-12&to=2026-10-12", "admin"), "rejected leave alone lists nobody")
	require.Equal(t, "no-store", h.header("/hr/leave-reports/monthly?from=2026-10-01&to=2026-10-31", "viewer", "Cache-Control"))
}

func TestLeaveStatusReportFiltersEveryStatus(t *testing.T) {
	f := newReportFixture(t)
	h := f.h
	base := "/hr/leave-reports/status?from=2026-10-01&to=2026-10-31"
	rows := reportRows(t, h, base, "admin")
	require.Len(t, rows, 8, "the request crossing into September overlaps the period")
	statuses := map[string]int{}
	for i, r := range rows {
		statuses[r["status"].(string)]++
		require.NotContains(t, r, "reason")
		if i > 0 {
			require.GreaterOrEqual(t, rows[i-1]["created_at"], r["created_at"], "newest filing first")
		}
		switch r["id"] {
		case f.pending, f.revoked:
			require.NotContains(t, r, "decided_by_subject", "only decided requests show their decider")
		case f.rejected:
			require.Equal(t, "user-approver", r["decided_by_subject"])
			require.EqualValues(t, 2, r["half_days"])
		case f.short:
			require.EqualValues(t, 60, r["minutes"])
			require.EqualValues(t, 0, r["half_days"])
		}
	}
	require.Equal(t, map[string]int{"approved": 5, "pending": 1, "rejected": 1, "cancelled": 1}, statuses)
	require.Equal(t, "Synthetic Ward", byStaff(rows)[f.alpha]["department"])

	for _, tc := range []struct {
		query string
		want  int
	}{
		{"&status=pending", 1},
		{"&leave_type_id=" + f.types.duty, 4},
		{"&leave_type_id=" + f.types.duty + "&status=approved", 1},
		{"&q=_", 3},
		{"&q=%25", 0},
		{"&q=syn-ra", 5},
		{"&q=alpha&leave_type_id=" + f.types.duty, 1},
		{"&staff_id=" + f.beta, 3},
		{"&department_id=" + f.department, 5},
		{"&department_id=" + f.department + "&status=pending", 1},
		{"&department_id=" + uuid.NewString(), 0},
	} {
		require.Len(t, reportRows(t, h, base+tc.query, "admin"), tc.want, tc.query)
	}
	require.Len(t, reportRows(t, h, "/hr/leave-reports/status?from=2026-11-01&to=2026-11-01", "admin"), 1, "only the request reaching into November")
	require.Empty(t, reportRows(t, h, "/hr/leave-reports/status?from=2026-03-01&to=2026-03-31", "admin"))
	for _, r := range reportRows(t, h, base, "viewer") {
		require.NotContains(t, r, "department")
		require.NotContains(t, r, "joined_on")
	}
}

func TestLeaveReportsPaginateWithoutGapsOrRepeats(t *testing.T) {
	f := newReportFixture(t)
	for _, tc := range []struct {
		path, key string
		limit     int
		want      int
	}{{"/hr/leave-reports/monthly?from=2026-09-01&to=2026-11-30", "staff_id", 1, 2}, {"/hr/leave-reports/status?from=2026-09-01&to=2026-11-30", "id", 2, 8}} {
		seen := map[string]bool{}
		cursor := ""
		for pages := 0; ; pages++ {
			require.Less(t, pages, tc.want, "pagination terminates")
			code, out := f.h.call("GET", tc.path+"&limit="+strconv.Itoa(tc.limit)+cursor, "admin", nil)
			require.Equal(t, 200, code, out)
			data := out["data"].([]any)
			require.LessOrEqual(t, len(data), tc.limit)
			for _, r := range data {
				id := r.(map[string]any)[tc.key].(string)
				require.False(t, seen[id], "no repeats across pages")
				seen[id] = true
			}
			page := out["page"].(map[string]any)
			if page["has_more"] != true {
				break
			}
			cursor = "&cursor=" + url.QueryEscape(page["next_cursor"].(string))
		}
		require.Len(t, seen, tc.want, tc.path)
	}
}

func TestLeaveReportsValidateAndScopeAccess(t *testing.T) {
	f := newReportFixture(t)
	h := f.h
	for _, report := range []string{"monthly", "status"} {
		path := "/hr/leave-reports/" + report
		for _, query := range []string{"", "?from=2026-10-01", "?to=2026-10-01", "?from=2026-10-02&to=2026-10-01", "?from=2026-01-01&to=2027-01-02",
			"?from=01-10-2026&to=31-10-2026", "?from=1999-12-31&to=2000-01-01", "?from=2026-10-01&to=2026-10-31&limit=101",
			"?from=2026-10-01&to=2026-10-31&q=", "?from=2026-10-01&to=2026-10-31&q=" + strings.Repeat("a", 101),
			"?from=2026-10-01&to=2026-10-31&staff_id=nope", "?from=2026-10-01&to=2026-10-31&department_id=" + uuid.Nil.String()} {
			code, out := h.call("GET", path+query, "admin", nil)
			require.Equal(t, 400, code, "%s%s: %v", path, query, out)
		}
		code, out := h.call("GET", path+"?from=2026-01-01&to=2027-01-01", "admin", nil)
		require.Equal(t, 200, code, "366 days is the widest period: %v", out)
		inPeriod := path + "?from=2026-10-01&to=2026-10-31"
		for _, user := range []string{"viewer", "profiler"} {
			code, out = h.call("GET", inPeriod+"&department_id="+f.department, user, nil)
			require.Equal(t, 403, code, "%s: department filters need enrollment access: %v", user, out)
		}
		code, _ = h.call("GET", inPeriod, "foreign", nil)
		require.Equal(t, 404, code, "other tenants see nothing")
		code, _ = h.do("GET", h.base+inPeriod, "", "")
		require.Equal(t, 401, code)
		code, _ = h.do("GET", "/v1/workforce/hospitals/"+uuid.NewString()+inPeriod, "admin", "")
		require.Equal(t, 404, code, "a hospital without a grant is not found")
	}
	for _, query := range []string{"&status=draft", "&leave_type_id=nope"} {
		code, out := h.call("GET", "/hr/leave-reports/status?from=2026-10-01&to=2026-10-31"+query, "admin", nil)
		require.Equal(t, 400, code, "%s: %v", query, out)
	}
}
