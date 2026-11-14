package workforce

import (
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

//go:embed person_review_authority.sql
var personReviewAuthoritySchema string

var errPersonReviewActorHidden = errors.New("reviewed person actor unavailable")

type personReviewAuthority struct {
	Staff      *reviewedLeaveActor
	Assignment authz.ScopeGrant
	Governance *authz.GovernanceSourceEvidence
	Capability authz.GovernanceCapability
}

func personReviewCapability(permission authz.Permission) (authz.GovernanceCapability, string) {
	switch permission {
	case PermIdentityChecksManage:
		return authz.GovernancePersonPrepare, "workforce.person_preparer.v1"
	case PermIdentityChecksReview:
		return authz.GovernancePersonReview, "workforce.person_reviewer.v1"
	default:
		return "", ""
	}
}

func newPersonReviewGovernanceAuthority(p authn.Principal, hospital uuid.UUID, permission authz.Permission, source authz.GovernanceSourceEvidence) (personReviewAuthority, error) {
	capability, template := personReviewCapability(permission)
	if template == "" || !validWorkforceGovernanceSource(p, establishment{kind: hospitalEstablishmentKind, id: hospital}, source, template) {
		return personReviewAuthority{}, authz.ErrScopedAuthorizationUnavailable
	}
	scopeID := *source.Scope.ID
	source.Scope.ID = &scopeID
	return personReviewAuthority{Governance: &source, Capability: capability}, nil
}

func (a personReviewAuthority) isStaff(id uuid.UUID) bool {
	return a.Staff != nil && a.Staff.StaffID == id
}

func (a personReviewAuthority) same(b personReviewAuthority) bool {
	if a.Staff != nil || b.Staff != nil {
		return a.Staff != nil && b.Staff != nil && a.Governance == nil && b.Governance == nil && *a.Staff == *b.Staff && a.Assignment == b.Assignment
	}
	if a.Governance == nil || b.Governance == nil || a.Capability != b.Capability {
		return false
	}
	x, y := *a.Governance, *b.Governance
	return x.Kind == y.Kind && x.OwnerMembershipID == y.OwnerMembershipID && x.OwnerCount == y.OwnerCount && x.GrantID == y.GrantID && x.Version == y.Version && x.MembershipEpoch == y.MembershipEpoch && x.TemplateKey == y.TemplateKey && x.Scope.TenantID == y.Scope.TenantID && x.Scope.Kind == y.Scope.Kind && x.Scope.ID != nil && y.Scope.ID != nil && *x.Scope.ID == *y.Scope.ID && x.ExpiresAt.Equal(y.ExpiresAt)
}

func (a personReviewAuthority) auditValues() (map[string]any, error) {
	if a.Staff != nil && a.Governance == nil {
		return map[string]any{"authority_kind": "scoped_staff", "assignment_id": a.Assignment.AssignmentID, "assignment_version": a.Assignment.Version, "scope_epoch": a.Assignment.ScopeEpoch, "membership_epoch": a.Assignment.MembershipEpoch, "actor_link_id": a.Staff.ID, "actor_link_version": a.Staff.Version}, nil
	}
	if a.Staff != nil || a.Governance == nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	v := a.Governance
	values := map[string]any{"authority_kind": v.Kind, "capability": a.Capability, "membership_epoch": v.MembershipEpoch, "owner_count": v.OwnerCount, "source_expires_at": v.ExpiresAt}
	switch v.Kind {
	case authz.GovernanceOwnerSource:
		values["owner_membership_id"] = v.OwnerMembershipID
	case authz.GovernanceGrantSource:
		values["source_grant_id"] = v.GrantID
		values["source_grant_version"] = v.Version
		values["template_key"] = v.TemplateKey
	default:
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return values, nil
}

func (h *handlers) withPersonReviewAuthority(c *gin.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, fn func(context.Context, personReviewAuthority) error) error {
	if !authn.Refresh(c) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) > 1 || len(subjects) > 1 || (len(tenants) == 1 && tenants[0] != p.TenantID) || (len(subjects) == 1 && subjects[0] != p.Subject) {
		return errIntakeWorkspaceRequired
	}
	// Preserve existing scoped Staff calls; governance additionally requires explicit workspace binding.
	boundWorkspace := len(tenants) == 1 && len(subjects) == 1
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ctx, budget := context.WithTimeout(ctx, 5*time.Second)
	defer budget()
	return h.withPersonReviewAuthorityContext(ctx, p, hospital, permission, permissions.Has(permission) && boundWorkspace, fn)
}

func (h *handlers) withPersonReviewAuthorityContext(ctx context.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, governanceAllowed bool, fn func(context.Context, personReviewAuthority) error) error {
	called := false
	staffHidden := false
	err := h.withIdentityScopeGrantContext(ctx, p, hospital, permission, func(ctx context.Context, grant authz.ScopeGrant) error {
		return h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
			actor, err := lockReviewedLeaveActor(tx, p, hospital)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				staffHidden = true
				return authz.ErrScopedCommandDenied
			}
			if err != nil {
				return err
			}
			called = true
			return fn(ctx, personReviewAuthority{Staff: &actor, Assignment: grant})
		})
	})
	if called || !errors.Is(err, authz.ErrScopedCommandDenied) {
		return err
	}
	if !governanceAllowed {
		if staffHidden {
			return errors.Join(authz.ErrScopedCommandDenied, errPersonReviewActorHidden)
		}
		return authz.ErrScopedCommandDenied
	}
	capability, template := personReviewCapability(permission)
	if template == "" || h.governanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	callbacks := 0
	var actionErr error
	err = h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: hospitalEstablishmentKind, ID: &hospital}}, func(ctx context.Context, source authz.GovernanceSourceEvidence) error {
		callbacks++
		if callbacks != 1 || !h.db.GuardedCommandActive(ctx) || tenantdb.ActorFrom(ctx) != p.Subject {
			actionErr = authz.ErrScopedAuthorizationUnavailable
			return actionErr
		}
		authority, validationErr := newPersonReviewGovernanceAuthority(p, hospital, permission, source)
		if validationErr != nil {
			actionErr = validationErr
			return actionErr
		}
		actionErr = fn(ctx, authority)
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
	if staffHidden && errors.Is(err, authz.ErrScopedCommandDenied) {
		return errors.Join(authz.ErrScopedCommandDenied, errPersonReviewActorHidden)
	}
	return err
}

func preparePersonBrowseAuthority(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, authority personReviewAuthority) error {
	if authority.Governance == nil {
		return nil
	}
	var installed bool
	if err := tx.Raw(`SELECT to_regprocedure('public.workforce_person_governance_source(uuid,uuid)') IS NOT NULL`).Scan(&installed).Error; err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	values, err := authority.auditValues()
	if err != nil {
		return err
	}
	values["tenant_id"], values["hospital_id"], values["actor_subject"], values["purpose"] = p.TenantID, hospital, p.Subject, "person_browse"
	values["authenticated_at"], values["idle_deadline"] = p.AuthTime, p.IdleDeadline
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return tx.Exec(`SELECT set_config('app.workforce_person_authority',?,true)`, string(raw)).Error
}

func preparePersonReviewAuthority(c *gin.Context, tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, authority personReviewAuthority) error {
	if authority.Governance == nil {
		return nil
	}
	var installed bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='public.workforce_person_reviews'::regclass AND attname='governance_authority' AND NOT attisdropped) AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.workforce_person_reviews'::regclass AND tgname='workforce_person_review_zz_authority' AND tgenabled IN('O','A'))`).Scan(&installed).Error; err != nil {
		return err
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	values, err := authority.auditValues()
	if err != nil {
		return err
	}
	values["tenant_id"], values["hospital_id"], values["staff_id"], values["actor_subject"] = p.TenantID, hospital, staff, p.Subject
	values["authenticated_at"], values["idle_deadline"] = p.AuthTime, p.IdleDeadline
	if id := c.Param("review"); id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return err
		}
		values["review_id"] = parsed.String()
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return tx.Exec("SELECT set_config('app.workforce_person_authority',?,true)", string(raw)).Error
}
