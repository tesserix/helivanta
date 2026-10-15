package workforce_test

import (
	"encoding/json"
	"maps"
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

type leaveHarness struct {
	t    *testing.T
	do   func(method, path, user, body string) (int, string)
	base string
}

func newLeaveHarness(t *testing.T) (*leaveHarness, *tenantdb.DB, string) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermLeaveConfigure}, "reader": {workforce.PermLeaveConfigure}, "directory": {}, "foreign": {workforce.PermLeaveConfigure}}, Modules: []platform.Module{workforce.New()}})
	hospital := uuid.NewString()
	h := &leaveHarness{t: t, base: "/v1/workforce/hospitals/" + hospital, do: func(method, path, user, body string) (int, string) {
		w := testutil.Do(r, method, path, user, body)
		return w.Code, w.Body.String()
	}}
	code, out := h.do("PUT", h.base, "admin", `{"name":"Synthetic Leave Hospital"}`)
	require.Equal(t, 200, code, out)
	for user, level := range map[string]string{"admin": "manager", "reader": "reader", "directory": "manager"} {
		code, out = h.do("PUT", h.base+"/access/user-"+user, "admin", `{"level":"`+level+`"}`)
		require.Equal(t, 200, code, out)
	}
	return h, db, hospital
}

func (h *leaveHarness) put(id string, body map[string]any) (int, map[string]any) {
	data, err := json.Marshal(body)
	require.NoError(h.t, err)
	code, raw := h.do("PUT", h.base+"/hr/leave-configs/"+id, "admin", string(data))
	var out map[string]any
	require.NoError(h.t, json.Unmarshal([]byte(raw), &out), raw)
	return code, out
}

func leaveType(code, name string, tracked bool) map[string]any {
	return map[string]any{"expected_version": 0, "kind": "leave_type", "code": code, "name": name, "description": "", "active": true, "effective_on": "2026-01-01", "reviewed": true, "rules": map[string]any{"paid": true, "balance_tracked": tracked, "half_day_allowed": true, "short_leave_minutes": 120, "clubbable": false}, "holidays": []any{}, "entries": []any{}}
}

func with(body map[string]any, changes map[string]any) map[string]any {
	out := maps.Clone(body)
	maps.Copy(out, changes)
	return out
}

func TestLeaveTypesReplayVersionAndScopeAccess(t *testing.T) {
	h, db, hospital := newLeaveHarness(t)
	id := uuid.NewString()
	body := leaveType("syn-casual", "  Synthetic   Casual  ", true)
	codes := make([]int, 4)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Go(func() { codes[i], _ = h.put(id, body) })
	}
	wg.Wait()
	for _, code := range codes {
		require.Equal(t, 200, code)
	}
	code, created := h.put(id, body)
	require.Equal(t, 200, code, created)
	require.Equal(t, "SYN-CASUAL", created["code"])
	require.Equal(t, "Synthetic Casual", created["name"])
	require.EqualValues(t, 1, created["version"])
	require.Equal(t, map[string]any{"paid": true, "balance_tracked": true, "half_day_allowed": true, "short_leave_minutes": float64(120), "clubbable": false}, created["rules"])

	data, err := json.Marshal(body)
	require.NoError(t, err)
	for _, tc := range []struct {
		user   string
		status int
	}{{"reader", 404}, {"directory", 403}, {"foreign", 404}, {"", 401}} {
		code, _ := h.do("PUT", h.base+"/hr/leave-configs/"+id, tc.user, string(data))
		require.Equal(t, tc.status, code, tc.user)
	}
	code, raw := h.do("GET", h.base+"/hr/leave-configs?kind=leave_type", "reader", "")
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, `"code":"SYN-CASUAL"`)
	code, _ = h.do("GET", h.base+"/hr/leave-configs?kind=holiday_calendar", "reader", "")
	require.Equal(t, 200, code)
	code, _ = h.do("GET", h.base+"/hr/leave-configs?kind=payroll", "reader", "")
	require.Equal(t, 400, code)
	code, _ = h.do("GET", h.base+"/hr/leave-configs", "foreign", "")
	require.Equal(t, 404, code)
	code, _ = h.do("GET", h.base+"/hr/leave-configs/"+uuid.NewString()+"/history", "reader", "")
	require.Equal(t, 404, code)

	revised := with(body, map[string]any{"expected_version": 1, "name": "Synthetic Casual Revised", "effective_on": "2026-04-01"})
	code, out := h.put(id, revised)
	require.Equal(t, 200, code, out)
	require.EqualValues(t, 2, out["version"])
	code, _ = h.put(id, with(revised, map[string]any{"name": "Stale"}))
	require.Equal(t, 409, code)
	code, _ = h.put(id, with(revised, map[string]any{"expected_version": 2, "code": "SYN-OTHER"}))
	require.Equal(t, 409, code)
	code, _ = h.put(id, with(revised, map[string]any{"expected_version": 2, "effective_on": "2026-03-01"}))
	require.Equal(t, 400, code)
	code, _ = h.put(uuid.NewString(), with(body, map[string]any{"expected_version": 3}))
	require.Equal(t, 404, code)
	code, _ = h.put(uuid.NewString(), with(body, map[string]any{"code": "SYN-COPY", "name": "synthetic casual revised"}))
	require.Equal(t, 409, code, "names are unique per kind")

	code, raw = h.do("GET", h.base+"/hr/leave-configs/"+id+"/history", "reader", "")
	require.Equal(t, 200, code, raw)
	require.Contains(t, raw, `"name":"Synthetic Casual"`)
	require.Contains(t, raw, `"name":"Synthetic Casual Revised"`)
	require.Contains(t, raw, `"config_id":"`+id+`"`)

	rules := body["rules"].(map[string]any)
	for name, invalid := range map[string]map[string]any{
		"unreviewed":       {"reviewed": false},
		"impossible date":  {"effective_on": "2026-02-30"},
		"missing rules":    {"rules": nil},
		"short leave step": {"rules": with(rules, map[string]any{"short_leave_minutes": 20})},
		"short leave max":  {"rules": with(rules, map[string]any{"short_leave_minutes": 495})},
		"calendar year":    {"calendar_year": 2026},
		"holidays":         {"holidays": []any{map[string]any{"holiday_date": "2026-01-26", "name": "Synthetic Day", "category": "government"}}},
		"nul name":         {"name": "Synthetic\x00Casual"},
		"bad code":         {"code": "SYN CASUAL"},
		"injected tenant":  {"tenant_id": testutil.TenantB},
		"unknown kind":     {"kind": "payroll"},
	} {
		changes := map[string]any{"code": "SYN-INVALID", "name": "Synthetic Invalid"}
		maps.Copy(changes, invalid)
		code, out := h.put(uuid.NewString(), with(body, changes))
		require.Equal(t, 400, code, "%s: %v", name, out)
	}

	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("workforce_leave_type_rules").Count(&n).Error)
		require.Zero(t, n, "forced RLS without hospital scope")
		require.NoError(t, tx.Table("outbox_events").Where("subject = ?", workforce.LeaveConfigChanged).Count(&n).Error)
		require.EqualValues(t, 2, n)
		require.NoError(t, tx.Table("workforce_audit").Where("action = ?", "workforce.leave_config.changed").Count(&n).Error)
		require.EqualValues(t, 2, n)
		return nil
	}))
	authorized := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave','authorized',true)`, hospital).Error
	}
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := authorized(tx); err != nil {
			return err
		}
		return tx.Exec("UPDATE workforce_leave_type_rules SET paid = false WHERE revision_id = ?", created["revision_id"]).Error
	})
	require.Error(t, err, "type rules are immutable")
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := authorized(tx); err != nil {
			return err
		}
		return tx.Exec("UPDATE workforce_leave_config_revisions SET name = 'Forbidden' WHERE id = ?", created["revision_id"]).Error
	})
	require.Error(t, err, "revisions are append only")
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := authorized(tx); err != nil {
			return err
		}
		return tx.Exec("DELETE FROM workforce_leave_configs WHERE id = ?", id).Error
	})
	require.Error(t, err, "configurations are never deleted")
	err = db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := authorized(tx); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_leave_holidays(tenant_id,hospital_id,revision_id,holiday_date,name,category) VALUES(?,?,?,'2026-01-26','Late','regular')`, testutil.TenantA, hospital, created["revision_id"]).Error
	})
	require.Error(t, err, "children only join their revision in its creating transaction")
}

func TestHolidayCalendarsCoverOneYearWithUniqueDates(t *testing.T) {
	h, _, _ := newLeaveHarness(t)
	holiday := func(date, name, category string) map[string]any {
		return map[string]any{"holiday_date": date, "name": name, "category": category}
	}
	body := map[string]any{"expected_version": 0, "kind": "holiday_calendar", "code": "SYN-2027", "name": "Synthetic 2027", "description": "Synthetic calendar", "active": true, "effective_on": "2026-12-01", "reviewed": true, "calendar_year": 2027, "holidays": []any{holiday("2027-08-15", "Synthetic Independence", "government"), holiday("2027-01-26", " Synthetic  Republic ", "government"), holiday("2027-03-22", "Synthetic Festival", "optional")}, "entries": []any{}}
	id := uuid.NewString()
	code, out := h.put(id, body)
	require.Equal(t, 200, code, out)
	days := out["holidays"].([]any)
	require.Len(t, days, 3)
	require.Equal(t, "2027-01-26", days[0].(map[string]any)["holiday_date"])
	require.Equal(t, "Synthetic Republic", days[0].(map[string]any)["name"])
	require.EqualValues(t, 2027, out["calendar_year"])
	code, _ = h.put(id, body)
	require.Equal(t, 200, code, "replay with unsorted holidays is idempotent")

	code, _ = h.put(uuid.NewString(), with(body, map[string]any{"code": "SYN-2027B", "name": "Synthetic Duplicate Year"}))
	require.Equal(t, 409, code, "one calendar per year")
	code, out = h.put(id, with(body, map[string]any{"expected_version": 1, "calendar_year": 2028, "holidays": []any{}}))
	require.Equal(t, 409, code, out)
	code, out = h.put(uuid.NewString(), with(body, map[string]any{"code": "SYN-EMPTY", "name": "Synthetic Empty", "calendar_year": 2028, "holidays": []any{}}))
	require.Equal(t, 200, code, out)

	for name, invalid := range map[string]map[string]any{
		"outside year":    {"holidays": []any{holiday("2030-01-01", "Synthetic", "regular")}},
		"duplicate date":  {"holidays": []any{holiday("2029-01-01", "Synthetic A", "regular"), holiday("2029-01-01", "Synthetic B", "regular")}},
		"effective late":  {"effective_on": "2029-01-02"},
		"missing year":    {"calendar_year": nil},
		"rules":           {"rules": leaveType("X", "X", true)["rules"]},
		"bad category":    {"holidays": []any{holiday("2029-01-01", "Synthetic", "bank")}},
		"blank name":      {"holidays": []any{holiday("2029-01-01", "   ", "regular")}},
		"impossible date": {"holidays": []any{holiday("2029-02-29", "Synthetic", "regular")}},
	} {
		changes := map[string]any{"code": "SYN-2029", "name": "Synthetic 2029", "calendar_year": 2029}
		maps.Copy(changes, invalid)
		code, out := h.put(uuid.NewString(), with(body, changes))
		require.Equal(t, 400, code, "%s: %v", name, out)
	}
}

func TestLeaveStructuresPinCurrentTypeRevisions(t *testing.T) {
	h, _, _ := newLeaveHarness(t)
	earned, unpaid := uuid.NewString(), uuid.NewString()
	earnedBody := leaveType("SYN-EARNED", "Synthetic Earned", true)
	code, typeA := h.put(earned, earnedBody)
	require.Equal(t, 200, code, typeA)
	code, typeB := h.put(unpaid, leaveType("SYN-UNPAID", "Synthetic Unpaid", false))
	require.Equal(t, 200, code, typeB)
	entry := func(revision any, half, carry int) map[string]any {
		return map[string]any{"leave_type_revision_id": revision, "annual_half_days": half, "accrual": "monthly", "carry_forward_half_days": carry, "proration": "join_month", "probation_eligible": false}
	}
	body := map[string]any{"expected_version": 0, "kind": "structure", "code": "SYN-NURSING", "name": "Synthetic Nursing", "description": "", "active": true, "effective_on": "2026-04-01", "reviewed": true, "holidays": []any{}, "entries": []any{entry(typeA["revision_id"], 24, 10), entry(typeB["revision_id"], 0, 0)}}
	id := uuid.NewString()
	code, structure := h.put(id, body)
	require.Equal(t, 200, code, structure)
	entries := structure["entries"].([]any)
	require.Len(t, entries, 2)
	first := entries[0].(map[string]any)
	require.Equal(t, "SYN-EARNED", first["code"])
	require.Equal(t, earned, first["leave_type_id"])
	require.EqualValues(t, 24, first["annual_half_days"])
	require.Equal(t, false, entries[1].(map[string]any)["balance_tracked"])

	code, revisedType := h.put(earned, with(earnedBody, map[string]any{"expected_version": 1, "name": "Synthetic Earned Revised"}))
	require.Equal(t, 200, code, revisedType)
	kept := with(body, map[string]any{"expected_version": 1, "description": "Pinned revision retained"})
	code, out := h.put(id, kept)
	require.Equal(t, 200, code, out)
	require.Equal(t, typeA["revision_id"], out["entries"].([]any)[0].(map[string]any)["leave_type_revision_id"])
	code, out = h.put(uuid.NewString(), with(body, map[string]any{"code": "SYN-STALE", "name": "Synthetic Stale"}))
	require.Equal(t, 400, code, "a new structure cannot pin a superseded type revision: %v", out)
	require.Equal(t, "invalid_reference", out["error"])
	code, out = h.put(id, with(kept, map[string]any{"expected_version": 2, "effective_on": "2026-05-01"}))
	require.Equal(t, 400, code, "moving the effective date re-validates every reference: %v", out)

	late := uuid.NewString()
	code, lateType := h.put(late, with(leaveType("SYN-LATE", "Synthetic Late", true), map[string]any{"effective_on": "2026-06-01"}))
	require.Equal(t, 200, code, lateType)
	current := []any{entry(revisedType["revision_id"], 24, 10)}
	for name, invalid := range map[string]map[string]any{
		"untracked allowance": {"entries": []any{entry(typeB["revision_id"], 2, 0)}},
		"untracked carry":     {"entries": []any{entry(typeB["revision_id"], 0, 2)}},
		"duplicate type":      {"entries": append(current, entry(revisedType["revision_id"], 2, 0))},
		"empty":               {"entries": []any{}},
		"unknown":             {"entries": []any{entry(uuid.NewString(), 2, 0)}},
		"later type":          {"entries": []any{entry(lateType["revision_id"], 2, 0)}},
		"structure revision":  {"entries": []any{entry(structure["revision_id"], 2, 0)}},
		"bad accrual":         {"entries": []any{with(entry(revisedType["revision_id"], 2, 0), map[string]any{"accrual": "weekly"})}},
		"over allowance":      {"entries": []any{entry(revisedType["revision_id"], 731, 0)}},
		"calendar year":       {"calendar_year": 2026, "entries": current},
	} {
		changes := map[string]any{"code": "SYN-BAD", "name": "Synthetic Bad"}
		maps.Copy(changes, invalid)
		code, out := h.put(uuid.NewString(), with(body, changes))
		require.Equal(t, 400, code, "%s: %v", name, out)
	}

	code, retired := h.put(unpaid, with(leaveType("SYN-UNPAID", "Synthetic Unpaid", false), map[string]any{"expected_version": 1, "active": false}))
	require.Equal(t, 200, code, retired)
	code, out = h.put(uuid.NewString(), with(body, map[string]any{"code": "SYN-RETIRED", "name": "Synthetic Retired", "entries": []any{entry(retired["revision_id"], 0, 0)}}))
	require.Equal(t, 400, code, "retired types cannot join new structures: %v", out)
	code, out = h.put(uuid.NewString(), with(body, map[string]any{"code": "SYN-CURRENT", "name": "Synthetic Current", "entries": current}))
	require.Equal(t, 200, code, out)
}
