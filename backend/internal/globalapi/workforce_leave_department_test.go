package globalapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const departmentLeaveTemplate = "workforce.department_leave_reviewer.v1"

type departmentLeaveFixture struct {
	call             func(*testing.T, string, string, string, any, int) map[string]any
	request          func(method, path, subject string, body []byte, contentType string) *httptest.ResponseRecorder
	grant            func(subject, template string, permission authz.Permission)
	grantDepartment  func(subject, department string, path []string, template string, permission authz.Permission) string
	pauseScopedCheck func(subject string, permission authz.Permission) (<-chan struct{}, func())
	unavailable      *atomic.Bool
	db               *tenantdb.DB
	hospital, base   string
	organization     string
	staff, staffBase string
	// requester holds the Staff login; filer is the hospital manager who files on their behalf.
	requester, filer, reviewer, approver, cutoffAdmin string
	parent, sub, sibling                              string
	subRevision, siblingRevision                      string
	subInput, parentInput                             map[string]any
	draft                                             string
	receipt, source, generation                       map[string]any
}

func (f departmentLeaveFixture) configureLeave(t *testing.T) string {
	t.Helper()
	f.call(t, "PUT", f.base+"/hr/leave-settings", f.filer, map[string]any{"year_start_month": 1, "reviewed": true}, 200)
	f.call(t, "PUT", f.base+"/hr/leave-years/2026", f.filer, nil, 200)
	f.call(t, "PUT", f.base+"/hr/leave-configs/"+uuid.NewString(), f.filer, map[string]any{
		"expected_version": 0, "kind": "holiday_calendar", "code": "SYN-DL-CALENDAR", "name": "Synthetic department leave calendar", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true,
		"calendar_year": 2026, "holidays": []any{}, "entries": []any{},
	}, 200)
	leaveType := uuid.NewString()
	kind := f.call(t, "PUT", f.base+"/hr/leave-configs/"+leaveType, f.filer, map[string]any{
		"expected_version": 0, "kind": "leave_type", "code": "SYN-DL-LEAVE", "name": "Synthetic department leave", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true,
		"rules": map[string]any{"paid": false, "balance_tracked": false, "half_day_allowed": true, "short_leave_minutes": 120, "clubbable": true}, "holidays": []any{}, "entries": []any{},
	}, 200)
	structure := f.call(t, "PUT", f.base+"/hr/leave-configs/"+uuid.NewString(), f.filer, map[string]any{
		"expected_version": 0, "kind": "structure", "code": "SYN-DL-STRUCTURE", "name": "Synthetic department leave structure", "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true,
		"holidays": []any{}, "entries": []any{map[string]any{"leave_type_revision_id": kind["revision_id"], "annual_half_days": 0, "accrual": "upfront", "carry_forward_half_days": 0, "proration": "join_month", "probation_eligible": true}},
	}, 200)
	f.call(t, "PUT", f.staffPath()+"/leave-assignment", f.filer, map[string]any{"expected_version": 0, "structure_revision_id": structure["revision_id"], "active": true, "effective_on": "2026-01-01", "reviewed": true}, 200)
	return leaveType
}

func (f departmentLeaveFixture) staffPath() string { return f.base + "/staff/" + f.staff }

func (f departmentLeaveFixture) file(t *testing.T, leaveType, date string) string {
	t.Helper()
	id := uuid.NewString()
	f.call(t, "PUT", f.staffPath()+"/leave-requests/"+id, f.filer, map[string]any{"leave_type_id": leaveType, "unit": "full_day", "start_on": date, "end_on": date, "reason": "Synthetic private department leave reason"}, 200)
	return id
}

// approvePosting places the requester in department through the reviewed hospital-management root flow.
func (f departmentLeaveFixture) approvePosting(t *testing.T, draft string, receipt map[string]any) (string, map[string]any) {
	t.Helper()
	root := f.staffBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pending := f.call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), f.requester, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": f.generation["id"], "staff_version": f.source["staff_version"], "employment_epoch": f.source["employment_epoch"], "reason": "Synthetic department leave placement", "sources_examined": true, "supervision": map[string]any{"kind": "hospital_management"}}, 200)
	approved := f.call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), f.approver, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent synthetic department leave placement review", "sources_examined": true, "hospital_management_reviewed": true}, 200)
	require.Equal(t, "approved", approved["status"])
	return root, approved
}

func (f departmentLeaveFixture) cutoff(t *testing.T, root string, approved map[string]any) {
	t.Helper()
	f.call(t, "PUT", root+"/cutoffs/commands/"+uuid.Must(uuid.NewV7()).String(), f.cutoffAdmin, map[string]any{"expected_revision_id": approved["revision_id"], "expected_cutoff_version": 0, "mode": "immediate", "reason": "Synthetic department leave posting end", "sources_examined": true}, 200)
}

func (f departmentLeaveFixture) queue(t *testing.T, subject, department string, status int) []any {
	t.Helper()
	body := f.call(t, "GET", f.base+"/department-leave-review-requests?department="+department, subject, nil, status)
	if status != 200 {
		require.NotContains(t, body, "data")
		return nil
	}
	return body["data"].([]any)
}

func (f departmentLeaveFixture) detailPath(request, department string) string {
	return f.staffPath() + "/department-leave-review-requests/" + request + "?department=" + department
}

func (f departmentLeaveFixture) decisionPath(request, department string) string {
	return f.staffPath() + "/leave-requests/" + request + "/department-decision?department=" + department
}

// pausedDecision holds the reviewer's leave graph check open while change commits, then returns the decision response.
func (f departmentLeaveFixture) pausedDecision(t *testing.T, request, department string, change func()) *httptest.ResponseRecorder {
	t.Helper()
	paused, release := f.pauseScopedCheck(f.reviewer, "workforce.leave.approve")
	defer release()
	body, err := json.Marshal(map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent synthetic department decision"})
	require.NoError(t, err)
	decided := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		decided <- f.request("POST", f.decisionPath(request, department), f.reviewer, body, "application/json")
	}()
	select {
	case <-paused:
	case <-time.After(10 * time.Second):
		release()
		t.Fatal("the department decision never reached its leave graph check")
	}
	change()
	release()
	return <-decided
}

func (f departmentLeaveFixture) approvalAudit(t *testing.T, request string) map[string]any {
	t.Helper()
	var changes []json.RawMessage
	require.NoError(t, f.db.WithTenant(tenantdb.WithActor(t.Context(), f.filer), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, f.filer, f.hospital).Error; err != nil {
			return err
		}
		return tx.Table("workforce_audit").Where("resource_id=? AND action='workforce.leave_request.approved'", request).Pluck("changes", &changes).Error
	}))
	require.Len(t, changes, 1, "exactly one approval audit")
	var out map[string]any
	require.NoError(t, json.Unmarshal(changes[0], &out))
	return out
}

func (f departmentLeaveFixture) scope(t *testing.T, subject string) map[string]any {
	t.Helper()
	for _, row := range f.call(t, "GET", "/v1/workforce/leave-review-scopes", subject, nil, 200)["data"].([]any) {
		if scope := row.(map[string]any); scope["hospital_id"] == f.hospital {
			return scope
		}
	}
	return nil
}

// assertDepartmentLeaveReviewer drives a department leave reviewer through the Global API (ADR-0055 slice 2).
func assertDepartmentLeaveReviewer(t *testing.T, f departmentLeaveFixture) {
	t.Helper()
	leaveType := f.configureLeave(t)
	subRoot, subApproved := f.approvePosting(t, f.draft, f.receipt)
	require.Equal(t, f.sub, subApproved["department_id"])
	first := f.file(t, leaveType, "2026-12-01")
	decision := map[string]any{"expected_version": 1, "decision": "approve", "note": "Independent synthetic department decision"}

	f.queue(t, f.reviewer, f.parent, 403)
	f.call(t, "POST", f.decisionPath(first, f.parent), f.reviewer, decision, 403)
	assignment := f.grantDepartment(f.reviewer, f.parent, []string{f.parent}, departmentLeaveTemplate, "workforce.leave.approve")

	scope := f.scope(t, f.reviewer)
	require.NotNil(t, scope, "a department leave grant discovers its hospital")
	require.Equal(t, "department", scope["scope_kind"])
	require.Equal(t, []any{f.parent}, scope["department_ids"])

	f.call(t, "GET", f.base+"/department-leave-review-requests", f.reviewer, nil, 400)
	f.call(t, "GET", f.base+"/department-leave-review-requests?department=not-a-department", f.reviewer, nil, 400)
	f.queue(t, f.reviewer, f.sibling, 404)
	f.queue(t, f.reviewer, uuid.NewString(), 404)
	f.call(t, "GET", "/v1/workforce/hospitals/"+uuid.NewString()+"/department-leave-review-requests?department="+f.parent, f.reviewer, nil, 404)
	for _, held := range []string{f.parent, f.sub} {
		listed := f.queue(t, f.reviewer, held, 200)
		require.Len(t, listed, 1, "the held subtree covers the sub-unit requester")
		row := listed[0].(map[string]any)
		require.Equal(t, first, row["id"])
		require.Equal(t, f.staff, row["staff_id"])
		require.NotContains(t, row, "reason")
	}

	f.call(t, "GET", f.base+"/leave-review-requests", f.reviewer, nil, 403)
	f.call(t, "POST", f.staffPath()+"/leave-requests/"+first+"/decision", f.reviewer, decision, 403)
	f.queue(t, f.filer, f.parent, 403)
	f.grant(f.approver, "workforce.leave_reviewer.v1", "workforce.leave.approve")
	require.Len(t, f.call(t, "GET", f.base+"/leave-review-requests", f.approver, nil, 200)["data"], 1)
	f.queue(t, f.approver, f.parent, 403)

	detail := f.call(t, "GET", f.detailPath(first, f.parent), f.reviewer, nil, 200)
	require.Equal(t, "Synthetic private department leave reason", detail["request"].(map[string]any)["reason"])
	f.call(t, "GET", f.detailPath(first, f.sibling), f.reviewer, nil, 404)
	f.call(t, "GET", f.base+"/staff/"+uuid.NewString()+"/department-leave-review-requests/"+first+"?department="+f.parent, f.reviewer, nil, 404)
	f.call(t, "GET", f.staffPath()+"/department-leave-review-requests/"+first, f.reviewer, nil, 400)

	f.grantDepartment(f.requester, f.parent, []string{f.parent}, departmentLeaveTemplate, "workforce.leave.approve")
	require.Empty(t, f.queue(t, f.requester, f.parent, 200), "a reviewer never sees their own request")
	f.call(t, "POST", f.decisionPath(first, f.parent), f.requester, decision, 403)

	func() {
		f.unavailable.Store(true)
		defer f.unavailable.Store(false)
		f.queue(t, f.reviewer, f.parent, 503)
		f.call(t, "GET", f.detailPath(first, f.parent), f.reviewer, nil, 503)
		f.call(t, "POST", f.decisionPath(first, f.parent), f.reviewer, decision, 503)
	}()

	widened := postingDepartmentFixture{call: f.call, db: f.db, hospital: f.hospital, organization: f.organization, maker: f.filer, parent: f.parent}
	retireWide := widened.widenParent(t)
	f.queue(t, f.reviewer, f.parent, 503)
	f.call(t, "POST", f.decisionPath(first, f.parent), f.reviewer, decision, 503)
	require.Len(t, f.queue(t, f.reviewer, f.sub, 200), 1, "a bounded inner subtree still answers")
	retireWide()

	f.call(t, "POST", f.decisionPath(first, f.sibling), f.reviewer, decision, 404)
	f.call(t, "POST", f.staffPath()+"/leave-requests/"+first+"/department-decision", f.reviewer, decision, 400)
	approved := f.call(t, "POST", f.decisionPath(first, f.parent), f.reviewer, decision, 200)
	require.Equal(t, "approved", approved["request"].(map[string]any)["status"])
	require.Equal(t, f.reviewer, approved["request"].(map[string]any)["decided_by_subject"])
	require.Equal(t, approved, f.call(t, "POST", f.decisionPath(first, f.parent), f.reviewer, decision, 200), "a replayed decision returns the retained result")
	audit := f.approvalAudit(t, first)
	require.Equal(t, "department", audit["scope_kind"])
	require.Equal(t, f.parent, audit["department_id"])
	require.Equal(t, []any{f.sub}, audit["requester_departments"])
	require.NotContains(t, fmt.Sprint(audit), "Synthetic private department leave reason")
	require.Empty(t, f.queue(t, f.reviewer, f.parent, 200))

	second := f.file(t, leaveType, "2026-12-02")
	moved := f.pausedDecision(t, second, f.sub, func() {
		f.revise(t, f.sub, f.subInput, map[string]any{"parent_id": f.sibling, "reason": "Synthetic sub-unit move between hint and lock"})
	})
	require.Equal(t, 409, moved.Code, "a path changed after the hint is refused, never allowed: %s", moved.Body.String())
	require.Contains(t, moved.Body.String(), `"department_scope_changed"`)
	require.Empty(t, f.queue(t, f.reviewer, f.parent, 200), "a sub-unit moved out takes its requesters with it")
	f.call(t, "GET", f.detailPath(second, f.parent), f.reviewer, nil, 404)
	f.revise(t, f.sub, f.subInput, map[string]any{"parent_id": f.parent, "reason": "Synthetic sub-unit returns under the held department"})
	require.Len(t, f.queue(t, f.reviewer, f.parent, 200), 1)

	siblingDraft := uuid.Must(uuid.NewV7()).String()
	siblingReceipt := f.call(t, "PUT", f.staffBase+"/posting-drafts/"+siblingDraft+"/commands/"+uuid.Must(uuid.NewV7()).String(), f.requester, map[string]any{"expected_version": 0, "staff_version": f.source["staff_version"], "employment_epoch": f.source["employment_epoch"], "department_id": f.sibling, "department_revision_id": f.siblingRevision, "starts_at": time.Now().UTC().Format(time.RFC3339), "ends_at": f.receipt["ends_at"], "reason": "Synthetic second posting outside the held subtree"}, 200)
	siblingRoot, siblingApproved := f.approvePosting(t, siblingDraft, siblingReceipt)
	require.Empty(t, f.queue(t, f.reviewer, f.parent, 200), "a requester also posted outside the subtree leaves department scope")
	f.call(t, "GET", f.detailPath(second, f.parent), f.reviewer, nil, 404)
	f.call(t, "POST", f.decisionPath(second, f.parent), f.reviewer, decision, 404)
	f.cutoff(t, siblingRoot, siblingApproved)
	require.Len(t, f.queue(t, f.reviewer, f.parent, 200), 1, "a past cutoff ends the outside posting")
	f.call(t, "GET", f.detailPath(second, f.parent), f.reviewer, nil, 200)

	ended := f.pausedDecision(t, second, f.parent, func() { f.cutoff(t, subRoot, subApproved) })
	require.Equal(t, 404, ended.Code, "a cutoff committed first denies the decision: %s", ended.Body.String())
	require.Empty(t, f.queue(t, f.reviewer, f.parent, 200), "a requester with no live posting is outside every department")
	f.call(t, "GET", f.detailPath(second, f.parent), f.reviewer, nil, 404)
	require.Equal(t, "pending", f.call(t, "GET", f.staffPath()+"/leave-requests/"+second, f.filer, nil, 200)["request"].(map[string]any)["status"])

	revoked := f.pausedDecision(t, second, f.parent, func() {
		standing := f.call(t, "GET", assignment, f.filer, nil, 200)
		f.call(t, "POST", assignment+"/decision", f.filer, map[string]any{"expected_version": standing["version"], "decision": "withdraw", "reason": "Synthetic department leave authority cutoff"}, 200)
	})
	require.Equal(t, 403, revoked.Code, "a withdrawal committed first denies the decision: %s", revoked.Body.String())
	f.queue(t, f.reviewer, f.parent, 403)
	require.Nil(t, f.scope(t, f.reviewer), "a withdrawn department grant stops discovering its hospital")
}

func (f departmentLeaveFixture) revise(t *testing.T, department string, input map[string]any, change map[string]any) {
	t.Helper()
	postingDepartmentFixture{call: f.call, organization: f.organization, maker: f.filer}.revise(t, department, input, change)
}
