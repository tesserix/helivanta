package workforce_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
)

func taxSlab(from int64, to any, rate int) map[string]any {
	return map[string]any{"from_paise": from, "to_paise": to, "rate_ppm": rate}
}

// Synthetic values only: a two-band ladder, a rebate and one surcharge band prove behaviour, never a Finance Act's schedule.
func incomeTaxRule(kind string, version int, effective, year string) map[string]any {
	return map[string]any{"expected_version": version, "kind": kind, "name": "Synthetic income tax", "covered": true, "effective_on": effective, "citation": "Synthetic Finance Act", "reference": "",
		"income_tax": map[string]any{"tax_year": year, "standard_deduction_paise": 1000000, "taxable_rounding_mode": "nearest", "taxable_rounding_unit_paise": 1,
			"tax_rounding_mode": "nearest", "tax_rounding_unit_paise": 1, "cess_rate_ppm": 50000,
			"age_bands": []any{
				map[string]any{"min_age": 60, "slabs": []any{taxSlab(0, 20000000, 0), taxSlab(20000000, nil, 100000)}},
				map[string]any{"min_age": 0, "slabs": []any{taxSlab(10000000, nil, 100000), taxSlab(0, 10000000, 0)}},
			},
			"rebate":          map[string]any{"income_limit_paise": 15000000, "max_paise": 1000000, "marginal_relief": true},
			"surcharge_bands": []any{map[string]any{"above_paise": 50000000, "rate_ppm": 100000}},
		}}
}

func TestIncomeTaxRulesFollowTheLifecycle(t *testing.T) {
	h := newStatutoryHarness(t)
	id := uuid.NewString()
	h.approved(id, incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-27"))
	code, out := h.call("GET", "/statutory-rules/"+id+"/history", "viewer", nil)
	require.Equal(t, 200, code, out)
	params := out["data"].([]any)[0].(map[string]any)["income_tax"].(map[string]any)
	require.Equal(t, "2026-27", params["tax_year"])
	bands := params["age_bands"].([]any)
	require.EqualValues(t, 0, bands[0].(map[string]any)["min_age"], "bands come back by age")
	require.EqualValues(t, 0, bands[0].(map[string]any)["slabs"].([]any)[0].(map[string]any)["from_paise"], "slabs come back by income")
	require.Equal(t, true, params["rebate"].(map[string]any)["marginal_relief"])
	require.Len(t, params["surcharge_bands"], 1)
	code, out = h.propose(id, "admin", incomeTaxRule("income_tax_new", 1, "2026-04-01", "2026-27"))
	require.Equal(t, 200, code, "the revision in force replays despite entry order: %v", out)
	require.Nil(t, out["pending"])

	noRebate := incomeTaxRule("income_tax_old", 0, "2026-04-01", "2026-27")
	delete(noRebate["income_tax"].(map[string]any), "rebate")
	old := uuid.NewString()
	h.approved(old, withParams(noRebate, "income_tax", map[string]any{"surcharge_bands": []any{}}))
	code, out = h.call("GET", "/statutory-rules/"+old+"/history", "viewer", nil)
	require.Equal(t, 200, code, out)
	params = out["data"].([]any)[0].(map[string]any)["income_tax"].(map[string]any)
	require.Nil(t, params["rebate"], "no rebate is stored as none")
	require.Empty(t, params["surcharge_bands"])

	code, out = h.propose(id, "admin", incomeTaxRule("income_tax_new", 1, "2027-04-01", "2027-28"))
	require.Equal(t, 200, code, out)
	proposal := out["pending"].(map[string]any)["id"]
	code, out = h.decide(id, "admin", map[string]any{"proposal_id": proposal, "expected_version": 1, "decision": "approve", "note": ""})
	require.Equal(t, 403, code, "the proposer never approves their own income-tax rule: %v", out)
	code, out = h.decide(id, "approver", map[string]any{"proposal_id": proposal, "expected_version": 1, "decision": "reject", "note": "Wrong citation"})
	require.Equal(t, 200, code, out)

	taxError, ruleError := "Review the income-tax rule", "Review the rule"
	rule := func(change func(params map[string]any)) map[string]any {
		body := incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-27")
		params := withParams(body, "income_tax", map[string]any{})["income_tax"].(map[string]any)
		change(params)
		body["income_tax"] = params
		return body
	}
	for name, tc := range map[string]struct {
		body map[string]any
		want string
	}{
		"effective before the year": {incomeTaxRule("income_tax_new", 0, "2026-03-31", "2026-27"), taxError},
		"effective after the year":  {incomeTaxRule("income_tax_new", 0, "2027-04-01", "2026-27"), taxError},
		"malformed year":            {incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-28"), taxError},
		"slab gap": {rule(func(p map[string]any) {
			p["age_bands"] = []any{map[string]any{"min_age": 0, "slabs": []any{taxSlab(0, 100, 0), taxSlab(101, nil, 1)}}}
		}), taxError},
		"no age 0 band": {rule(func(p map[string]any) {
			p["age_bands"] = []any{map[string]any{"min_age": 1, "slabs": []any{taxSlab(0, nil, 0)}}}
		}), taxError},
		"repeated age": {rule(func(p map[string]any) { p["age_bands"] = append(p["age_bands"].([]any), p["age_bands"].([]any)[1]) }), taxError},
		"non-increasing surcharge": {rule(func(p map[string]any) {
			p["surcharge_bands"] = []any{map[string]any{"above_paise": 2, "rate_ppm": 1}, map[string]any{"above_paise": 2, "rate_ppm": 2}}
		}), taxError},
		"income tax on PF":           {with(pfRule(0, "2026-04-01"), map[string]any{"income_tax": incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-27")["income_tax"]}), ruleError},
		"covered without parameters": {with(incomeTaxRule("income_tax_old", 0, "2026-04-01", "2026-27"), map[string]any{"income_tax": nil}), ruleError},
	} {
		code, out := h.propose(uuid.NewString(), "admin", tc.body)
		require.Equal(t, 400, code, "%s: %v", name, out)
		require.Contains(t, fmt.Sprint(out), tc.want, name)
	}
	for name, body := range map[string]map[string]any{
		"partial rebate":    rule(func(p map[string]any) { p["rebate"] = map[string]any{"income_limit_paise": 1, "max_paise": 1} }),
		"no bands":          rule(func(p map[string]any) { p["age_bands"] = []any{} }),
		"five bands":        rule(func(p map[string]any) { p["age_bands"] = []any{1, 2, 3, 4, 5} }),
		"age above 120":     rule(func(p map[string]any) { p["age_bands"].([]any)[0].(map[string]any)["min_age"] = 121 }),
		"rate above 100%":   rule(func(p map[string]any) { p["cess_rate_ppm"] = 1000001 }),
		"odd rounding":      rule(func(p map[string]any) { p["tax_rounding_unit_paise"] = 10 }),
		"nine surcharges":   rule(func(p map[string]any) { p["surcharge_bands"] = make([]any, 9) }),
		"missing year":      rule(func(p map[string]any) { delete(p, "tax_year") }),
		"null surcharges":   rule(func(p map[string]any) { p["surcharge_bands"] = nil }),
		"deduction too big": rule(func(p map[string]any) { p["standard_deduction_paise"] = 100000001 }),
	} {
		code, out := h.propose(uuid.NewString(), "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out = h.propose(old, "admin", with(incomeTaxRule("income_tax_old", 1, "2026-04-01", "2026-27"), map[string]any{"covered": false, "income_tax": nil, "citation": "Synthetic: old regime not offered"}))
	require.Equal(t, 200, code, "an uncovered regime is an explicit, cited choice: %v", out)

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var changes []string
		require.NoError(t, tx.Table("workforce_audit").Where("action LIKE ? AND changes->>'kind' LIKE 'income_tax_%'", "workforce.statutory_rule.%").Pluck("changes::text", &changes).Error)
		require.Len(t, changes, 7)
		for _, c := range changes {
			require.NotContains(t, c, "paise")
			require.NotContains(t, c, "2026-27")
		}
		var events []string
		require.NoError(t, tx.Table("outbox_events").Where("subject=? AND payload::text LIKE ?", workforce.StatutoryRuleChanged, "%income_tax_%").Pluck("payload::text", &events).Error)
		require.Len(t, events, 7)
		for _, e := range events {
			require.NotContains(t, e, "paise")
			require.NotContains(t, e, "Synthetic Finance Act")
		}
		return nil
	}))
}

func TestIncomeTaxRulesAreEnforcedByTheDatabase(t *testing.T) {
	h := newStatutoryHarness(t)
	id := uuid.NewString()
	revision := h.approved(id, incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-27"))
	// Each case writes a covered proposal outside the API; the deferred trigger must refuse it at commit.
	paramsKind, rebate := "income_tax_new", "NULL,NULL,NULL"
	write := func(effective, year string, rows func(tx *gorm.DB, revision uuid.UUID) error) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			revision := uuid.New()
			if err := tx.Exec(`INSERT INTO workforce_statutory_rule_revisions(tenant_id,hospital_id,id,rule_id,kind,sequence,base_version,status,name,covered,effective_on,citation,reference,proposed_by_subject,decision_note)
 SELECT tenant_id,hospital_id,?,id,kind,version+10,version,'pending','Synthetic',true,?,'Synthetic','','user-admin','' FROM workforce_statutory_rules WHERE tenant_id=? AND hospital_id=? AND id=?`, revision, effective, testutil.TenantA, h.hospital, id).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_income_tax_params(tenant_id,hospital_id,revision_id,kind,tax_year,standard_deduction_paise,taxable_rounding_mode,taxable_rounding_unit_paise,tax_rounding_mode,tax_rounding_unit_paise,rebate_income_limit_paise,rebate_max_paise,rebate_marginal_relief,cess_rate_ppm)
 VALUES(?,?,?,?,?,0,'nearest',1,'nearest',1,`+rebate+`,0)`, testutil.TenantA, h.hospital, revision, paramsKind, year).Error; err != nil {
				return err
			}
			return rows(tx, revision)
		})
	}
	sql := func(statements ...string) func(*gorm.DB, uuid.UUID) error {
		return func(tx *gorm.DB, revision uuid.UUID) error {
			for _, s := range statements {
				if err := tx.Exec(s, testutil.TenantA, h.hospital, revision).Error; err != nil {
					return err
				}
			}
			return nil
		}
	}
	band := func(age int) string {
		return fmt.Sprintf("INSERT INTO workforce_income_tax_age_bands(tenant_id,hospital_id,revision_id,min_age) VALUES(?,?,?,%d)", age)
	}
	slab := func(age, position int, bounds string) string {
		return fmt.Sprintf("INSERT INTO workforce_income_tax_slabs(tenant_id,hospital_id,revision_id,min_age,position,from_paise,to_paise,rate_ppm) VALUES(?,?,?,%d,%d,%s,0)", age, position, bounds)
	}
	surcharge := func(position int, above int64) string {
		return fmt.Sprintf("INSERT INTO workforce_income_tax_surcharges(tenant_id,hospital_id,revision_id,position,above_paise,rate_ppm) VALUES(?,?,?,%d,%d,0)", position, above)
	}
	complete := []string{band(0), slab(0, 1, "0,100"), slab(0, 2, "100,NULL")}
	for name, tc := range map[string]struct {
		effective, year string
		rows            []string
	}{
		"no bands":                 {"2026-04-01", "2026-27", nil},
		"band without slabs":       {"2026-04-01", "2026-27", []string{band(0)}},
		"no age 0 band":            {"2026-04-01", "2026-27", []string{band(60), slab(60, 1, "0,NULL")}},
		"slab gap":                 {"2026-04-01", "2026-27", []string{band(0), slab(0, 1, "0,100"), slab(0, 2, "101,NULL")}},
		"bounded last":             {"2026-04-01", "2026-27", []string{band(0), slab(0, 1, "0,100")}},
		"nonzero start":            {"2026-04-01", "2026-27", []string{band(0), slab(0, 1, "1,NULL")}},
		"second band incomplete":   {"2026-04-01", "2026-27", append(complete, band(60), slab(60, 1, "0,100"))},
		"effective outside year":   {"2027-04-01", "2026-27", complete},
		"equal surcharge":          {"2026-04-01", "2026-27", append(complete, surcharge(1, 500), surcharge(2, 500))},
		"falling surcharge":        {"2026-04-01", "2026-27", append(complete, surcharge(1, 500), surcharge(2, 400))},
		"surcharge order by entry": {"2026-04-01", "2026-27", append(complete, surcharge(2, 400), surcharge(1, 500))},
	} {
		require.ErrorContains(t, write(tc.effective, tc.year, sql(tc.rows...)), "income-tax rules need", name)
	}
	require.ErrorContains(t, write("2026-04-01", "2026-28", sql(complete...)), "workforce_income_tax_params_tax_year_check")
	paramsKind = "income_tax_old"
	require.ErrorContains(t, write("2026-04-01", "2026-27", sql(complete...)), "foreign key", "parameters are pinned to their revision's regime")
	paramsKind = "income_tax_new"
	for _, partial := range []string{"1,1,NULL", "NULL,1,true", "1,NULL,NULL"} {
		rebate = partial
		require.ErrorContains(t, write("2026-04-01", "2026-27", sql(complete...)), "workforce_income_tax_rebate", partial)
	}
	rebate = "1,1,true"
	require.NoError(t, write("2027-03-31", "2026-27", sql(append(complete, band(60), slab(60, 1, "0,NULL"), surcharge(1, 400), surcharge(2, 500))...)), "a complete rule commits under the same session")

	run := func(statement string) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement).Error
		})
	}
	for name, tc := range map[string]struct{ statement, guard string }{
		"slabs are immutable":      {"UPDATE workforce_income_tax_slabs SET rate_ppm=1 WHERE revision_id='" + revision + "'", "append only"},
		"bands are kept":           {"DELETE FROM workforce_income_tax_age_bands", "append only"},
		"surcharges are immutable": {"UPDATE workforce_income_tax_surcharges SET rate_ppm=1", "append only"},
		"params are immutable":     {"UPDATE workforce_income_tax_params SET cess_rate_ppm=1 WHERE revision_id='" + revision + "'", "append only"},
		"slabs added later":        {"INSERT INTO workforce_income_tax_slabs(tenant_id,hospital_id,revision_id,min_age,position,from_paise,to_paise,rate_ppm) SELECT tenant_id,hospital_id,revision_id,0,9,0,NULL,0 FROM workforce_income_tax_params WHERE revision_id='" + revision + "'", "revision creating transaction"},
	} {
		require.ErrorContains(t, run(tc.statement), tc.guard, name)
	}
}

func TestIncomeTaxPreviewReportsEachRegime(t *testing.T) {
	h := newStatutoryHarness(t)
	preview := func(user string, body map[string]any) (int, map[string]map[string]any) {
		code, out := h.call("POST", "/income-tax-preview", user, body)
		if code != 200 {
			return code, map[string]map[string]any{"error": out}
		}
		got := map[string]map[string]any{}
		for _, r := range out["results"].([]any) {
			got[r.(map[string]any)["kind"].(string)] = r.(map[string]any)
		}
		return code, got
	}
	// ₹2.6 lakh salary: ₹2.5 lakh taxable after the ₹10,000 deduction.
	body := map[string]any{"on": "2026-10-01", "annual_income_paise": 26000000}
	code, got := preview("viewer", body)
	require.Equal(t, 200, code, got)
	require.Len(t, got, 2, "only the two regimes, never the monthly kinds")
	for _, kind := range []string{"income_tax_new", "income_tax_old"} {
		require.Equal(t, "not_configured", got[kind]["status"], "nothing is defaulted: %s", kind)
		require.EqualValues(t, 0, got[kind]["total_paise"])
	}
	newRegime := h.approved(uuid.NewString(), incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-27"))
	single := withParams(incomeTaxRule("income_tax_old", 0, "2026-04-01", "2026-27"), "income_tax", map[string]any{"age_bands": []any{map[string]any{"min_age": 0, "slabs": []any{taxSlab(0, nil, 0)}}}})
	h.approved(uuid.NewString(), with(single, map[string]any{"covered": false, "income_tax": nil}))

	code, got = preview("viewer", body)
	require.Equal(t, 200, code, got)
	require.Equal(t, "age_required", got["income_tax_new"]["status"], "two age bands and no age: nothing is guessed")
	require.Equal(t, "not_covered", got["income_tax_old"]["status"])
	code, got = preview("viewer", with(body, map[string]any{"age": 40}))
	require.Equal(t, 200, code, got)
	res := got["income_tax_new"]
	require.Equal(t, "applied", res["status"])
	require.Equal(t, newRegime, res["revision_id"])
	require.Equal(t, "2026-27", res["tax_year"])
	require.EqualValues(t, 0, res["age_band_min_age"])
	require.EqualValues(t, 1000000, res["standard_deduction_paise"])
	require.EqualValues(t, 25000000, res["taxable_paise"])
	require.EqualValues(t, 1500000, res["slab_tax_paise"])
	require.EqualValues(t, 0, res["rebate_paise"], "above the rebate limit without enough relief")
	require.EqualValues(t, 0, res["surcharge_paise"])
	require.EqualValues(t, 75000, res["cess_paise"])
	require.EqualValues(t, 1575000, res["total_paise"])
	code, got = preview("viewer", with(body, map[string]any{"age": 60}))
	require.Equal(t, 200, code, got)
	require.EqualValues(t, 60, got["income_tax_new"]["age_band_min_age"])
	require.EqualValues(t, 500000, got["income_tax_new"]["slab_tax_paise"])

	code, got = preview("viewer", map[string]any{"on": "2027-04-01", "annual_income_paise": 26000000, "age": 40})
	require.Equal(t, 200, code, got)
	require.Equal(t, "tax_year_missing", got["income_tax_new"]["status"], "last year's rates never roll into a new tax year")
	require.Equal(t, "2026-27", got["income_tax_new"]["tax_year"])
	require.EqualValues(t, 0, got["income_tax_new"]["total_paise"])
	code, got = preview("viewer", map[string]any{"on": "2026-03-31", "annual_income_paise": 26000000, "age": 40})
	require.Equal(t, 200, code, got)
	require.Equal(t, "not_configured", got["income_tax_new"]["status"], "no revision is in force before its effective date")

	for name, bad := range map[string]map[string]any{
		"impossible date": {"on": "2026-02-30", "annual_income_paise": 0},
		"no income":       {"on": "2026-10-01"},
		"negative income": {"on": "2026-10-01", "annual_income_paise": -1},
		"age above 120":   {"on": "2026-10-01", "annual_income_paise": 0, "age": 121},
	} {
		code, out := h.call("POST", "/income-tax-preview", "viewer", bad)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	for user, status := range map[string]int{"directory": 403, "foreign": 404} {
		code, _ := preview(user, body)
		require.Equal(t, status, code, user)
	}
	code, raw := h.do("POST", h.other+"/hr/payroll/income-tax-preview", "admin", `{"on":"2026-10-01","annual_income_paise":0}`)
	require.Equal(t, 404, code, raw)

	code, out := h.call("POST", "/statutory-preview", "viewer", map[string]any{"structure_id": uuid.NewString(), "on": "2026-10-01", "payable_half_days": 0, "disability": false})
	require.Equal(t, 404, code, "the monthly preview still needs a structure: %v", out)
}
