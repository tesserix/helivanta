package workforce_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

var everyGender = []string{"female", "male", "other"}

func ptSlab(months []int, genders []string, from int64, to any, amount int64) map[string]any {
	return map[string]any{"months": months, "genders": genders, "from_paise": from, "to_paise": to, "amount_paise": amount}
}

// Synthetic values only: an eleven-month band, a February band and a gender split that prove behaviour, never a state's schedule.
func ptRule(version int, effective string) map[string]any {
	rest := []int{12, 1, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	return map[string]any{"expected_version": version, "kind": "pt", "name": "Synthetic PT", "covered": true, "effective_on": effective, "citation": "Synthetic PT notification", "reference": "",
		"pt": map[string]any{"state_code": "IN-ZZ", "wage_basis": "gross", "slabs": []any{
			ptSlab(rest, []string{"other", "male"}, 0, 1000000, 0),
			ptSlab(rest, []string{"female"}, 0, 1500000, 0),
			ptSlab(rest, []string{"male", "other"}, 1000000, 1500000, 17500),
			ptSlab(rest, everyGender, 1500000, nil, 20000),
			ptSlab([]int{2}, everyGender, 0, 1000000, 0),
			ptSlab([]int{2}, everyGender, 1000000, nil, 30000),
		}}}
}

func lwfRule(version int, effective string) map[string]any {
	return map[string]any{"expected_version": version, "kind": "lwf", "name": "Synthetic LWF", "covered": true, "effective_on": effective, "citation": "Synthetic LWF notification", "reference": "",
		"lwf": map[string]any{"state_code": "IN-ZZ", "wage_basis": "gross", "deduction_months": []int{12, 6}, "rounding_mode": "nearest", "rounding_unit_paise": 1, "slabs": []any{
			map[string]any{"from_paise": 0, "to_paise": 300000, "employee_fixed_paise": 0, "employee_rate_ppm": 0, "employer_fixed_paise": 0, "employer_rate_ppm": 0},
			map[string]any{"from_paise": 300000, "to_paise": nil, "employee_fixed_paise": 0, "employee_rate_ppm": 2000, "employee_cap_paise": 2000, "employer_fixed_paise": 500, "employer_rate_ppm": 4000},
		}}}
}

func withSlabs(body map[string]any, key string, slabs ...any) map[string]any {
	return withParams(body, key, map[string]any{"slabs": slabs})
}

func TestPTAndLWFRulesNeedCompleteSlabs(t *testing.T) {
	h := newStatutoryHarness(t)
	pt := uuid.NewString()
	h.approved(pt, ptRule(0, "2026-04-01"))
	code, rule := h.call("GET", "/statutory-rules/"+pt+"/history", "viewer", nil)
	require.Equal(t, 200, code, rule)
	params := rule["data"].([]any)[0].(map[string]any)["pt"].(map[string]any)
	require.Equal(t, "IN-ZZ", params["state_code"])
	first := params["slabs"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"male", "other"}, first["genders"], "genders are stored as a set in a fixed order")
	require.Equal(t, []any{1.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0, 11.0, 12.0}, first["months"])
	require.Len(t, params["slabs"], 6, "slabs come back in entry order")
	require.Nil(t, params["slabs"].([]any)[3].(map[string]any)["to_paise"])
	code, out := h.propose(pt, "admin", ptRule(1, "2026-04-01"))
	require.Equal(t, 200, code, "the revision in force replays despite set order: %v", out)
	require.Nil(t, out["pending"])

	lwf := uuid.NewString()
	h.approved(lwf, lwfRule(0, "2026-04-01"))
	code, out = h.propose(lwf, "admin", lwfRule(1, "2026-04-01"))
	require.Equal(t, 200, code, out)
	require.Nil(t, out["pending"], "a replay of the LWF revision in force")

	slabsError := "Review the slabs"
	ruleError := "Review the rule"
	rest := []int{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	for name, tc := range map[string]struct {
		body map[string]any
		want string
	}{
		"PT gap":                             {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, everyGender, 0, 1000000, 0), ptSlab(allMonthsList, everyGender, 1000001, nil, 1)), slabsError},
		"PT overlap":                         {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, everyGender, 0, 1000000, 0), ptSlab(allMonthsList, everyGender, 999999, nil, 1)), slabsError},
		"PT gender missing in a named month": {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(rest, everyGender, 0, nil, 0), ptSlab([]int{2}, []string{"female", "male"}, 0, nil, 1)), slabsError},
		"PT bounded last":                    {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, everyGender, 0, 1000000, 0)), slabsError},
		"PT nonzero start":                   {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, everyGender, 1, nil, 0)), slabsError},
		"LWF gap":                            {withSlabs(lwfRule(0, "2026-04-01"), "lwf", map[string]any{"from_paise": 1, "employee_fixed_paise": 0, "employee_rate_ppm": 0, "employer_fixed_paise": 0, "employer_rate_ppm": 0}), slabsError},
		"PT repeated month":                  {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab([]int{1, 1}, everyGender, 0, nil, 0)), ruleError},
		"PT repeated gender":                 {withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, []string{"female", "female", "male"}, 0, nil, 0)), ruleError},
		"LWF repeated month":                 {withParams(lwfRule(0, "2026-04-01"), "lwf", map[string]any{"deduction_months": []int{6, 6}}), ruleError},
		"state code":                         {withParams(ptRule(0, "2026-04-01"), "pt", map[string]any{"state_code": "IN-mh"}), ruleError},
		"PT parameters on LWF":               {with(lwfRule(0, "2026-04-01"), map[string]any{"pt": ptRule(0, "2026-04-01")["pt"]}), ruleError},
		"covered PT without parameters":      {with(ptRule(0, "2026-04-01"), map[string]any{"pt": nil}), ruleError},
	} {
		code, out := h.propose(uuid.NewString(), "admin", tc.body)
		require.Equal(t, 400, code, "%s: %v", name, out)
		require.Contains(t, fmt.Sprint(out), tc.want, name)
	}
	for name, body := range map[string]map[string]any{
		"unknown gender": withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, []string{"unknown"}, 0, nil, 0)),
		"month 13":       withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab([]int{13}, everyGender, 0, nil, 0)),
		"no slabs":       withSlabs(ptRule(0, "2026-04-01"), "pt"),
		"amount too big": withSlabs(ptRule(0, "2026-04-01"), "pt", ptSlab(allMonthsList, everyGender, 0, nil, 10000001)),
	} {
		code, out := h.propose(uuid.NewString(), "admin", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out = h.propose(pt, "admin", with(ptRule(1, "2026-05-01"), map[string]any{"covered": false, "pt": nil, "citation": "No professional tax in this synthetic state"}))
	require.Equal(t, 200, code, "no PT is an explicit, cited choice: %v", out)

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var changes []string
		require.NoError(t, tx.Table("workforce_audit").Where("action LIKE ? AND changes->>'kind' IN ('pt','lwf')", "workforce.statutory_rule.%").Pluck("changes::text", &changes).Error)
		require.Len(t, changes, 5)
		for _, c := range changes {
			require.NotContains(t, c, "paise")
			require.NotContains(t, c, "IN-ZZ")
		}
		return nil
	}))
}

var allMonthsList = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

func TestPTAndLWFSlabsAreEnforcedByTheDatabase(t *testing.T) {
	h := newStatutoryHarness(t)
	pt, lwf := uuid.NewString(), uuid.NewString()
	ptRevision := h.approved(pt, ptRule(0, "2026-04-01"))
	h.approved(lwf, lwfRule(0, "2026-04-01"))
	// Each case writes a covered proposal outside the API; the deferred trigger must refuse it at commit.
	write := func(kind string, slabs func(tx *gorm.DB, revision uuid.UUID) error) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			revision := uuid.New()
			if err := tx.Exec(`INSERT INTO workforce_statutory_rule_revisions(tenant_id,hospital_id,id,rule_id,kind,sequence,base_version,status,name,covered,effective_on,citation,reference,proposed_by_subject,decision_note)
 SELECT tenant_id,hospital_id,?,id,kind,version+10,version,'pending','Synthetic',true,'2026-04-01','Synthetic','','user-admin','' FROM workforce_statutory_rules WHERE tenant_id=? AND hospital_id=? AND kind=?`, revision, testutil.TenantA, h.hospital, kind).Error; err != nil {
				return err
			}
			params := map[string]string{
				"pt":  `INSERT INTO workforce_statutory_pt_params(tenant_id,hospital_id,revision_id,state_code,wage_basis) VALUES(?,?,?,'IN-ZZ','gross')`,
				"lwf": `INSERT INTO workforce_statutory_lwf_params(tenant_id,hospital_id,revision_id,state_code,wage_basis,deduction_mask,rounding_mode,rounding_unit_paise) VALUES(?,?,?,'IN-ZZ','gross',1,'nearest',1)`,
			}[kind]
			if err := tx.Exec(params, testutil.TenantA, h.hospital, revision).Error; err != nil {
				return err
			}
			return slabs(tx, revision)
		})
	}
	ptSlabs := func(rows ...string) func(*gorm.DB, uuid.UUID) error {
		return func(tx *gorm.DB, revision uuid.UUID) error {
			for i, r := range rows {
				if err := tx.Exec(`INSERT INTO workforce_statutory_pt_slabs(tenant_id,hospital_id,revision_id,position,months_mask,genders_mask,from_paise,to_paise,amount_paise) VALUES(?,?,?,?,`+r+`)`, testutil.TenantA, h.hospital, revision, i+1).Error; err != nil {
					return err
				}
			}
			return nil
		}
	}
	lwfSlabs := func(rows ...string) func(*gorm.DB, uuid.UUID) error {
		return func(tx *gorm.DB, revision uuid.UUID) error {
			for i, r := range rows {
				if err := tx.Exec(`INSERT INTO workforce_statutory_lwf_slabs(tenant_id,hospital_id,revision_id,position,from_paise,to_paise,employee_fixed_paise,employee_rate_ppm,employer_fixed_paise,employer_rate_ppm) VALUES(?,?,?,?,`+r+`,0,0,0,0)`, testutil.TenantA, h.hospital, revision, i+1).Error; err != nil {
					return err
				}
			}
			return nil
		}
	}
	for name, tc := range map[string]struct {
		kind  string
		slabs func(*gorm.DB, uuid.UUID) error
	}{
		"PT without slabs":         {"pt", ptSlabs()},
		"PT gap":                   {"pt", ptSlabs("4095,7,0,100,0", "4095,7,101,NULL,1")},
		"PT overlap":               {"pt", ptSlabs("4095,7,0,100,0", "4095,7,99,NULL,1")},
		"PT gender missing":        {"pt", ptSlabs("4095,3,0,NULL,0")},
		"PT gender missing in Feb": {"pt", ptSlabs("4093,7,0,NULL,0", "2,6,0,NULL,1")},
		"PT bounded last":          {"pt", ptSlabs("4095,7,0,100,0")},
		"PT nonzero start":         {"pt", ptSlabs("4095,7,1,NULL,0")},
		"LWF without slabs":        {"lwf", lwfSlabs()},
		"LWF bounded last":         {"lwf", lwfSlabs("0,100")},
		"LWF two open-ended":       {"lwf", lwfSlabs("0,NULL", "100,NULL")},
	} {
		require.ErrorContains(t, write(tc.kind, tc.slabs), "slabs must cover every wage once", name)
	}
	require.NoError(t, write("pt", ptSlabs("4093,7,0,NULL,0", "2,7,0,100,0", "2,7,100,NULL,1")), "a complete table commits under the same session")
	require.NoError(t, write("lwf", lwfSlabs("100,NULL", "0,100")), "entry order is not band order")

	run := func(statement string) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement).Error
		})
	}
	for name, tc := range map[string]struct{ statement, guard string }{
		"slabs are immutable":  {"UPDATE workforce_statutory_pt_slabs SET amount_paise=1 WHERE revision_id='" + ptRevision + "'", "append only"},
		"slabs are kept":       {"DELETE FROM workforce_statutory_lwf_slabs", "append only"},
		"params are immutable": {"UPDATE workforce_statutory_pt_params SET state_code='IN-AA' WHERE revision_id='" + ptRevision + "'", "append only"},
		"slabs added later":    {"INSERT INTO workforce_statutory_pt_slabs(tenant_id,hospital_id,revision_id,position,months_mask,genders_mask,from_paise,to_paise,amount_paise) SELECT tenant_id,hospital_id,revision_id,50,1,1,0,NULL,0 FROM workforce_statutory_pt_params WHERE revision_id='" + ptRevision + "'", "revision creating transaction"},
		"unknown kind":         {"INSERT INTO workforce_statutory_rules(tenant_id,hospital_id,id,kind,version) VALUES('" + testutil.TenantA + "','" + h.hospital + "',gen_random_uuid(),'gratuity',0)", "workforce_statutory_rules_kind_check"},
	} {
		require.ErrorContains(t, run(tc.statement), tc.guard, name)
	}
}

func TestStatutoryPreviewAppliesPTAndLWF(t *testing.T) {
	h := newStatutoryHarness(t)
	basic := h.head(fixedEarning("SYN-BASIC", "wages"))
	structure := uuid.NewString()
	code, out := h.putStructure(structure, structureBody("SYN-NURSE", fixedLine(basic, 1200000)))
	require.Equal(t, 200, code, out)
	// Every preview pays the full month, so wages stay at the structure's 12,000.
	preview := func(on string, gender any) map[string]map[string]any {
		day, err := time.Parse(time.DateOnly, on)
		require.NoError(t, err)
		body := map[string]any{"structure_id": structure, "on": on, "payable_half_days": 2 * time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day(), "disability": false}
		if gender != nil {
			body["gender"] = gender
		}
		code, out := h.call("POST", "/statutory-preview", "viewer", body)
		require.Equal(t, 200, code, out)
		got := map[string]map[string]any{"totals": out}
		for _, r := range out["results"].([]any) {
			got[r.(map[string]any)["kind"].(string)] = r.(map[string]any)
		}
		return got
	}
	got := preview("2026-03-16", nil)
	require.Equal(t, "not_configured", got["pt"]["status"])
	require.Equal(t, "not_configured", got["lwf"]["status"])
	h.approved(uuid.NewString(), ptRule(0, "2026-01-01"))
	lwfRevision := h.approved(uuid.NewString(), lwfRule(0, "2026-01-01"))

	got = preview("2026-03-16", nil)
	require.Equal(t, "gender_required", got["pt"]["status"], "12,000 sits in the band that differs by gender")
	require.Empty(t, got["pt"]["components"])
	require.Equal(t, "not_due", got["lwf"]["status"], "March is not a deduction month")
	require.EqualValues(t, 0, got["totals"]["employee_paise"])
	got = preview("2026-03-16", "male")
	require.Equal(t, "applied", got["pt"]["status"])
	require.EqualValues(t, 1200000, got["pt"]["wages_paise"])
	require.Equal(t, map[string]int64{"pt_employee": 17500}, componentAmounts(got["pt"]))
	got = preview("2026-03-16", "female")
	require.Equal(t, map[string]int64{"pt_employee": 0}, componentAmounts(got["pt"]), "an exempt band is an explicit zero")
	got = preview("2026-02-16", nil)
	require.Equal(t, map[string]int64{"pt_employee": 30000}, componentAmounts(got["pt"]), "February has its own amount for every gender")

	got = preview("2026-12-16", "female")
	require.Equal(t, "applied", got["lwf"]["status"])
	require.Equal(t, lwfRevision, got["lwf"]["revision_id"])
	require.Equal(t, map[string]int64{"lwf_employee": 2000, "lwf_employer": 500 + 4800}, componentAmounts(got["lwf"]), "the employee share is capped and the employer's is not")
	require.EqualValues(t, 2000, got["totals"]["employee_paise"])
	require.EqualValues(t, 5300, got["totals"]["employer_paise"])
	salary := got["totals"]["salary"].(map[string]any)["totals"].(map[string]any)
	require.EqualValues(t, salary["net_paise"].(float64)-2000, got["totals"]["net_after_statutory_paise"])

	code, out = h.call("POST", "/statutory-preview", "viewer", map[string]any{"structure_id": structure, "on": "2026-12-16", "payable_half_days": 62, "disability": false, "gender": "unknown"})
	require.Equal(t, 400, code, out)
}
