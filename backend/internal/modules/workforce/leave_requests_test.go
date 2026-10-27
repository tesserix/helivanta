package workforce_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type caller func(method, path, user string, body any) (int, map[string]any)

type noReviewedLeaveAuthority struct{}

func (noReviewedLeaveAuthority) AuthorizeScope(context.Context, authz.ScopeRequest) (authz.ScopeGrant, error) {
	return authz.ScopeGrant{}, nil
}

// leaveTypes are the configured type ids plus the structure revision that offers them.
type leaveTypes struct {
	earned, duty, locked, structure string
}

type requestHarness struct {
	*ledgerHarness
	header func(path, user, name string) string
}

func newRequestHarness(t *testing.T, configure ...func(*testutil.HarnessOptions)) *requestHarness {
	opts := testutil.HarnessOptions{
		ScopedAuthorizer: func(*tenantdb.DB) authz.ScopedAuthorizer { return noReviewedLeaveAuthority{} },
		Tokens:           map[string]string{"admin": testutil.TenantA, "approver": testutil.TenantA, "clerk": testutil.TenantA, "viewer": testutil.TenantA, "reader": testutil.TenantA, "profiler": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin":    {workforce.PermHospitalManage, workforce.PermLeaveConfigure, workforce.PermLeaveAllocate, workforce.PermLeaveCorrect, workforce.PermLeaveRequest, workforce.PermEnrollmentManage, workforce.PermMasterManage},
			"approver": {workforce.PermLeaveApprove, workforce.PermLeaveRequest, workforce.PermEnrollmentManage},
			"clerk":    {workforce.PermLeaveRequest},
			"viewer":   {},
			"reader":   {workforce.PermLeaveRequest, workforce.PermLeaveApprove},
			"profiler": {workforce.PermEnrollmentManage},
			"foreign":  {workforce.PermLeaveRequest, workforce.PermLeaveApprove, workforce.PermEnrollmentManage},
		},
		Modules: []platform.Module{workforce.New()},
	}
	for _, change := range configure {
		change(&opts)
	}
	r, db, _, _ := testutil.NewHarness(t, opts)
	hospital := uuid.NewString()
	h := &leaveHarness{t: t, base: "/v1/workforce/hospitals/" + hospital, do: func(method, path, user, body string) (int, string) {
		w := testutil.Do(r, method, path, user, body)
		return w.Code, w.Body.String()
	}}
	code, out := h.do("PUT", h.base, "admin", `{"name":"Synthetic Request Hospital"}`)
	require.Equal(t, 200, code, out)
	for user, level := range map[string]string{"admin": "manager", "approver": "manager", "clerk": "manager", "viewer": "manager", "reader": "reader", "profiler": "reader"} {
		code, out = h.do("PUT", h.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`)
		require.Equal(t, 200, code, out)
	}
	header := func(path, user, name string) string {
		return testutil.Do(r, "GET", h.base+path, user, "").Header().Get(name)
	}
	return &requestHarness{ledgerHarness: &ledgerHarness{leaveHarness: h, db: db, hospital: hospital}, header: header}
}

// openLeaveYears opens each calendar-aligned year, optionally with a holiday calendar.
func openLeaveYears(t *testing.T, call caller, calendars bool, years ...int) {
	t.Helper()
	code, out := call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 1, "reviewed": true})
	require.Equal(t, 200, code, out)
	holiday := func(date, category string) map[string]any {
		return map[string]any{"holiday_date": date, "name": "Synthetic " + category + " " + date, "category": category}
	}
	for _, year := range years {
		y := strconv.Itoa(year)
		code, out = call("PUT", "/hr/leave-years/"+y, "admin", nil)
		require.Equal(t, 200, code, out)
		if !calendars {
			continue
		}
		code, out = call("PUT", "/hr/leave-configs/"+uuid.NewString(), "admin", map[string]any{"expected_version": 0, "kind": "holiday_calendar", "code": "SYN-CAL-" + y, "name": "Synthetic " + y, "description": "", "active": true, "effective_on": y + "-01-01", "reviewed": true, "calendar_year": year,
			"holidays": []any{holiday(y+"-10-02", "government"), holiday(y+"-10-20", "optional"), holiday(y+"-11-09", "regular")}, "entries": []any{}})
		require.Equal(t, 200, code, out)
	}
}

// leaveStructure offers a tracked non-clubbable type, an untracked full-day type and a type closed during probation.
func leaveStructure(t *testing.T, call caller, effective string) leaveTypes {
	t.Helper()
	kind := func(code string, tracked, half bool, short int, clubbable bool) (string, any) {
		id := uuid.NewString()
		status, out := call("PUT", "/hr/leave-configs/"+id, "admin", with(leaveType(code, "Synthetic "+code, tracked), map[string]any{"effective_on": effective,
			"rules": map[string]any{"paid": true, "balance_tracked": tracked, "half_day_allowed": half, "short_leave_minutes": short, "clubbable": clubbable}}))
		require.Equal(t, 200, status, out)
		return id, out["revision_id"]
	}
	var types leaveTypes
	var earned, duty, locked any
	types.earned, earned = kind("SYN-EARNED", true, true, 120, false)
	types.duty, duty = kind("SYN-DUTY", false, false, 0, true)
	types.locked, locked = kind("SYN-LOCKED", false, true, 0, true)
	entry := func(revision any, half int, probation bool) map[string]any {
		return map[string]any{"leave_type_revision_id": revision, "annual_half_days": half, "accrual": "upfront", "carry_forward_half_days": 0, "proration": "join_month", "probation_eligible": probation}
	}
	code, out := call("PUT", "/hr/leave-configs/"+uuid.NewString(), "admin", map[string]any{"expected_version": 0, "kind": "structure", "code": "SYN-REQUESTS", "name": "Synthetic Requests", "description": "", "active": true, "effective_on": effective, "reviewed": true,
		"holidays": []any{}, "entries": []any{entry(earned, 12, true), entry(duty, 0, true), entry(locked, 0, false)}})
	require.Equal(t, 200, code, out)
	types.structure = out["revision_id"].(string)
	return types
}

func configureLeave(t *testing.T, call caller, years ...int) leaveTypes {
	openLeaveYears(t, call, true, years...)
	return leaveStructure(t, call, strconv.Itoa(years[0])+"-01-01")
}

func full(leaveType, start, end string) map[string]any {
	return map[string]any{"leave_type_id": leaveType, "unit": "full_day", "start_on": start, "end_on": end, "reason": ""}
}

func half(leaveType, date, session string) map[string]any {
	return map[string]any{"leave_type_id": leaveType, "unit": "half_day", "start_on": date, "end_on": date, "session": session, "reason": ""}
}

func short(leaveType, date string, minutes int) map[string]any {
	return map[string]any{"leave_type_id": leaveType, "unit": "short_leave", "start_on": date, "end_on": date, "minutes": minutes, "reason": ""}
}

func request(out map[string]any) map[string]any {
	return out["request"].(map[string]any)
}

func (h *ledgerHarness) file(staff, id, user string, body map[string]any) (int, map[string]any) {
	return h.call("PUT", "/staff/"+staff+"/leave-requests/"+id, user, body)
}

func (h *ledgerHarness) act(staff, id, action, user string, body map[string]any) (int, map[string]any) {
	return h.call("POST", "/staff/"+staff+"/leave-requests/"+id+"/"+action, user, body)
}

func (h *ledgerHarness) enrolled(code string, types leaveTypes, effective string) string {
	id := h.staff(code)
	status, out := h.assign(id, types.structure, effective, 0)
	require.Equal(h.t, 200, status, out)
	return id
}

// scoped runs a statement with hospital scope and, when flagged, the transaction-local ledger write flag.
func (h *ledgerHarness) scoped(flagged bool, run func(tx *gorm.DB) error) error {
	return h.db.WithTenant(h.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		flag := ""
		if flagged {
			flag = "authorized"
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_ledger',?,true)`, h.hospital, flag).Error; err != nil {
			return err
		}
		return run(tx)
	})
}

func (h *ledgerHarness) count(table, where string, args ...any) int64 {
	var n int64
	require.NoError(h.t, h.scoped(false, func(tx *gorm.DB) error { return tx.Table(table).Where(where, args...).Count(&n).Error }))
	return n
}

func TestLinkedStaffCannotDecideOwnLeaveFiledBySomeoneElse(t *testing.T) {
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), testutil.TenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), testutil.TenantA, "user-approver", authz.RoleStaffMember))
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Roles = fga
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermLoginLinkManage)
		opts.Perms["profiler"] = append(opts.Perms["profiler"], workforce.PermLoginLinkReview)
		opts.Perms["viewer"] = append(opts.Perms["viewer"], workforce.PermLeaveApprove)
	})
	code, out := h.call("PUT", "/access/user-profiler", "admin", map[string]any{"level": "manager"})
	require.Equal(t, 200, code, out)
	types := configureLeave(t, h.call, 2026)
	staff := h.enrolled("SYN-LINKED-SELF", types, "2026-01-01")
	link := "/staff/" + staff + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	code, out = h.call("PUT", link, "admin", map[string]any{"subject": "user-approver", "reason": "Synthetic verified identity"})
	require.Equal(t, 200, code, out)
	code, out = h.call("POST", link+"/decision", "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "Independent synthetic verification"})
	require.Equal(t, 200, code, out)
	for i, decision := range []string{"approve", "reject"} {
		t.Run(decision, func(t *testing.T) {
			id := uuid.NewString()
			date := fmt.Sprintf("2026-10-%02d", 5+i)
			status, result := h.file(staff, id, "clerk", full(types.duty, date, date))
			require.Equal(t, 200, status, result)
			command := map[string]any{"expected_version": 1, "decision": decision, "note": "Synthetic review"}
			status, result = h.act(staff, id, "decision", "approver", command)
			require.Equal(t, 403, status, result)
			status, result = h.call("GET", "/staff/"+staff+"/leave-requests/"+id, "clerk", nil)
			require.Equal(t, 200, status, result)
			require.Equal(t, "pending", request(result)["status"])
			require.EqualValues(t, 1, request(result)["version"])
			status, result = h.act(staff, id, "decision", "viewer", command)
			require.Equal(t, 200, status, result)
			if decision == "approve" {
				revocation := map[string]any{"expected_version": 2, "note": "Synthetic independent correction"}
				status, result = h.act(staff, id, "revocation", "approver", revocation)
				require.Equal(t, 403, status, result)
				status, result = h.act(staff, id, "revocation", "viewer", revocation)
				require.Equal(t, 200, status, result)
			}
		})
	}
}

func TestLeaveRequestsFileDecideAndReverseExactlyOnce(t *testing.T) {
	h := newRequestHarness(t)
	types := configureLeave(t, h.call, 2026)
	staff := h.enrolled("SYN-R1", types, "2026-01-01")
	code, out := h.post("2026", 1)
	require.Equal(t, 200, code, out)
	require.Equal(t, 12, h.balances(staff, "leave_year=2026")[types.earned])

	reason := "Synthetic family circumstance"
	body := with(full(types.earned, "2026-10-01", "2026-10-03"), map[string]any{"reason": reason})
	id := uuid.NewString()
	code, out = h.file(staff, id, "admin", body)
	require.Equal(t, 200, code, out)
	filed := request(out)
	require.Equal(t, "pending", filed["status"])
	require.EqualValues(t, 1, filed["version"])
	require.EqualValues(t, 4, filed["half_days"], "the government holiday on 2 October counts nothing")
	require.Equal(t, "SYN-EARNED", filed["code"])
	require.Equal(t, reason, filed["reason"])
	days := out["days"].([]any)
	require.Len(t, days, 3)
	require.Equal(t, true, days[1].(map[string]any)["holiday"])
	require.EqualValues(t, 0, days[1].(map[string]any)["half_days"])
	require.EqualValues(t, 2, days[2].(map[string]any)["half_days"], "weekly offs count")
	code, again := h.file(staff, id, "admin", body)
	require.Equal(t, 200, code, "an identical retry returns the stored request")
	require.Equal(t, out, again)
	code, _ = h.file(staff, id, "admin", with(body, map[string]any{"reason": "Synthetic other"}))
	require.Equal(t, 409, code, "an id is never reused for different content")
	code, _ = h.file(h.staff("SYN-R1B"), id, "admin", body)
	require.Equal(t, 409, code, "an id belongs to one staff member")
	require.Equal(t, 12, h.balances(staff, "leave_year=2026")[types.earned], "filing writes nothing to the ledger")

	code, out = h.call("GET", "/staff/"+staff+"/leave-requests/"+id, "viewer", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, request(out)["reason"], "reasons stay with request and approve holders")
	code, out = h.call("GET", "/staff/"+staff+"/leave-requests/"+id, "approver", nil)
	require.Equal(t, 200, code, out)
	require.Equal(t, reason, request(out)["reason"])
	require.Equal(t, "no-store", h.header("/staff/"+staff+"/leave-requests/"+id, "approver", "Cache-Control"))

	code, out = h.act(staff, id, "decision", "approver", map[string]any{"expected_version": 2, "decision": "approve"})
	require.Equal(t, 409, code, out)
	approve := map[string]any{"expected_version": 1, "decision": "approve", "note": ""}
	code, out = h.act(staff, id, "decision", "approver", approve)
	require.Equal(t, 200, code, out)
	require.Equal(t, "approved", request(out)["status"])
	require.EqualValues(t, 2, request(out)["version"])
	require.Equal(t, "user-approver", request(out)["decided_by_subject"])
	require.Equal(t, 8, h.balances(staff, "leave_year=2026")[types.earned])
	code, again = h.act(staff, id, "decision", "approver", approve)
	require.Equal(t, 200, code, "a retried approval returns the decided request")
	require.Equal(t, out, again)
	require.EqualValues(t, 1, h.count("workforce_leave_ledger", "request_id=? AND kind='consumption'", id))
	code, _ = h.act(staff, id, "decision", "approver", with(approve, map[string]any{"note": "Synthetic different"}))
	require.Equal(t, 409, code, "a retry must match the recorded decision")

	code, out = h.act(staff, id, "revocation", "approver", map[string]any{"expected_version": 2})
	require.Equal(t, 400, code, "revoking needs a note: %v", out)
	code, _ = h.act(staff, id, "revocation", "admin", map[string]any{"expected_version": 2, "note": "Synthetic"})
	require.Equal(t, 403, code, "revocation needs the approve permission")
	code, _ = h.act(staff, id, "cancellation", "admin", map[string]any{"expected_version": 2})
	require.Equal(t, 409, code, "approved leave is revoked, not cancelled")
	revoke := map[string]any{"expected_version": 2, "note": "Synthetic shift cover restored"}
	code, out = h.act(staff, id, "revocation", "approver", revoke)
	require.Equal(t, 200, code, out)
	require.Equal(t, "cancelled", request(out)["status"])
	require.Equal(t, "user-approver", request(out)["decided_by_subject"], "revocation keeps the original decider")
	require.Equal(t, 12, h.balances(staff, "leave_year=2026")[types.earned], "the reversal restores the balance")
	code, _ = h.act(staff, id, "revocation", "approver", revoke)
	require.Equal(t, 200, code, "a retried revocation writes nothing")
	require.EqualValues(t, 1, h.count("workforce_leave_ledger", "request_id=? AND kind='reversal'", id))

	code, out = h.call("GET", "/staff/"+staff+"/leave-requests/"+id+"/history", "approver", nil)
	require.Equal(t, 200, code, out)
	events := out["data"].([]any)
	require.Len(t, events, 3)
	require.Equal(t, "Synthetic shift cover restored", events[0].(map[string]any)["note"])
	code, out = h.call("GET", "/staff/"+staff+"/leave-requests/"+id+"/history", "viewer", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"].([]any)[0].(map[string]any)["note"], "notes follow reason redaction")

	rejected := uuid.NewString()
	code, out = h.file(staff, rejected, "admin", half(types.earned, "2026-10-12", "first_half"))
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, request(out)["half_days"])
	code, _ = h.act(staff, rejected, "decision", "approver", map[string]any{"expected_version": 1, "decision": "reject"})
	require.Equal(t, 400, code, "rejections need a note")
	code, out = h.act(staff, rejected, "decision", "approver", map[string]any{"expected_version": 1, "decision": "reject", "note": "Synthetic audit week"})
	require.Equal(t, 200, code, out)
	require.Equal(t, "rejected", request(out)["status"])
	code, _ = h.act(staff, rejected, "cancellation", "admin", map[string]any{"expected_version": 2})
	require.Equal(t, 409, code, "rejection is final")

	cancelled := uuid.NewString()
	code, out = h.file(staff, cancelled, "admin", half(types.earned, "2026-10-12", "first_half"))
	require.Equal(t, 200, code, "a rejected request frees its slot: %v", out)
	code, out = h.act(staff, cancelled, "cancellation", "admin", map[string]any{"expected_version": 1})
	require.Equal(t, 200, code, "the filer may withdraw pending leave: %v", out)
	require.Equal(t, "cancelled", request(out)["status"])
	require.Nil(t, request(out)["decided_by_subject"], "withdrawal is not a decision")
	require.Zero(t, h.count("workforce_leave_ledger", "request_id IN ?", []string{rejected, cancelled}), "undecided leave never touches the ledger")

	own := uuid.NewString()
	code, out = h.file(staff, own, "approver", full(types.duty, "2026-10-14", "2026-10-14"))
	require.Equal(t, 200, code, out)
	code, out = h.act(staff, own, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 403, code, out)
	require.Contains(t, out["message"], "Someone other than")
	require.Zero(t, h.count("workforce_leave_ledger", "request_id=?", own))

	code, out = h.call("GET", "/hr/leave-requests?status=cancelled&staff_id="+staff+"&leave_year=2026", "viewer", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 2)
	for _, item := range out["data"].([]any) {
		require.Empty(t, item.(map[string]any)["reason"])
	}
	for _, query := range []string{"status=open", "staff_id=nope", "leave_year=1999", "limit=101"} {
		code, _ = h.call("GET", "/hr/leave-requests?"+query, "viewer", nil)
		require.Equal(t, 400, code, query)
	}

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var payloads []string
		require.NoError(t, tx.Raw("SELECT payload::text FROM outbox_events WHERE subject = ?", workforce.LeaveRequestChanged).Scan(&payloads).Error)
		require.Len(t, payloads, 8, "filed, approved, revoked; filed, rejected; filed, cancelled; filed")
		for _, p := range payloads {
			require.NotContains(t, p, "Synthetic")
		}
		return nil
	}))
	var changes []string
	require.NoError(t, h.scoped(false, func(tx *gorm.DB) error {
		return tx.Raw("SELECT changes::text FROM workforce_audit WHERE action LIKE 'workforce.leave_request.%'").Scan(&changes).Error
	}))
	require.Len(t, changes, 8)
	for _, c := range changes {
		require.NotContains(t, c, "Synthetic")
	}
}

func TestLeaveRequestRulesFailClosed(t *testing.T) {
	h := newRequestHarness(t)
	openLeaveYears(t, h.call, false, 2026)
	types := leaveStructure(t, h.call, "2026-01-01")
	staff := h.enrolled("SYN-F1", types, "2026-01-01")
	inactive := h.enrolled("SYN-F2", types, "2026-01-01")
	probation := func(code, end string) string {
		id := uuid.NewString()
		status, out := h.call("PUT", "/staff/"+id+"/enrollment", "admin", map[string]any{"expected_version": 0,
			"staff":       map[string]any{"employee_code": code, "display_name": "Synthetic " + code, "job_category": "nurse", "employment_status": "active"},
			"profile":     map[string]any{"personal_email": strings.ToLower(code) + "@example.invalid", "engagement_type": "employee", "start_date": "2026-01-01", "probation_end_date": end},
			"credentials": []any{}, "qualifications": []any{}, "experiences": []any{}})
		require.Equal(t, 200, status, out)
		status, out = h.assign(id, types.structure, "2026-01-01", 0)
		require.Equal(t, 200, status, out)
		return id
	}
	serving, cleared := probation("SYN-PS", "2026-12-01"), probation("SYN-PC", "2026-11-30")
	code, out := h.post("2026", 1)
	require.Equal(t, 200, code, out)

	code, out = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-10-05", "2026-10-05"))
	require.Equal(t, 409, code, out)
	require.Contains(t, out["message"], "holiday calendar", "no calendar, no count")
	code, out = h.call("PUT", "/hr/leave-configs/"+uuid.NewString(), "admin", map[string]any{"expected_version": 0, "kind": "holiday_calendar", "code": "SYN-CAL-2026", "name": "Synthetic 2026", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true, "calendar_year": 2026,
		"holidays": []any{map[string]any{"holiday_date": "2026-10-02", "name": "Synthetic Government", "category": "government"}, map[string]any{"holiday_date": "2026-10-20", "name": "Synthetic Optional", "category": "optional"}}, "entries": []any{}})
	require.Equal(t, 200, code, out)

	for name, tc := range map[string]struct {
		body map[string]any
		code int
	}{
		"end before start":        {full(types.earned, "2026-10-06", "2026-10-05"), 400},
		"half day over two dates": {with(half(types.earned, "2026-10-06", "first_half"), map[string]any{"end_on": "2026-10-07"}), 400},
		"half day sessionless":    {with(half(types.earned, "2026-10-06", "first_half"), map[string]any{"session": nil}), 400},
		"full day with session":   {with(full(types.earned, "2026-10-06", "2026-10-06"), map[string]any{"session": "first_half"}), 400},
		"minutes off the grid":    {short(types.earned, "2026-10-06", 130), 400},
		"minutes over the limit":  {short(types.earned, "2026-10-06", 135), 400},
		"half day not allowed":    {half(types.duty, "2026-10-06", "first_half"), 400},
		"short leave not allowed": {short(types.duty, "2026-10-06", 60), 400},
		"unknown type":            {full(uuid.NewString(), "2026-10-06", "2026-10-06"), 400},
		"holiday only":            {full(types.earned, "2026-10-02", "2026-10-02"), 400},
		"crosses the year end":    {full(types.duty, "2026-12-31", "2027-01-01"), 400},
		"year not open":           {full(types.duty, "2025-10-06", "2025-10-06"), 409},
		"long reason":             {with(full(types.duty, "2026-10-06", "2026-10-06"), map[string]any{"reason": strings.Repeat("x", 501)}), 400},
	} {
		code, out = h.file(staff, uuid.NewString(), "admin", tc.body)
		require.Equal(t, tc.code, code, "%s: %v", name, out)
	}
	code, out = h.file(staff, uuid.NewString(), "admin", short(types.earned, "2026-10-06", 120))
	require.Equal(t, 200, code, "short leave within the limit: %v", out)
	require.EqualValues(t, 0, request(out)["half_days"])
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-10-20", "2026-10-20"))
	require.Equal(t, 200, code, "optional holidays are working days: %v", out)
	require.EqualValues(t, 2, request(out)["half_days"])

	code, _ = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-10-06", "2026-10-07"))
	require.Equal(t, 409, code, "short leave occupies its whole date")
	code, out = h.file(staff, uuid.NewString(), "admin", half(types.earned, "2026-10-08", "first_half"))
	require.Equal(t, 200, code, out)
	code, out = h.file(staff, uuid.NewString(), "admin", half(types.earned, "2026-10-08", "second_half"))
	require.Equal(t, 200, code, "two halves of one date coexist: %v", out)
	code, _ = h.file(staff, uuid.NewString(), "admin", half(types.earned, "2026-10-08", "second_half"))
	require.Equal(t, 409, code, "a half is taken once")
	code, _ = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-10-08", "2026-10-08"))
	require.Equal(t, 409, code, "a full day needs both halves free")

	code, out = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-10-12", "2026-10-12"))
	require.Equal(t, 200, code, out)
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-10-13", "2026-10-13"))
	require.Equal(t, 409, code, "a clubbable type cannot follow a non-clubbable one: %v", out)
	require.Contains(t, out["message"], "non-clubbable")
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-10-15", "2026-10-15"))
	require.Equal(t, 200, code, out)
	code, _ = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-10-14", "2026-10-14"))
	require.Equal(t, 409, code, "a non-clubbable type cannot precede another type")
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-10-11", "2026-10-11"))
	require.Equal(t, 200, code, "a type may sit alongside itself: %v", out)
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-10-16", "2026-10-16"))
	require.Equal(t, 200, code, "clubbable types join each other: %v", out)

	// Pending earned holds: two halves on 8 October and full days on 11 and 12 October, six of twelve.
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-11-02", "2026-11-05"))
	require.Equal(t, 409, code, "eight half-days exceed the six left after holds: %v", out)
	require.Contains(t, out["message"], "balance")
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.earned, "2026-11-02", "2026-11-04"))
	require.Equal(t, 200, code, "six fit exactly: %v", out)
	code, out = h.file(staff, uuid.NewString(), "admin", half(types.earned, "2026-11-25", "first_half"))
	require.Equal(t, 409, code, "nothing is left: %v", out)
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-11-24", "2026-11-26"))
	require.Equal(t, 200, code, "untracked types need no balance: %v", out)

	code, out = h.file(staff, uuid.NewString(), "clerk", full(types.locked, "2026-12-01", "2026-12-01"))
	require.Equal(t, 403, code, "the probation rule needs enrollment access: %v", out)
	code, out = h.file(staff, uuid.NewString(), "admin", full(types.locked, "2026-12-01", "2026-12-01"))
	require.Equal(t, 409, code, "no probation end date fails closed: %v", out)
	code, out = h.file(serving, uuid.NewString(), "admin", full(types.locked, "2026-12-01", "2026-12-01"))
	require.Equal(t, 409, code, "probation must end before the start date: %v", out)
	require.Contains(t, out["message"], "probation")
	locked := uuid.NewString()
	code, out = h.file(cleared, locked, "admin", full(types.locked, "2026-12-01", "2026-12-01"))
	require.Equal(t, 200, code, out)
	code, out = h.act(cleared, locked, "decision", "reader", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 404, code, "reader access cannot decide: %v", out)

	pending := uuid.NewString()
	code, out = h.file(inactive, pending, "admin", full(types.duty, "2026-10-06", "2026-10-06"))
	require.Equal(t, 200, code, out)
	code, out = h.call("PUT", "/staff/"+inactive, "admin", map[string]any{"employee_code": "SYN-F2", "display_name": "Synthetic SYN-F2", "job_category": "nurse", "employment_status": "inactive"})
	require.Equal(t, 200, code, out)
	code, out = h.file(inactive, uuid.NewString(), "admin", full(types.duty, "2026-10-08", "2026-10-08"))
	require.Equal(t, 409, code, "inactive staff cannot file: %v", out)
	code, out = h.act(inactive, pending, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 409, code, "approval re-checks employment: %v", out)
	code, _ = h.file(uuid.NewString(), uuid.NewString(), "admin", full(types.duty, "2026-10-06", "2026-10-06"))
	require.Equal(t, 404, code, "unknown staff")
}

func TestLeaveRequestsSerialiseSlotsAndBalance(t *testing.T) {
	h := newRequestHarness(t)
	types := configureLeave(t, h.call, 2026)
	staff := h.enrolled("SYN-C1", types, "2026-01-01")
	code, out := h.post("2026", 1)
	require.Equal(t, 200, code, out)

	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() {
			codes[i], _ = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2026-10-05", "2026-10-06"))
		})
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409, 409, 409, 409, 409}, codes, "one filing holds the slots")

	big, small := uuid.NewString(), uuid.NewString()
	code, out = h.file(staff, big, "admin", full(types.earned, "2026-11-02", "2026-11-05"))
	require.Equal(t, 200, code, out)
	code, out = h.file(staff, small, "admin", full(types.earned, "2026-11-17", "2026-11-18"))
	require.Equal(t, 200, code, "eight held plus four fill the twelve: %v", out)
	code, out = h.correction(staff, uuid.NewString(), "admin", map[string]any{"leave_type_id": types.earned, "leave_year": 2026, "half_days": -4, "effective_on": "2026-10-01", "reason": "Synthetic overgrant recovery"})
	require.Equal(t, 200, code, out)
	decisions := make([]int, 2)
	for i, id := range []string{big, small} {
		wg.Go(func() {
			decisions[i], _ = h.act(staff, id, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
		})
	}
	wg.Wait()
	require.ElementsMatch(t, []int{200, 409}, decisions, "after the correction only one approval fits")
	left := 4
	if decisions[0] == 200 {
		left = 0
	}
	require.Equal(t, left, h.balances(staff, "leave_year=2026")[types.earned], "the winner alone is consumed")
}

func TestLeaveRequestAccessIsScoped(t *testing.T) {
	h := newRequestHarness(t)
	types := configureLeave(t, h.call, 2026)
	staff := h.enrolled("SYN-A1", types, "2026-01-01")
	for _, tc := range []struct {
		user, date string
		code       int
	}{{"viewer", "2026-10-05", 403}, {"foreign", "2026-10-06", 404}, {"reader", "2026-10-07", 404}, {"approver", "2026-10-08", 200}} {
		code, out := h.file(staff, uuid.NewString(), tc.user, full(types.duty, tc.date, tc.date))
		require.Equal(t, tc.code, code, "%s: %v", tc.user, out)
	}
	id := uuid.NewString()
	code, out := h.file(staff, id, "clerk", full(types.duty, "2026-10-12", "2026-10-12"))
	require.Equal(t, 200, code, out)
	code, _ = h.do("GET", h.base+"/staff/"+staff+"/leave-requests/"+id, "", "")
	require.Equal(t, 401, code)
	code, _ = h.act(staff, id, "decision", "clerk", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 403, code, "filing does not grant approval")
	code, _ = h.act(staff, id, "decision", "foreign", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 404, code, "other tenants see nothing")
	code, _ = h.call("GET", "/staff/"+staff+"/leave-requests/"+id, "foreign", nil)
	require.Equal(t, 404, code)
	code, _ = h.call("GET", "/hr/leave-requests", "foreign", nil)
	require.Equal(t, 404, code)
	code, _ = h.call("GET", "/staff/"+uuid.NewString()+"/leave-requests/"+id, "approver", nil)
	require.Equal(t, 404, code, "a request is addressed through its staff member")
	code, _ = h.act(staff, id, "decision", "approver", map[string]any{"expected_version": 1, "decision": "maybe"})
	require.Equal(t, 400, code)
	code, out = h.act(staff, id, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)
	code, out = h.act(staff, id, "revocation", "approver", map[string]any{"expected_version": 2, "note": "Synthetic"})
	require.Equal(t, 200, code, out)
	require.Zero(t, h.count("workforce_leave_ledger", "request_id=?", id), "untracked leave has nothing to reverse")
}

func TestLeaveYearCloseWaitsForPendingRequests(t *testing.T) {
	h := newRequestHarness(t)
	types := configureLeave(t, h.call, 2025, 2026)
	staff := h.enrolled("SYN-Y1", types, "2025-01-01")
	for month := 1; month <= 12; month++ {
		code, out := h.post("2025", month)
		require.Equal(t, 200, code, out)
	}
	id := uuid.NewString()
	code, out := h.file(staff, id, "admin", full(types.earned, "2025-12-01", "2025-12-02"))
	require.Equal(t, 200, code, out)
	code, out = h.call("POST", "/hr/leave-years/2025/close", "admin", nil)
	require.Equal(t, 409, code, out)
	require.Contains(t, out["message"], "pending leave request")
	require.Error(t, h.scoped(true, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_leave_years SET closed=true,closed_by_subject='user-admin',closed_at=now(),version=version+1 WHERE leave_year=2025`).Error
	}), "the database refuses the close too")
	code, out = h.act(staff, id, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)
	code, out = h.call("POST", "/hr/leave-years/2025/close", "admin", nil)
	require.Equal(t, 200, code, out)
	code, out = h.act(staff, id, "revocation", "approver", map[string]any{"expected_version": 2, "note": "Synthetic late revocation"})
	require.Equal(t, 409, code, "closed years are frozen: %v", out)
	require.Contains(t, out["message"], "not open")
	code, _ = h.file(staff, uuid.NewString(), "admin", full(types.duty, "2025-12-08", "2025-12-08"))
	require.Equal(t, 409, code, "closed years take no filings")
	require.Zero(t, h.count("workforce_leave_ledger", "request_id=? AND kind='reversal'", id))
}

func TestLeaveRequestTablesEnforceInvariantsBelowTheAPI(t *testing.T) {
	h := newRequestHarness(t)
	types := configureLeave(t, h.call, 2026)
	staff := h.enrolled("SYN-D1", types, "2026-01-01")
	code, out := h.post("2026", 1)
	require.Equal(t, 200, code, out)
	pending, approved := uuid.NewString(), uuid.NewString()
	code, out = h.file(staff, pending, "admin", full(types.earned, "2026-10-05", "2026-10-05"))
	require.Equal(t, 200, code, out)
	code, out = h.file(staff, approved, "admin", full(types.earned, "2026-10-07", "2026-10-07"))
	require.Equal(t, 200, code, out)
	code, out = h.act(staff, approved, "decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
	require.Equal(t, 200, code, out)

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_leave_requests").Count(&n).Error)
		require.Zero(t, n, "forced RLS without hospital scope")
		return nil
	}))
	exec := func(statements ...string) error {
		return h.scoped(true, func(tx *gorm.DB) error {
			for _, statement := range statements {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	decided := func(subject string) []string {
		return []string{`UPDATE workforce_leave_requests SET status='approved',decided_by_subject='` + subject + `',decided_at=now(),version=2 WHERE id='` + pending + `'`,
			`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,2,'approved','` + subject + `' FROM workforce_leave_requests WHERE id='` + pending + `'`}
	}
	require.Error(t, exec(decided("user-admin")...), "the filer cannot be the decider")
	// copyPending files a copy of the pending request directly, shifting its dates and its day rows independently.
	copyPending := func(id string, shift, dayShift int, event bool) []string {
		statements := []string{
			fmt.Sprintf(`INSERT INTO workforce_leave_requests(tenant_id,hospital_id,id,staff_id,leave_type_id,leave_type_revision_id,assignment_revision_id,leave_year,unit,start_on,end_on,half_days,reason,status,requested_by_subject,version)
 SELECT tenant_id,hospital_id,'%s',staff_id,leave_type_id,leave_type_revision_id,assignment_revision_id,leave_year,unit,start_on+%d,end_on+%d,half_days,'','pending','user-admin',1 FROM workforce_leave_requests WHERE id='%s'`, id, shift, shift, pending),
			fmt.Sprintf(`INSERT INTO workforce_leave_request_days(tenant_id,hospital_id,request_id,day,half_days,holiday,calendar_revision_id)
 SELECT tenant_id,hospital_id,'%s',day+%d,half_days,holiday,calendar_revision_id FROM workforce_leave_request_days WHERE request_id='%s'`, id, dayShift, pending),
		}
		if event {
			statements = append(statements, fmt.Sprintf(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,1,'pending','user-admin' FROM workforce_leave_requests WHERE id='%s'`, id))
		}
		return statements
	}
	advance := func(id, status string, version int) []string {
		return []string{fmt.Sprintf(`UPDATE workforce_leave_requests SET status='%s',version=%d WHERE id='%s'`, status, version, id),
			fmt.Sprintf(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) SELECT tenant_id,hospital_id,gen_random_uuid(),id,%d,'%s','user-admin' FROM workforce_leave_requests WHERE id='%s'`, version, status, id)}
	}
	ledger := `INSERT INTO workforce_leave_ledger(tenant_id,hospital_id,id,staff_id,leave_type_id,leave_type_revision_id,leave_year,kind,half_days,effective_on,idempotency_key,request_id,actor_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,leave_type_id,leave_type_revision_id,leave_year,'%s',%d,start_on,gen_random_uuid()::text,%s,'user-admin' FROM workforce_leave_requests WHERE id='%s'`
	for name, statements := range map[string][]string{
		"skip a version":         advance(pending, "cancelled", 3),
		"undo a decision":        advance(approved, "pending", 3),
		"approve without event":  {`UPDATE workforce_leave_requests SET status='approved',decided_by_subject='user-approver',decided_at=now(),version=version+1 WHERE id='` + pending + `'`},
		"change the reason":      {`UPDATE workforce_leave_requests SET reason='Synthetic edit',status='cancelled',version=2 WHERE id='` + pending + `'`, advance(pending, "cancelled", 2)[1]},
		"delete a request":       {`DELETE FROM workforce_leave_requests WHERE id='` + pending + `'`},
		"edit an event":          {`UPDATE workforce_leave_request_events SET note='x'`},
		"delete an event":        {`DELETE FROM workforce_leave_request_events`},
		"edit a day":             {`UPDATE workforce_leave_request_days SET half_days=1`},
		"overlapping request":    copyPending(uuid.NewString(), 0, 0, true),
		"days outside the dates": copyPending(uuid.NewString(), 7, 8, true),
		"request without event":  copyPending(uuid.NewString(), 7, 7, false),
		"consume pending leave":  {fmt.Sprintf(ledger, "consumption", -2, "id", pending)},
		"consume twice":          {fmt.Sprintf(ledger, "consumption", -2, "id", approved)},
		"reverse unrevoked":      {fmt.Sprintf(ledger, "reversal", 2, "id", approved)},
		"consumption without id": {fmt.Sprintf(ledger, "consumption", -2, "NULL", approved)},
	} {
		require.Error(t, exec(statements...), name)
	}
	require.NoError(t, exec(copyPending(uuid.NewString(), 7, 7, true)...), "a complete direct filing is accepted")
	require.Error(t, h.scoped(false, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) VALUES(?,?,gen_random_uuid(),?,1,'pending','user-admin')`, testutil.TenantA, h.hospital, pending).Error
	}), "writes need the transaction-local ledger flag")
	require.EqualValues(t, 1, h.count("workforce_leave_ledger", "request_id=?", approved), "nothing above slipped through")
	require.NoError(t, exec(decided("user-approver")...), "the same decision by someone else is accepted")
	require.EqualValues(t, 3, h.count("workforce_leave_requests", "true"))
}

func TestRosterPopulationSkipsApprovedFullDayLeave(t *testing.T) {
	f := newRosterFixture(t, "Asia/Kolkata", 0, "2026-10-05")
	call := func(method, path, user string, body any) (int, map[string]any) {
		data := ""
		if body != nil {
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			data = string(raw)
		}
		w := f.do(t, method, path, user, data)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
		return w.Code, out
	}
	types := configureLeave(t, call, 2026)
	n1, n2 := f.staff["SYN-N1"], f.staff["SYN-N2"]
	for _, staff := range []string{n1, n2} {
		code, out := call("PUT", "/staff/"+staff+"/leave-assignment", "admin", map[string]any{"expected_version": 0, "structure_revision_id": types.structure, "active": true, "effective_on": "2026-01-01", "reviewed": true})
		require.Equal(t, 200, code, out)
	}
	code, out := call("POST", "/hr/leave-years/2026/postings", "admin", map[string]any{"month": 1})
	require.Equal(t, 200, code, out)
	file := func(staff string, body map[string]any, approve bool) string {
		id := uuid.NewString()
		code, out := call("PUT", "/staff/"+staff+"/leave-requests/"+id, "admin", body)
		require.Equal(t, 200, code, out)
		if approve {
			code, out = call("POST", "/staff/"+staff+"/leave-requests/"+id+"/decision", "approver", map[string]any{"expected_version": 1, "decision": "approve"})
			require.Equal(t, 200, code, out)
		}
		return id
	}
	away := file(n1, full(types.duty, "2026-10-13", "2026-10-14"), true)
	file(n2, half(types.earned, "2026-10-15", "first_half"), true)
	undecided := file(n2, full(types.duty, "2026-10-19", "2026-10-19"), false)

	roster := uuid.NewString()
	require.Equal(t, 200, f.do(t, "PUT", "/hr/rosters/"+roster, "planner", `{"name":"Synthetic October","start_date":"2026-10-12","end_date":"2026-10-25","reviewed":true}`).Code)
	w := f.write(t, roster, "populate", "planner", 1, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	res := decodeBody[rosterResult](t, w)
	require.Equal(t, 2, res.Revision.SkippedLeave, "approved full days keep N1 off the roster")
	require.Equal(t, 18, res.Revision.Added, "half days and pending leave still roster")
	require.Zero(t, f.count(t, "workforce_roster_allocations", "staff_id=? AND roster_date BETWEEN '2026-10-13' AND '2026-10-14'", n1))

	code, out = call("GET", "/staff/"+n2+"/leave-requests/"+undecided, "admin", nil)
	require.Equal(t, 200, code, out)
	clashes := out["roster_clashes"].([]any)
	require.Len(t, clashes, 1, "the approver sees the shift pending leave would vacate")
	require.Equal(t, "2026-10-19", clashes[0].(map[string]any)["roster_date"])
	require.Equal(t, "Synthetic October", clashes[0].(map[string]any)["roster_name"])
	code, out = call("GET", "/staff/"+n1+"/leave-requests/"+away, "admin", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["roster_clashes"])
}
