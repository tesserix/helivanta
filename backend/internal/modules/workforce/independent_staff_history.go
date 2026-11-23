package workforce

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (g *independentStaffGuard) WithIndependentStaffHistoryReview(ctx context.Context, scope authz.IndependentStaffScope, expected authz.IndependentStaffReference, participant authz.IndependentStaffObservation, action func(context.Context) error) error {
	ref := participant.Reference
	if action == nil || tenantdb.ActorFrom(ctx) != scope.Subject || !validLoginSubject(participant.Subject) || ref.StaffID.Version() != 7 || ref.StaffID.Variant() != uuid.RFC4122 || ref.LinkID.Version() != 7 || ref.LinkID.Variant() != uuid.RFC4122 || ref.StaffVersion < 1 || ref.LinkVersion < 2 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.WithCurrentIndependentStaff(ctx, scope, func(currentCtx context.Context, current authz.IndependentStaffReference) error {
		if current != expected {
			return authz.ErrScopedCommandDenied
		}
		if scope.Subject == participant.Subject || current.StaffID == ref.StaffID {
			return authz.ErrScopedCommandDenied
		}
		return g.db.WithTenantCommand(currentCtx, scope.TenantID, func(tx *gorm.DB) (err error) {
			var previous string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_independent_identity','app.workforce_independent_employer','app.workforce_independent_kind','app.workforce_independent_subject','app.workforce_independent_staff','app.workforce_independent_actor','app.workforce_independent_proposer'])AS key`).Scan(&previous).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			restored := false
			restore := func() error {
				return tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error
			}
			defer func() {
				if !restored {
					err = errors.Join(err, restore())
				}
			}()
			if err = tx.Exec(`SELECT set_config('app.workforce_independent_identity','source',true),set_config('app.workforce_independent_employer',?,true),set_config('app.workforce_independent_kind',?,true),set_config('app.workforce_independent_subject',?,true),set_config('app.workforce_independent_staff',?,true),set_config('app.workforce_independent_actor',?,true),set_config('app.workforce_independent_proposer',?,true)`, scope.EmployerID.String(), scope.EmployerKind, participant.Subject, current.StaffID.String(), scope.Subject, participant.Subject).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			var exists bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id)
    WHERE l.tenant_id=? AND l.hospital_id=? AND l.id=? AND l.staff_id=? AND l.subject=? AND l.independent_staff_version=?
    AND l.employment_generation_id IS NULL AND h.employment_generation_id IS NULL AND h.version=? AND h.status='active')`, scope.TenantID, scope.EmployerID, ref.LinkID, ref.StaffID, participant.Subject, ref.StaffVersion, ref.LinkVersion).Scan(&exists).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if !exists {
				return authz.ErrScopedCommandDenied
			}
			var conflicts bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id)
    WHERE l.tenant_id=? AND l.hospital_id=? AND h.status='active' AND ((l.staff_id=? AND l.subject=?)OR(l.staff_id=? AND l.subject=?)))
    OR EXISTS(SELECT 1 FROM workforce_login_links a JOIN workforce_login_link_history ah ON(ah.tenant_id,ah.hospital_id,ah.id)=(a.tenant_id,a.hospital_id,a.id)
    JOIN workforce_login_links b ON(b.tenant_id,b.hospital_id,b.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id)
    JOIN workforce_login_link_history bh ON(bh.tenant_id,bh.hospital_id,bh.id)=(b.tenant_id,b.hospital_id,b.id)
    WHERE a.tenant_id=? AND a.hospital_id=? AND a.subject=? AND b.subject=? AND ah.status='active' AND bh.status='active')`, scope.TenantID, scope.EmployerID, ref.StaffID, scope.Subject, current.StaffID, participant.Subject, scope.TenantID, scope.EmployerID, scope.Subject, participant.Subject).Scan(&conflicts).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if conflicts {
				return authz.ErrScopedCommandDenied
			}
			if err = tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.independent_staff.review_history',?,jsonb_build_object('employer_kind',?::text,'participant_staff_id',?::uuid,'participant_staff_version',?::bigint,'participant_link_id',?::uuid,'participant_link_version',?::bigint,'reviewer_link_id',?::uuid,'reviewer_link_version',?::bigint))`, scope.TenantID, scope.EmployerID, scope.Subject, current.StaffID, scope.EmployerKind, ref.StaffID, ref.StaffVersion, ref.LinkID, ref.LinkVersion, current.LinkID, current.LinkVersion).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if err = restore(); err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			restored = true
			return action(currentCtx)
		})
	})
}
