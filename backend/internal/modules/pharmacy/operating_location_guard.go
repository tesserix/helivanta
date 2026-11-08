package pharmacy

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type operatingLocationGuard struct{ db *tenantdb.DB }

func (*Module) OperatingLocations(db *tenantdb.DB) authz.CurrentOperatingLocationGuard {
	return &operatingLocationGuard{db: db}
}
func (g *operatingLocationGuard) CurrentOperatingLocation(ctx context.Context, scope authz.OperatingLocationScope) (authz.OperatingLocationReference, error) {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || !authz.ValidOperatingLocationScope(scope) {
		return authz.OperatingLocationReference{}, authz.ErrScopedAuthorizationUnavailable
	}
	if scope.EmployerKind != "pharmacy" {
		return authz.OperatingLocationReference{}, authz.ErrScopedCommandDenied
	}
	var country string
	err := g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT o.country_code FROM pharmacy_organizations p JOIN pharmacy_outlets o ON o.tenant_id=p.tenant_id AND o.pharmacy_id=p.id WHERE p.tenant_id=? AND p.id=? AND p.ownership_type IN('independent','chain')AND o.id=? AND o.country_code='IN' FOR SHARE OF p,o`, scope.TenantID, scope.EmployerID, scope.LocationID).Row().Scan(&country)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return authz.OperatingLocationReference{}, authz.ErrScopedCommandDenied
	}
	if err != nil {
		return authz.OperatingLocationReference{}, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	// These structural identities are immutable; legal operating eligibility has a separate owner and revision.
	return authz.OperatingLocationReference{Scope: scope, EmployerVersion: 1, LocationVersion: 1, Country: country}, nil
}
