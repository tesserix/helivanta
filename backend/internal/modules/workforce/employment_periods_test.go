package workforce_test

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
	"testing"
)

func TestEmploymentPeriodDraftKeepsVersionedContractTerms(t *testing.T) {
	h := newRequestHarness(t)
	staff := h.staff("SYN-PERIOD-WORKER")
	sponsor := h.staff("SYN-PERIOD-SPONSOR")
	code, source := h.call("GET", "/staff/"+staff+"/employment-source", "admin", nil)
	require.Equal(t, 200, code, source)
	path := "/staff/" + staff + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "agency_worker", "duration": "fixed", "starts_at": "2026-11-01T00:00:00Z", "ends_at": "2027-02-28T18:30:00Z", "contracting_party": "Synthetic Agency", "sponsor_id": sponsor, "reason": "Synthetic contractor intake", "evidence_reference": "SYN-CONTRACT-1"}
	code, first := h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, first)
	require.Equal(t, "draft", first["status"])
	require.Equal(t, "Synthetic Agency", first["contracting_party"])
	require.Equal(t, float64(1), first["version"])
	code, replay := h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, replay)
	require.Equal(t, first, replay)
	edit := with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1, "evidence_reference": "SYN-CONTRACT-2"})
	code, second := h.call("PUT", path, "approver", edit)
	require.Equal(t, 200, code, second)
	require.Equal(t, float64(2), second["version"])
	code, replay = h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, replay)
	require.Equal(t, first, replay)
	code, stale := h.call("PUT", path, "admin", with(edit, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String()}))
	require.Equal(t, 409, code, stale)
	code, current := h.call("GET", path, "admin", nil)
	require.Equal(t, 200, code, current)
	require.Equal(t, second, current)
	code, history := h.call("GET", path+"/history", "admin", nil)
	require.Equal(t, 200, code, history)
	require.Len(t, history["data"], 2)
	code, enrollment := h.call("GET", "/staff/"+staff+"/enrollment", "admin", nil)
	require.Equal(t, 200, code, enrollment)
	require.Equal(t, "active", enrollment["staff"].(map[string]any)["employment_status"], "draft terms do not change access or directory state")
}

func TestEmploymentPeriodDraftRejectsIncompleteAndForeignTerms(t *testing.T) {
	h := newRequestHarness(t)
	worker := h.staff("SYN-PERIOD-VALIDATE")
	sponsor := h.staff("SYN-PERIOD-SPONSOR-2")
	code, source := h.call("GET", "/staff/"+worker+"/employment-source", "admin", nil)
	require.Equal(t, 200, code, source)
	path := "/staff/" + worker + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "contractor", "duration": "fixed", "starts_at": "2026-11-01T00:00:00Z", "ends_at": "2027-02-01T00:00:00Z", "contracting_party": "Synthetic Contractor", "sponsor_id": sponsor, "reason": "Synthetic intake", "evidence_reference": "SYN-EVIDENCE"}
	for name, change := range map[string]map[string]any{
		"missing_end": {"ends_at": nil}, "reversed_period": {"ends_at": "2026-10-01T00:00:00Z"}, "ongoing_with_end": {"duration": "ongoing"}, "missing_start": {"starts_at": ""}, "null_start": {"starts_at": nil}, "missing_duration": {"duration": ""}, "missing_kind": {"engagement_kind": ""}, "missing_party": {"contracting_party": " "}, "missing_sponsor": {"sponsor_id": ""}, "self_sponsor": {"sponsor_id": worker}, "missing_reason": {"reason": " "}, "missing_evidence": {"evidence_reference": " "}, "missing_command": {"command_id": "00000000-0000-0000-0000-000000000000"}, "missing_expected": {"expected_version": nil}, "forged_approval": {"status": "approved"},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := h.call("PUT", path, "admin", with(input, change))
			require.Equal(t, 400, code, out)
		})
	}
	for _, user := range []string{"profiler", "foreign"} {
		code, out := h.call("PUT", path, user, input)
		require.Equal(t, 404, code, out)
	}
	code, out := h.call("PUT", path, "clerk", input)
	require.Equal(t, 403, code, out)
	code, out = h.call("PUT", path, "admin", with(input, map[string]any{"sponsor_id": uuid.NewString()}))
	require.Equal(t, 404, code, out)
	code, out = h.call("PUT", path, "admin", with(input, map[string]any{"staff_version": 100}))
	require.Equal(t, 409, code, out)
	code, out = h.call("PUT", path, "admin", with(input, map[string]any{"employment_epoch": 100}))
	require.Equal(t, 409, code, out)
	code, out = h.call("GET", path, "admin", nil)
	require.Equal(t, 404, code, out)
	code, out = h.call("PUT", path, "admin", with(input, map[string]any{"duration": "ongoing", "ends_at": nil}))
	require.Equal(t, 200, code, out)
	require.NotContains(t, out, "ends_at")
}

func TestEmploymentPeriodDraftHistoryIsPrivateAndImmutable(t *testing.T) {
	h := newRequestHarness(t)
	worker := h.staff("SYN-PERIOD-HISTORY")
	code, source := h.call("GET", "/staff/"+worker+"/employment-source", "admin", nil)
	require.Equal(t, 200, code, source)
	path := "/staff/" + worker + "/employment-periods/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-11-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic appointment", "evidence_reference": "SYN-APPOINTMENT"}
	code, out := h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, out)
	for _, table := range []string{"workforce_employment_period_drafts", "workforce_employment_period_history"} {
		for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
			require.NoError(t, h.db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
				var count int64
				err := tx.Table(table).Count(&count).Error
				require.Zero(t, count, "HR data requires enrollment scope even in its owning tenant")
				return err
			}))
		}
		scoped := func(sql string) error {
			return h.db.WithTenantCommand(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, h.hospital).Error; err != nil {
					return err
				}
				return tx.Exec(sql).Error
			})
		}
		require.Error(t, scoped("TRUNCATE "+table))
		if table == "workforce_employment_period_history" {
			require.Error(t, scoped("UPDATE "+table+" SET reason='tampered'"))
		}
	}
	changed := map[string]any{"employee_code": "SYN-PERIOD-HISTORY", "display_name": "Synthetic changed basis", "job_category": "nurse", "employment_status": "inactive"}
	code, out = h.call("PUT", "/staff/"+worker, "admin", changed)
	require.Equal(t, 200, code, out)
	code, out = h.call("PUT", path, "admin", with(input, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 1}))
	require.Equal(t, 409, code, out)
	code, out = h.call("PUT", path, "admin", input)
	require.Equal(t, 200, code, out, "replay returns the original draft without activating employment")
	code, out = h.call("GET", path+"/history?limit=1", "admin", nil)
	require.Equal(t, 200, code, out)
	require.Len(t, out["data"], 1)
}
