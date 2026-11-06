package workforce_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type leaveDepartmentSession struct {
	decision   bool
	department bool
	subtree    []string
}

func (h *requestHarness) leaveReviewRows(staff, request, actor string, s leaveDepartmentSession) []string {
	h.t.Helper()
	var ids []string
	require.NoError(h.t, h.db.WithTenant(h.t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-department-reviewer',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_review_actor',?,true)`, h.hospital, actor).Error; err != nil {
			return err
		}
		if s.decision {
			if err := tx.Exec(`SELECT set_config('app.workforce_leave_review','authorized',true),set_config('app.workforce_leave_review_staff',?,true),set_config('app.workforce_leave_review_request',?,true)`, staff, request).Error; err != nil {
				return err
			}
		} else if err := tx.Exec(`SELECT set_config('app.workforce_leave_review_read','authorized',true),set_config('app.workforce_leave_review_read_kind','queue',true)`).Error; err != nil {
			return err
		}
		if s.department {
			if err := tx.Exec(`SELECT set_config('app.workforce_leave_department','authorized',true),set_config('app.workforce_leave_department_subtree',?,true)`, strings.Join(s.subtree, ",")).Error; err != nil {
				return err
			}
		}
		if s.decision {
			return tx.Raw(`SELECT id::text FROM workforce_leave_requests WHERE id=?`, request).Scan(&ids).Error
		}
		return tx.Raw(`SELECT request_id::text FROM workforce_leave_review_read_requests()`).Scan(&ids).Error
	}))
	return ids
}

func TestDepartmentLeaveSessionsHideRequestersWithoutAPostingInTheSubtree(t *testing.T) {
	h := newRequestHarness(t)
	types := configureLeave(t, h.call, 2026)
	h.post("2026", 1)
	nurse := h.enrolled("SYN-DEPT-LEAVE-NURSE", types, "2026-01-01")
	filed := uuid.NewString()
	code, out := h.file(nurse, filed, "clerk", full(types.duty, "2026-10-05", "2026-10-05"))
	require.Equal(t, 200, code, out)
	actor, held := uuid.NewString(), []string{uuid.NewString(), uuid.NewString()}

	for _, decision := range []bool{false, true} {
		require.Equal(t, []string{filed}, h.leaveReviewRows(nurse, filed, actor, leaveDepartmentSession{decision: decision}), "an unflagged review session keeps the existing policies (decision=%v)", decision)
		require.Empty(t, h.leaveReviewRows(nurse, filed, actor, leaveDepartmentSession{decision: decision, department: true, subtree: held}), "a requester with no approved posting in the subtree is hidden (decision=%v)", decision)
		require.Empty(t, h.leaveReviewRows(nurse, filed, actor, leaveDepartmentSession{decision: decision, department: true}), "a department session without a subtree fails closed (decision=%v)", decision)
	}

	var plain int64
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-admin',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_leave_department','authorized',true)`, h.hospital).Error; err != nil {
			return err
		}
		return tx.Table("workforce_leave_requests").Where("id=?", filed).Count(&plain).Error
	}))
	require.EqualValues(t, 1, plain, "the department flag outside a leave review session changes no other read")

	require.NoError(t, h.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_drop_leave_department_review_guard", SQL: "DROP POLICY leave_department_review_guard ON workforce_leave_requests;"}}))
	require.Equal(t, []string{filed}, h.leaveReviewRows(nurse, filed, actor, leaveDepartmentSession{department: true, subtree: held}), "the hiding must come from the restrictive policy")
}
