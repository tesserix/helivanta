package workforce_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const notInStructure = "Select a leave type in the staff member's assigned leave structure on the effective date."

// balanceRows renders each balance as code:half-days:in-structure, in response order.
func (h *ledgerHarness) balanceRows(staff, query string) []string {
	h.t.Helper()
	code, out := h.call("GET", "/staff/"+staff+"/leave-balances?"+query, "reader", nil)
	require.Equal(h.t, 200, code, out)
	rows := []string{}
	for _, b := range out["balances"].([]any) {
		row := b.(map[string]any)
		rows = append(rows, fmt.Sprintf("%s:%v:%v", row["code"], row["half_days"], row["in_structure"]))
	}
	return rows
}

func TestLeaveOpeningBalancesFollowTheAssignedStructure(t *testing.T) {
	h := newLedgerHarness(t)
	code, out := h.call("PUT", "/hr/leave-settings", "admin", map[string]any{"year_start_month": 1, "reviewed": true})
	require.Equal(t, 200, code, out)
	earned, annual, outside := uuid.NewString(), uuid.NewString(), uuid.NewString()
	first := h.structure(earned, annual)
	code, outsideType := h.put(outside, with(leaveType("SYN-OUTSIDE", "Synthetic Outside", true), map[string]any{"effective_on": "2024-01-01"}))
	require.Equal(t, 200, code, outsideType)
	code, second := h.put(uuid.NewString(), map[string]any{"expected_version": 0, "kind": "structure", "code": "SYN-SECOND", "name": "Synthetic Second", "description": "", "active": true, "effective_on": "2024-01-01", "reviewed": true, "holidays": []any{},
		"entries": []any{map[string]any{"leave_type_revision_id": outsideType["revision_id"], "annual_half_days": 6, "accrual": "monthly", "carry_forward_half_days": 0, "proration": "none", "probation_eligible": false}}})
	require.Equal(t, 200, code, second)
	code, out = h.call("PUT", "/hr/leave-years/2024", "admin", nil)
	require.Equal(t, 200, code, out)
	joiner, unassigned := h.staff("SYN-O1"), h.staff("SYN-O2")
	code, out = h.assign(joiner, first["revision_id"], "2024-03-01", 0)
	require.Equal(t, 200, code, out)

	require.Equal(t, []string{"SYN-ANNUAL:0:true", "SYN-EARNED:0:true"}, h.balanceRows(joiner, "leave_year=2024&as_of=2024-03-31"), "a new assignment lists its structure at zero")
	require.Empty(t, h.balanceRows(joiner, "leave_year=2024&as_of=2024-02-28"), "no structure is in force before the assignment")
	require.Empty(t, h.balanceRows(unassigned, "leave_year=2024"))

	credit := func(staff, leaveType, effective string, halfDays int) (int, map[string]any) {
		return h.correction(staff, uuid.NewString(), "corrector", map[string]any{"leave_type_id": leaveType, "leave_year": 2024, "half_days": halfDays, "effective_on": effective, "reason": "Synthetic opening balance from go-live"})
	}
	for name, tc := range map[string]struct{ staff, leaveType, effective string }{
		"type outside the structure":  {joiner, outside, "2024-03-15"},
		"date before the assignment":  {joiner, earned, "2024-02-15"},
		"staff member never assigned": {unassigned, earned, "2024-03-15"},
	} {
		code, out = credit(tc.staff, tc.leaveType, tc.effective, 4)
		require.Equal(t, 400, code, "%s: %v", name, out)
		require.Equal(t, "invalid_reference", out["error"], name)
		require.Equal(t, notInStructure, out["message"], name)
	}
	code, out = credit(joiner, uuid.NewString(), "2024-03-15", 4)
	require.Equal(t, 400, code, "unknown type: %v", out)
	require.Equal(t, "invalid_reference", out["error"])

	code, out = credit(joiner, earned, "2024-03-01", 7)
	require.Equal(t, 200, code, out)
	require.Equal(t, []string{"SYN-ANNUAL:0:true", "SYN-EARNED:7:true"}, h.balanceRows(joiner, "leave_year=2024&as_of=2024-03-31"))

	// A transfer to another structure keeps the earlier balance visible but no longer correctable on later dates.
	code, out = h.call("PUT", "/staff/"+joiner+"/leave-assignment", "admin", map[string]any{"expected_version": 1, "structure_revision_id": second["revision_id"], "active": true, "effective_on": "2024-06-01", "reviewed": true})
	require.Equal(t, 200, code, out)
	require.Equal(t, []string{"SYN-EARNED:7:false", "SYN-OUTSIDE:0:true"}, h.balanceRows(joiner, "leave_year=2024&as_of=2024-06-30"))
	code, out = credit(joiner, earned, "2024-06-15", -2)
	require.Equal(t, 400, code, out)
	require.Equal(t, notInStructure, out["message"])
	code, out = credit(joiner, earned, "2024-05-01", -2)
	require.Equal(t, 200, code, "the structure in force on the entry's date governs: %v", out)

	// An inactive assignment still names its structure, so a carried balance stays correctable.
	code, out = h.call("PUT", "/staff/"+joiner+"/leave-assignment", "admin", map[string]any{"expected_version": 2, "structure_revision_id": second["revision_id"], "active": false, "effective_on": "2024-09-01", "reviewed": true})
	require.Equal(t, 200, code, out)
	code, out = credit(joiner, outside, "2024-09-15", 3)
	require.Equal(t, 200, code, out)
	require.Equal(t, []string{"SYN-EARNED:5:false", "SYN-OUTSIDE:3:true"}, h.balanceRows(joiner, "leave_year=2024"))

	code, page := h.call("GET", "/hr/leave-balances?leave_year=2024&as_of=2024-03-31&limit=100", "reader", nil)
	require.Equal(t, 200, code, page)
	listed := map[string][]any{}
	for _, row := range page["data"].([]any) {
		staff := row.(map[string]any)
		listed[staff["staff_id"].(string)] = staff["balances"].([]any)
	}
	require.Len(t, listed[joiner], 2, "the hospital list applies the same rule")
	require.Equal(t, true, listed[joiner][0].(map[string]any)["in_structure"])
	require.Empty(t, listed[unassigned])
}
