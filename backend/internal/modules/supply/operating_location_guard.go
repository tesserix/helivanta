package supply

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
	if scope.EmployerKind != "supplier" {
		return authz.OperatingLocationReference{}, authz.ErrScopedCommandDenied
	}
	var result authz.OperatingLocationReference
	err := g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) (err error) {
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.supply_employer_check','app.supply_operating_reference','app.supply_reference_party','app.supply_reference_premise'])AS key`).Scan(&previous).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
		}()
		if err = tx.Exec(`SELECT set_config('app.supply_employer_check','true',true),set_config('app.supply_operating_reference','true',true),set_config('app.supply_reference_party',?,true),set_config('app.supply_reference_premise',?,true)`, scope.EmployerID.String(), scope.LocationID.String()).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT p.version,l.version,l.country_code FROM supply_parties p JOIN supply_premises l ON l.tenant_id=p.tenant_id AND l.party_id=p.id WHERE p.tenant_id=? AND p.id=? AND p.record_kind='owned_organisation' AND p.country_code='IN' AND l.id=? AND l.status='active' AND l.kind IN('warehouse','depot') AND l.country_code='IN' FOR SHARE OF p,l`, scope.TenantID, scope.EmployerID, scope.LocationID).Row().Scan(&result.EmployerVersion, &result.LocationVersion, &result.Country)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return authz.OperatingLocationReference{}, authz.ErrScopedCommandDenied
	}
	if err != nil {
		return authz.OperatingLocationReference{}, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	result.Scope = scope
	return result, nil
}
