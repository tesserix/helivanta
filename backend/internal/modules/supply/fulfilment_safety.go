package supply

import (
	"context"
	"errors"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type warehouseShipmentSafety struct{ deps platform.Deps }

func (*Module) ShipmentSafety(deps platform.Deps) f.ShipmentSafety {
	return warehouseShipmentSafety{deps: deps}
}
func (g warehouseShipmentSafety) WithSafeShipment(ctx context.Context, s f.Shipment, action func(context.Context) error) error {
	if g.deps.DB == nil || g.deps.StockProducts == nil || action == nil || tenantdb.ActorFrom(ctx) == "" || f.ValidateShipment(s) != nil || s.Product.Owner != "supply" || s.Product.VersionID == nil {
		return f.ErrUnavailable
	}
	return g.deps.DB.WithTenant(ctx, s.SourceTenantID.String(), func(tx *gorm.DB) (err error) {
		var old string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.supply_subject','app.supply_party','app.supply_stock_access','app.supply_stock_party','app.supply_stock_premise','app.supply_stock_permission'])AS key`).Scan(&old).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, old).Error)
		}()
		if err = tx.Exec(`SELECT set_config('app.supply_subject',?,true),set_config('app.supply_party',?,true),set_config('app.supply_stock_access','authorized',true),set_config('app.supply_stock_party',?,true),set_config('app.supply_stock_premise',?,true),set_config('app.supply_stock_permission','supply.stock.fulfil',true)`, tenantdb.ActorFrom(ctx), s.SourceOrganizationID.String(), s.SourceOrganizationID.String(), s.SourceLocationID.String()).Error; err != nil {
			return err
		}
		store := supplierstock.Store{Owner: supplierstock.Warehouse}
		return store.Frame(tx, supplierstock.Scope{TenantID: s.SourceTenantID, OrganizationID: s.SourceOrganizationID, LocationID: s.SourceLocationID, AllocationID: s.AllocationID, Subject: tenantdb.ActorFrom(ctx), Mode: "safety"}, func() error {
			a, e := store.Current(tx, s.SourceTenantID, s.AllocationID)
			if e != nil {
				return e
			}
			if a.Plan.OrderID != s.OrderID || a.Plan.OrderInputSha256 != s.OrderInputSha256 || a.Plan.Input.LineID != s.LineID {
				return f.ErrUnavailable
			}
			adapter := warehouseFulfilment{}
			evidence, e := adapter.Snapshot(tx, a.Plan)
			if e != nil {
				return e
			}
			if evidence.LotNumber != s.LotNumber || evidence.ExpiresOn != s.ExpiresOn {
				return f.ErrConflict
			}
			return g.deps.StockProducts.WithCurrentStockProduct(ctx, *s.Product.VersionID, func(ctx context.Context, r stockproduct.Reference) error {
				if r.ProductID != s.Product.ProductID {
					return f.ErrConflict
				}
				return action(ctx)
			})
		})
	})
}
