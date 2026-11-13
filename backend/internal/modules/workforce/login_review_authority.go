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
	"gorm.io/gorm"
)

//go:embed login_review_authority.sql
var loginReviewAuthoritySchema string

type loginReviewAuthority struct {
	Capability         authz.GovernanceCapability `json:"capability"`
	IdleDeadline       time.Time                  `json:"idle_deadline"`
	Kind               authz.GovernanceSourceKind `json:"kind"`
	TenantID           string                     `json:"tenant_id"`
	EmployerID         uuid.UUID                  `json:"employer_id"`
	EmployerKind       string                     `json:"employer_kind"`
	StaffID            uuid.UUID                  `json:"staff_id"`
	LinkID             *uuid.UUID                 `json:"link_id"`
	ActorSubject       string                     `json:"actor_subject"`
	MembershipEpoch    int64                      `json:"membership_epoch"`
	SourceGrantID      *uuid.UUID                 `json:"source_grant_id"`
	SourceGrantVersion *int64                     `json:"source_grant_version"`
	OwnerMembershipID  *uuid.UUID                 `json:"owner_membership_id"`
	OwnerCount         int64                      `json:"owner_count"`
	TemplateKey        *string                    `json:"template_key"`
	ExpiresAt          time.Time                  `json:"expires_at"`
	AuthenticatedAt    time.Time                  `json:"authenticated_at"`
}

func (h *handlers) withAccountLinkAuthority(c *gin.Context, p authn.Principal, root establishment, staffID uuid.UUID, command string, action func(context.Context, *authz.GovernanceSourceEvidence) error) error {
	if command == "manage" || command == "inactivate" {
		return action(c.Request.Context(), nil)
	}
	review, manage := PermLoginLinkReview, PermLoginLinkManage
	if root.kind != hospitalEstablishmentKind {
		review, manage = PermIndependentLoginReview, PermIndependentLoginManage
	}
	permissions, ok := authz.PermissionsFrom(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if command == "" && !permissions.Has(review) {
		return action(c.Request.Context(), nil)
	}
	if h.governanceSources == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	called := false
	err := h.governanceSources.WithGovernanceSource(c.Request.Context(), authz.GovernanceSourceRequest{
		Principal: p, Capability: authz.GovernanceAccountLinkReview,
		Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: root.kind, ID: &root.id},
	}, func(ctx context.Context, evidence authz.GovernanceSourceEvidence) error {
		called = true
		if !validLoginReviewSource(p, root, evidence) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return action(ctx, &evidence)
	})
	if !called && command == "" && permissions.Has(manage) && errors.Is(err, authz.ErrScopedCommandDenied) {
		return action(c.Request.Context(), nil)
	}
	return err
}

func validLoginReviewSource(p authn.Principal, root establishment, source authz.GovernanceSourceEvidence) bool {
	if source.Scope.TenantID != p.TenantID || source.Scope.Kind != root.kind || source.Scope.ID == nil || *source.Scope.ID != root.id || source.MembershipEpoch < 1 || source.MembershipEpoch > 9007199254740991 || source.OwnerCount < 1 || source.OwnerCount > 1000 || source.ExpiresAt.IsZero() {
		return false
	}
	switch source.Kind {
	case authz.GovernanceOwnerSource:
		return source.OwnerMembershipID != uuid.Nil && source.OwnerMembershipID.Variant() == uuid.RFC4122 && source.GrantID == uuid.Nil && source.Version == 0 && source.TemplateKey == ""
	case authz.GovernanceGrantSource:
		return source.OwnerMembershipID == uuid.Nil && source.GrantID != uuid.Nil && source.GrantID.Variant() == uuid.RFC4122 && source.GrantID.Version() == 7 && source.Version >= 1 && source.Version <= 9007199254740991 && source.TemplateKey == "workforce.account_link_reviewer.v1"
	default:
		return false
	}
}

func prepareLoginReviewPurpose(c *gin.Context, tx *gorm.DB, p authn.Principal, root establishment, staffID uuid.UUID, command string, source authz.GovernanceSourceEvidence) error {
	if command == "review" {
		var installed bool
		if err := tx.Raw("SELECT to_regclass('public.workforce_login_link_authorities') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='public.workforce_login_links'::regclass AND tgname='workforce_login_review_record' AND tgenabled IN('O','A'))").Scan(&installed).Error; err != nil {
			return err
		}
		if !installed {
			return authz.ErrScopedAuthorizationUnavailable
		}
	}

	proof := loginReviewAuthority{Capability: authz.GovernanceAccountLinkReview, IdleDeadline: p.IdleDeadline, Kind: source.Kind, TenantID: p.TenantID, EmployerID: root.id, EmployerKind: root.kind, StaffID: staffID, ActorSubject: p.Subject, MembershipEpoch: source.MembershipEpoch, OwnerCount: source.OwnerCount, ExpiresAt: source.ExpiresAt, AuthenticatedAt: p.AuthTime}
	if source.Kind == authz.GovernanceOwnerSource {
		proof.OwnerMembershipID = &source.OwnerMembershipID
	} else {
		proof.SourceGrantID = &source.GrantID
		proof.SourceGrantVersion = &source.Version
		proof.TemplateKey = &source.TemplateKey
	}
	if value := c.Param("link"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return gorm.ErrRecordNotFound
		}
		proof.LinkID = &id
	}
	if command == "review" && proof.LinkID == nil {
		return gorm.ErrRecordNotFound
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	mode := "read"
	if command == "review" {
		mode = "review"
	}
	if err = tx.Exec("SELECT set_config('app.workforce_login_review_authority',?,true),set_config('app.workforce_login_review_mode',?,true),set_config('app.workforce_login_review_proposer','',true)", string(raw), mode).Error; err != nil {
		return err
	}
	if command == "review" {
		var parent struct{ ProposedBySubject string }
		if err = tx.Table("workforce_login_links").Select("proposed_by_subject").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, root.id, staffID, *proof.LinkID).Take(&parent).Error; err != nil {
			return err
		}
		return tx.Exec("SELECT set_config('app.workforce_login_review_proposer',?,true)", parent.ProposedBySubject).Error
	}
	return nil
}
