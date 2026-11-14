package supply

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// Legacy receipt rights describe administrative observation; managed duties are checked independently.
func (h *handlers) stockTransaction(c *gin.Context, p authn.Principal, party, premise uuid.UUID, permission authz.Permission, action func(*gorm.DB) error) error {
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if permissions.Has(permission) {
		err := h.transaction(c.Request.Context(), p, party, false, false, action)
		if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) || h.operating == nil {
			return err
		}
	}
	if h.operating == nil {
		return authz.ErrScopedCommandDenied
	}
	return h.managedStock(c.Request.Context(), p, party, premise, permission, func(_ context.Context, tx *gorm.DB) error { return action(tx) })
}
func (h *handlers) managedStock(ctx context.Context, p authn.Principal, party, premise uuid.UUID, permission authz.Permission, action func(context.Context, *gorm.DB) error) error {
	if h.operating == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.operating.WithOperatingScope(tenantdb.WithActor(ctx, p.Subject), authz.OperatingScopeRequest{Principal: p, EmployerKind: "supplier", EmployerID: party, LocationID: premise, Permission: permission}, func(ctx context.Context, grant authz.OperatingScopeGrant) error {
		if grant.Scope != (authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: "supplier", EmployerID: party, LocationID: premise}) || grant.Permission != permission {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
			var previous string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.supply_subject','app.supply_party','app.supply_stock_access','app.supply_stock_party','app.supply_stock_premise','app.supply_stock_permission'])AS key`).Scan(&previous).Error; err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
			}()
			if err = tx.Exec(`SELECT set_config('app.supply_subject',?,true),set_config('app.supply_party',?,true),set_config('app.supply_stock_access','authorized',true),set_config('app.supply_stock_party',?,true),set_config('app.supply_stock_premise',?,true),set_config('app.supply_stock_permission',?,true)`, p.Subject, party.String(), party.String(), premise.String(), string(permission)).Error; err != nil {
				return err
			}
			return action(ctx, tx)
		})
	})
}

func hasManagedStockScope(tx *gorm.DB, p authn.Principal, party, premise uuid.UUID) (bool, error) {
	var allowed bool
	err := tx.Raw(`SELECT supply_stock_scope(?::uuid,?::uuid,?::uuid,'read')`, p.TenantID, party, premise).Scan(&allowed).Error
	return allowed, err
}
func stockParent(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) (premise, error) {
	managed, err := hasManagedStockScope(tx, p, partyID, premiseID)
	if err != nil {
		return premise{}, err
	}
	if !managed {
		return operatingParent(tx, p, partyID, premiseID, roles)
	}
	var row premise
	err = tx.Select("id,party_id,country_code,kind,status").Where("tenant_id=? AND party_id=? AND id=? AND kind IN ? AND status='active' AND country_code='IN'", p.TenantID, partyID, premiseID, []string{"warehouse", "depot"}).Take(&row).Error
	return row, err
}
func stockHistoryParent(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID, roles []string) (premise, error) {
	managed, err := hasManagedStockScope(tx, p, partyID, premiseID)
	if err != nil {
		return premise{}, err
	}
	if !managed {
		return operatingHistoryParent(tx, p, partyID, premiseID, roles)
	}
	return stockParent(tx, p, partyID, premiseID, roles)
}

func (h *handlers) stockOwnerTransaction(ctx context.Context, p authn.Principal, party uuid.UUID, action func(*gorm.DB) error) error {
	if h.db.GuardedCommandActive(ctx) {
		return h.db.WithTenantCommand(ctx, p.TenantID, action)
	}
	return h.transaction(ctx, p, party, false, false, action)
}
func (h *handlers) withStockProduct(c *gin.Context, p authn.Principal, party, premise, version uuid.UUID, permission authz.Permission, action func(context.Context, stockproduct.Reference) error) error {
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if permissions.Has(permission) {
		err := h.stockProducts.WithCurrentStockProduct(c.Request.Context(), version, action)
		if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) || h.operating == nil {
			return err
		}
	}
	command, ok := h.stockProducts.(stockproduct.CommandGuard)
	if !ok {
		return stockproduct.ErrUnavailable
	}
	return h.managedStock(c.Request.Context(), p, party, premise, permission, func(ctx context.Context, _ *gorm.DB) error {
		return command.WithCurrentStockProductInCommand(ctx, p.TenantID, version, action)
	})
}
