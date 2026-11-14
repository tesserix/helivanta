package workforce

import (
	"context"
	"errors"
	"time"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (g *independentStaffGuard) WithGovernedIndependentEmployerAccess(ctx context.Context, request authz.GovernedIndependentEmployerRequest, action func(context.Context) error) error {
	p, scope, source := request.Principal, request.Scope, request.Source
	if g.db == nil || action == nil || !g.db.GuardedCommandActive(ctx) || tenantdb.ActorFrom(ctx) != p.Subject {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if scope.TenantID != p.TenantID || (scope.EmployerKind != pharmacyEstablishmentKind && scope.EmployerKind != supplierEstablishmentKind) || !validLoginSubject(p.Subject) {
		return authz.ErrScopedCommandDenied
	}
	if request.Capability != authz.GovernanceOperatingManage && request.Capability != authz.GovernanceOperatingReview {
		return authz.ErrScopedCommandDenied
	}
	root := establishment{id: scope.EmployerID, kind: scope.EmployerKind}
	if !validWorkforceGovernanceSource(p, root, source, "iam.operating_access_reviewer.v1") || p.AuthTime.IsZero() || !p.IdleDeadline.After(time.Now()) || !source.ExpiresAt.After(time.Now()) {
		return authz.ErrScopedCommandDenied
	}
	if err := g.db.ConstrainGuardedCommand(ctx, p.TenantID, source.ExpiresAt); err != nil {
		return err
	}
	err := g.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := root.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'workforce.operating.governance.source.read','workforce_hospitals',?)`, p.TenantID, p.Subject, root.id).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authz.ErrScopedCommandDenied
	}
	if err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return action(ctx)
}
