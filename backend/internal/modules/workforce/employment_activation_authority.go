package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed employment_activation_authority.sql
var employmentActivationAuthoritySchema string

type employmentActivationIntent struct {
	Generation, Command uuid.UUID
	StaffVersion, Epoch int64
}

type employmentActivationAuthority struct {
	Tenant, Subject          string
	Hospital, Target, Review uuid.UUID
	Staff                    *intakeAuthority
	Governance               *authz.GovernanceSourceEvidence
	Intent                   *employmentActivationIntent
	writer                   authz.EmploymentBasisWriter
	command                  context.Context
}

func (a *employmentActivationAuthority) isStaff(id uuid.UUID) bool {
	return a != nil && a.Staff != nil && a.Governance == nil && a.Staff.Actor.Staff == id
}

func (a *employmentActivationAuthority) valid() bool {
	if a == nil || a.Tenant == "" || a.Subject == "" || a.Hospital == uuid.Nil || a.Target == uuid.Nil || a.Review.Version() != 7 {
		return false
	}
	if a.Intent != nil && (a.Intent.Generation.Version() != 7 || a.Intent.Command.Version() != 7 || a.Intent.StaffVersion < 1 || a.Intent.Epoch < 1) {
		return false
	}
	if a.Staff != nil {
		if a.Governance != nil || !a.Staff.Scoped || a.Staff.Tenant != a.Tenant || a.Staff.Subject != a.Subject || a.Staff.Hospital != a.Hospital || a.Staff.Actor.Staff == uuid.Nil || a.Staff.Actor.Link == uuid.Nil || a.Staff.Actor.LinkVersion < 1 || a.Staff.Actor.EmploymentEpoch < 1 || len(a.Staff.Grants) != 2 {
			return false
		}
		for _, permission := range []authz.Permission{PermEmploymentActivate, PermEnrollmentManage} {
			grant, ok := a.Staff.Grants[permission]
			if !ok || grant.AssignmentID == "" || grant.Version < 1 || grant.ScopeEpoch < 1 || grant.MembershipEpoch < 1 {
				return false
			}
		}
		return true
	}
	return a.Governance != nil
}

func newGovernanceActivationAuthority(p authn.Principal, hospital, staff, review uuid.UUID, source authz.GovernanceSourceEvidence, intent *employmentActivationIntent) (*employmentActivationAuthority, error) {
	if !source.ExpiresAt.After(time.Now()) || !validWorkforceGovernanceSource(p, establishment{kind: hospitalEstablishmentKind, id: hospital}, source, "workforce.employment_activator.v1") {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	id := *source.Scope.ID
	source.Scope.ID = &id
	a := &employmentActivationAuthority{Tenant: p.TenantID, Subject: p.Subject, Hospital: hospital, Target: staff, Review: review, Governance: &source}
	if intent != nil {
		copy := *intent
		a.Intent = &copy
	}
	if !a.valid() {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return a, nil
}

func (a *employmentActivationAuthority) proof(p authn.Principal) (map[string]any, error) {
	if !a.valid() || a.Governance == nil || a.Tenant != p.TenantID || a.Subject != p.Subject {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	v := a.Governance
	values := map[string]any{"purpose": "employment_activation", "operation": "source", "tenant_id": a.Tenant, "hospital_id": a.Hospital, "staff_id": a.Target, "review_id": a.Review, "actor_subject": a.Subject, "capability": authz.GovernanceEmploymentActivate, "authority_kind": v.Kind, "membership_epoch": v.MembershipEpoch, "owner_count": v.OwnerCount, "source_expires_at": v.ExpiresAt, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline}
	if a.Intent != nil {
		values["operation"], values["generation_id"], values["command_id"] = "activate", a.Intent.Generation, a.Intent.Command
		values["expected_staff_version"], values["expected_employment_epoch"] = a.Intent.StaffVersion, a.Intent.Epoch
	}
	switch v.Kind {
	case authz.GovernanceOwnerSource:
		values["owner_membership_id"] = v.OwnerMembershipID
	case authz.GovernanceGrantSource:
		values["source_grant_id"], values["source_grant_version"], values["template_key"] = v.GrantID, v.Version, v.TemplateKey
	default:
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return values, nil
}

func (a *employmentActivationAuthority) consume(basis authz.EmploymentBasis) error {
	if !a.valid() || a.Intent == nil || a.writer == nil || a.command == nil || basis.TenantID != a.Tenant || basis.HospitalID != a.Hospital || basis.StaffID != a.Target || basis.GenerationID != a.Intent.Generation || basis.EmploymentEpoch != a.Intent.Epoch+1 || basis.Subject == a.Subject {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return a.writer.ConsumeEmploymentBasis(a.command, basis)
}

func installActivationAuthority(tx *gorm.DB, p authn.Principal, a *employmentActivationAuthority) error {
	if !a.valid() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	proof := ""
	if a.Governance != nil {
		var installed bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.workforce_employment_generations') AND attname='governance_authority' AND NOT attisdropped)
 AND NOT EXISTS(SELECT 1 FROM (VALUES
 ('workforce_employment_generations','workforce_employment_generation_zz_authority','workforce_employment_generation_governance_record',7),
 ('workforce_employment_generations','workforce_employment_generation_write','workforce_employment_generation_write',7),
 ('workforce_employment_generations','workforce_employment_generation_immutable','workforce_prevent_removal',58),
 ('workforce_staff','zz_workforce_governance_activation','workforce_governance_staff_activation',19),
 ('workforce_staff','workforce_reviewed_activation_required','workforce_reviewed_activation_required',17)
 ) AS required(table_name,trigger_name,function_name,trigger_type)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.'||required.table_name) AND tgname=required.trigger_name AND NOT tgisinternal AND tgtype=required.trigger_type AND tgenabled IN('O','A') AND tgfoid=to_regprocedure('public.'||required.function_name||'()')))
 AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid=to_regclass('public.workforce_staff') AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polcmd='w' AND polqual IS NOT NULL AND polwithcheck IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy policy WHERE policy.polrelid=c.oid AND policy.polname='activation_governance_purpose' AND NOT policy.polpermissive AND policy.polcmd='*' AND policy.polqual IS NOT NULL AND policy.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy policy WHERE policy.polrelid=c.oid AND policy.polname='activation_governance_delete' AND NOT policy.polpermissive AND policy.polcmd='d' AND policy.polqual IS NOT NULL)))`).Scan(&installed).Error; err != nil {
			return err
		}
		if !installed {
			return authz.ErrScopedAuthorizationUnavailable
		}
		values, err := a.proof(p)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		proof = string(raw)
	}
	return tx.Exec(`SELECT set_config('app.workforce_activation_authority',?,true),set_config('app.workforce_employment_review','manage',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_evidence','read',true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true)`, proof, a.Review.String(), a.Target.String()).Error
}

func (h *handlers) withActivationAuthority(c *gin.Context, p authn.Principal, hospital, staff, review uuid.UUID, guard authz.CompoundScopedCommandGuard, intent *employmentActivationIntent, fn func(*gorm.DB, IdentitySource, *employmentActivationAuthority) error) error {
	if !authn.Refresh(c) || p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ctx, budget := context.WithTimeout(ctx, 5*time.Second)
	defer budget()
	request := c.Request
	c.Request = request.WithContext(ctx)
	defer func() { c.Request = request }()
	run := func(tx *gorm.DB, authority *employmentActivationAuthority) error {
		if err := installActivationAuthority(tx, p, authority); err != nil {
			return err
		}
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		return fn(tx, source, authority)
	}
	called := false
	err := h.intakeAuthorityWithGuard(c, p, hospital, []authz.Permission{PermEmploymentActivate, PermEnrollmentManage}, true, guard, func(tx *gorm.DB, scoped bool) error {
		called = true
		value, present := c.Get(intakeAuthorityKey)
		current, valid := value.(*intakeAuthority)
		if !scoped || !present || !valid || current == nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		a := &employmentActivationAuthority{Tenant: p.TenantID, Subject: p.Subject, Hospital: hospital, Target: staff, Review: review, Staff: current, Intent: intent}
		if intent != nil {
			writer, valid := guard.(*activationCommandGuard)
			if !valid || writer.writer == nil || writer.ctx == nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			a.writer, a.command = writer.writer, writer.ctx
		}
		return run(tx, a)
	})
	if called || !errors.Is(err, authz.ErrScopedCommandDenied) {
		return err
	}
	if !permissions.Has(PermEmploymentActivate) {
		return authz.ErrScopedCommandDenied
	}
	if h.governanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	req := authz.GovernanceSourceRequest{Principal: p, Capability: authz.GovernanceEmploymentActivate, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: hospitalEstablishmentKind, ID: &hospital}}
	callbacks := 0
	var actionErr error
	action := func(commandCtx context.Context, source authz.GovernanceSourceEvidence, writer authz.EmploymentBasisWriter) error {
		callbacks++
		if callbacks != 1 || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject || (intent != nil && writer == nil) {
			actionErr = authz.ErrScopedAuthorizationUnavailable
			return actionErr
		}
		authority, err := newGovernanceActivationAuthority(p, hospital, staff, review, source, intent)
		if err != nil {
			actionErr = err
			return err
		}
		authority.writer, authority.command = writer, commandCtx
		defer func() { authority.writer, authority.command = nil, nil }()
		actionErr = h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error { return run(tx, authority) })
		return actionErr
	}
	if intent == nil {
		err = h.governanceSources.WithGovernanceSource(ctx, req, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
			return action(commandCtx, source, nil)
		})
	} else {
		owner, supported := h.governanceSources.(authz.GovernanceEmploymentActivationGuard)
		if !supported {
			return authz.ErrScopedAuthorizationUnavailable
		}
		err = owner.WithGovernanceEmploymentActivation(ctx, req, staff, action)
	}
	if actionErr != nil {
		return actionErr
	}
	if err != nil && callbacks != 0 {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			requestid.Logger(c).Error("employment activation authority completion refused", "sqlstate", pg.Code, "constraint", pg.ConstraintName, "table", pg.TableName)
		}
	}
	if err == nil && callbacks != 1 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if errors.Is(err, authz.ErrGovernanceScopeHidden) {
		return gorm.ErrRecordNotFound
	}
	return err
}
