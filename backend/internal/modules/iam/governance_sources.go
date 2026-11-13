package iam

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type governanceSourceGuard struct{ handlers *governanceHandlers }

// GovernanceSources binds synchronous product commands to the current IAM grant ledger.
func (*Module) GovernanceSources(db *tenantdb.DB, topology authz.CurrentGovernanceTopologyGuard) authz.CurrentGovernanceSourceGuard {
	return &governanceSourceGuard{handlers: &governanceHandlers{db: db, topology: topology}}
}

func (g *governanceSourceGuard) WithGovernanceSource(ctx context.Context, req authz.GovernanceSourceRequest, action func(context.Context, authz.GovernanceSourceEvidence) error) error {
	if action == nil || g == nil || g.handlers == nil || g.handlers.db == nil || g.handlers.topology == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if req.Capability != authz.GovernanceAccountLinkReview || req.Scope.TenantID != req.Principal.TenantID || req.Scope.ID == nil || *req.Scope.ID == uuid.Nil {
		return authz.ErrScopedCommandDenied
	}
	switch req.Scope.Kind {
	case "hospital", "pharmacy", "supplier":
	default:
		return authz.ErrScopedCommandDenied
	}
	id := *req.Scope.ID
	req.Scope.ID = &id
	const template = "workforce.account_link_reviewer.v1"
	h := g.handlers
	var callbackErr error
	err := h.withActor(ctx, req.Principal, func(commandCtx context.Context, tx *gorm.DB, owners []string) error {
		if slices.Contains(owners, req.Principal.Subject) {
			var owner member
			if err := tx.Select("id").Where("tenant_id=? AND subject=? AND role_key='tenant_admin'", req.Principal.TenantID, req.Principal.Subject).Clauses(clause.Locking{Strength: "SHARE"}).Take(&owner).Error; err != nil {
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
