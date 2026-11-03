package workforce

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type staffActorGuard struct{ db *tenantdb.DB }

// StaffActors exposes the Workforce-owned current actor boundary without a repository.
func (*Module) StaffActors(db *tenantdb.DB) authz.CurrentStaffActorGuard {
	return &staffActorGuard{db: db}
}

func (g *staffActorGuard) WithCurrentStaffActor(ctx context.Context, scope authz.StaffActorScope, action func(context.Context, authz.StaffActorReference) error) error {
	tenant, err := uuid.Parse(scope.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != scope.TenantID || scope.HospitalID == uuid.Nil || !validLoginSubject(scope.Subject) || action == nil || g.db == nil || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), Permission: authz.StaffActorUse}
	grant, ok := authz.ScopedCommandGrant(ctx, request)
	if !ok || grant.AssignmentID == "" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) (err error) {
		if err = tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, scope.TenantID, scope.HospitalID).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.workforce_subject','app.workforce_hospital','app.workforce_leave_actor_check','app.workforce_leave_review_actor']) AS key`).Scan(&previous).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		restored := false
		restore := func() error {
			return tx.Exec(`SELECT set_config(key,value,true) FROM jsonb_each_text(?::jsonb)`, previous).Error
		}
		defer func() {
			if !restored {
				err = errors.Join(err, restore())
			}
		}()
		if err = tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, scope.Subject, scope.HospitalID.String()).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		principal := authn.Principal{TenantID: scope.TenantID, Subject: scope.Subject}
		actor, err := lockStaffActorLink(tx, principal, scope.HospitalID)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && actor.GenerationID == uuid.Nil {
			return authz.ErrScopedCommandDenied
		}
		if err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		generation, until, err := currentDigitalActorGeneration(tx, principal, scope.HospitalID, actor)
		if err != nil {
			return err
		}
		if err = g.db.ConstrainGuardedCommand(ctx, scope.TenantID, until); err != nil {
			if errors.Is(err, tenantdb.ErrGuardedCommandExpired) {
				return authz.ErrScopedCommandDenied
			}
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		reference := authz.StaffActorReference{StaffID: actor.StaffID, LinkID: actor.ID, LinkVersion: actor.Version, GenerationID: generation.ID, EmploymentEpoch: generation.EmploymentEpoch}
		data, err := json.Marshal(map[string]any{"actor_link_id": actor.ID, "actor_link_version": actor.Version, "generation_id": generation.ID, "employment_epoch": generation.EmploymentEpoch, "assignment_id": grant.AssignmentID, "assignment_version": grant.Version})
		if err != nil {
			return err
		}
		if err = tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.staff_actor.use',?,?::jsonb)`, scope.TenantID, scope.HospitalID, scope.Subject, actor.StaffID, string(data)).Error; err != nil {
			return err
		}
		if err = restore(); err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		restored = true
		return action(ctx, reference)
	})
}
