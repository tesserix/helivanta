package workforce_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
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

// newStatutoryHarness adds an approver beside the slice 1 users; "admin" also holds approve so self-decision is refused on separation, not permission.
func newStatutoryHarness(t *testing.T) *payrollHarness {
	t.Helper()
	all := []authz.Permission{workforce.PermPayrollConfigure, workforce.PermPayrollView, workforce.PermPayrollApprove}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "approver": testutil.TenantA, "viewer": testutil.TenantA, "maker": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"admin": append([]authz.Permission{workforce.PermHospitalManage, workforce.PermMasterManage}, all...), "approver": all,
			"viewer": {workforce.PermPayrollView}, "maker": {workforce.PermPayrollConfigure, workforce.PermPayrollView}, "reader": all, "directory": {}, "foreign": all},
		Modules: []platform.Module{workforce.New()},
	})
	h := &payrollHarness{t: t, db: db, hospital: uuid.NewString(), do: func(method, path, user, body string) (int, string) {
		w := testutil.Do(r, method, path, user, body)
		return w.Code, w.Body.String()
	}}
	h.base = "/v1/workforce/hospitals/" + h.hospital
	h.other = "/v1/workforce/hospitals/" + uuid.NewString()
	for _, base := range []string{h.base, h.other} {
		code, out := h.do("PUT", base, "admin", `{"name":"Synthetic Statutory Hospital"}`)
		require.Equal(t, 200, code, out)
	}
	for user, level := range map[string]string{"admin": "manager", "approver": "manager", "maker": "manager", "viewer": "reader", "reader": "reader", "directory": "manager"} {
		code, out := h.do("PUT", h.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`)
		require.Equal(t, 200, code, out)
	}
	return h
}

// Synthetic values only: these prove behaviour, never statutory rates.
func pfRule(version int, effective string) map[string]any {
	return map[string]any{"expected_version": version, "kind": "pf", "name": "Synthetic PF", "covered": true, "effective_on": effective, "citation": "Synthetic notification 1", "reference": "",
		"pf": map[string]any{"wage_basis": "statutory_wages", "contribution_basis": "capped", "wage_ceiling_paise": 1000000, "employee_rate_ppm": 100000, "employer_rate_ppm": 100000,
			"eps_rate_ppm": 60000, "eps_wage_ceiling_paise": 1000000, "edli_rate_ppm": 4000, "edli_wage_ceiling_paise": 1000000, "admin_rate_ppm": 3000, "admin_minimum_paise": 0,
			"rounding_mode": "nearest", "rounding_unit_paise": 1}}
}

func esiRule(version int, effective string) map[string]any {
	return map[string]any{"expected_version": version, "kind": "esi", "name": "Synthetic ESI", "covered": true, "effective_on": effective, "citation": "Synthetic notification 2", "reference": "https://example.invalid/esi",
		"esi": map[string]any{"wage_basis": "gross", "wage_ceiling_paise": 1500000, "disability_wage_ceiling_paise": 2000000, "employee_rate_ppm": 10000, "employer_rate_ppm": 30000,
			"employee_exempt_daily_paise": 20000, "period_start_months": []int{10, 4}, "rounding_mode": "nearest", "rounding_unit_paise": 1}}
}

func withParams(body map[string]any, key string, changes map[string]any) map[string]any {
	out := maps.Clone(body)
	params := maps.Clone(body[key].(map[string]any))
	maps.Copy(params, changes)
	out[key] = params
	return out
}

func (h *payrollHarness) propose(id, user string, body map[string]any) (int, map[string]any) {
	return h.call("PUT", "/statutory-rules/"+id, user, body)
}

func (h *payrollHarness) decide(id, user string, body map[string]any) (int, map[string]any) {
	return h.call("POST", "/statutory-rules/"+id+"/decision", user, body)
}

// approved proposes as admin and approves as approver, returning the approved revision id.
func (h *payrollHarness) approved(id string, body map[string]any) string {
	h.t.Helper()
	code, out := h.propose(id, "admin", body)
	require.Equal(h.t, 200, code, out)
	proposal := out["pending"].(map[string]any)["id"]
	code, out = h.decide(id, "approver", map[string]any{"proposal_id": proposal, "expected_version": body["expected_version"], "decision": "approve", "note": ""})
	require.Equal(h.t, 200, code, out)
	return proposal.(string)
}

func TestStatutoryProposalsNeedASecondPerson(t *testing.T) {
	h := newStatutoryHarness(t)
	id := uuid.NewString()
	body := with(pfRule(0, "2026-01-01"), map[string]any{"name": "  Synthetic   PF  ", "citation": " Synthetic notification 1 "})
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i], _ = h.propose(id, "admin", body) })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code, "concurrent identical proposals converge on one")
	}
	code, rule := h.propose(id, "admin", body)
	require.Equal(t, 200, code, rule)
	require.EqualValues(t, 0, rule["version"])
	require.Nil(t, rule["current"], "nothing is in force before approval")
	pending := rule["pending"].(map[string]any)
	proposal := pending["id"].(string)
	require.Equal(t, "pending", pending["status"])
	require.Equal(t, "Synthetic PF", pending["name"])
	require.Equal(t, "Synthetic notification 1", pending["citation"])
	require.Equal(t, "user-admin", pending["proposed_by_subject"])
	require.EqualValues(t, 1, pending["sequence"])
	require.EqualValues(t, 100000, pending["pf"].(map[string]any)["employee_rate_ppm"])

	code, out := h.propose(id, "admin", with(pfRule(0, "2026-01-01"), map[string]any{"name": "Synthetic PF Two"}))
	requireSalaryConflict(t, code, out, "proposal_pending", "", "", "")
	approve := map[string]any{"proposal_id": proposal, "expected_version": 0, "decision": "approve", "note": ""}
	code, out = h.decide(id, "admin", approve)
	require.Equal(t, 403, code, "the proposer never decides their own proposal: %v", out)
	for user, status := range map[string]int{"viewer": 403, "maker": 403, "directory": 403, "reader": 404, "foreign": 404} {
		code, out = h.decide(id, user, approve)
		require.Equal(t, status, code, "%s: %v", user, out)
	}
	code, _ = h.do("POST", h.base+"/hr/payroll/statutory-rules/"+id+"/decision", "", `{}`)
	require.Equal(t, 401, code)
	code, out = h.decide(id, "approver", with(approve, map[string]any{"decision": "reject"}))
	require.Equal(t, 400, code, "a rejection needs a note")
	require.Contains(t, fmt.Sprint(out), "Give a reason", "the handler answers before the table check")
	code, _ = h.decide(id, "approver", with(approve, map[string]any{"decision": "defer"}))
	require.Equal(t, 400, code)
	code, _ = h.decide(id, "approver", with(approve, map[string]any{"expected_version": 1}))
	require.Equal(t, 409, code, "a stale expected version is refused")
	code, _ = h.decide(id, "approver", with(approve, map[string]any{"proposal_id": uuid.NewString()}))
	require.Equal(t, 404, code)
	code, raw := h.do("POST", h.other+"/hr/payroll/statutory-rules/"+id+"/decision", "approver", `{"proposal_id":"`+proposal+`","expected_version":0,"decision":"approve","note":""}`)
	require.Equal(t, 404, code, raw)

	code, out = h.decide(id, "approver", approve)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Equal(t, "2026-01-01", out["effective_on"])
	require.Nil(t, out["pending"])
	current := out["current"].(map[string]any)
	require.Equal(t, proposal, current["id"])
	require.Equal(t, "approved", current["status"])
	require.Equal(t, "user-approver", current["decided_by_subject"])
	require.EqualValues(t, 1, current["approved_version"])
	code, out = h.decide(id, "approver", approve)
	require.Equal(t, 200, code, "the same decider replaying gets the result: %v", out)
	code, out = h.decide(id, "approver", with(approve, map[string]any{"decision": "reject", "note": "Synthetic reason"}))
	requireSalaryConflict(t, code, out, "not_pending", "", "", "")
	code, out = h.call("POST", "/statutory-rules/"+id+"/withdraw", "admin", map[string]any{"proposal_id": proposal, "note": ""})
	requireSalaryConflict(t, code, out, "not_pending", "", "", "")
	code, out = h.propose(id, "admin", body)
	require.Equal(t, 200, code, "replaying the approved body returns the rule: %v", out)
	require.Nil(t, out["pending"])

	code, _ = h.propose(id, "admin", pfRule(0, "2026-02-01"))
	require.Equal(t, 409, code, "stale expected version")
	code, _ = h.propose(id, "admin", esiRule(1, "2026-02-01"))
	require.Equal(t, 409, code, "kind is immutable")
	code, _ = h.propose(id, "admin", pfRule(1, "2025-12-31"))
	require.Equal(t, 400, code, "a proposal never takes effect before the revision in force")
	code, _ = h.propose(uuid.NewString(), "admin", pfRule(0, "2026-01-01"))
	require.Equal(t, 409, code, "one rule of each kind per hospital")
	code, _ = h.propose(uuid.NewString(), "admin", pfRule(2, "2026-01-01"))
	require.Equal(t, 404, code)
	for user, status := range map[string]int{"viewer": 403, "directory": 403, "reader": 404, "foreign": 404} {
		code, out = h.propose(id, user, pfRule(1, "2026-03-01"))
		require.Equal(t, status, code, "%s: %v", user, out)
	}

	code, out = h.propose(id, "maker", with(pfRule(1, "2026-09-17"), map[string]any{"name": "Synthetic PF Revised"}))
	require.Equal(t, 200, code, out)
	revised := out["pending"].(map[string]any)["id"].(string)
	withdraw := map[string]any{"proposal_id": revised, "note": "Synthetic withdrawal"}
	code, _ = h.call("POST", "/statutory-rules/"+id+"/withdraw", "admin", withdraw)
	require.Equal(t, 403, code, "only the proposer withdraws")
	code, _ = h.call("POST", "/statutory-rules/"+id+"/withdraw", "viewer", withdraw)
	require.Equal(t, 403, code)
	code, out = h.call("POST", "/statutory-rules/"+id+"/withdraw", "maker", withdraw)
	require.Equal(t, 200, code, out)
	require.Nil(t, out["pending"])
	require.EqualValues(t, 1, out["version"], "a withdrawal never advances the rule")
	code, out = h.call("POST", "/statutory-rules/"+id+"/withdraw", "maker", withdraw)
	require.Equal(t, 200, code, "withdraw replays: %v", out)

	code, out = h.propose(id, "maker", with(pfRule(1, "2026-09-17"), map[string]any{"name": "Synthetic PF Rejected"}))
	require.Equal(t, 200, code, out)
	rejected := out["pending"].(map[string]any)["id"].(string)
	code, out = h.decide(id, "approver", map[string]any{"proposal_id": rejected, "expected_version": 1, "decision": "reject", "note": " Synthetic rejection "})
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"])
	require.Equal(t, proposal, out["current"].(map[string]any)["id"], "a rejection leaves the revision in force")

	code, raw = h.do("GET", h.base+"/hr/payroll/statutory-rules/"+id+"/history", "viewer", "")
	require.Equal(t, 200, code, raw)
	history := decodeRaw[struct {
		Data []struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			DecisionNote string `json:"decision_note"`
		}
	}](t, raw)
	statuses := map[string]string{}
	for _, r := range history.Data {
		statuses[r.ID] = r.Status
	}
	require.Equal(t, map[string]string{proposal: "approved", revised: "withdrawn", rejected: "rejected"}, statuses)
	require.Equal(t, rejected, history.Data[0].ID, "newest first")
	require.Equal(t, "Synthetic rejection", history.Data[0].DecisionNote)
	code, raw = h.do("GET", h.base+"/hr/payroll/statutory-rules", "viewer", "")
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, `"id":"`+id+`"`)
	for path, status := range map[string]int{"/statutory-rules?limit=101": 400, "/statutory-rules/" + uuid.NewString() + "/history": 404, "/statutory-rules/not-a-uuid/history": 404} {
		code, _ = h.do("GET", h.base+"/hr/payroll"+path, "viewer", "")
		require.Equal(t, status, code, path)
	}
	for user, status := range map[string]int{"directory": 403, "foreign": 404, "": 401} {
		code, _ = h.do("GET", h.base+"/hr/payroll/statutory-rules", user, "")
		require.Equal(t, status, code, user)
	}
	code, raw = h.do("GET", h.other+"/hr/payroll/statutory-rules/"+id+"/history", "admin", "")
	require.Equal(t, 404, code, raw)

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var events []struct{ Payload []byte }
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject = ?", workforce.StatutoryRuleChanged).Order("id").Find(&events).Error)
		got := []string{}
		for _, e := range events {
			var envelope struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(e.Payload, &envelope))
			require.ElementsMatch(t, []string{"hospital_id", "rule_id", "revision_id", "kind", "status", "version"}, slices.Collect(maps.Keys(envelope.Data)), "events carry identifiers only")
			got = append(got, envelope.Data["status"].(string))
		}
		require.ElementsMatch(t, []string{"proposed", "approved", "proposed", "withdrawn", "proposed", "rejected"}, got, "one event per change, none for replays")
		var changes []string
		require.NoError(t, tx.Table("workforce_audit").Where("action LIKE ?", "workforce.statutory_rule.%").Pluck("changes::text", &changes).Error)
		require.Len(t, changes, 6)
		for _, c := range changes {
			require.NotContains(t, c, "rate")
			require.NotContains(t, c, "paise")
			require.NotContains(t, c, "Synthetic")
		}
		return nil
	}))
}

func TestStatutoryRulesRequireExplicitCoverage(t *testing.T) {
	h := newStatutoryHarness(t)
	pf, esi := pfRule(0, "2026-01-01"), esiRule(0, "2026-01-01")
	for name, invalid := range map[string]map[string]any{
		"blank citation":    with(pf, map[string]any{"citation": "  "}),
		"blank name":        with(pf, map[string]any{"name": "  "}),
		"nul citation":      with(pf, map[string]any{"citation": "Synthetic\x00"}),
		"impossible date":   with(pf, map[string]any{"effective_on": "2026-02-30"}),
		"date out of range": with(pf, map[string]any{"effective_on": "1999-12-31"}),
		"unknown kind":      with(pf, map[string]any{"kind": "pt"}),
		"missing covered":   with(pf, map[string]any{"covered": nil}),
		"unknown field":     with(pf, map[string]any{"rate_ppm": 1}),
		"injected tenant":   with(pf, map[string]any{"tenant_id": testutil.TenantB}),
		"rate above 100%":   withParams(pf, "pf", map[string]any{"employee_rate_ppm": 1000001}),
		"bad rounding unit": withParams(pf, "pf", map[string]any{"rounding_unit_paise": 25}),
		"missing PF rate":   withParams(pf, "pf", map[string]any{"edli_rate_ppm": nil}),
		"unknown PF basis":  withParams(pf, "pf", map[string]any{"contribution_basis": "basic"}),
		"no period months":  withParams(esi, "esi", map[string]any{"period_start_months": []int{}}),
		"month 13":          withParams(esi, "esi", map[string]any{"period_start_months": []int{13}}),
		"zero exempt wage":  withParams(esi, "esi", map[string]any{"employee_exempt_daily_paise": 0}),
	} {
		code, out := h.propose(uuid.NewString(), "admin", invalid)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	// The validator answers these before any write; the table checks behind it are proven in the database test.
	for name, invalid := range map[string]map[string]any{
		"covered without parameters": with(pf, map[string]any{"pf": nil}),
		"uncovered with parameters":  with(pf, map[string]any{"covered": false}),
		"other kind's parameters":    with(pf, map[string]any{"esi": esi["esi"]}),
		"EPS above employer":         withParams(pf, "pf", map[string]any{"eps_rate_ppm": 100001}),
		"EPS ceiling above ceiling":  withParams(pf, "pf", map[string]any{"eps_wage_ceiling_paise": 1000001}),
		"disability below standard":  withParams(esi, "esi", map[string]any{"disability_wage_ceiling_paise": 1499999}),
		"duplicate period month":     withParams(esi, "esi", map[string]any{"period_start_months": []int{4, 4}}),
		"unsorted duplicate month":   withParams(esi, "esi", map[string]any{"period_start_months": []int{4, 10, 4}}),
	} {
		code, out := h.propose(uuid.NewString(), "admin", invalid)
		require.Equal(t, 400, code, "%s: %v", name, out)
		require.Contains(t, fmt.Sprint(out), "Review the rule", name)
	}
	id := uuid.NewString()
	uncovered := with(esi, map[string]any{"covered": false, "esi": nil, "citation": "Synthetic exemption order"})
	h.approved(id, uncovered)
	code, out := h.propose(id, "admin", with(esi, map[string]any{"expected_version": 1, "effective_on": "2026-04-01"}))
	require.Equal(t, 200, code, out)
	pending := out["pending"].(map[string]any)["esi"].(map[string]any)
	require.Equal(t, []any{4.0, 10.0}, pending["period_start_months"], "months are stored as a set")
	require.Nil(t, out["current"].(map[string]any)["esi"], "an uncovered revision carries no parameters")
}

func TestStatutoryPreviewResolvesTheRevisionInForce(t *testing.T) {
	h := newStatutoryHarness(t)
	basic := h.head(fixedEarning("SYN-BASIC", "wages"))
	special := h.head(with(fixedEarning("SYN-SPECIAL", "excluded"), map[string]any{"proration": "none", "payslip_order": 15}))
	structure := uuid.NewString()
	code, out := h.putStructure(structure, structureBody("SYN-NURSE", fixedLine(basic, 1200000), fixedLine(special, 100000)))
	require.Equal(t, 200, code, out)
	preview := func(user, on string, halfDays int, disability bool) (int, map[string]any) {
		return h.call("POST", "/statutory-preview", user, map[string]any{"structure_id": structure, "on": on, "payable_half_days": halfDays, "disability": disability})
	}
	results := func(out map[string]any) map[string]map[string]any {
		got := map[string]map[string]any{}
		for _, r := range out["results"].([]any) {
			got[r.(map[string]any)["kind"].(string)] = r.(map[string]any)
		}
		return got
	}

	code, out = preview("viewer", "2026-09-16", 60, false)
	require.Equal(t, 200, code, out)
	for _, kind := range []string{"pf", "esi"} {
		require.Equal(t, "not_configured", results(out)[kind]["status"], "nothing is defaulted: %s", kind)
	}
	require.EqualValues(t, 0, out["employee_paise"])
	pf := uuid.NewString()
	first := h.approved(pf, pfRule(0, "2026-01-01"))
	second := h.approved(pf, withParams(pfRule(1, "2026-09-17"), "pf", map[string]any{"wage_ceiling_paise": 2000000, "eps_wage_ceiling_paise": 1500000}))
	code, out = h.propose(pf, "admin", withParams(pfRule(2, "2026-09-18"), "pf", map[string]any{"employee_rate_ppm": 500000}))
	require.Equal(t, 200, code, out)

	code, out = preview("viewer", "2026-09-16", 60, false)
	require.Equal(t, 200, code, out)
	before := results(out)["pf"]
	require.Equal(t, "applied", before["status"])
	require.Equal(t, first, before["revision_id"])
	require.EqualValues(t, 1200000, before["wages_paise"], "Labour Code wages: the excluded share is within half of remuneration")
	require.EqualValues(t, 1000000, before["base_paise"], "capped at the ceiling in force")
	require.Equal(t, map[string]int64{"pf_employee": 100000, "pf_employer_epf": 40000, "pf_employer_eps": 60000, "pf_edli": 4000, "pf_admin": 3000}, componentAmounts(before))
	code, out = preview("viewer", "2026-09-17", 60, false)
	require.Equal(t, 200, code, out)
	after := results(out)["pf"]
	require.Equal(t, second, after["revision_id"], "the revision dated that day is in force from that day")
	require.EqualValues(t, 1200000, after["base_paise"])
	require.Equal(t, map[string]int64{"pf_employee": 120000, "pf_employer_epf": 48000, "pf_employer_eps": 72000, "pf_edli": 4000, "pf_admin": 3600}, componentAmounts(after))
	code, out = preview("viewer", "2026-10-01", 62, false)
	require.Equal(t, 200, code, out)
	require.Equal(t, second, results(out)["pf"]["revision_id"], "a pending proposal is never in force")

	esi := uuid.NewString()
	h.approved(esi, with(esiRule(0, "2026-01-01"), map[string]any{"covered": false, "esi": nil}))
	code, out = preview("viewer", "2026-09-17", 60, false)
	require.Equal(t, 200, code, out)
	require.Equal(t, "not_covered", results(out)["esi"]["status"])
	require.Empty(t, results(out)["esi"]["components"])
	h.approved(esi, withParams(esiRule(1, "2026-04-01"), "esi", map[string]any{"wage_ceiling_paise": 1200000}))
	code, out = preview("viewer", "2026-09-17", 60, false)
	require.Equal(t, 200, code, out)
	require.Equal(t, "above_ceiling", results(out)["esi"]["status"], "gross 13,000 exceeds the 12,000 ceiling")
	code, out = preview("viewer", "2026-09-17", 60, true)
	require.Equal(t, 200, code, out)
	got := results(out)["esi"]
	require.Equal(t, "applied", got["status"], "the disability ceiling covers the same wages")
	require.Equal(t, "gross", got["wage_basis"])
	require.Equal(t, map[string]int64{"esi_employee": 13000, "esi_employer": 39000}, componentAmounts(got))
	require.EqualValues(t, 120000+13000, out["employee_paise"])
	require.EqualValues(t, 48000+72000+4000+3600+39000, out["employer_paise"])
	salary := out["salary"].(map[string]any)["totals"].(map[string]any)
	require.EqualValues(t, salary["net_paise"].(float64)-133000, out["net_after_statutory_paise"])

	for name, body := range map[string]map[string]any{
		"more half-days than the month": {"structure_id": structure, "on": "2026-09-17", "payable_half_days": 61, "disability": false},
		"impossible date":               {"structure_id": structure, "on": "2026-02-30", "payable_half_days": 0, "disability": false},
		"missing disability":            {"structure_id": structure, "on": "2026-09-17", "payable_half_days": 0},
		"nil structure":                 {"structure_id": uuid.Nil, "on": "2026-09-17", "payable_half_days": 0, "disability": false},
	} {
		code, out := h.call("POST", "/statutory-preview", "viewer", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, _ = h.call("POST", "/statutory-preview", "viewer", map[string]any{"structure_id": uuid.NewString(), "on": "2026-09-17", "payable_half_days": 0, "disability": false})
	require.Equal(t, 404, code)
	for user, status := range map[string]int{"directory": 403, "foreign": 404} {
		code, _ = preview(user, "2026-09-17", 60, false)
		require.Equal(t, status, code, user)
	}
	code, raw := h.do("POST", h.other+"/hr/payroll/statutory-preview", "admin", `{"structure_id":"`+structure+`","on":"2026-09-17","payable_half_days":60,"disability":false}`)
	require.Equal(t, 404, code, raw)
}

func componentAmounts(result map[string]any) map[string]int64 {
	out := map[string]int64{}
	for _, c := range result["components"].([]any) {
		component := c.(map[string]any)
		out[component["component"].(string)] = int64(component["amount_paise"].(float64))
	}
	return out
}

func TestStatutoryDatabaseEnforcesSeparationAndImmutability(t *testing.T) {
	h := newStatutoryHarness(t)
	id := uuid.NewString()
	approved := h.approved(id, pfRule(0, "2026-03-01"))
	code, out := h.propose(id, "admin", pfRule(1, "2026-04-01"))
	require.Equal(t, 200, code, out)
	pending := out["pending"].(map[string]any)["id"].(string)
	authorized := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error
	}
	run := func(statement string, args ...any) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := authorized(tx); err != nil {
				return err
			}
			return tx.Exec(statement, args...).Error
		})
	}
	uncoveredID := uuid.NewString()
	code, out = h.propose(uncoveredID, "admin", with(esiRule(0, "2026-03-01"), map[string]any{"covered": false, "esi": nil}))
	require.Equal(t, 200, code, out)
	uncovered := out["pending"].(map[string]any)["id"].(string)
	// Each statement names the guard that must reject it, so a different failure cannot stand in for it.
	for name, tc := range map[string]struct{ statement, guard string }{
		"self-approval":                    {"UPDATE workforce_statutory_rule_revisions SET status='approved',approved_version=2,decided_by_subject=proposed_by_subject,decided_at=now() WHERE id='" + pending + "'", "workforce_statutory_separation"},
		"self-rejection":                   {"UPDATE workforce_statutory_rule_revisions SET status='rejected',decided_by_subject=proposed_by_subject,decided_at=now(),decision_note='x' WHERE id='" + pending + "'", "workforce_statutory_separation"},
		"withdrawal by another":            {"UPDATE workforce_statutory_rule_revisions SET status='withdrawn',decided_by_subject='user-approver',decided_at=now() WHERE id='" + pending + "'", "workforce_statutory_withdrawn_by_proposer"},
		"rejection without a note":         {"UPDATE workforce_statutory_rule_revisions SET status='rejected',decided_by_subject='user-approver',decided_at=now() WHERE id='" + pending + "'", "workforce_statutory_rejection_note"},
		"approval not put in force":        {"UPDATE workforce_statutory_rule_revisions SET status='approved',approved_version=2,decided_by_subject='user-approver',decided_at=now() WHERE id='" + pending + "'", "must become the rule in force"},
		"content changed with a decision":  {"UPDATE workforce_statutory_rule_revisions SET status='withdrawn',decided_by_subject=proposed_by_subject,decided_at=now(),citation='Other' WHERE id='" + pending + "'", "content is immutable"},
		"decided proposal changed":         {"UPDATE workforce_statutory_rule_revisions SET decision_note='Other' WHERE id='" + approved + "'", "content is immutable"},
		"proposals are never deleted":      {"DELETE FROM workforce_statutory_rule_revisions WHERE id='" + pending + "'", "destructive workforce operation"},
		"rules are never deleted":          {"DELETE FROM workforce_statutory_rules WHERE id='" + id + "'", "destructive workforce operation"},
		"parameters are immutable":         {"UPDATE workforce_statutory_pf_params SET employee_rate_ppm=1 WHERE revision_id='" + approved + "'", "append only"},
		"parameters added later":           {"INSERT INTO workforce_statutory_esi_params(tenant_id,hospital_id,revision_id,wage_basis,wage_ceiling_paise,disability_wage_ceiling_paise,employee_rate_ppm,employer_rate_ppm,period_start_mask,rounding_mode,rounding_unit_paise) SELECT tenant_id,hospital_id,id,'gross',1,1,0,0,1,'nearest',1 FROM workforce_statutory_rule_revisions WHERE id='" + uncovered + "'", "revision creating transaction"},
		"rule skips a version":             {"UPDATE workforce_statutory_rules SET version=version+2 WHERE id='" + id + "'", "advances one approved version"},
		"rule points at an unapproved one": {"UPDATE workforce_statutory_rules SET version=2,revision_id='" + pending + "',effective_on='2026-04-01' WHERE id='" + id + "'", "workforce_statutory_rule_current"},
		"rule moves earlier":               {"UPDATE workforce_statutory_rules SET version=2,effective_on='2026-02-01' WHERE id='" + id + "'", "cannot take effect before"},
		"proposal starts decided":          {"INSERT INTO workforce_statutory_rule_revisions(tenant_id,hospital_id,id,rule_id,kind,sequence,base_version,status,name,covered,effective_on,citation,reference,proposed_by_subject,decided_by_subject,decided_at,decision_note) SELECT tenant_id,hospital_id,gen_random_uuid(),rule_id,kind,99,1,'withdrawn',name,false,effective_on,citation,'',proposed_by_subject,proposed_by_subject,now(),'' FROM workforce_statutory_rule_revisions WHERE id='" + pending + "'", "starts pending on the current version"},
	} {
		require.ErrorContains(t, run(tc.statement), tc.guard, name)
	}
	require.NoError(t, run("UPDATE workforce_statutory_rule_revisions SET status='withdrawn',decided_by_subject=proposed_by_subject,decided_at=now(),decision_note='' WHERE id=?", pending), "the one legal transition succeeds under the same session")
	err := h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, h.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_statutory_rules(tenant_id,hospital_id,id,kind,version) VALUES(?,?,?,'esi',0)`, testutil.TenantA, h.hospital, uuid.New()).Error
	})
	require.Error(t, err, "writes need the payroll authorization flag")
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_statutory_rule_revisions").Count(&n).Error)
		require.Zero(t, n, "forced RLS without hospital scope")
		return nil
	}))
}
