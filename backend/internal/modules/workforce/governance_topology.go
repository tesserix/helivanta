package workforce

import (
	"context"
	_ "embed"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed governance_topology.sql
var governanceTopologySchema string

type governanceTopologyGuard struct{ db *tenantdb.DB }

// GovernanceTopology retains the canonical establishment topology for IAM governance commands.
func (*Module) GovernanceTopology(db *tenantdb.DB) authz.CurrentGovernanceTopologyGuard {
	return &governanceTopologyGuard{db: db}
}

func (g *governanceTopologyGuard) WithGovernanceTopology(ctx context.Context, scope authz.GovernanceEstablishmentScope, action func(context.Context, authz.GovernanceTopologyReference) error) error {
	tenant, err := uuid.Parse(scope.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != scope.TenantID || g.db == nil || action == nil || !validLoginSubject(tenantdb.ActorFrom(ctx)) || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	switch scope.Kind {
	case "tenant":
		if scope.ID != nil {
			return authz.ErrGovernanceScopeHidden
		}
	case hospitalEstablishmentKind, pharmacyEstablishmentKind, supplierEstablishmentKind:
		if scope.ID == nil || *scope.ID == uuid.Nil || scope.ID.Variant() != uuid.RFC4122 || scope.Kind != hospitalEstablishmentKind && scope.ID.Version() != 7 {
			return authz.ErrGovernanceScopeHidden
		}
	default:
		return authz.ErrGovernanceScopeHidden
	}
	return g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT workforce_lock_governance_topology(false)`).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if scope.ID != nil {
			var row struct{ ID uuid.UUID }
			err := tx.Table("workforce_hospitals").Select("id").Where("tenant_id=? AND id=? AND establishment_kind=?", scope.TenantID, *scope.ID, scope.Kind).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return authz.ErrGovernanceScopeHidden
			}
			if err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
		}
		var ref authz.GovernanceTopologyReference
		if err := tx.Table("workforce_hospitals").Where("tenant_id=? AND establishment_kind='hospital'", scope.TenantID).Count(&ref.HospitalCount).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		return action(ctx, ref)
	})
}
