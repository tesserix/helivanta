package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

//go:embed employment_basis.sql
var employmentBasisSchema string

//go:embed governance_employment_basis.sql
var governanceEmploymentBasisSchema string

type employmentBasisProofKey struct{}
type employmentBasisWriter struct {
	owner               *assignmentAuthority
	request             authz.ScopeRequest
	active              atomic.Bool
	target              uuid.UUID
	governanceAuthority string
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
	if !w.accepts(ctx, b) || w.owner == nil || w.owner.db == nil {
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
		var roles []authz.Role
		if err := tx.Table("iam_members").Where("tenant_id=? AND subject=?", b.TenantID, b.Subject).Order("role_key").Limit(3).Pluck("role_key", &roles).Error; err != nil {
			return err
		}
		if !employmentMembershipRoles(roles) {
			return authz.ErrScopedCommandDenied
		}
		if err := tx.Exec(`SELECT set_config('app.iam_employment_basis','consume',true),set_config('app.actor_subject',?,true),set_config('app.iam_employment_governance',?,true)`, w.request.Subject, w.governanceAuthority).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_employment_bases(tenant_id,hospital_id,subject,staff_id,generation_id,employment_epoch,valid_until,actor_subject) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,subject) DO UPDATE SET staff_id=EXCLUDED.staff_id,generation_id=EXCLUDED.generation_id,employment_epoch=EXCLUDED.employment_epoch,valid_until=EXCLUDED.valid_until,actor_subject=EXCLUDED.actor_subject`, b.TenantID, b.HospitalID, b.Subject, b.StaffID, b.GenerationID, b.EmploymentEpoch, b.Until, w.request.Subject).Error
	})
}

func employmentMembershipRoles(roles []authz.Role) bool {
	if len(roles) < 1 || len(roles) > 2 {
		return false
	}
	ordinary, governance := false, false
	for _, role := range roles {
		switch role {
		case authz.RoleStaffMember:
			if ordinary {
				return false
			}
			ordinary = true
		case authz.RoleGovernanceDelegate:
			if governance {
				return false
			}
			governance = true
		default:
			return false
		}
	}
	return ordinary
}

func (w *employmentBasisWriter) accepts(ctx context.Context, b authz.EmploymentBasis) bool {
	return w != nil && ctx != nil && ctx.Err() == nil && w.active.Load() && ctx.Value(employmentBasisProofKey{}) == w &&
		b.TenantID == w.request.TenantID && b.HospitalID.String() == w.request.HospitalID && proposalSubject(b.Subject) && b.Subject != w.request.Subject &&
		b.StaffID != uuid.Nil && (w.target == uuid.Nil || b.StaffID == w.target) && b.GenerationID.Version() == 7 && b.GenerationID.Variant() == uuid.RFC4122 && b.EmploymentEpoch >= 2 && !b.Until.IsZero()
}

func (g *governanceSourceGuard) WithGovernanceEmploymentActivation(ctx context.Context, req authz.GovernanceSourceRequest, target uuid.UUID, action func(context.Context, authz.GovernanceSourceEvidence, authz.EmploymentBasisWriter) error) error {
	if req.Capability != authz.GovernanceEmploymentActivate || req.Scope.Kind != "hospital" || target == uuid.Nil || target.Variant() != uuid.RFC4122 {
		return authz.ErrScopedCommandDenied
	}
	if action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if req.Scope.ID == nil || *req.Scope.ID == uuid.Nil {
		return authz.ErrScopedCommandDenied
	}
	hospital := *req.Scope.ID
	req.Scope.ID = &hospital
	return g.withGovernanceSource(ctx, req, true, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		var installed bool
		if err := g.handlers.db.WithTenantCommand(commandCtx, req.Principal.TenantID, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT to_regclass('public.iam_employment_basis_authorities') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.iam_employment_bases') AND tgname='iam_employment_basis_zz_authority' AND tgfoid=to_regprocedure('public.iam_employment_basis_governance_record()') AND tgtype=21 AND NOT tgisinternal AND tgenabled IN('O','A'))`).Scan(&installed).Error
		}); err != nil {
			return err
		}
		if !installed {
			return authz.ErrScopedAuthorizationUnavailable
		}
		proof := map[string]any{"capability": authz.GovernanceEmploymentActivate, "tenant_id": req.Principal.TenantID, "hospital_id": hospital, "staff_id": target, "actor_subject": req.Principal.Subject, "authority_kind": source.Kind, "membership_epoch": source.MembershipEpoch, "owner_count": source.OwnerCount, "expires_at": source.ExpiresAt, "authenticated_at": req.Principal.AuthTime, "idle_deadline": req.Principal.IdleDeadline}
		switch source.Kind {
		case authz.GovernanceOwnerSource:
			proof["owner_membership_id"] = source.OwnerMembershipID
		case authz.GovernanceGrantSource:
			proof["source_grant_id"] = source.GrantID
			proof["source_grant_version"] = source.Version
			proof["template_key"] = source.TemplateKey
		default:
			return authz.ErrScopedAuthorizationUnavailable
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}

		writer := &employmentBasisWriter{owner: &assignmentAuthority{db: g.handlers.db}, request: authz.ScopeRequest{TenantID: req.Principal.TenantID, Subject: req.Principal.Subject, HospitalID: req.Scope.ID.String()}, target: target, governanceAuthority: string(raw)}
		writer.active.Store(true)
		defer writer.active.Store(false)
		return action(context.WithValue(commandCtx, employmentBasisProofKey{}, writer), source, writer)
	})
}

var _ authz.GovernanceEmploymentActivationGuard = (*governanceSourceGuard)(nil)
