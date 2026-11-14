package workforce

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed employment_review_authority.sql
var employmentReviewAuthoritySchema string

const employmentReviewAuthorityKey = "workforce.employment.review.authority"

type employmentReviewAuthority struct {
	Tenant, Subject          string
	Hospital, Target, Review uuid.UUID
	Capability               authz.GovernanceCapability
	Staff                    *intakeAuthority
	Governance               *authz.GovernanceSourceEvidence
}

func employmentReviewCapability(permission authz.Permission) (authz.GovernanceCapability, string) {
	switch permission {
	case PermEmploymentPrepare:
		return authz.GovernanceEmploymentPrepare, "workforce.employment_preparer.v1"
	case PermEmploymentReview:
		return authz.GovernanceEmploymentReview, "workforce.employment_reviewer.v1"
	default:
		return "", ""
	}
}

func newEmploymentReviewGovernanceAuthority(p authn.Principal, hospital, staff, review uuid.UUID, permission authz.Permission, source authz.GovernanceSourceEvidence) (*employmentReviewAuthority, error) {
	capability, template := employmentReviewCapability(permission)
	if template == "" || p.Subject == "" || hospital == uuid.Nil || staff == uuid.Nil || review.Version() != 7 || !source.ExpiresAt.After(time.Now()) || !validWorkforceGovernanceSource(p, establishment{kind: hospitalEstablishmentKind, id: hospital}, source, template) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	scope := *source.Scope.ID
	source.Scope.ID = &scope
	return &employmentReviewAuthority{Tenant: p.TenantID, Subject: p.Subject, Hospital: hospital, Target: staff, Review: review, Capability: capability, Governance: &source}, nil
}

func (a *employmentReviewAuthority) isStaff(id uuid.UUID) bool {
	return a != nil && a.Staff != nil && a.Governance == nil && a.Staff.Actor.Staff == id
}

func (a *employmentReviewAuthority) valid() bool {
	if a == nil || a.Tenant == "" || a.Subject == "" || a.Hospital == uuid.Nil || a.Target == uuid.Nil || a.Review.Version() != 7 || (a.Capability != authz.GovernanceEmploymentPrepare && a.Capability != authz.GovernanceEmploymentReview) {
		return false
	}
	if a.Staff != nil {
		if a.Governance != nil || !a.Staff.Scoped || a.Staff.Tenant != a.Tenant || a.Staff.Subject != a.Subject || a.Staff.Hospital != a.Hospital || a.Staff.Actor.Staff == uuid.Nil || a.Staff.Actor.Link == uuid.Nil || a.Staff.Actor.LinkVersion < 1 || a.Staff.Actor.EmploymentEpoch < 1 {
			return false
		}
		required := []authz.Permission{PermEmploymentReview}
		if a.Capability == authz.GovernanceEmploymentPrepare {
			required = []authz.Permission{PermEnrollmentManage, PermDocumentManage}
		}
		if len(a.Staff.Grants) != len(required) {
			return false
		}
		for _, permission := range required {
			grant, ok := a.Staff.Grants[permission]
			if !ok || grant.AssignmentID == "" || grant.Version < 1 || grant.ScopeEpoch < 1 || grant.MembershipEpoch < 1 {
				return false
			}
		}
		return true
	}
	return a.Governance != nil
}

func (a *employmentReviewAuthority) same(b *employmentReviewAuthority) bool {
	if !a.valid() || !b.valid() {
		return false
	}
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}

func (a *employmentReviewAuthority) governanceProof(p authn.Principal) (map[string]any, error) {
	if !a.valid() || a.Governance == nil || a.Tenant != p.TenantID || a.Subject != p.Subject {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	v := a.Governance
	values := map[string]any{"purpose": "employment_review", "tenant_id": a.Tenant, "hospital_id": a.Hospital, "staff_id": a.Target, "review_id": a.Review, "actor_subject": a.Subject, "capability": a.Capability, "authority_kind": v.Kind, "membership_epoch": v.MembershipEpoch, "owner_count": v.OwnerCount, "source_expires_at": v.ExpiresAt, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline}
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

func employmentReviewGovernanceAudit(tx *gorm.DB, p authn.Principal, row EmploymentReview, action string, authority *employmentReviewAuthority, extra map[string]any) error {
	values, err := authority.governanceProof(p)
	if err != nil {
		return err
	}
	if row.ID != authority.Review || row.HospitalID != authority.Hospital || row.StaffID != authority.Target {
		return authz.ErrScopedAuthorizationUnavailable
	}
	values["version"], values["revision_id"], values["period_id"], values["person_review_id"] = row.Version, row.RevisionID, row.PeriodID, row.PersonReviewID
	values["status"] = row.Status
	for key, value := range extra {
		if _, exists := values[key]; exists {
			return authz.ErrScopedAuthorizationUnavailable
		}
		values[key] = value
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,?::jsonb)`, p.TenantID, row.HospitalID, p.Subject, action, row.ID, string(raw)).Error
}

func installEmploymentReviewAuthority(tx *gorm.DB, p authn.Principal, a *employmentReviewAuthority) error {
	if !a.valid() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	proof, actor, link := "", "", ""
	if a.Governance != nil {
		var installed bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.workforce_employment_reviews') AND attname='governance_authority' AND NOT attisdropped)
 AND EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.workforce_employment_review_history') AND attname='governance_authority' AND NOT attisdropped)
 AND NOT EXISTS(SELECT 1 FROM (VALUES
 ('workforce_employment_reviews','workforce_employment_review_zz_authority','workforce_employment_review_governance_record',23),
 ('workforce_employment_reviews','workforce_employment_review_write','workforce_employment_review_write',31),
 ('workforce_employment_reviews','workforce_employment_review_snapshot','workforce_employment_review_snapshot',21),
 ('workforce_employment_review_history','workforce_employment_review_history_guard','workforce_employment_review_history_guard',31)
 ) AS required(table_name,trigger_name,function_name,trigger_type)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=to_regclass('public.'||required.table_name)
 AND tgname=required.trigger_name AND NOT tgisinternal AND tgtype=required.trigger_type AND tgenabled IN('O','A')
 AND tgfoid=to_regprocedure('public.'||required.function_name||'()')))
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy policy WHERE policy.polrelid=c.oid AND policy.polname='employment_governance_purpose'
 AND NOT policy.polpermissive AND policy.polcmd='*' AND policy.polqual IS NOT NULL AND policy.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy policy WHERE policy.polrelid=c.oid AND policy.polname='employment_governance_delete'
 AND NOT policy.polpermissive AND policy.polcmd='d' AND policy.polqual IS NOT NULL)))`).Scan(&installed).Error; err != nil {
			return err
		}
		if !installed {
			return authz.ErrScopedAuthorizationUnavailable
		}
		values, err := a.governanceProof(p)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		proof = string(raw)
	} else {
		actor, link = a.Staff.Actor.Staff.String(), a.Staff.Actor.Link.String()
	}
	mode := "manage"
	if a.Capability == authz.GovernanceEmploymentReview {
		mode = "review"
	}
	return tx.Exec(`SELECT set_config('app.workforce_employment_authority',?,true),set_config('app.workforce_employment_review',?,true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_review_actor',?,true),set_config('app.workforce_employment_review_link',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true),set_config('app.workforce_employment_evidence','read',true)`, proof, mode, a.Review.String(), actor, link, a.Target.String()).Error
}

func (h *handlers) withEmploymentReviewAuthority(c *gin.Context, p authn.Principal, hospital, staff, review uuid.UUID, permission authz.Permission, required []authz.Permission, fn func(*gorm.DB, IdentitySource, *employmentReviewAuthority) error) error {
	capability, template := employmentReviewCapability(permission)
	if template == "" || !authn.Refresh(c) || p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		return errIntakeWorkspaceRequired
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	var original *employmentReviewAuthority
	if value, present := c.Get(employmentReviewAuthorityKey); present {
		original, ok = value.(*employmentReviewAuthority)
		if !ok || !original.valid() || original.Tenant != p.TenantID || original.Subject != p.Subject || original.Hospital != hospital || original.Target != staff || original.Review != review || original.Capability != capability {
			return authz.ErrScopedCommandDenied
		}
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ctx, budget := context.WithTimeout(ctx, 5*time.Second)
	defer budget()
	request := c.Request
	c.Request = request.WithContext(ctx)
	defer func() { c.Request = request }()
	run := func(tx *gorm.DB, authority *employmentReviewAuthority) error {
		if !authority.valid() || (original != nil && !original.same(authority)) {
			return authz.ErrScopedCommandDenied
		}
		if err := installEmploymentReviewAuthority(tx, p, authority); err != nil {
			return err
		}
		if capability == authz.GovernanceEmploymentReview {
			var parent struct{ ID uuid.UUID }
			if err := tx.Table("workforce_employment_reviews").Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, review).Take(&parent).Error; err != nil {
				return err
			}
		}
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		if original == nil {
			c.Set(employmentReviewAuthorityKey, authority)
		}
		return fn(tx, source, authority)
	}
	if original == nil || original.Staff != nil {
		called := false
		err := h.intakeAuthorityTransaction(c, p, hospital, required, true, func(tx *gorm.DB, scoped bool) error {
			called = true
			value, present := c.Get(intakeAuthorityKey)
			current, valid := value.(*intakeAuthority)
			if !scoped || !present || !valid || current == nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			return run(tx, &employmentReviewAuthority{Tenant: p.TenantID, Subject: p.Subject, Hospital: hospital, Target: staff, Review: review, Capability: capability, Staff: current})
		})
		if called || !errors.Is(err, authz.ErrScopedCommandDenied) || (original != nil && original.Staff != nil) {
			return err
		}
	}
	if !permissions.Has(permission) {
		return authz.ErrScopedCommandDenied
	}
	if h.governanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	callbacks := 0
	var actionErr error
	err := h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: hospitalEstablishmentKind, ID: &hospital}}, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		callbacks++
		if callbacks != 1 || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject {
			actionErr = authz.ErrScopedAuthorizationUnavailable
			return actionErr
		}
		authority, err := newEmploymentReviewGovernanceAuthority(p, hospital, staff, review, permission, source)
		if err != nil {
			actionErr = err
			return err
		}
		actionErr = h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error { return run(tx, authority) })
		return actionErr
	})
	if actionErr != nil {
		return actionErr
	}
	if err == nil && callbacks != 1 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if errors.Is(err, authz.ErrGovernanceScopeHidden) {
		return gorm.ErrRecordNotFound
	}
	return err
}
