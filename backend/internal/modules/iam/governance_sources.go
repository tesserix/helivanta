package iam

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type governanceSourceGuard struct{ handlers *governanceHandlers }

// GovernanceSources binds synchronous product commands to the current IAM grant ledger.
func (*Module) GovernanceSources(db *tenantdb.DB, topology authz.CurrentGovernanceTopologyGuard) authz.CurrentGovernanceSourceGuard {
	return &governanceSourceGuard{handlers: &governanceHandlers{db: db, topology: topology}}
}

func (g *governanceSourceGuard) WithGovernanceSource(ctx context.Context, req authz.GovernanceSourceRequest, action func(context.Context, authz.GovernanceSourceEvidence) error) error {
	return g.withGovernanceSource(ctx, req, false, action)
}

func (g *governanceSourceGuard) withGovernanceSource(ctx context.Context, req authz.GovernanceSourceRequest, activation bool, action func(context.Context, authz.GovernanceSourceEvidence) error) error {
	if action == nil || g == nil || g.handlers == nil || g.handlers.db == nil || g.handlers.topology == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	template := governanceSourceTemplate(req.Capability, req.Scope.Kind)
	if template == "" || req.Scope.TenantID != req.Principal.TenantID || req.Scope.ID == nil || *req.Scope.ID == uuid.Nil {
		return authz.ErrScopedCommandDenied
	}
	switch req.Scope.Kind {
	case "hospital", "pharmacy", "supplier":
	default:
		return authz.ErrScopedCommandDenied
	}
	id := *req.Scope.ID
	req.Scope.ID = &id
	h := g.handlers
	var callbackErr error
	var prelude func(*gorm.DB) error
	if activation {
		prelude = func(tx *gorm.DB) error {
			return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "iam.employment:"+req.Scope.TenantID+":"+id.String()).Error
		}
	}
	err := h.withActorPrelude(ctx, req.Principal, prelude, func(commandCtx context.Context, tx *gorm.DB, owners []string) error {
		if slices.Contains(owners, req.Principal.Subject) {
			var owner member
			// Owner removal can hold this tuple before waiting for our shared owner-count gate.
			if err := tx.Select("id").Where("tenant_id=? AND subject=? AND role_key='tenant_admin'", req.Principal.TenantID, req.Principal.Subject).Take(&owner).Error; err != nil {
				return err
			}
			epoch, err := proposalMembershipEpoch(tx, req.Principal.TenantID, req.Principal.Subject)
			if err != nil {
				return err
			}
			return h.topology.WithGovernanceTopology(commandCtx, req.Scope, func(heldCtx context.Context, ref authz.GovernanceTopologyReference) error {
				if ref.HospitalCount > 1 && len(owners) < 2 {
					return errGovernanceDenied
				}
				if err := tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'iam.governance.owner.source.use','iam_members',?)", req.Principal.TenantID, req.Principal.Subject, owner.ID).Error; err != nil {
					return err
				}
				callbackErr = action(heldCtx, authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: owner.ID, OwnerCount: int64(len(owners)), MembershipEpoch: epoch, Scope: req.Scope, ExpiresAt: req.Principal.IdleDeadline})
				return callbackErr
			})
		}
		return h.withCurrentGovernanceSource(commandCtx, tx, req.Principal, owners, template, req.Scope, nil, false, func(heldCtx context.Context, _ *gorm.DB, _ []string, authority governanceReviewAuthority) error {
			if err := governanceAudit(tx, req.Principal, *authority.GrantID, "iam.governance.source.use"); err != nil {
				return err
			}
			callbackErr = action(heldCtx, authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, OwnerCount: int64(len(owners)), GrantID: *authority.GrantID, Version: authority.GrantVersion, MembershipEpoch: authority.MembershipEpoch, TemplateKey: template, Scope: req.Scope, ExpiresAt: authority.ExpiresAt})
			return callbackErr
		})
	})
	if callbackErr != nil {
		return callbackErr
	}
	if errors.Is(err, errGovernanceDenied) {
		return errors.Join(authz.ErrScopedCommandDenied, err)
	}
	if err != nil && !errors.Is(err, authz.ErrScopedCredentialRevoked) && !errors.Is(err, authz.ErrGovernanceScopeHidden) {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return err
}

func governanceSourceTemplate(capability authz.GovernanceCapability, kind string) string {
	switch capability {
	case authz.GovernanceAccountLinkReview:
		if kind == "hospital" || kind == "pharmacy" || kind == "supplier" {
			return "workforce.account_link_reviewer.v1"
		}
	case authz.GovernancePersonPrepare:
		if kind == "hospital" {
			return "workforce.person_preparer.v1"
		}
	case authz.GovernancePersonReview:
		if kind == "hospital" {
			return "workforce.person_reviewer.v1"
		}
	}
	if kind == "hospital" {
		switch capability {
		case authz.GovernanceEmploymentPrepare:
			return "workforce.employment_preparer.v1"
		case authz.GovernanceEmploymentReview:
			return "workforce.employment_reviewer.v1"
		case authz.GovernanceEmploymentActivate:
			return "workforce.employment_activator.v1"
		}
	}
	return ""
}
