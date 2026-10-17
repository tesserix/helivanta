package workforce_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

// Synthetic claim heads only: HRA at 10%/50%/40%, a ₹15,000 group over two investments, a ₹5,000 medical cap and ₹20,000 interest.
func claimHeads() map[string]any {
	return map[string]any{
		"claim_groups": []any{map[string]any{"code": "invest", "label": "Investments", "cap_paise": 1500000}},
		"claims": []any{
			map[string]any{"code": "HRA", "label": "House rent", "kind": "hra", "hra_rent_excess_rate_ppm": 100000, "hra_metro_rate_ppm": 500000, "hra_other_rate_ppm": 400000, "landlord_pan_rent_above_paise": 10000000},
			map[string]any{"code": " savings ", "label": "Savings", "kind": "deduction", "cap_paise": 1200000, "cap_group": "INVEST"},
			map[string]any{"code": "PENSION", "label": "Pension", "kind": "deduction", "cap_group": "invest"},
			map[string]any{"code": "MEDICAL", "label": "Medical", "kind": "deduction", "cap_paise": 500000},
			map[string]any{"code": "HOMELOAN", "label": "Home loan interest", "kind": "house_property_interest", "cap_paise": 2000000},
		},
	}
}

func incomeTaxRuleWithClaims(kind string, version int) map[string]any {
	return withParams(incomeTaxRule(kind, version, "2026-04-01", "2026-27"), "income_tax", claimHeads())
}

func TestIncomeTaxClaimsFollowTheLifecycle(t *testing.T) {
	h := newStatutoryHarness(t)
	id := uuid.NewString()
	h.approved(id, incomeTaxRuleWithClaims("income_tax_old", 0))
	code, out := h.call("GET", "/statutory-rules/"+id+"/history", "viewer", nil)
	require.Equal(t, 200, code, out)
	params := out["data"].([]any)[0].(map[string]any)["income_tax"].(map[string]any)
	require.Equal(t, []any{map[string]any{"code": "INVEST", "label": "Investments", "cap_paise": float64(1500000)}}, params["claim_groups"])
	var codes []string
	for _, c := range params["claims"].([]any) {
		codes = append(codes, c.(map[string]any)["code"].(string))
	}
	require.Equal(t, []string{"HRA", "SAVINGS", "PENSION", "MEDICAL", "HOMELOAN"}, codes, "claims come back in entry order, codes upper-cased")
	hra := params["claims"].([]any)[0].(map[string]any)
	require.EqualValues(t, 10000000, hra["landlord_pan_rent_above_paise"])
	require.NotContains(t, hra, "cap_paise", "fields of another kind stay absent")
	require.Equal(t, "INVEST", params["claims"].([]any)[2].(map[string]any)["cap_group"])

	code, out = h.propose(id, "admin", incomeTaxRuleWithClaims("income_tax_old", 1))
	require.Equal(t, 200, code, "a revision with claims replays: %v", out)
	require.Nil(t, out["pending"])
	code, out = h.propose(id, "admin", withParams(incomeTaxRuleWithClaims("income_tax_old", 1), "income_tax", map[string]any{"claim_groups": []any{map[string]any{"code": "INVEST", "label": "Investments", "cap_paise": 1500001}}}))
	require.Equal(t, 200, code, out)
	require.NotNil(t, out["pending"], "a changed group cap is a new proposal")

	claimsError := "Review the claims"
	claims := func(change func(groups, claims []any) ([]any, []any)) map[string]any {
		heads := claimHeads()
		groups, list := change(heads["claim_groups"].([]any), heads["claims"].([]any))
		return withParams(incomeTaxRule("income_tax_old", 0, "2026-04-01", "2026-27"), "income_tax", map[string]any{"claim_groups": groups, "claims": list})
	}
	field := func(i int, key string, value any) func(groups, claims []any) ([]any, []any) {
		return func(groups, list []any) ([]any, []any) {
			c := with(list[i].(map[string]any), map[string]any{key: value})
			if value == nil {
				delete(c, key)
			}
			list[i] = c
			return groups, list
		}
	}
	for name, change := range map[string]func(groups, claims []any) ([]any, []any){
		"duplicate code":       field(3, "code", "savings"),
		"unknown group":        field(1, "cap_group", "OTHER"),
		"interest without cap": field(4, "cap_paise", nil),
		"interest in a group":  field(4, "cap_group", "INVEST"),
		"HRA with a cap":       field(0, "cap_paise", 1),
		"HRA without a rate":   field(0, "hra_metro_rate_ppm", nil),
		"deduction HRA field":  field(3, "hra_other_rate_ppm", 1),
		"code with a space":    field(3, "code", "MED ICAL"),
		"blank label":          field(3, "label", "  "),
		"unused group": func(_, list []any) ([]any, []any) {
			return []any{map[string]any{"code": "SPARE", "label": "Spare", "cap_paise": 1}}, list[3:]
		},
		"duplicate group": func(groups, list []any) ([]any, []any) { return append(groups, groups[0]), list },
		"second HRA": func(groups, list []any) ([]any, []any) {
			return groups, append(list, with(list[0].(map[string]any), map[string]any{"code": "HRA2"}))
		},
		"second interest": func(groups, list []any) ([]any, []any) {
			return groups, append(list, with(list[4].(map[string]any), map[string]any{"code": "LOAN2"}))
		},
	} {
		code, out := h.propose(uuid.NewString(), "admin", claims(change))
		require.Equal(t, 400, code, "%s: %v", name, out)
		require.Contains(t, fmt.Sprint(out), claimsError, name)
	}
	for name, body := range map[string]map[string]any{
		"unknown kind":    claims(field(3, "kind", "lta")),
		"nine groups":     claims(func(_, list []any) ([]any, []any) { return make([]any, 9), list }),
		"25 claims":       claims(func(groups, _ []any) ([]any, []any) { return groups, make([]any, 25) }),
		"rate above 100%": claims(field(0, "hra_metro_rate_ppm", 1000001)),
		"cap above ₹1 cr": claims(field(3, "cap_paise", 1000000001)),
		"long label":      claims(field(3, "label", string(make([]byte, 101)))),
		"group cap absent": claims(func(_, list []any) ([]any, []any) {
			return []any{map[string]any{"code": "INVEST", "label": "Investments"}}, list
		}),
	} {
		code, out := h.propose(uuid.NewString(), "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
}

func TestIncomeTaxClaimsAreEnforcedByTheDatabase(t *testing.T) {
	h := newStatutoryHarness(t)
	id := uuid.NewString()
	revision := h.approved(id, incomeTaxRuleWithClaims("income_tax_old", 0))
	// Each case writes a complete covered proposal outside the API with the given claim rows; the tables or trigger must refuse it.
	write := func(rows ...string) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			revision := uuid.New()
			statements := append([]string{
				`INSERT INTO workforce_statutory_rule_revisions(tenant_id,hospital_id,id,rule_id,kind,sequence,base_version,status,name,covered,effective_on,citation,reference,proposed_by_subject,decision_note)
 SELECT tenant_id,hospital_id,@revision,id,kind,version+10,version,'pending','Synthetic',true,'2026-04-01','Synthetic','','user-admin','' FROM workforce_statutory_rules WHERE tenant_id=@tenant AND hospital_id=@hospital AND id=@rule`,
				`INSERT INTO workforce_income_tax_params(tenant_id,hospital_id,revision_id,kind,tax_year,standard_deduction_paise,taxable_rounding_mode,taxable_rounding_unit_paise,tax_rounding_mode,tax_rounding_unit_paise,cess_rate_ppm)
 VALUES(@tenant,@hospital,@revision,'income_tax_old','2026-27',0,'nearest',1,'nearest',1,0)`,
				`INSERT INTO workforce_income_tax_age_bands(tenant_id,hospital_id,revision_id,min_age) VALUES(@tenant,@hospital,@revision,0)`,
				`INSERT INTO workforce_income_tax_slabs(tenant_id,hospital_id,revision_id,min_age,position,from_paise,to_paise,rate_ppm) VALUES(@tenant,@hospital,@revision,0,1,0,NULL,0)`,
			}, rows...)
			for _, s := range statements {
				if err := tx.Exec(s, map[string]any{"tenant": testutil.TenantA, "hospital": h.hospital, "revision": revision, "rule": id}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	group := func(position int, code string) string {
		return fmt.Sprintf("INSERT INTO workforce_income_tax_claim_groups(tenant_id,hospital_id,revision_id,position,code,label,cap_paise) VALUES(@tenant,@hospital,@revision,%d,'%s','Group',100)", position, code)
	}
	claim := func(position int, code, kind, fields string) string {
		return fmt.Sprintf(`INSERT INTO workforce_income_tax_claims(tenant_id,hospital_id,revision_id,position,code,label,kind,cap_paise,cap_group,hra_rent_excess_rate_ppm,hra_metro_rate_ppm,hra_other_rate_ppm,landlord_pan_rent_above_paise)
 VALUES(@tenant,@hospital,@revision,%d,'%s','Claim','%s',%s)`, position, code, kind, fields)
	}
	hraFields, interestFields := "NULL,NULL,1,1,1,1", "100,NULL,NULL,NULL,NULL,NULL"
	deduction := func(position int, code, cap, group string) string {
		return claim(position, code, "deduction", cap+","+group+",NULL,NULL,NULL,NULL")
	}
	for name, tc := range map[string]struct {
		rows  []string
		guard string
	}{
		"unused group":          {[]string{group(1, "INVEST"), deduction(1, "SAVINGS", "100", "NULL")}, "every income-tax claim group caps at least one claim"},
		"unknown group":         {[]string{deduction(1, "SAVINGS", "NULL", "'INVEST'")}, "workforce_income_tax_claim_group"},
		"duplicate code":        {[]string{deduction(1, "SAVINGS", "NULL", "NULL"), deduction(2, "SAVINGS", "100", "NULL")}, "workforce_income_tax_claim_code"},
		"duplicate group code":  {[]string{group(1, "INVEST"), group(2, "INVEST")}, "workforce_income_tax_claim_group_code"},
		"second HRA":            {[]string{claim(1, "HRA", "hra", hraFields), claim(2, "HRA2", "hra", hraFields)}, "workforce_income_tax_claim_one_kind"},
		"second interest":       {[]string{claim(1, "LOAN", "house_property_interest", interestFields), claim(2, "LOAN2", "house_property_interest", interestFields)}, "workforce_income_tax_claim_one_kind"},
		"HRA with a cap":        {[]string{claim(1, "HRA", "hra", "100,NULL,1,1,1,1")}, "workforce_income_tax_claim_fields"},
		"HRA without PAN limit": {[]string{claim(1, "HRA", "hra", "NULL,NULL,1,1,1,NULL")}, "workforce_income_tax_claim_fields"},
		"interest without cap":  {[]string{claim(1, "LOAN", "house_property_interest", "NULL,NULL,NULL,NULL,NULL,NULL")}, "workforce_income_tax_claim_fields"},
		"interest in a group":   {[]string{group(1, "INVEST"), claim(1, "LOAN", "house_property_interest", "100,'INVEST',NULL,NULL,NULL,NULL")}, "workforce_income_tax_claim_fields"},
		"deduction HRA field":   {[]string{claim(1, "MEDICAL", "deduction", "NULL,NULL,1,NULL,NULL,NULL")}, "workforce_income_tax_claim_fields"},
		"lower-case code":       {[]string{deduction(1, "savings", "NULL", "NULL")}, "workforce_income_tax_claims_code_check"},
		"unknown kind":          {[]string{claim(1, "LTA", "lta", "NULL,NULL,NULL,NULL,NULL,NULL")}, "workforce_income_tax_claims_kind_check"},
	} {
		require.ErrorContains(t, write(tc.rows...), tc.guard, name)
	}
	require.NoError(t, write(group(1, "INVEST"), claim(1, "HRA", "hra", hraFields), deduction(2, "SAVINGS", "NULL", "'INVEST'"), claim(3, "LOAN", "house_property_interest", interestFields)),
		"a complete set of claims commits under the same session")

	run := func(tenant, statement string) error {
		return h.db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement).Error
		})
	}
	for name, tc := range map[string]struct{ statement, guard string }{
		"claims are immutable": {"UPDATE workforce_income_tax_claims SET cap_paise=1 WHERE revision_id='" + revision + "'", "append only"},
		"groups are kept":      {"DELETE FROM workforce_income_tax_claim_groups", "append only"},
		"claims added later": {"INSERT INTO workforce_income_tax_claims(tenant_id,hospital_id,revision_id,position,code,label,kind) SELECT tenant_id,hospital_id,revision_id,9,'LATE','Late','deduction' FROM workforce_income_tax_params WHERE revision_id='" + revision + "'",
			"revision creating transaction"},
	} {
		require.ErrorContains(t, run(testutil.TenantA, tc.statement), tc.guard, name)
	}
	var visible int64
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Raw("SELECT (SELECT count(*) FROM workforce_income_tax_claims)+(SELECT count(*) FROM workforce_income_tax_claim_groups)").Scan(&visible).Error
	}))
	require.Zero(t, visible, "another tenant sees no claim heads")
}

func TestIncomeTaxPreviewAppliesClaimsPerRegime(t *testing.T) {
	h := newStatutoryHarness(t)
	h.approved(uuid.NewString(), incomeTaxRule("income_tax_new", 0, "2026-04-01", "2026-27"))
	old := h.approved(uuid.NewString(), incomeTaxRuleWithClaims("income_tax_old", 0))
	body := map[string]any{"on": "2026-10-01", "annual_income_paise": 30000000, "age": 40,
		"claims": []any{map[string]any{"code": "medical", "amount_paise": 300000}, map[string]any{"code": "PENSION", "amount_paise": 800000}, map[string]any{"code": "UNKNOWN", "amount_paise": 100000},
			map[string]any{"code": "HOMELOAN", "amount_paise": 2500000}, map[string]any{"code": "SAVINGS", "amount_paise": 1500000}},
		"hra": map[string]any{"received_paise": 300000, "salary_paise": 1000000, "rent_paid_paise": 600000, "metro": true}}
	code, out := h.call("POST", "/income-tax-preview", "viewer", body)
	require.Equal(t, 200, code, out)
	got := map[string]map[string]any{}
	for _, r := range out["results"].([]any) {
		got[r.(map[string]any)["kind"].(string)] = r.(map[string]any)
	}
	res := got["income_tax_old"]
	require.Equal(t, old, res["revision_id"])
	require.Equal(t, "applied", res["status"])
	require.EqualValues(t, 300000, res["exemption_paise"])
	require.EqualValues(t, 2000000, res["house_property_loss_paise"])
	require.EqualValues(t, 1800000, res["deductions_paise"])
	require.EqualValues(t, 30000000-300000-1000000-2000000-1800000, res["taxable_paise"])
	statuses := map[string]any{}
	for _, l := range res["claims"].([]any) {
		statuses[l.(map[string]any)["code"].(string)] = l.(map[string]any)["status"]
	}
	require.Equal(t, map[string]any{"HRA": "allowed", "SAVINGS": "capped", "PENSION": "capped", "MEDICAL": "allowed", "HOMELOAN": "capped", "UNKNOWN": "not_allowed"}, statuses)

	res = got["income_tax_new"]
	require.EqualValues(t, 30000000-1000000, res["taxable_paise"], "a regime without the heads allows none of the claims")
	require.EqualValues(t, 0, res["exemption_paise"])
	require.Len(t, res["claims"], 6)
	for _, l := range res["claims"].([]any) {
		require.Equal(t, "not_allowed", l.(map[string]any)["status"], l)
	}

	for name, bad := range map[string]map[string]any{
		"duplicate claim":      with(body, map[string]any{"claims": []any{map[string]any{"code": "MEDICAL", "amount_paise": 1}, map[string]any{"code": "medical", "amount_paise": 1}}}),
		"negative amount":      with(body, map[string]any{"claims": []any{map[string]any{"code": "MEDICAL", "amount_paise": -1}}}),
		"claim without amount": with(body, map[string]any{"claims": []any{map[string]any{"code": "MEDICAL"}}}),
		"partial HRA":          with(body, map[string]any{"hra": map[string]any{"received_paise": 1}}),
		"bad code":             with(body, map[string]any{"claims": []any{map[string]any{"code": "MED ICAL", "amount_paise": 1}}}),
	} {
		code, out := h.call("POST", "/income-tax-preview", "viewer", bad)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
}
