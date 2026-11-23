package workforce

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func validStocktakeActorReference(ref authz.StaffActorReference) bool {
	return ref.StaffID != uuid.Nil && ref.LinkID.Version() == 7 && ref.LinkID.Variant() == uuid.RFC4122 &&
		ref.GenerationID.Version() == 7 && ref.GenerationID.Variant() == uuid.RFC4122 &&
		ref.LinkVersion >= 2 && ref.LinkVersion <= 9007199254740991 && ref.EmploymentEpoch > 0 && ref.EmploymentEpoch <= 9007199254740991
}

func (g *stocktakeActorGuard) WithStocktakeActorHistoryReview(ctx context.Context, scope authz.StaffActorScope, expected authz.StaffActorReference, observed authz.StocktakeActorObservation, action func(context.Context) error) error {
	if action == nil || tenantdb.ActorFrom(ctx) != scope.Subject || !validLoginSubject(observed.Subject) || !validStocktakeActorReference(expected) || !validStocktakeActorReference(observed.Reference) || len(scope.DepartmentPath) == 0 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), DepartmentPath: scope.DepartmentPath, Permission: "pharmacy.stocktake.review"}
	if grant, ok := authz.ScopedCommandGrant(ctx, request); !ok || grant.AssignmentID == "" || grant.ScopeKind != "department" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.WithCurrentStocktakeActor(ctx, scope, func(currentCtx context.Context, current authz.StaffActorReference) error {
		if current != expected || scope.Subject == observed.Subject || current.StaffID == observed.Reference.StaffID {
			return authz.ErrScopedCommandDenied
		}
		return g.actors.db.WithTenantCommand(currentCtx, scope.TenantID, func(tx *gorm.DB) (err error) {
			var supported bool
			if err = tx.Raw(`SELECT workforce_stocktake_history_support()`).Scan(&supported).Error; err != nil || !supported {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			var previous string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_subject','app.workforce_hospital','app.workforce_stocktake_history','app.workforce_stocktake_history_actor','app.workforce_stocktake_history_subject','app.workforce_stocktake_history_generation','app.workforce_stocktake_history_staff'])AS key`).Scan(&previous).Error; err != nil {
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
			ref := observed.Reference
			if err = tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_stocktake_history','authorized',true),set_config('app.workforce_stocktake_history_actor',?,true),set_config('app.workforce_stocktake_history_subject',?,true),set_config('app.workforce_stocktake_history_generation',?,true),set_config('app.workforce_stocktake_history_staff',?,true)`, scope.Subject, scope.HospitalID.String(), scope.Subject, observed.Subject, ref.GenerationID.String(), ref.StaffID.String()).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			var retained bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links l
 JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id)
 JOIN workforce_employment_generations g ON(g.tenant_id,g.hospital_id,g.id)=(l.tenant_id,l.hospital_id,l.employment_generation_id)
 WHERE l.tenant_id=? AND l.hospital_id=? AND l.id=? AND l.staff_id=? AND l.subject=?
 AND h.version=? AND h.status='active' AND h.employment_generation_id=g.id
 AND g.id=? AND g.staff_id=l.staff_id AND g.employment_epoch=? AND g.access_intent='digital' AND g.declared_subject=l.subject)`, scope.TenantID, scope.HospitalID, ref.LinkID, ref.StaffID, observed.Subject, ref.LinkVersion, ref.GenerationID, ref.EmploymentEpoch).Scan(&retained).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if !retained {
				return authz.ErrScopedCommandDenied
			}
			var conflicts bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_login_link_history h ON(h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.id)
 WHERE l.tenant_id=? AND l.hospital_id=? AND h.status='active' AND((l.staff_id=? AND l.subject=?)OR(l.staff_id=? AND l.subject=?)))
 OR EXISTS(SELECT 1 FROM workforce_login_links a JOIN workforce_login_link_history ah ON(ah.tenant_id,ah.hospital_id,ah.id)=(a.tenant_id,a.hospital_id,a.id)
 JOIN workforce_login_links b ON(b.tenant_id,b.hospital_id,b.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_login_link_history bh ON(bh.tenant_id,bh.hospital_id,bh.id)=(b.tenant_id,b.hospital_id,b.id)
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.subject=? AND b.subject=? AND ah.status='active' AND bh.status='active')`, scope.TenantID, scope.HospitalID, ref.StaffID, scope.Subject, current.StaffID, observed.Subject, scope.TenantID, scope.HospitalID, scope.Subject, observed.Subject).Scan(&conflicts).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if conflicts {
				return authz.ErrScopedCommandDenied
			}
			if err = tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.stocktake_actor.review_history',?,jsonb_build_object('participant_staff_id',?::uuid,'participant_link_id',?::uuid,'participant_link_version',?::bigint,'participant_generation_id',?::uuid,'participant_employment_epoch',?::bigint,'reviewer_link_id',?::uuid,'reviewer_link_version',?::bigint,'reviewer_generation_id',?::uuid,'reviewer_employment_epoch',?::bigint))`, scope.TenantID, scope.HospitalID, scope.Subject, current.StaffID, ref.StaffID, ref.LinkID, ref.LinkVersion, ref.GenerationID, ref.EmploymentEpoch, current.LinkID, current.LinkVersion, current.GenerationID, current.EmploymentEpoch).Error; err != nil {
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
