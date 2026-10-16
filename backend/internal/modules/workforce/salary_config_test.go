package workforce_test

import (
	"encoding/json"
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
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type payrollHarness struct {
	t                     *testing.T
	db                    *tenantdb.DB
	hospital, base, other string
	do                    func(method, path, user, body string) (int, string)
}

func newPayrollHarness(t *testing.T) *payrollHarness {
	t.Helper()
	both := []authz.Permission{workforce.PermPayrollConfigure, workforce.PermPayrollView}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "viewer": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"admin": append([]authz.Permission{workforce.PermHospitalManage, workforce.PermMasterManage}, both...), "viewer": {workforce.PermPayrollView},
			"reader": both, "directory": {}, "foreign": both},
		Modules: []platform.Module{workforce.New()},
	})
	h := &payrollHarness{t: t, db: db, hospital: uuid.NewString(), do: func(method, path, user, body string) (int, string) {
		w := testutil.Do(r, method, path, user, body)
		return w.Code, w.Body.String()
	}}
	h.base = "/v1/workforce/hospitals/" + h.hospital
	h.other = "/v1/workforce/hospitals/" + uuid.NewString()
	for _, base := range []string{h.base, h.other} {
		code, out := h.do("PUT", base, "admin", `{"name":"Synthetic Payroll Hospital"}`)
		require.Equal(t, 200, code, out)
	}
	for user, level := range map[string]string{"admin": "manager", "viewer": "reader", "reader": "reader", "directory": "manager"} {
		code, out := h.do("PUT", h.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`)
		require.Equal(t, 200, code, out)
	}
	return h
}

func (h *payrollHarness) call(method, path, user string, body any) (int, map[string]any) {
	h.t.Helper()
	data := ""
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(h.t, err)
		data = string(raw)
	}
	code, raw := h.do(method, h.base+"/hr/payroll"+path, user, data)
	var out map[string]any
	require.NoError(h.t, json.Unmarshal([]byte(raw), &out), raw)
	return code, out
}

func (h *payrollHarness) putHead(id string, body map[string]any) (int, map[string]any) {
	return h.call("PUT", "/salary-heads/"+id, "admin", body)
}

func (h *payrollHarness) putStructure(id string, body map[string]any) (int, map[string]any) {
	return h.call("PUT", "/salary-structures/"+id, "admin", body)
}

// head creates a head that must succeed and returns its id.
func (h *payrollHarness) head(body map[string]any) string {
	h.t.Helper()
	id := uuid.NewString()
	code, out := h.putHead(id, body)
	require.Equal(h.t, 200, code, out)
	return id
}

func allMonths() []int { return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} }

// fixedEarning is a calendar-prorated taxable earning in gross and CTC, rounded to the nearest rupee.
func fixedEarning(code, wageClass string) map[string]any {
	return map[string]any{"expected_version": 0, "kind": "earning", "code": code, "name": "Synthetic " + code, "active": true, "basis": "fixed", "base_head_ids": []string{},
		"proration": "calendar_days", "minimum_payable_half_days": 0, "months": allMonths(), "rounding_mode": "nearest", "rounding_unit_paise": 100,
		"in_gross": true, "in_ctc": true, "arrears_eligible": true, "taxability": "taxable", "wage_class": wageClass, "payslip_label": code, "payslip_order": 10,
		"show_when_zero": false, "effective_on": "2026-01-01", "reviewed": true}
}

func percentHead(kind, code string, ppm int, bases ...string) map[string]any {
	body := with(fixedEarning(code, "excluded"), map[string]any{"kind": kind, "basis": "percent", "rate_ppm": ppm, "base_head_ids": bases, "proration": "none", "payslip_order": 20})
	switch kind {
	case "deduction":
		maps.Copy(body, map[string]any{"in_gross": false, "in_ctc": false, "taxability": "not_applicable", "wage_class": "outside", "payslip_order": 30})
	case "employer_contribution":
		maps.Copy(body, map[string]any{"in_gross": false, "wage_class": "outside", "payslip_order": 40})
	}
	return body
}

func structureBody(code string, lines ...map[string]any) map[string]any {
	return map[string]any{"expected_version": 0, "code": code, "name": "Synthetic " + code, "description": "", "active": true, "lines": lines, "effective_on": "2026-01-01", "reviewed": true}
}

func fixedLine(head string, paise int64) map[string]any {
	return map[string]any{"head_id": head, "amount_paise": paise}
}

func percentLine(head string) map[string]any { return map[string]any{"head_id": head} }

func requireSalaryConflict(t *testing.T, code int, out map[string]any, reason, field, id, on string) {
	t.Helper()
	require.Equal(t, 409, code, out)
	require.Equal(t, reason, out["reason"], out)
	if field != "" {
		require.Equal(t, id, out[field], out)
	}
	if on != "" {
		require.Equal(t, on, out["on"], out)
	}
}

func TestSalaryHeadsVersionReplayAndAccess(t *testing.T) {
	h := newPayrollHarness(t)
	id := uuid.NewString()
	body := with(fixedEarning("syn-basic", "wages"), map[string]any{"name": "  Synthetic   Basic  ", "payslip_label": " Basic  pay "})
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i], _ = h.putHead(id, body) })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code, "concurrent identical creates converge on one revision")
	}
	code, created := h.putHead(id, body)
	require.Equal(t, 200, code, created)
	require.Equal(t, id, created["id"])
	require.Equal(t, "SYN-BASIC", created["code"])
	require.Equal(t, "Synthetic Basic", created["name"])
	require.Equal(t, "Basic pay", created["payslip_label"])
	require.EqualValues(t, 1, created["version"])
	require.Equal(t, "user-admin", created["reviewed_by_subject"])
	require.Nil(t, created["head_id"], "current rows are keyed by the head")

	for _, tc := range []struct {
		user   string
		status int
	}{{"viewer", 403}, {"directory", 403}, {"reader", 404}, {"foreign", 404}, {"", 401}} {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		code, _ := h.do("PUT", h.base+"/hr/payroll/salary-heads/"+id, tc.user, string(raw))
		require.Equal(t, tc.status, code, tc.user)
	}
	code, raw := h.do("GET", h.base+"/hr/payroll/salary-heads?kind=earning", "viewer", "")
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, `"code":"SYN-BASIC"`)
	code, raw = h.do("GET", h.base+"/hr/payroll/salary-heads?kind=deduction", "viewer", "")
	require.Equal(t, 200, code, raw)
	require.NotContains(t, raw, "SYN-BASIC")
	for _, query := range []string{"?kind=payroll", "?limit=101"} {
		code, _ = h.do("GET", h.base+"/hr/payroll/salary-heads"+query, "viewer", "")
		require.Equal(t, 400, code, query)
	}
	for user, status := range map[string]int{"directory": 403, "foreign": 404, "": 401} {
		code, _ = h.do("GET", h.base+"/hr/payroll/salary-heads", user, "")
		require.Equal(t, status, code, user)
	}
	code, _ = h.do("GET", h.other+"/hr/payroll/salary-heads/"+id+"/history", "admin", "")
	require.Equal(t, 404, code, "heads never resolve in another hospital")
	code, _ = h.do("GET", h.base+"/hr/payroll/salary-heads/"+uuid.NewString()+"/history", "viewer", "")
	require.Equal(t, 404, code)

	revised := with(body, map[string]any{"expected_version": 1, "name": "Synthetic Basic Revised", "effective_on": "2026-04-01"})
	code, out := h.putHead(id, revised)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	require.NotEqual(t, created["revision_id"], out["revision_id"])
	code, out = h.putHead(id, revised)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"], "an identical replay returns the current revision")
	code, _ = h.putHead(id, with(revised, map[string]any{"name": "Stale"}))
	require.Equal(t, 409, code, "stale expected version")
	code, _ = h.putHead(id, with(revised, map[string]any{"expected_version": 2, "code": "SYN-OTHER"}))
	require.Equal(t, 409, code, "code is immutable")
	code, _ = h.putHead(id, with(revised, map[string]any{"expected_version": 2, "kind": "employer_contribution", "in_gross": false}))
	require.Equal(t, 409, code, "kind is immutable")
	code, _ = h.putHead(id, with(revised, map[string]any{"expected_version": 2, "effective_on": "2026-03-01"}))
	require.Equal(t, 400, code, "effective date never moves earlier")
	code, _ = h.putHead(uuid.NewString(), with(body, map[string]any{"expected_version": 3}))
	require.Equal(t, 404, code)
	code, _ = h.putHead(uuid.NewString(), with(body, map[string]any{"name": "Synthetic Copy"}))
	require.Equal(t, 409, code, "codes are unique per hospital")
	code, _ = h.putHead(uuid.NewString(), with(body, map[string]any{"code": "SYN-COPY", "name": "synthetic basic revised"}))
	require.Equal(t, 409, code, "names are unique per hospital, ignoring case")

	code, raw = h.do("GET", h.base+"/hr/payroll/salary-heads/"+id+"/history", "viewer", "")
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, `"name":"Synthetic Basic"`)
	require.Contains(t, raw, `"name":"Synthetic Basic Revised"`)
	require.Contains(t, raw, `"head_id":"`+id+`"`)

	deduction := percentHead("deduction", "SYN-PF", 120000, id)
	for name, invalid := range map[string]map[string]any{
		"unreviewed":            {"reviewed": false},
		"impossible date":       {"effective_on": "2026-02-30"},
		"date out of range":     {"effective_on": "1999-12-31"},
		"bad code":              {"code": "SYN PF"},
		"nul name":              {"name": "Synthetic\x00PF"},
		"blank label":           {"payslip_label": "   "},
		"percent without rate":  {"rate_ppm": nil},
		"percent without bases": {"base_head_ids": []string{}},
		"rate above 100%":       {"rate_ppm": 1000001},
		"percent prorated":      {"proration": "calendar_days"},
		"duplicate base":        {"base_head_ids": []string{id, id}},
		"too many bases":        {"base_head_ids": manyIDs(21)},
		"deduction in ctc":      {"in_ctc": true},
		"deduction in gross":    {"in_gross": true},
		"deduction taxed":       {"taxability": "taxable"},
		"deduction as wages":    {"wage_class": "wages"},
		"bad rounding unit":     {"rounding_unit_paise": 25},
		"floor off the unit":    {"min_amount_paise": 150},
		"cap off the unit":      {"max_amount_paise": 150},
		"floor above cap":       {"min_amount_paise": 2000, "max_amount_paise": 1000},
		"no months":             {"months": []int{}},
		"month 13":              {"months": []int{13}},
		"duplicate month":       {"months": []int{3, 3}},
		"minimum above 31 days": {"minimum_payable_half_days": 63},
		"unknown kind":          {"kind": "loan"},
		"injected tenant":       {"tenant_id": testutil.TenantB},
		"unknown field":         {"amount_paise": 100},
	} {
		code, out := h.putHead(uuid.NewString(), with(deduction, invalid))
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	earning := fixedEarning("SYN-INVALID", "wages")
	for name, invalid := range map[string]map[string]any{
		"fixed with rate":         {"rate_ppm": 100000},
		"fixed with bases":        {"base_head_ids": []string{id}},
		"fixed days without days": {"proration": "fixed_days"},
		"days without fixed days": {"proration_days": 26},
		"earning untaxed":         {"taxability": "not_applicable"},
		"exempt without note":     {"taxability": "exempt"},
		"note without exempt":     {"tax_note": "Synthetic section"},
		"blank note":              {"taxability": "exempt", "tax_note": "  "},
	} {
		code, out := h.putHead(uuid.NewString(), with(earning, invalid))
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	h.head(with(earning, map[string]any{"code": "SYN-EXEMPT", "name": "Synthetic Exempt", "taxability": "exempt", "tax_note": " Synthetic exemption basis ", "proration": "fixed_days", "proration_days": 26}))

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_salary_head_revisions").Count(&n).Error)
		require.Zero(t, n, "forced RLS without hospital scope")
		var events []struct{ Payload []byte }
		require.NoError(t, tx.Table("outbox_events").Select("payload").Where("subject = ?", workforce.SalaryConfigChanged).Find(&events).Error)
		require.Len(t, events, 3)
		for _, e := range events {
			var envelope struct{ Data map[string]any }
			require.NoError(t, json.Unmarshal(e.Payload, &envelope))
			require.ElementsMatch(t, []string{"hospital_id", "config_id", "revision_id", "kind", "version"}, slices.Collect(maps.Keys(envelope.Data)), "events carry identifiers only")
			require.Equal(t, "salary_head", envelope.Data["kind"])
		}
		var changes []string
		require.NoError(t, tx.Table("workforce_audit").Where("action = ?", "workforce.salary_head.changed").Pluck("changes::text", &changes).Error)
		require.Len(t, changes, 3)
		for _, c := range changes {
			require.NotContains(t, c, "paise")
			require.NotContains(t, c, "rate")
		}
		return nil
	}))
	authorized := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_payroll','authorized',true)`, h.hospital).Error
	}
	for name, statement := range map[string]string{
		"revisions are append only":      "UPDATE workforce_salary_head_revisions SET rate_ppm = 1 WHERE head_id = '" + id + "'",
		"heads are never deleted":        "DELETE FROM workforce_salary_heads WHERE id = '" + id + "'",
		"bases join only at creation":    "INSERT INTO workforce_salary_head_bases(tenant_id,hospital_id,revision_id,base_head_id) SELECT tenant_id,hospital_id,id,head_id FROM workforce_salary_head_revisions WHERE head_id = '" + id + "' LIMIT 1",
		"versions advance one at a time": "UPDATE workforce_salary_heads SET version = version + 2 WHERE id = '" + id + "'",
	} {
		err := h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := authorized(tx); err != nil {
				return err
			}
			return tx.Exec(statement).Error
		})
		require.Error(t, err, name)
	}
	err := h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, h.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_salary_heads(tenant_id,hospital_id,id,kind,code,name,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,'earning','SYN-RAW','Synthetic Raw',true,'2026-01-01','user-admin',1,?)`, testutil.TenantA, h.hospital, uuid.New(), uuid.New()).Error
	})
	require.Error(t, err, "writes need the payroll authorization flag")
}

func TestSalaryHeadGraphGuards(t *testing.T) {
	h := newPayrollHarness(t)
	basic := h.head(fixedEarning("SYN-BASIC", "wages"))
	pf := h.head(percentHead("deduction", "SYN-PF", 120000, basic))

	code, out := h.putHead(uuid.NewString(), percentHead("earning", "SYN-ON-PF", 100000, pf))
	require.Equal(t, 400, code, out)
	require.Equal(t, "invalid_reference", out["error"], "a deduction is never a base")
	code, out = h.putHead(uuid.NewString(), percentHead("earning", "SYN-ON-NONE", 100000, uuid.NewString()))
	require.Equal(t, 400, code, out)
	code, raw := h.do("GET", h.other+"/hr/payroll/salary-heads/"+basic+"/history", "admin", "")
	require.Equal(t, 404, code, raw)

	self := uuid.NewString()
	code, out = h.putHead(self, percentHead("earning", "SYN-SELF", 100000, self))
	requireSalaryConflict(t, code, out, "cycle", "head_id", self, "2026-01-01")

	a := h.head(percentHead("earning", "SYN-A", 100000, basic))
	b := h.head(percentHead("earning", "SYN-B", 100000, a))
	code, out = h.putHead(a, with(percentHead("earning", "SYN-A", 100000, b), map[string]any{"expected_version": 1}))
	require.Equal(t, 409, code, out)
	require.Equal(t, "cycle", out["reason"], out)
	require.Contains(t, []any{a, b}, out["head_id"])
	code, out = h.putHead(a, with(percentHead("earning", "SYN-A", 100000, b), map[string]any{"expected_version": 1, "effective_on": "2026-07-01"}))
	require.Equal(t, 409, code, out)
	require.Equal(t, "cycle", out["reason"], out)
	require.Equal(t, "2026-07-01", out["on"], "a later-dated revision is proved on its own date")

	code, out = h.putHead(basic, with(fixedEarning("SYN-BASIC", "wages"), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 409, code, out)
	require.Equal(t, "base_unavailable", out["reason"], out)
	require.Contains(t, []any{pf, a}, out["head_id"], "the dependent head is named")

	dormant := h.head(with(fixedEarning("SYN-DORMANT", "wages"), map[string]any{"active": false}))
	code, out = h.putHead(uuid.NewString(), percentHead("earning", "SYN-ON-DORMANT", 100000, dormant))
	require.Equal(t, 409, code, out)
	require.Equal(t, "base_unavailable", out["reason"], out)

	// Deactivating the dependents first frees the base.
	code, out = h.putHead(b, with(percentHead("earning", "SYN-B", 100000, a), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 200, code, out)
	code, out = h.putHead(a, with(percentHead("earning", "SYN-A", 100000, basic), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 200, code, out)
	code, out = h.putHead(pf, with(percentHead("deduction", "SYN-PF", 120000, basic), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 200, code, out)
	code, out = h.putHead(basic, with(fixedEarning("SYN-BASIC", "wages"), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 200, code, out)
}

func manyIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	return ids
}

func TestSalaryStructuresStayComputable(t *testing.T) {
	h := newPayrollHarness(t)
	basic := h.head(fixedEarning("SYN-BASIC", "wages"))
	hra := h.head(percentHead("earning", "SYN-HRA", 400000, basic))
	loan := h.head(with(fixedEarning("SYN-LOAN", "outside"), map[string]any{"kind": "deduction", "in_gross": false, "in_ctc": false, "taxability": "not_applicable", "proration": "none"}))
	master := func(kind, code string) string {
		id := uuid.NewString()
		status, raw := h.do("PUT", h.base+"/hr/masters/"+id, "admin", `{"expected_version":0,"kind":"`+kind+`","code":"`+code+`","name":"Synthetic `+code+`","description":"","active":true}`)
		require.Equal(t, 200, status, raw)
		return id
	}
	department, grade := master("department", "SYN-CARE"), master("grade", "SYN-G1")

	code, out := h.putStructure(uuid.NewString(), structureBody("SYN-NO-BASE", percentLine(hra)))
	require.Equal(t, 409, code, out)
	require.Equal(t, "structure_incomplete", out["reason"], "a percentage head needs its base in the structure")
	for name, lines := range map[string][]map[string]any{
		"fixed head without amount": {percentLine(basic)},
		"percent head with amount":  {fixedLine(basic, 3000000), fixedLine(hra, 100)},
	} {
		code, out := h.putStructure(uuid.NewString(), structureBody("SYN-BAD", lines...))
		require.Equal(t, 409, code, "%s: %v", name, out)
		require.Equal(t, "structure_incomplete", out["reason"], name)
	}
	for name, body := range map[string]map[string]any{
		"unknown head":       structureBody("SYN-BAD", fixedLine(uuid.NewString(), 100)),
		"wrong master kind":  with(structureBody("SYN-BAD", fixedLine(basic, 100)), map[string]any{"grade_id": department}),
		"unknown master":     with(structureBody("SYN-BAD", fixedLine(basic, 100)), map[string]any{"department_id": uuid.NewString()}),
		"duplicate head":     structureBody("SYN-BAD", fixedLine(basic, 100), fixedLine(basic, 200)),
		"no lines":           structureBody("SYN-BAD"),
		"amount above limit": structureBody("SYN-BAD", fixedLine(basic, 1000000001)),
		"unreviewed":         with(structureBody("SYN-BAD", fixedLine(basic, 100)), map[string]any{"reviewed": false}),
	} {
		code, out := h.putStructure(uuid.NewString(), body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}

	id := uuid.NewString()
	body := with(structureBody("syn-nurse", fixedLine(basic, 3000000), percentLine(hra), fixedLine(loan, 50000)), map[string]any{"department_id": department, "grade_id": grade})
	code, created := h.putStructure(id, body)
	require.Equal(t, 200, code, created)
	require.Equal(t, "SYN-NURSE", created["code"])
	require.EqualValues(t, 1, created["version"])
	lines := created["lines"].([]any)
	require.Len(t, lines, 3)
	require.Equal(t, map[string]any{"position": float64(1), "head_id": basic, "code": "SYN-BASIC", "name": "Synthetic SYN-BASIC", "kind": "earning", "basis": "fixed", "amount_paise": float64(3000000)}, lines[0])
	require.Nil(t, lines[1].(map[string]any)["amount_paise"])
	code, out = h.putStructure(id, body)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["version"], "an identical replay returns the current revision")
	code, _ = h.call("PUT", "/salary-structures/"+id, "viewer", body)
	require.Equal(t, 403, code)
	code, _ = h.call("PUT", "/salary-structures/"+id, "foreign", body)
	require.Equal(t, 404, code)

	code, out = h.putHead(basic, with(fixedEarning("SYN-BASIC", "wages"), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 409, code, out)
	require.Equal(t, "base_unavailable", out["reason"], "the structure's percentage head still uses it")
	code, out = h.putHead(loan, with(percentHead("deduction", "SYN-LOAN", 10000, basic), map[string]any{"expected_version": 1, "effective_on": "2026-05-01"}))
	requireSalaryConflict(t, code, out, "structure_incomplete", "structure_id", id, "2026-05-01")

	code, _ = h.call("PUT", "/salary-heads/"+hra, "admin", with(percentHead("earning", "SYN-HRA", 400000, basic), map[string]any{"expected_version": 1, "active": false, "effective_on": "2026-06-01"}))
	require.Equal(t, 409, code, "an active structure cannot lose an active head")
	revised := with(body, map[string]any{"expected_version": 1, "lines": []map[string]any{fixedLine(basic, 3200000), fixedLine(loan, 50000)}, "effective_on": "2026-06-01"})
	code, out = h.putStructure(id, revised)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	code, out = h.call("PUT", "/salary-heads/"+hra, "admin", with(percentHead("earning", "SYN-HRA", 400000, basic), map[string]any{"expected_version": 1, "active": false, "effective_on": "2026-06-01"}))
	require.Equal(t, 200, code, "the structure no longer uses it from that date: %v", out)
	code, out = h.putStructure(id, with(revised, map[string]any{"expected_version": 2, "code": "SYN-OTHER"}))
	require.Equal(t, 409, code, out)
	code, out = h.putStructure(id, with(revised, map[string]any{"expected_version": 1, "name": "Stale"}))
	require.Equal(t, 409, code, out)

	code, raw := h.do("GET", h.base+"/hr/payroll/salary-structures/"+id+"/history", "viewer", "")
	require.Equal(t, 200, code, raw)
	history := decodeRaw[struct {
		Data []struct {
			StructureID string `json:"structure_id"`
			Version     int64  `json:"version"`
			Lines       []any  `json:"lines"`
		} `json:"data"`
	}](t, raw)
	require.Len(t, history.Data, 2)
	require.Equal(t, id, history.Data[0].StructureID)
	require.EqualValues(t, 2, history.Data[0].Version, "newest first")
	require.Len(t, history.Data[0].Lines, 2)
	require.Len(t, history.Data[1].Lines, 3, "earlier revisions keep their own lines")
	code, raw = h.do("GET", h.base+"/hr/payroll/salary-structures", "viewer", "")
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, `"code":"SYN-NURSE"`)
	code, _ = h.do("GET", h.other+"/hr/payroll/salary-structures/"+id+"/history", "admin", "")
	require.Equal(t, 404, code)

	retired := master("grade", "SYN-G0")
	code, raw = h.do("PUT", h.base+"/hr/masters/"+retired, "admin", `{"expected_version":1,"kind":"grade","code":"SYN-G0","name":"Synthetic SYN-G0","description":"","active":false}`)
	require.Equal(t, 200, code, raw)
	code, out = h.putStructure(uuid.NewString(), with(structureBody("SYN-INACTIVE", fixedLine(basic, 100)), map[string]any{"grade_id": retired}))
	require.Equal(t, 400, code, out)
	require.Equal(t, "invalid_reference", out["error"], "scope masters must be active")
}

func decodeRaw[T any](t *testing.T, raw string) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal([]byte(raw), &out), raw)
	return out
}

func TestSalaryPreviewComputesWithoutStoring(t *testing.T) {
	h := newPayrollHarness(t)
	basic := h.head(fixedEarning("SYN-BASIC", "wages"))
	hra := h.head(percentHead("earning", "SYN-HRA", 400000, basic))
	special := h.head(with(fixedEarning("SYN-SPECIAL", "excluded"), map[string]any{"proration": "none", "payslip_order": 15}))
	pf := h.head(with(percentHead("deduction", "SYN-PF", 120000, basic), map[string]any{"max_amount_paise": 180000}))
	epf := h.head(with(percentHead("employer_contribution", "SYN-EPF", 120000, basic), map[string]any{"max_amount_paise": 180000}))
	bonus := h.head(with(fixedEarning("SYN-BONUS", "excluded"), map[string]any{"months": []int{12}, "proration": "none", "payslip_order": 50}))
	id := uuid.NewString()
	code, out := h.putStructure(id, with(structureBody("SYN-NURSE", fixedLine(basic, 3000000), percentLine(hra), fixedLine(special, 2000000), percentLine(pf), percentLine(epf), fixedLine(bonus, 500000)), map[string]any{"effective_on": "2026-02-01"}))
	require.Equal(t, 200, code, out)
	structureRevision := out["revision_id"]
	var before int64
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Count(&before).Error
	}))

	preview := func(user string, body map[string]any) (int, map[string]any) {
		return h.call("POST", "/salary-structures/"+id+"/preview", user, body)
	}
	code, full := preview("viewer", map[string]any{"on": "2026-03-15", "payable_half_days": 62})
	require.Equal(t, 200, code, full)
	require.Equal(t, structureRevision, full["structure_revision_id"])
	require.EqualValues(t, 31, full["period_days"])
	amounts := map[string]any{}
	order := []any{}
	for _, l := range full["lines"].([]any) {
		line := l.(map[string]any)
		amounts[line["code"].(string)] = line["amount_paise"]
		order = append(order, line["code"])
		if line["code"] == "SYN-BONUS" {
			require.Equal(t, false, line["applicable"], "the bonus only applies in December")
			require.Equal(t, false, line["shown"])
		}
	}
	require.Equal(t, map[string]any{"SYN-BASIC": float64(3000000), "SYN-HRA": float64(1200000), "SYN-SPECIAL": float64(2000000), "SYN-PF": float64(180000), "SYN-EPF": float64(180000), "SYN-BONUS": float64(0)}, amounts)
	require.Equal(t, []any{"SYN-BASIC", "SYN-SPECIAL", "SYN-HRA", "SYN-PF", "SYN-EPF", "SYN-BONUS"}, order, "lines follow payslip order")
	require.Equal(t, map[string]any{"gross_paise": float64(6200000), "deductions_paise": float64(180000), "employer_paise": float64(180000), "net_paise": float64(6020000), "ctc_paise": float64(6380000), "negative_net": false}, full["totals"])
	require.Equal(t, map[string]any{"wages_paise": float64(3000000), "excluded_paise": float64(3200000), "remuneration_paise": float64(6200000), "add_back_paise": float64(100000), "statutory_wages_paise": float64(3100000), "excluded_share_ppm": float64(516129)}, full["wage_test"])

	code, partial := preview("viewer", map[string]any{"on": "2026-03-15", "payable_half_days": 60})
	require.Equal(t, 200, code, partial)
	for _, l := range partial["lines"].([]any) {
		line := l.(map[string]any)
		switch line["code"] {
		case "SYN-BASIC":
			require.EqualValues(t, 2903200, line["amount_paise"], "30 of 31 days, rounded to the rupee")
		case "SYN-HRA":
			require.EqualValues(t, 1161300, line["amount_paise"], "a percentage of the prorated base")
		case "SYN-SPECIAL":
			require.EqualValues(t, 2000000, line["amount_paise"], "unprorated")
		}
	}

	code, december := preview("viewer", map[string]any{"on": "2026-12-01", "payable_half_days": 62})
	require.Equal(t, 200, code, december)
	require.EqualValues(t, 6700000, december["totals"].(map[string]any)["gross_paise"])

	for name, body := range map[string]map[string]any{
		"more days than february": {"on": "2026-02-10", "payable_half_days": 57},
		"impossible date":         {"on": "2026-02-30", "payable_half_days": 2},
		"negative days":           {"on": "2026-03-15", "payable_half_days": -1},
		"missing days":            {"on": "2026-03-15"},
	} {
		code, out := preview("viewer", body)
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out = preview("viewer", map[string]any{"on": "2026-01-15", "payable_half_days": 62})
	require.Equal(t, 409, code, out)
	require.Equal(t, "not_in_force", out["reason"])
	code, _ = h.call("POST", "/salary-structures/"+uuid.NewString()+"/preview", "viewer", map[string]any{"on": "2026-03-15", "payable_half_days": 62})
	require.Equal(t, 404, code)
	for user, status := range map[string]int{"directory": 403, "foreign": 404} {
		code, _ = preview(user, map[string]any{"on": "2026-03-15", "payable_half_days": 62})
		require.Equal(t, status, code, user)
	}
	code, raw := h.do("POST", h.other+"/hr/payroll/salary-structures/"+id+"/preview", "admin", `{"on":"2026-03-15","payable_half_days":62}`)
	require.Equal(t, 404, code, raw)

	var after int64
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Count(&after).Error
	}))
	require.Equal(t, before, after, "previews store nothing")
}
