package workforce

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

const workforcePolicyReads = `WITH workforce_policy_reads AS (
 SELECT p.*,CASE
 WHEN p.polrelid=to_regclass('public.workforce_staff') AND p.polname='tenant_isolation'
 AND position('workforce_staff_read_policy' IN pg_get_expr(p.polqual,p.polrelid))>0
 AND to_regprocedure('public.workforce_staff_read_policy(workforce_staff)') IS NOT NULL
 THEN pg_get_functiondef(to_regprocedure('public.workforce_staff_read_policy(workforce_staff)'))
 WHEN p.polrelid=to_regclass('public.workforce_staff') AND p.polname='partner_candidate_update_guard'
 AND position('workforce_staff_lock_policy' IN pg_get_expr(p.polqual,p.polrelid))>0
 AND to_regprocedure('public.workforce_staff_lock_policy(workforce_staff)') IS NOT NULL
 THEN pg_get_functiondef(to_regprocedure('public.workforce_staff_lock_policy(workforce_staff)'))
 ELSE pg_get_expr(p.polqual,p.polrelid) END AS read_expression
 FROM pg_policy p
) `

func scanWorkforcePolicySupport(tx *gorm.DB, query string, supported *bool) error {
	var planned bool
	if err := tx.Raw(`SELECT to_regprocedure('public.workforce_staff_read_policy(workforce_staff)') IS NOT NULL
 OR to_regprocedure('public.workforce_staff_lock_policy(workforce_staff)') IS NOT NULL`).Scan(&planned).Error; err != nil {
		return err
	}
	if planned {
		var intact bool
		if err := tx.Raw(`SELECT workforce_assignment_selection_integrity() AND workforce_operating_selection_integrity()`).Scan(&intact).Error; err != nil {
			return err
		}
		// Only the fingerprinted predicates may stand in for direct policy text.
		if !intact {
			return authz.ErrScopedAuthorizationUnavailable
		}
	}
	return tx.Raw(workforcePolicyReads + query).Scan(supported).Error
}
