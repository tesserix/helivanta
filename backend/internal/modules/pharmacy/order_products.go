package pharmacy

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed order_products.sql
var orderProductsSchema string

type orderProductsGuard struct{ db *tenantdb.DB }

// SupplierOrderProducts exposes only current private product identity and owner units.
func (*Module) SupplierOrderProducts(db *tenantdb.DB, _ stockproduct.Guard) supplierorder.CurrentOrderProducts {
	return orderProductsGuard{db: db}
}
func (g orderProductsGuard) WithCurrentOrderProducts(ctx context.Context, e suppliertrade.Endpoint, lines []supplierorder.SupplierOrderLine, action func(context.Context) error) error {
	if g.db == nil || action == nil || !g.db.GuardedCommandActive(ctx) {
		return supplierorder.ErrUnavailable
	}
	scope := authz.OperatingLocationScope{TenantID: e.TenantID.String(), EmployerKind: "pharmacy", EmployerID: e.OrganizationID, LocationID: e.LocationID}
	if e.Product != "pharmacy" || !authz.ValidOperatingLocationScope(scope) {
		return supplierorder.ErrIneligible
	}
	parent, err := (&operatingLocationGuard{db: g.db}).CurrentOperatingLocation(ctx, scope)
	if err != nil {
		return err
	}
	if parent.Scope != scope || parent.EmployerVersion != e.OrganizationVersion || parent.LocationVersion != e.LocationVersion || parent.Country != "IN" {
		return supplierorder.ErrIneligible
	}
	if supplierorder.ValidateProductLines(lines, e.Product) != nil {
		return supplierorder.ErrInvalid
	}
	ordered := slices.Clone(lines)
	slices.SortFunc(ordered, func(a, b supplierorder.SupplierOrderLine) int {
		return strings.Compare(a.Product.ProductID.String(), b.Product.ProductID.String())
	})
	return g.db.WithTenantCommand(ctx, e.TenantID.String(), func(tx *gorm.DB) (err error) {
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.pharmacy_order_catalogue','app.pharmacy_order_outlet','app.pharmacy_order_subject'])AS key`).Scan(&previous).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
		}()
		actor := tenantdb.ActorFrom(ctx)
		if actor == "" {
			return supplierorder.ErrUnavailable
		}
		if err = tx.Exec(`SELECT set_config('app.pharmacy_order_catalogue','read',true),set_config('app.pharmacy_order_outlet',?,true),set_config('app.pharmacy_order_subject',?,true)`, e.LocationID.String(), actor).Error; err != nil {
			return err
		}
		for _, line := range ordered {
			var product struct {
				ID                  uuid.UUID
				Version             int64
				UnitOfIssue, Status string
			}
			if err = tx.Raw(`SELECT id,version,unit_of_issue,status FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=? FOR SHARE`, e.TenantID, e.LocationID, line.Product.ProductID).Scan(&product).Error; err != nil {
				return err
			}
			if product.ID == uuid.Nil || product.Version != *line.Product.Version || product.UnitOfIssue != line.UnitCode || product.Status != "active" {
				return supplierorder.ErrIneligible
			}
		}
		// Do not leak a catalogue purpose into a caller's order writes.
		if err = tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error; err != nil {
			return err
		}
		return action(ctx)
	})
}
