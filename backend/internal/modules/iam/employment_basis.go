package iam

import (
	"context"
	_ "embed"
	"github.com/google/uuid"
	"sync/atomic"
	"time"

	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

//go:embed employment_basis.sql
var employmentBasisSchema string

type employmentBasisProofKey struct{}
type employmentBasisWriter struct {
	owner   *assignmentAuthority
	request authz.ScopeRequest
	active  atomic.Bool
}

func (h *assignmentAuthority) WithEmploymentActivation(ctx context.Context, checks []authz.ScopeCheck, authTime time.Time, action func(context.Context, authz.EmploymentBasisWriter) error) error {
	if !validScopeChecks(checks) || len(checks) != 2 || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	permissions := map[authz.Permission]bool{}
	for _, c := range checks {
		permissions[c.Request.Permission] = true
	}
	if !permissions["workforce.employment.activate"] || !permissions["workforce.enrollment.manage"] {
		return authz.ErrScopedCommandDenied
	}
	return h.withScopeGrants(ctx, checks, authTime, true, func(commandCtx context.Context) error {
		writer := &employmentBasisWriter{owner: h, request: checks[0].Request}
		writer.active.Store(true)
		defer writer.active.Store(false)
		return action(context.WithValue(commandCtx, employmentBasisProofKey{}, writer), writer)
	})
}

func (w *employmentBasisWriter) ConsumeEmploymentBasis(ctx context.Context, b authz.EmploymentBasis) error {
	if !w.active.Load() || ctx.Value(employmentBasisProofKey{}) != w || b.TenantID != w.request.TenantID || b.HospitalID.String() != w.request.HospitalID || !proposalSubject(b.Subject) || b.Subject == w.request.Subject || b.StaffID == uuid.Nil || b.GenerationID.Version() != 7 || b.EmploymentEpoch < 2 || b.Until.IsZero() {
		return authz.ErrScopedCommandDenied
	}
	return w.owner.db.WithTenantCommand(ctx, b.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		epoch, err := proposalMembershipEpoch(tx, b.TenantID, b.Subject)
		if err != nil {
			return err
		}
		if epoch == 0 {
			return authz.ErrScopedCommandDenied
		}
		var broad bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=? AND subject=? AND role_key<>'staff_member')`, b.TenantID, b.Subject).Scan(&broad).Error; err != nil {
			return err
		}
		if broad {
			return authz.ErrScopedCommandDenied
		}
		if err := tx.Exec(`SELECT set_config('app.iam_employment_basis','consume',true),set_config('app.actor_subject',?,true)`, w.request.Subject).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_employment_bases(tenant_id,hospital_id,subject,staff_id,generation_id,employment_epoch,valid_until,actor_subject) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,subject) DO UPDATE SET staff_id=EXCLUDED.staff_id,generation_id=EXCLUDED.generation_id,employment_epoch=EXCLUDED.employment_epoch,valid_until=EXCLUDED.valid_until,actor_subject=EXCLUDED.actor_subject`, b.TenantID, b.HospitalID, b.Subject, b.StaffID, b.GenerationID, b.EmploymentEpoch, b.Until, w.request.Subject).Error
	})
}
