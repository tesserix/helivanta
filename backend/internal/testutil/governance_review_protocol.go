package testutil

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GovernanceReviewProtocol supplies an explicit IAM boundary double to focused Workforce/gateway tests.
// Real grant/owner admission, cutoffs and source provenance are tested with IAM in bootstrap.
func GovernanceReviewProtocol(db *tenantdb.DB) authz.CurrentGovernanceSourceGuard {
	return &governanceReviewProtocol{db: db, id: uuid.Must(uuid.NewV7())}
}

type governanceReviewProtocol struct {
	db *tenantdb.DB
	id uuid.UUID
}

func (g *governanceReviewProtocol) WithGovernanceSource(ctx context.Context, req authz.GovernanceSourceRequest, action func(context.Context, authz.GovernanceSourceEvidence) error) error {
	if g.db == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if req.Capability != authz.GovernanceAccountLinkReview || req.Scope.ID == nil || req.Scope.TenantID != req.Principal.TenantID {
		return authz.ErrScopedCommandDenied
	}
	if req.Principal.AuthTime.IsZero() || !req.Principal.IdleDeadline.After(time.Now()) {
		return authz.ErrScopedCredentialRevoked
	}
	until := time.Now().Add(5 * time.Second)
	if req.Principal.IdleDeadline.Before(until) {
		until = req.Principal.IdleDeadline
	}
	ctx = tenantdb.WithActor(ctx, req.Principal.Subject)
	return g.db.WithGuardedTenantCommand(ctx, req.Principal.TenantID, until, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject',?,true)", req.Principal.Subject).Error; err != nil {
			return err
		}
		if req.Scope.Kind != "hospital" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "workforce.independent_identity:"+req.Principal.TenantID+":"+req.Scope.ID.String()).Error; err != nil {
				return err
			}
		}
		var access struct{ ID uuid.UUID }
		return tx.Table("workforce_hospital_access a").Select("a.id").
			Joins("JOIN workforce_hospitals e ON e.tenant_id=a.tenant_id AND e.id=a.hospital_id AND e.establishment_kind=?", req.Scope.Kind).
			Where("a.tenant_id=? AND a.hospital_id=? AND a.subject=? AND a.level='manager'", req.Principal.TenantID, *req.Scope.ID, req.Principal.Subject).
			Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "a"}}).Take(&access).Error
	}, func(commandCtx context.Context) error {
		return action(commandCtx, authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: g.id, Version: 1, MembershipEpoch: 1, OwnerCount: 1, TemplateKey: "workforce.account_link_reviewer.v1", Scope: req.Scope, ExpiresAt: until})
	})
}
