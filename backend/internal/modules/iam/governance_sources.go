package iam

import (
	"context"
	"errors"

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
		return h.withCurrentGovernanceSource(commandCtx, tx, req.Principal, owners, template, req.Scope, nil, false, func(heldCtx context.Context, _ *gorm.DB, _ []string, authority governanceReviewAuthority) error {
			if err := governanceAudit(tx, req.Principal, *authority.GrantID, "iam.governance.source.use"); err != nil {
				return err
			}
			callbackErr = action(heldCtx, authz.GovernanceSourceEvidence{GrantID: *authority.GrantID, Version: authority.GrantVersion, MembershipEpoch: authority.MembershipEpoch, TemplateKey: template, Scope: req.Scope, ExpiresAt: authority.ExpiresAt})
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
