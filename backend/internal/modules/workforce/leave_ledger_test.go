package workforce_test

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type ledgerHarness struct {
	*leaveHarness
	db       *tenantdb.DB
	hospital string
}

func newLedgerHarness(t *testing.T) *ledgerHarness {
	all := []authz.Permission{workforce.PermHospitalManage, workforce.PermLeaveConfigure, workforce.PermLeaveAllocate, workforce.PermLeaveCorrect}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "corrector": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin":     {workforce.PermHospitalManage, workforce.PermLeaveConfigure, workforce.PermLeaveAllocate},
			"corrector": {workforce.PermLeaveCorrect},
			"reader":    all,
			"directory": {},
			"foreign":   all,
		},
		Modules: []platform.Module{workforce.New()},
	})
	hospital := uuid.NewString()
	h := &leaveHarness{t: t, base: "/v1/workforce/hospitals/" + hospital, do: func(method, path, user, body string) (int, string) {
		w := testutil.Do(r, method, path, user, body)
		return w.Code, w.Body.String()
	}}
	code, out := h.do("PUT", h.base, "admin", `{"name":"Synthetic Ledger Hospital"}`)
	require.Equal(t, 200, code, out)
	for user, level := range map[string]string{"admin": "manager", "corrector": "manager", "reader": "reader", "directory": "manager"} {
		code, out = h.do("PUT", h.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`)
		require.Equal(t, 200, code, out)
	}
	return &ledgerHarness{leaveHarness: h, db: db, hospital: hospital}
}

func (h *ledgerHarness) call(method, path, user string, body any) (int, map[string]any) {
	h.t.Helper()
	data := ""
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(h.t, err)
		data = string(raw)
	}
	code, raw := h.do(method, h.base+path, user, data)
	var out map[string]any
	require.NoError(h.t, json.Unmarshal([]byte(raw), &out), raw)
	return code, out
}

func (h *ledgerHarness) staff(code string) string {
	id := uuid.NewString()
	status, out := h.call("PUT", "/staff/"+id, "admin", map[string]any{"employee_code": code, "display_name": "Synthetic " + code, "job_category": "nurse", "employment_status": "active"})
	require.Equal(h.t, 200, status, out)
	return id
}

func (h *ledgerHarness) assign(staff string, structure any, effective string, version int) (int, map[string]any) {
	return h.call("PUT", "/staff/"+staff+"/leave-assignment", "admin", map[string]any{"expected_version": version, "structure_revision_id": structure, "active": true, "effective_on": effective, "reviewed": true})
}

func (h *ledgerHarness) post(year string, month int) (int, map[string]any) {
	return h.call("POST", "/hr/leave-years/"+year+"/postings", "admin", map[string]any{"month": month})
}

// balances maps leave type id to half-days for one staff member.
func (h *ledgerHarness) balances(staff, query string) map[string]int {
	h.t.Helper()
	code, out := h.call("GET", "/staff/"+staff+"/leave-balances?"+query, "reader", nil)
	require.Equal(h.t, 200, code, out)
	result := map[string]int{}
	for _, b := range out["balances"].([]any) {
		row := b.(map[string]any)
		result[row["leave_type_id"].(string)] = int(row["half_days"].(float64))
	}
	return result
}

func (h *ledgerHarness) correction(staff, id, user string, body map[string]any) (int, map[string]any) {
	return h.call("PUT", "/staff/"+staff+"/leave-corrections/"+id, user, body)
}

func (h *ledgerHarness) structure(earned, annual string) map[string]any {
	dated := func(code, name string) map[string]any {
		return with(leaveType(code, name, true), map[string]any{"effective_on": "2024-01-01"})
	}
	code, typeA := h.put(earned, dated("SYN-EARNED", "Synthetic Earned"))
	require.Equal(h.t, 200, code, typeA)
	code, typeB := h.put(annual, dated("SYN-ANNUAL", "Synthetic Annual"))
	require.Equal(h.t, 200, code, typeB)
	entry := func(revision any, accrual string, half, carry int) map[string]any {
		return map[string]any{"leave_type_revision_id": revision, "annual_half_days": half, "accrual": accrual, "carry_forward_half_days": carry, "proration": "join_month", "probation_eligible": false}
	}
	code, structure := h.put(uuid.NewString(), map[string]any{"expected_version": 0, "kind": "structure", "code": "SYN-LEDGER", "name": "Synthetic Ledger", "description": "", "active": true, "effective_on": "2024-01-01", "reviewed": true, "holidays": []any{}, "entries": []any{entry(typeA["revision_id"], "monthly", 24, 10), entry(typeB["revision_id"], "upfront", 12, 0)}})
	require.Equal(h.t, 200, code, structure)
	return structure
}

func TestLeaveLedgerPostsCorrectsAndCarriesExactlyOnce(t *testing.T) {
	h := newLedgerHarness(t)
	code, out := h.call("PUT", "/hr/leave-years/2024", "admin", nil)
	require.Equal(t, 409, code, "a year needs the start month first: %v", out)
	code, out = h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 1, "reviewed": false})
	require.Equal(t, 400, code, out)
	code, out = h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 1, "reviewed": true})
	require.Equal(t, 200, code, out)
	require.Equal(t, "user-admin", out["reviewed_by_subject"])
	code, _ = h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 1, "reviewed": true})
	require.Equal(t, 200, code, "replay")
	code, _ = h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 4, "reviewed": true})
	require.Equal(t, 409, code, "the start month is fixed")
	code, out = h.call("GET", "/hr/leave-settings", "reader", nil)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 1, out["year_start_month"])

	earned, annual := uuid.NewString(), uuid.NewString()
	structure := h.structure(earned, annual)
	full, joiner, late := h.staff("SYN-L1"), h.staff("SYN-L2"), h.staff("SYN-L3")
	code, out = h.assign(full, structure["revision_id"], "2023-12-01", 0)
	require.Equal(t, 400, code, "assignments cannot predate the structure: %v", out)
	code, out = h.assign(full, structure["revision_id"], "2024-01-01", 0)
	require.Equal(t, 200, code, out)
	require.Equal(t, "SYN-LEDGER", out["structure_code"])
	code, _ = h.assign(full, structure["revision_id"], "2024-01-01", 0)
	require.Equal(t, 200, code, "replay")
	code, out = h.assign(joiner, structure["revision_id"], "2024-03-15", 0)
	require.Equal(t, 200, code, out)
	code, _ = h.assign(uuid.NewString(), structure["revision_id"], "2024-01-01", 0)
	require.Equal(t, 404, code, "unknown staff")

	for _, year := range []string{"2024", "2025"} {
		code, out = h.call("PUT", "/hr/leave-years/"+year, "admin", nil)
		require.Equal(t, 200, code, out)
	}
	require.Equal(t, "2025-01-01", out["start_on"])
	require.Equal(t, "2025-12-31", out["end_on"])
	code, _ = h.call("PUT", "/hr/leave-years/1999", "admin", nil)
	require.Equal(t, 404, code)

	code, _ = h.post("2024", 2)
	require.Equal(t, 409, code, "months post in order")
	runs := make([]map[string]any, 4)
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range runs {
		wg.Go(func() { codes[i], runs[i] = h.post("2024", 1) })
	}
	wg.Wait()
	for i := range runs {
		require.Equal(t, 200, codes[i], runs[i])
		require.Equal(t, runs[0]["id"], runs[i]["id"], "concurrent posts share one run")
	}
	for month := 2; month <= 3; month++ {
		code, out = h.post("2024", month)
		require.Equal(t, 200, code, out)
	}

	// A first assignment inside posted months is caught up by the next run; later changes there are refused.
	code, out = h.assign(late, structure["revision_id"], "2024-01-01", 0)
	require.Equal(t, 200, code, out)
	code, out = h.assign(late, structure["revision_id"], "2024-02-01", 1)
	require.Equal(t, 409, code, out)
	code, out = h.post("2024", 4)
	require.Equal(t, 200, code, out)
	require.Equal(t, map[string]int{earned: 8, annual: 12}, h.balances(late, "leave_year=2024&as_of=2024-04-30"))
	require.Equal(t, map[string]int{earned: 4, annual: 12}, h.balances(full, "leave_year=2024&as_of=2024-02-28"))

	for month := 5; month <= 12; month++ {
		code, out = h.post("2024", month)
		require.Equal(t, 200, code, out)
	}
	code, replay := h.post("2024", 12)
	require.Equal(t, 200, code)
	require.Equal(t, out["id"], replay["id"], "reposting a month returns its run")
	require.Equal(t, map[string]int{earned: 24, annual: 12}, h.balances(full, "leave_year=2024"))
	require.Equal(t, map[string]int{earned: 20, annual: 10}, h.balances(joiner, "leave_year=2024"), "join-month proration")
	code, _ = h.call("GET", "/staff/"+full+"/leave-balances?leave_year=2024&as_of=2025-01-01", "reader", nil)
	require.Equal(t, 400, code)

	reason := "Synthetic recredit after attendance audit"
	correction := map[string]any{"leave_type_id": earned, "leave_year": 2024, "half_days": 6, "effective_on": "2024-12-01", "reason": reason}
	entry := uuid.NewString()
	code, _ = h.correction(full, entry, "admin", correction)
	require.Equal(t, 403, code, "allocation does not imply correction")
	for name, invalid := range map[string]map[string]any{"short reason": {"reason": "  too short "}, "zero": {"half_days": 0}, "bad date": {"effective_on": "2024-13-01"}} {
		code, out = h.correction(full, uuid.NewString(), "corrector", with(correction, invalid))
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
	code, out = h.correction(full, uuid.NewString(), "corrector", with(correction, map[string]any{"effective_on": "2025-01-01"}))
	require.Equal(t, 409, code, "dates stay inside the year: %v", out)
	code, out = h.correction(full, entry, "corrector", correction)
	require.Equal(t, 200, code, out)
	require.Equal(t, "correction", out["kind"])
	require.Equal(t, reason, out["reason"])
	code, again := h.correction(full, entry, "corrector", correction)
	require.Equal(t, 200, code)
	require.Equal(t, out["created_at"], again["created_at"], "replay returns the original entry")
	code, _ = h.correction(full, entry, "corrector", with(correction, map[string]any{"half_days": 7}))
	require.Equal(t, 409, code, "an id is bound to its values")
	code, _ = h.correction(joiner, entry, "corrector", correction)
	require.Equal(t, 409, code, "an id is bound to its staff member")

	code, _ = h.call("POST", "/hr/leave-years/2025/close", "admin", nil)
	require.Equal(t, 409, code, "unposted months block a close")
	closes := make([]map[string]any, 3)
	codes = make([]int, 3)
	for i := range closes {
		wg.Go(func() { codes[i], closes[i] = h.call("POST", "/hr/leave-years/2024/close", "admin", nil) })
	}
	wg.Wait()
	for i := range closes {
		require.Equal(t, 200, codes[i], closes[i])
		require.Equal(t, closes[0]["id"], closes[i]["id"], "one year-end run")
	}
	require.EqualValues(t, 13, closes[0]["step"])
	require.EqualValues(t, 12, closes[0]["entries"], "per staff member: carry out, carry in and expiry for earned, expiry for annual")

	require.Equal(t, map[string]int{earned: 0, annual: 0}, h.balances(full, "leave_year=2024"))
	require.Equal(t, map[string]int{earned: 30, annual: 12}, h.balances(full, "leave_year=2024&as_of=2024-12-30"))
	require.Equal(t, map[string]int{earned: 10}, h.balances(full, "leave_year=2025"), "carry is capped at the structure limit")
	require.Equal(t, map[string]int{earned: 10}, h.balances(joiner, "leave_year=2025"))

	code, _ = h.correction(full, uuid.NewString(), "corrector", with(correction, map[string]any{"effective_on": "2024-12-15"}))
	require.Equal(t, 409, code, "closed years take no entries")
	code, _ = h.post("2024", 12)
	require.Equal(t, 200, code, "replay of a closed year's last month")
	code, out = h.assign(full, structure["revision_id"], "2024-06-01", 1)
	require.Equal(t, 409, code, "closed history is frozen: %v", out)
	code, out = h.call("PUT", "/staff/"+full+"/leave-assignment", "admin", map[string]any{"expected_version": 1, "structure_revision_id": structure["revision_id"], "active": false, "effective_on": "2025-01-01", "reviewed": true})
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])

	code, page := h.call("GET", "/staff/"+full+"/leave-ledger?leave_year=2024&limit=100", "reader", nil)
	require.Equal(t, 200, code, page)
	kinds := map[string]int{}
	for _, e := range page["data"].([]any) {
		kinds[e.(map[string]any)["kind"].(string)]++
	}
	require.Equal(t, map[string]int{"accrual": 12, "allocation": 1, "correction": 1, "carry_out": 1, "expiry": 2}, kinds)
	reasons := func(user string) []string {
		code, page := h.call("GET", "/staff/"+full+"/leave-ledger?leave_year=2024&limit=100", user, nil)
		require.Equal(t, 200, code, page)
		var out []string
		for _, e := range page["data"].([]any) {
			if r := e.(map[string]any)["reason"].(string); r != "" {
				out = append(out, r)
			}
		}
		return out
	}
	require.Equal(t, []string{reason}, reasons("corrector"))
	require.Empty(t, reasons("directory"), "reasons are redacted without a leave permission")
	code, page = h.call("GET", "/hr/leave-balances?leave_year=2025&limit=2", "reader", nil)
	require.Equal(t, 200, code, page)
	require.Len(t, page["data"], 2)
	require.Equal(t, true, page["page"].(map[string]any)["has_more"])
	code, page = h.call("GET", "/hr/leave-years/2024/runs?limit=100", "reader", nil)
	require.Equal(t, 200, code, page)
	require.Len(t, page["data"], 13)
	code, page = h.call("GET", "/staff/"+full+"/leave-assignment/history", "reader", nil)
	require.Equal(t, 200, code, page)
	require.Len(t, page["data"], 2)

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", workforce.LeaveLedgerChanged).Count(&n).Error)
		require.EqualValues(t, 14, n, "twelve postings, one correction, one close")
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", workforce.StaffLeaveAssignmentChanged).Count(&n).Error)
		require.EqualValues(t, 4, n)
		require.NoError(t, tx.Table("outbox_events").Where("subject = ? AND payload::text LIKE ?", workforce.LeaveLedgerChanged, "%audit%").Count(&n).Error)
		require.Zero(t, n, "reasons never leave the ledger")
		require.NoError(t, tx.Table("workforce_audit").Where("action LIKE 'workforce.leave_%' AND changes::text LIKE ?", "%audit%").Count(&n).Error)
		require.Zero(t, n)
		return nil
	}))
}

func TestLeaveLedgerIsolationPermissionsAndImmutability(t *testing.T) {
	h := newLedgerHarness(t)
	code, out := h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 4, "reviewed": true})
	require.Equal(t, 200, code, out)
	code, year := h.call("PUT", "/hr/leave-years/2024", "admin", nil)
	require.Equal(t, 200, code, year)
	require.Equal(t, "2024-04-01", year["start_on"])
	require.Equal(t, "2025-03-31", year["end_on"])
	code, again := h.call("PUT", "/hr/leave-years/2024", "admin", nil)
	require.Equal(t, 200, code)
	require.Equal(t, year["id"], again["id"])
	structure := h.structure(uuid.NewString(), uuid.NewString())
	person := h.staff("SYN-I1")
	code, out = h.assign(person, structure["revision_id"], "2024-04-01", 0)
	require.Equal(t, 200, code, out)

	for _, tc := range []struct {
		user   string
		status int
	}{{"reader", 404}, {"directory", 403}, {"corrector", 403}, {"foreign", 404}, {"", 401}} {
		code, _ := h.do("POST", h.base+"/hr/leave-years/2024/postings", tc.user, `{"month":1}`)
		require.Equal(t, tc.status, code, tc.user)
		code, _ = h.do("PUT", h.base+"/staff/"+person+"/leave-assignment", tc.user, `{"expected_version":1,"structure_revision_id":"`+structure["revision_id"].(string)+`","active":false,"effective_on":"2024-05-01","reviewed":true}`)
		require.Equal(t, tc.status, code, tc.user)
	}
	for _, path := range []string{"/hr/leave-settings", "/hr/leave-years", "/hr/leave-assignments", "/hr/leave-balances?leave_year=2024", "/staff/" + person + "/leave-balances?leave_year=2024", "/staff/" + person + "/leave-ledger?leave_year=2024"} {
		code, _ := h.do("GET", h.base+path, "foreign", "")
		require.Equal(t, 404, code, path)
		code, _ = h.do("GET", h.base+path, "reader", "")
		require.Equal(t, 200, code, path)
	}
	code, _ = h.call("GET", "/hr/leave-balances?leave_year=2030", "reader", nil)
	require.Equal(t, 404, code, "unopened year")
	code, _ = h.call("GET", "/staff/"+person+"/leave-ledger", "reader", nil)
	require.Equal(t, 400, code, "leave_year is required")
	code, out = h.post("2024", 1)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["entries"], "one monthly accrual and one upfront allocation")

	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_leave_ledger").Count(&n).Error)
		require.Zero(t, n, "forced RLS without hospital scope")
		return nil
	}))
	authorized := func(statement string, args ...any) error {
		return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_ledger','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(statement, args...).Error
		})
	}
	require.Error(t, authorized(`UPDATE workforce_leave_ledger SET half_days = 99`))
	require.Error(t, authorized(`DELETE FROM workforce_leave_ledger`))
	require.Error(t, authorized(`DELETE FROM workforce_leave_years`))
	require.Error(t, authorized(`UPDATE workforce_leave_years SET posted_months = 3, version = version + 1`), "months advance one at a time")
	require.Error(t, authorized(`UPDATE workforce_leave_runs SET entries = 0`))
	require.Error(t, authorized(`INSERT INTO workforce_leave_ledger(tenant_id,hospital_id,id,staff_id,leave_type_id,leave_type_revision_id,leave_year,kind,half_days,effective_on,idempotency_key,reason,actor_subject)
 SELECT tenant_id,hospital_id,gen_random_uuid(),staff_id,leave_type_id,leave_type_revision_id,leave_year,'correction',2,'2026-01-01','synthetic-outside','Synthetic outside the year','user-admin' FROM workforce_leave_ledger LIMIT 1`), "entries outside the year are refused")
	err := h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true)`, h.hospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_leave_runs(tenant_id,hospital_id,id,leave_year,step,entries,actor_subject) VALUES(?,?,gen_random_uuid(),2024,2,0,'user-admin')`, testutil.TenantA, h.hospital).Error
	})
	require.Error(t, err, "writes need the transaction-local ledger flag")
}

func TestLeaveLedgerRefusesPeriodsThatHaveNotArrived(t *testing.T) {
	h := newLedgerHarness(t)
	code, out := h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 1, "reviewed": true})
	require.Equal(t, 200, code, out)
	current := time.Now().Add(-12 * time.Hour).UTC().Year()
	early := "Open at most next year, post a month once it starts and close a year after it ends."
	code, out = h.call("PUT", "/hr/leave-years/"+strconv.Itoa(current+2), "admin", nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, early, out["message"])
	for _, year := range []int{current, current + 1} {
		code, out = h.call("PUT", "/hr/leave-years/"+strconv.Itoa(year), "admin", nil)
		require.Equal(t, 200, code, out)
	}
	code, out = h.post(strconv.Itoa(current+1), 1)
	require.Equal(t, 409, code, out)
	require.Equal(t, early, out["message"], "next year's first month has not started")

	// Stand in for twelve postings so only the year-end guard can refuse the close.
	for range 12 {
		require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_ledger','authorized',true)`, h.hospital).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workforce_leave_years SET posted_months=posted_months+1,version=version+1 WHERE hospital_id=? AND leave_year=?`, h.hospital, current).Error
		}))
	}
	code, out = h.call("POST", "/hr/leave-years/"+strconv.Itoa(current)+"/close", "admin", nil)
	require.Equal(t, 409, code, out)
	require.Equal(t, early, out["message"], "the year has not ended")
}
