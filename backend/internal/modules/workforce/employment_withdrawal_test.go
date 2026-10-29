package workforce_test

import (
	"encoding/json"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEmploymentDraftWithdrawalIsTerminalAndRetained(t *testing.T) {
	h := newRequestHarness(t)
	worker := h.staff("SYN-WITHDRAW-TERMS")
	code, source := h.call("GET", "/staff/"+worker+"/employment-source", "admin", nil)
	require.Equal(t, 200, code, source)
	path := "/staff/" + worker + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-11-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic appointment", "evidence_reference": "SYN-WITHDRAW-REFERENCE"}
	code, first := h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, first)
	withdraw := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "reason": "Synthetic appointment preparation cancelled"}
	code, closed := h.call("POST", path+"/withdraw", "admin", withdraw)
	require.Equal(t, 200, code, closed)
	require.Equal(t, "withdrawn", closed["status"])
	require.Equal(t, float64(2), closed["version"])
	require.Equal(t, withdraw["reason"], closed["reason"])
	for _, field := range []string{"id", "staff_id", "staff_version", "employment_epoch", "engagement_kind", "duration", "starts_at", "evidence_reference", "proposed_by_subject"} {
		require.Equal(t, first[field], closed[field], field)
	}
	code, receipt := h.call("POST", path+"/withdraw", "admin", withdraw)
	require.Equal(t, 200, code, receipt)
	require.Equal(t, closed, receipt)
	code, current := h.call("GET", path+"?lifecycle=withdrawal-v1", "admin", nil)
	require.Equal(t, 200, code, current)
	require.Equal(t, closed, current)
	code, history := h.call("GET", path+"/history?lifecycle=withdrawal-v1", "admin", nil)
	require.Equal(t, 200, code, history)
	require.Len(t, history["data"], 2)
	code, out := h.call("PUT", path, "admin", with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 2}))
	require.Equal(t, 409, code, out)
	code, out = h.call("GET", path, "admin", nil)
	require.Equal(t, 404, code, out, "legacy detail must not receive a new terminal status")
	code, history = h.call("GET", path+"/history", "admin", nil)
	require.Equal(t, 200, code, history)
	require.Len(t, history["data"], 1, "legacy history retains its draft-only wire contract")
}

func prepareWithdrawalDraft(t *testing.T, h *requestHarness, codeName string) (string, string, map[string]any) {
	t.Helper()
	worker := h.staff(codeName)
	code, source := h.call("GET", "/staff/"+worker+"/employment-source", "admin", nil)
	require.Equal(t, 200, code, source)
	path := "/staff/" + worker + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-11-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic appointment", "evidence_reference": "SYN-WITHDRAW-REFERENCE"}
	code, out := h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, out)
	return worker, path, input
}

func TestEmploymentWithdrawalValidationAuthorityAndReceipts(t *testing.T) {
	h := newRequestHarness(t)
	_, path, input := prepareWithdrawalDraft(t, h, "SYN-WITHDRAW-SCOPE")
	command := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "reason": "Synthetic cancelled preparation"}
	for name, change := range map[string]map[string]any{
		"blank reason": {"reason": " "}, "nul": {"reason": "a\x00b"}, "too long": {"reason": strings.Repeat("a", 501)}, "no version": {"expected_version": nil}, "zero version": {"expected_version": 0}, "unsafe version": {"expected_version": 9007199254740990}, "wrong command kind": {"command_id": uuid.NewString()}, "forged status": {"status": "approved"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := h.call("POST", path+"/withdraw", "admin", with(command, change))
			require.Equal(t, 400, code, out)
		})
	}
	for user, status := range map[string]int{"clerk": 403, "profiler": 404, "foreign": 404} {
		code, out := h.call("POST", path+"/withdraw", user, command)
		require.Equal(t, status, code, out)
	}
	code, out := h.call("POST", path+"/withdraw", "admin", with(command, map[string]any{"expected_version": 2}))
	require.Equal(t, 409, code, out)
	code, out = h.call("POST", path+"/withdraw", "admin", with(command, map[string]any{"command_id": input["command_id"]}))
	require.Equal(t, 409, code, out)
	code, closed := h.call("POST", path+"/withdraw", "admin", command)
	require.Equal(t, 200, code, closed)
	code, out = h.call("POST", path+"/withdraw", "admin", with(command, map[string]any{"reason": "Changed intent"}))
	require.Equal(t, 409, code, out)
	code, out = h.call("POST", path+"/withdraw", "approver", command)
	require.Equal(t, 409, code, out)
	code, out = h.call("POST", path+"/withdraw", "admin", with(command, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 2}))
	require.Equal(t, 409, code, out)
	for _, suffix := range []string{"?lifecycle=unknown", "?lifecycle=withdrawal-v1&lifecycle=withdrawal-v1", "/history?lifecycle="} {
		code, out = h.call("GET", path+suffix, "admin", nil)
		require.Equal(t, 400, code, out)
	}
	list := path[:strings.LastIndex(path, "/")]
	code, out = h.call("GET", list, "admin", nil)
	require.Equal(t, 200, code, out)
	require.Empty(t, out["data"])
	code, out = h.call("GET", list+"?lifecycle=withdrawal-v1&limit=1", "admin", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
}

func TestEmploymentWithdrawalClosesObsoleteSourceWithoutChangingTerms(t *testing.T) {
	h := newRequestHarness(t)
	worker, path, input := prepareWithdrawalDraft(t, h, "SYN-WITHDRAW-DRIFT")
	sponsor := h.staff("SYN-WITHDRAW-SPONSOR")
	edit := with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "engagement_kind": "agency_worker", "contracting_party": "Synthetic agency", "sponsor_id": sponsor})
	code, before := h.call("PUT", path, "admin", edit)
	require.Equal(t, 200, code, before)
	for _, s := range []struct{ id, code string }{{worker, "SYN-WITHDRAW-DRIFT"}, {sponsor, "SYN-WITHDRAW-SPONSOR"}} {
		code, out := h.call("PUT", "/staff/"+s.id, "admin", map[string]any{"employee_code": s.code, "display_name": "Synthetic source changed", "job_category": "nurse", "employment_status": "inactive"})
		require.Equal(t, 200, code, out)
	}
	command := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 2, "reason": "Synthetic obsolete preparation cancelled"}
	code, closed := h.call("POST", path+"/withdraw", "admin", command)
	require.Equal(t, 200, code, closed)
	for _, field := range []string{"staff_version", "employment_epoch", "sponsor_id", "contracting_party", "starts_at", "evidence_reference"} {
		require.Equal(t, before[field], closed[field], field)
	}
	code, staff := h.call("GET", "/staff/"+worker, "admin", nil)
	require.Equal(t, 200, code, staff)
	require.Equal(t, "inactive", staff["employment_status"])
}

func TestEmploymentWithdrawalDatabaseGuardsRetainTermsAndFinality(t *testing.T) {
	h := newRequestHarness(t)
	worker, path, _ := prepareWithdrawalDraft(t, h, "SYN-WITHDRAW-GUARD")
	run := func(context, changes string) error {
		return h.db.WithTenantCommand(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_employment_withdrawal',?,true)`, h.hospital, context).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workforce_employment_period_drafts SET revision_id=?,version=version+1,`+changes+` WHERE tenant_id=? AND hospital_id=? AND staff_id=?`, uuid.Must(uuid.NewV7()), testutil.TenantA, h.hospital, worker).Error
		})
	}
	require.Error(t, run("", "status='withdrawn'"))
	require.Error(t, run("authorized", "status='withdrawn',starts_at=starts_at+interval '1 day'"))
	code, out := h.call("POST", path+"/withdraw", "admin", map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "reason": "Synthetic guard test"})
	require.Equal(t, 200, code, out)
	require.Error(t, run("authorized", "status='draft'"))
	require.Error(t, run("authorized", "reason='rewritten'"))
}

func TestEmploymentWithdrawalConcurrentExactCommandsCreateOneRevision(t *testing.T) {
	h := newRequestHarness(t)
	_, path, _ := prepareWithdrawalDraft(t, h, "SYN-WITHDRAW-REPLAY")
	command := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "reason": "Synthetic duplicate command"}
	raw, err := json.Marshal(command)
	require.NoError(t, err)
	type result struct {
		code int
		body string
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			code, body := h.do("POST", h.base+path+"/withdraw", "admin", string(raw))
			results <- result{code, body}
		}()
	}
	first, second := <-results, <-results
	require.Equal(t, 200, first.code, first.body)
	require.Equal(t, 200, second.code, second.body)
	var a, b map[string]any
	require.NoError(t, json.Unmarshal([]byte(first.body), &a))
	require.NoError(t, json.Unmarshal([]byte(second.body), &b))
	require.Equal(t, a, b)
	code, history := h.call("GET", path+"/history?lifecycle=withdrawal-v1", "admin", nil)
	require.Equal(t, 200, code, history)
	require.Len(t, history["data"], 2)
}
