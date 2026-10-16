package workforce_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

// payFixture is a structure with two fixed earnings and a percentage deduction on basic, plus one active staff member.
type payFixture struct {
	*payrollHarness
	basic, hra, pf, structure, staff string
}

func newPayFixture(t *testing.T) *payFixture {
	t.Helper()
	h := newStatutoryHarness(t)
	f := &payFixture{payrollHarness: h}
	f.basic = h.head(fixedEarning("BASIC", "wages"))
	f.hra = h.head(fixedEarning("HRA", "excluded"))
	f.pf = h.head(percentHead("deduction", "EPF", 120000, f.basic))
	f.structure = uuid.NewString()
	code, out := h.putStructure(f.structure, structureBody("STAFF", fixedLine(f.basic, 3000000), fixedLine(f.hra, 1000000), percentLine(f.pf)))
	require.Equal(t, 200, code, out)
	f.staff = f.enrol("PAY-01")
	return f
}

func (f *payFixture) enrol(code string) string {
	id := uuid.NewString()
	status, raw := f.do("PUT", f.base+"/staff/"+id, "admin", `{"employee_code":"`+code+`","display_name":"Synthetic `+code+`","job_category":"nurse","employment_status":"active"}`)
	require.Equal(f.t, 200, status, raw)
	return id
}

func assignmentBody(version int, structure, effective string, amounts ...map[string]any) map[string]any {
	if amounts == nil {
		amounts = []map[string]any{}
	}
	return map[string]any{"expected_version": version, "structure_id": structure, "effective_on": effective, "reason": "Synthetic appointment", "amounts": amounts}
}

func amount(head string, paise int64) map[string]any {
	return map[string]any{"head_id": head, "amount_paise": paise}
}

func (f *payFixture) path(staff, suffix string) string {
	return "/staff/" + staff + "/salary-assignment" + suffix
}

func (f *payFixture) proposePay(staff, user string, body map[string]any) (int, map[string]any) {
	return f.call("PUT", f.path(staff, ""), user, body)
}

func (f *payFixture) decidePay(staff, user string, body map[string]any) (int, map[string]any) {
	return f.call("POST", f.path(staff, "/decision"), user, body)
}

// approvedPay proposes as admin and approves as approver, returning the approved revision id.
func (f *payFixture) approvedPay(staff string, body map[string]any) string {
	f.t.Helper()
	code, out := f.proposePay(staff, "admin", body)
	require.Equal(f.t, 200, code, out)
	proposal := out["pending"].(map[string]any)["id"]
	code, out = f.decidePay(staff, "approver", map[string]any{"proposal_id": proposal, "expected_version": body["expected_version"], "decision": "approve", "note": ""})
	require.Equal(f.t, 200, code, out)
	return proposal.(string)
}

func (f *payFixture) preview(staff, user, on string) (int, map[string]any) {
	return f.call("POST", "/staff/"+staff+"/salary-preview", user, map[string]any{"on": on, "payable_half_days": 60, "disability": false})
}

func previewLines(t *testing.T, out map[string]any) map[string]float64 {
	t.Helper()
	lines := map[string]float64{}
	for _, l := range out["salary"].(map[string]any)["lines"].([]any) {
		line := l.(map[string]any)
		lines[line["head_id"].(string)] = line["amount_paise"].(float64)
	}
	return lines
}

func TestSalaryAssignmentsNeedASecondPerson(t *testing.T) {
	f := newPayFixture(t)
	code, out := f.call("GET", f.path(f.staff, ""), "viewer", nil)
	require.Equal(t, 404, code, "no pay before a proposal: %v", out)

	body := with(assignmentBody(0, f.structure, "2026-04-01", amount(f.hra, 1200000), amount(f.basic, 3500000)), map[string]any{"reason": "  Synthetic   appointment "})
	code, out = f.proposePay(f.staff, "admin", body)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 0, out["version"])
	require.Nil(t, out["current"], "nothing is in force before approval")
	pending := out["pending"].(map[string]any)
	proposal := pending["id"].(string)
	require.Equal(t, "pending", pending["status"])
	require.Equal(t, "Synthetic appointment", pending["reason"])
	require.Equal(t, "user-admin", pending["proposed_by_subject"])
	require.Len(t, pending["amounts"], 2)

	code, replay := f.proposePay(f.staff, "admin", assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, 3500000), amount(f.hra, 1200000)))
	require.Equal(t, 200, code, replay)
	require.Equal(t, proposal, replay["pending"].(map[string]any)["id"], "a replay in any amount order returns the proposal unchanged")
	code, out = f.proposePay(f.staff, "admin", assignmentBody(0, f.structure, "2026-04-01"))
	requireSalaryConflict(t, code, out, "proposal_pending", "", "", "")
	code, out = f.proposePay(uuid.NewString(), "admin", assignmentBody(0, f.structure, "2026-04-01"))
	require.Equal(t, 404, code, "an unknown staff member: %v", out)
	code, out = f.proposePay(f.enrol("PAY-02"), "admin", assignmentBody(1, f.structure, "2026-04-01"))
	require.Equal(t, 409, code, "a first proposal starts at version 0: %v", out)

	approve := map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "approve", "note": ""}
	code, out = f.decidePay(f.staff, "admin", approve)
	require.Equal(t, 403, code, "the proposer never decides their own proposal: %v", out)
	for user, status := range map[string]int{"viewer": 403, "maker": 403, "directory": 403, "reader": 404, "foreign": 404} {
		code, out = f.decidePay(f.staff, user, approve)
		require.Equal(t, status, code, "%s: %v", user, out)
	}
	code, out = f.decidePay(f.staff, "approver", with(approve, map[string]any{"decision": "reject"}))
	require.Equal(t, 400, code, out)
	require.Contains(t, fmt.Sprint(out), "Give a reason")
	code, _ = f.decidePay(f.staff, "approver", with(approve, map[string]any{"expected_version": 1}))
	require.Equal(t, 409, code, "a stale expected version is refused")
	code, _ = f.decidePay(f.staff, "approver", with(approve, map[string]any{"proposal_id": uuid.NewString()}))
	require.Equal(t, 404, code)
	code, raw := f.do("POST", f.other+"/hr/payroll"+f.path(f.staff, "/decision"), "approver", `{"proposal_id":"`+proposal+`","expected_version":0,"decision":"approve","note":""}`)
	require.Equal(t, 404, code, raw)

	code, out = f.decidePay(f.staff, "approver", approve)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Equal(t, "2026-04-01", out["effective_on"])
	require.Nil(t, out["pending"])
	current := out["current"].(map[string]any)
	require.Equal(t, "approved", current["status"])
	require.Equal(t, "user-approver", current["decided_by_subject"])
	code, again := f.decidePay(f.staff, "approver", approve)
	require.Equal(t, 200, code, "the same decision replays: %v", again)
	require.EqualValues(t, 1, again["version"])
	code, out = f.proposePay(f.staff, "admin", body)
	require.Equal(t, 200, code, out)
	require.Nil(t, out["pending"], "replaying the content in force adds no proposal")

	code, out = f.proposePay(f.staff, "admin", assignmentBody(1, f.structure, "2026-03-01"))
	require.Equal(t, 400, code, "a proposal cannot predate the assignment in force: %v", out)
	code, out = f.proposePay(f.staff, "admin", assignmentBody(0, f.structure, "2026-05-01"))
	require.Equal(t, 409, code, "a stale expected version: %v", out)

	code, out = f.proposePay(f.staff, "admin", assignmentBody(1, f.structure, "2026-05-01"))
	require.Equal(t, 200, code, out)
	second := out["pending"].(map[string]any)["id"].(string)
	code, out = f.call("POST", f.path(f.staff, "/withdraw"), "approver", map[string]any{"proposal_id": second, "note": ""})
	require.Equal(t, 403, code, "only the proposer withdraws: %v", out)
	code, out = f.call("POST", f.path(f.staff, "/withdraw"), "admin", map[string]any{"proposal_id": second, "note": "Synthetic error"})
	require.Equal(t, 200, code, out)
	require.Nil(t, out["pending"])
	require.EqualValues(t, 1, out["version"])
	code, out = f.decidePay(f.staff, "approver", map[string]any{"proposal_id": second, "expected_version": 1, "decision": "approve", "note": ""})
	requireSalaryConflict(t, code, out, "not_pending", "", "", "")

	code, out = f.proposePay(f.staff, "admin", assignmentBody(1, f.structure, "2026-06-01", amount(f.basic, 4000000)))
	require.Equal(t, 200, code, out)
	third := out["pending"].(map[string]any)["id"].(string)
	code, out = f.decidePay(f.staff, "approver", map[string]any{"proposal_id": third, "expected_version": 1, "decision": "reject", "note": "Synthetic budget"})
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])

	code, history := f.call("GET", f.path(f.staff, "/history"), "viewer", nil)
	require.Equal(t, 200, code, history)
	statuses := []string{}
	for _, r := range history["data"].([]any) {
		statuses = append(statuses, r.(map[string]any)["status"].(string))
	}
	require.Equal(t, []string{"rejected", "withdrawn", "approved"}, statuses, "newest first")
	require.Len(t, history["data"].([]any)[0].(map[string]any)["amounts"], 1)

	code, list := f.call("GET", "/salary-assignments", "viewer", nil)
	require.Equal(t, 200, code, list)
	require.Len(t, list["data"], 1)
	require.Equal(t, f.staff, list["data"].([]any)[0].(map[string]any)["staff_id"])
	rejected := f.enrol("PAY-03")
	code, out = f.proposePay(rejected, "admin", assignmentBody(0, f.structure, "2026-04-01"))
	require.Equal(t, 200, code, out)
	code, out = f.decidePay(rejected, "approver", map[string]any{"proposal_id": out["pending"].(map[string]any)["id"], "expected_version": 0, "decision": "reject", "note": "Synthetic"})
	require.Equal(t, 200, code, out)
	code, list = f.call("GET", "/salary-assignments", "viewer", nil)
	require.Equal(t, 200, code, list)
	require.Len(t, list["data"], 1, "a staff member with nothing current or pending is not listed")
	code, out = f.call("GET", f.path(rejected, ""), "viewer", nil)
	require.Equal(t, 404, code, out)

	for path, status := range map[string]int{"/salary-assignments?limit=101": 400, f.path(uuid.NewString(), "/history"): 404, "/staff/not-a-uuid/salary-assignment": 404} {
		code, _ = f.do("GET", f.base+"/hr/payroll"+path, "viewer", "")
		require.Equal(t, status, code, path)
	}
	for user, status := range map[string]int{"directory": 403, "foreign": 404, "": 401} {
		code, _ = f.do("GET", f.base+"/hr/payroll"+f.path(f.staff, ""), user, "")
		require.Equal(t, status, code, user)
	}
	for user, status := range map[string]int{"viewer": 403, "reader": 404, "foreign": 404} {
		code, out = f.proposePay(f.staff, user, assignmentBody(1, f.structure, "2026-07-01"))
		require.Equal(t, status, code, "%s: %v", user, out)
	}
	code, raw = f.do("GET", f.other+"/hr/payroll"+f.path(f.staff, ""), "admin", "")
	require.Equal(t, 404, code, raw)

	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var events []struct{ Payload []byte }
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject = ?", workforce.SalaryAssignmentChanged).Order("id").Find(&events).Error)
		got := []string{}
		for _, e := range events {
			var envelope struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(e.Payload, &envelope))
			require.ElementsMatch(t, []string{"hospital_id", "staff_id", "revision_id", "status", "version"}, slices.Collect(maps.Keys(envelope.Data)), "events carry identifiers only")
			got = append(got, envelope.Data["status"].(string))
		}
		require.ElementsMatch(t, []string{"proposed", "approved", "proposed", "withdrawn", "proposed", "rejected", "proposed", "rejected"}, got, "one event per change, none for replays")
		var changes []string
		require.NoError(t, tx.Table("workforce_audit").Where("action LIKE ?", "workforce.salary_assignment.%").Pluck("changes::text", &changes).Error)
		require.Len(t, changes, 8)
		for _, c := range changes {
			require.NotContains(t, c, "paise")
			require.NotContains(t, c, "structure")
			require.NotContains(t, c, "Synthetic")
		}
		return nil
	}))
}

func TestSalaryAssignmentAmountsMustFitTheStructure(t *testing.T) {
	f := newPayFixture(t)
	outside := f.head(fixedEarning("SPECIAL", "wages"))
	for name, amounts := range map[string][]map[string]any{
		"a percentage head":            {amount(f.pf, 100)},
		"a head outside the structure": {amount(outside, 100)},
	} {
		code, out := f.proposePay(f.staff, "admin", assignmentBody(0, f.structure, "2026-04-01", amounts...))
		requireSalaryConflict(t, code, out, "assignment_incomplete", "staff_id", f.staff, "2026-04-01")
		require.NotContains(t, fmt.Sprint(out), "pending", name)
	}
	for name, body := range map[string]map[string]any{
		"a head given twice":   assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, 1), amount(f.basic, 2)),
		"a negative amount":    assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, -1)),
		"an amount over limit": assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, 1000000001)),
		"a blank reason":       with(assignmentBody(0, f.structure, "2026-04-01"), map[string]any{"reason": "   "}),
		"an impossible date":   assignmentBody(0, f.structure, "2026-02-30"),
		"too many amounts":     assignmentBody(0, f.structure, "2026-04-01", slices.Repeat([]map[string]any{amount(f.basic, 1)}, 61)...),
	} {
		code, out := f.proposePay(f.staff, "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out := f.proposePay(f.staff, "admin", assignmentBody(0, uuid.NewString(), "2026-04-01"))
	require.Equal(t, 404, code, "an unknown structure: %v", out)
	foreignHead, foreign := uuid.NewString(), uuid.NewString()
	code, raw := f.do("PUT", f.other+"/access/user-admin", "admin", `{"level":"manager"}`)
	require.Equal(t, 200, code, raw)
	code, raw = f.do("PUT", f.other+"/hr/payroll/salary-heads/"+foreignHead, "admin", string(mustJSON(t, fixedEarning("BASIC", "wages"))))
	require.Equal(t, 200, code, raw)
	code, raw = f.do("PUT", f.other+"/hr/payroll/salary-structures/"+foreign, "admin", string(mustJSON(t, structureBody("OTHER", fixedLine(foreignHead, 100)))))
	require.Equal(t, 200, code, raw)
	code, out = f.proposePay(f.staff, "admin", assignmentBody(0, foreign, "2026-04-01"))
	require.Equal(t, 404, code, "another hospital's structure: %v", out)

	f.approvedPay(f.staff, assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, 3500000)))
	deactivated := with(structureBody("STAFF", fixedLine(f.basic, 3000000), fixedLine(f.hra, 1000000), percentLine(f.pf)), map[string]any{"expected_version": 1, "active": false, "effective_on": "2026-08-01"})
	code, out = f.putStructure(f.structure, deactivated)
	requireSalaryConflict(t, code, out, "assignment_incomplete", "staff_id", f.staff, "2026-08-01")
	without := with(structureBody("STAFF", fixedLine(f.hra, 1000000)), map[string]any{"expected_version": 1, "effective_on": "2026-09-01"})
	code, out = f.putStructure(f.structure, without)
	requireSalaryConflict(t, code, out, "assignment_incomplete", "staff_id", f.staff, "2026-09-01")
	code, out = f.putStructure(f.structure, with(deactivated, map[string]any{"effective_on": "2026-03-01"}))
	requireSalaryConflict(t, code, out, "assignment_incomplete", "staff_id", f.staff, "2026-04-01")
	code, out = f.putStructure(f.structure, with(deactivated, map[string]any{"effective_on": "2025-12-01"}))
	require.Equal(t, 400, code, "a structure change cannot predate its revision in force: %v", out)

	// A later assignment that drops the amount frees the structure from 2026-11-01, but the earlier one still binds until then.
	f.approvedPay(f.staff, assignmentBody(1, f.structure, "2026-11-01"))
	code, out = f.putStructure(f.structure, with(without, map[string]any{"effective_on": "2026-10-15"}))
	requireSalaryConflict(t, code, out, "assignment_incomplete", "staff_id", f.staff, "2026-10-15")
	code, out = f.putStructure(f.structure, with(without, map[string]any{"effective_on": "2026-11-01"}))
	require.Equal(t, 200, code, "once no assignment sets basic, it can leave the structure: %v", out)
	code, out = f.proposePay(f.staff, "admin", assignmentBody(2, f.structure, "2026-12-01", amount(f.basic, 1)))
	requireSalaryConflict(t, code, out, "assignment_incomplete", "staff_id", f.staff, "2026-12-01")
}

func TestSalaryAssignmentsNeedAnActiveStaffMember(t *testing.T) {
	f := newPayFixture(t)
	f.approvedPay(f.staff, assignmentBody(0, f.structure, "2026-04-01"))
	status, raw := f.do("PUT", f.base+"/staff/"+f.staff, "admin", `{"employee_code":"PAY-01","display_name":"Synthetic PAY-01","job_category":"nurse","employment_status":"inactive"}`)
	require.Equal(t, 200, status, raw)
	code, out := f.proposePay(f.staff, "admin", assignmentBody(1, f.structure, "2026-05-01"))
	requireSalaryConflict(t, code, out, "staff_inactive", "", "", "")
	code, out = f.proposePay(f.staff, "admin", assignmentBody(0, f.structure, "2026-04-01"))
	require.Equal(t, 200, code, "replaying the assignment in force is still answered: %v", out)
	code, out = f.call("GET", f.path(f.staff, ""), "viewer", nil)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"], "approved history stays readable")
	code, out = f.preview(f.staff, "viewer", "2026-04-15")
	require.Equal(t, 200, code, out)
}

func TestStaffSalaryPreviewUsesThePersonsAmounts(t *testing.T) {
	f := newPayFixture(t)
	code, out := f.preview(f.staff, "viewer", "2026-04-15")
	requireSalaryConflict(t, code, out, "not_assigned", "", "", "")
	f.approvedPay(f.staff, assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, 3500000)))
	code, out = f.preview(f.staff, "viewer", "2026-03-31")
	requireSalaryConflict(t, code, out, "not_assigned", "", "", "")

	code, out = f.preview(f.staff, "viewer", "2026-04-15")
	require.Equal(t, 200, code, out)
	require.Equal(t, map[string]float64{f.basic: 3500000, f.hra: 1000000, f.pf: 420000}, previewLines(t, out), "the person's basic, the structure's HRA, and EPF on the person's basic")
	require.Equal(t, f.staff, out["staff_id"])
	require.Equal(t, f.structure, out["salary"].(map[string]any)["structure_id"])
	require.Equal(t, "2026-27", out["tax_year"])
	require.Equal(t, "regime_not_elected", out["tax_regime_status"], "no regime is assumed")
	require.NotContains(t, out, "tax_regime")
	results := out["results"].([]any)
	require.Len(t, results, 4, "each monthly statutory kind is reported")
	for _, r := range results {
		require.Equal(t, "not_configured", r.(map[string]any)["status"])
	}

	code, structurePreview := f.call("POST", "/salary-structures/"+f.structure+"/preview", "viewer", map[string]any{"on": "2026-04-15", "payable_half_days": 60})
	require.Equal(t, 200, code, structurePreview)
	require.Equal(t, map[string]float64{f.basic: 3000000, f.hra: 1000000, f.pf: 360000}, previewLines(t, map[string]any{"salary": structurePreview}), "the structure itself is unchanged")

	code, out = f.call("PUT", "/staff/"+f.staff+"/tax-regimes/2026-27", "admin", map[string]any{"expected_version": 0, "regime": "old", "intimated_on": "2026-04-02", "note": ""})
	require.Equal(t, 200, code, out)
	code, out = f.preview(f.staff, "viewer", "2026-04-15")
	require.Equal(t, 200, code, out)
	require.Equal(t, "elected", out["tax_regime_status"])
	require.Equal(t, "old", out["tax_regime"])
	code, out = f.preview(f.staff, "viewer", "2027-04-15")
	require.Equal(t, 200, code, out)
	require.Equal(t, "regime_not_elected", out["tax_regime_status"], "an election covers its own tax year only")

	for user, status := range map[string]int{"directory": 403, "foreign": 404} {
		code, out = f.preview(f.staff, user, "2026-04-15")
		require.Equal(t, status, code, "%s: %v", user, out)
	}
	code, out = f.preview(uuid.NewString(), "viewer", "2026-04-15")
	require.Equal(t, 404, code, out)
	code, out = f.call("POST", "/staff/"+f.staff+"/salary-preview", "viewer", map[string]any{"on": "2026-04-15", "payable_half_days": 61, "disability": false})
	require.Equal(t, 400, code, "more payable days than April has: %v", out)
}

func TestSalaryAssignmentDatabaseGuards(t *testing.T) {
	f := newPayFixture(t)
	approved := f.approvedPay(f.staff, assignmentBody(0, f.structure, "2026-04-01", amount(f.basic, 3500000)))
	code, out := f.proposePay(f.staff, "admin", assignmentBody(1, f.structure, "2026-05-01"))
	require.Equal(t, 200, code, out)
	pending := out["pending"].(map[string]any)["id"].(string)
	run := func(statement string, args ...any) error {
		return f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, f.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement, args...).Error
		})
	}
	// Each statement names the guard that must reject it, so a different failure cannot stand in for it.
	for name, tc := range map[string]struct{ statement, guard string }{
		"self-approval":                  {"UPDATE workforce_salary_assignment_revisions SET status='approved',approved_version=2,decided_by_subject=proposed_by_subject,decided_at=now() WHERE id='" + pending + "'", "workforce_salary_assignment_separation"},
		"withdrawal by another":          {"UPDATE workforce_salary_assignment_revisions SET status='withdrawn',decided_by_subject='user-approver',decided_at=now() WHERE id='" + pending + "'", "workforce_salary_assignment_withdrawn_by_proposer"},
		"rejection without a note":       {"UPDATE workforce_salary_assignment_revisions SET status='rejected',decided_by_subject='user-approver',decided_at=now() WHERE id='" + pending + "'", "workforce_salary_assignment_rejection_note"},
		"approval not put in force":      {"UPDATE workforce_salary_assignment_revisions SET status='approved',approved_version=2,decided_by_subject='user-approver',decided_at=now() WHERE id='" + pending + "'", "must become the one in force"},
		"content changed with decision":  {"UPDATE workforce_salary_assignment_revisions SET status='withdrawn',decided_by_subject=proposed_by_subject,decided_at=now(),reason='Other' WHERE id='" + pending + "'", "content is immutable"},
		"decided proposal changed":       {"UPDATE workforce_salary_assignment_revisions SET decision_note='Other' WHERE id='" + approved + "'", "content is immutable"},
		"proposals are never deleted":    {"DELETE FROM workforce_salary_assignment_revisions WHERE id='" + pending + "'", "destructive workforce operation"},
		"assignments are never deleted":  {"DELETE FROM workforce_salary_assignments WHERE staff_id='" + f.staff + "'", "destructive workforce operation"},
		"amounts are immutable":          {"UPDATE workforce_salary_assignment_amounts SET amount_paise=1 WHERE revision_id='" + approved + "'", "append only"},
		"amounts added later":            {"INSERT INTO workforce_salary_assignment_amounts(tenant_id,hospital_id,revision_id,head_id,amount_paise) SELECT tenant_id,hospital_id,id,'" + f.hra + "',1 FROM workforce_salary_assignment_revisions WHERE id='" + pending + "'", "revision creating transaction"},
		"assignment skips a version":     {"UPDATE workforce_salary_assignments SET version=version+2 WHERE staff_id='" + f.staff + "'", "advances one approved version"},
		"assignment points at a pending": {"UPDATE workforce_salary_assignments SET version=2,revision_id='" + pending + "',effective_on='2026-05-01' WHERE staff_id='" + f.staff + "'", "workforce_salary_assignment_current"},
		"assignment moves earlier":       {"UPDATE workforce_salary_assignments SET version=2,effective_on='2026-03-01' WHERE staff_id='" + f.staff + "'", "cannot take effect before"},
		"proposal starts decided":        {"INSERT INTO workforce_salary_assignment_revisions(tenant_id,hospital_id,id,staff_id,structure_id,sequence,base_version,status,effective_on,reason,proposed_by_subject,decided_by_subject,decided_at,decision_note) SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,structure_id,99,1,'withdrawn',effective_on,reason,proposed_by_subject,proposed_by_subject,now(),'' FROM workforce_salary_assignment_revisions WHERE id='" + pending + "'", "starts pending on the current version"},
		"second pending proposal":        {"INSERT INTO workforce_salary_assignment_revisions(tenant_id,hospital_id,id,staff_id,structure_id,sequence,base_version,status,effective_on,reason,proposed_by_subject,decision_note) SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,structure_id,99,1,'pending',effective_on,reason,proposed_by_subject,'' FROM workforce_salary_assignment_revisions WHERE id='" + pending + "'", "workforce_salary_assignment_one_pending"},
		"proposal predates the current":  {"INSERT INTO workforce_salary_assignment_revisions(tenant_id,hospital_id,id,staff_id,structure_id,sequence,base_version,status,effective_on,reason,proposed_by_subject,decision_note) SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,structure_id,99,1,'pending','2026-03-01',reason,proposed_by_subject,'' FROM workforce_salary_assignment_revisions WHERE id='" + approved + "'", "cannot take effect before"},
	} {
		require.ErrorContains(t, run(tc.statement), tc.guard, name)
	}
	require.NoError(t, run("UPDATE workforce_salary_assignment_revisions SET status='withdrawn',decided_by_subject=proposed_by_subject,decided_at=now(),decision_note='' WHERE id=?", pending), "the one legal transition succeeds under the same session")
	err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, f.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_salary_assignments(tenant_id,hospital_id,staff_id,version) VALUES(?,?,?,0)`, testutil.TenantA, f.hospital, f.enrol("PAY-04")).Error
	})
	require.Error(t, err, "writes need the payroll authorization flag")
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		for _, table := range []string{"workforce_salary_assignments", "workforce_salary_assignment_revisions", "workforce_salary_assignment_amounts"} {
			var n int64
			require.NoError(t, tx.Table(table).Count(&n).Error)
			require.Zero(t, n, "forced RLS without hospital scope: %s", table)
		}
		return nil
	}))
}
