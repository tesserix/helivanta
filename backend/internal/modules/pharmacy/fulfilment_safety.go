package pharmacy

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"reflect"
)

type outletShipmentSafety struct{ deps platform.Deps }

func (*Module) ShipmentSafety(deps platform.Deps) f.ShipmentSafety {
	return outletShipmentSafety{deps: deps}
}
func (g outletShipmentSafety) WithSafeShipment(ctx context.Context, s f.Shipment, action func(context.Context) error) error {
	if g.deps.DB == nil || action == nil || tenantdb.ActorFrom(ctx) == "" || f.ValidateShipment(s) != nil || s.Product.Owner != "pharmacy" {
		return f.ErrUnavailable
	}
	return g.deps.DB.WithTenant(ctx, s.SourceTenantID.String(), func(tx *gorm.DB) (err error) {
		return pharmacyStockFrame(tx, tenantdb.ActorFrom(ctx), s.SourceOrganizationID, s.SourceLocationID, PermStockFulfil, func() error {
			store := supplierstock.Store{Owner: supplierstock.Outlet}
			return store.Frame(tx, supplierstock.Scope{TenantID: s.SourceTenantID, OrganizationID: s.SourceOrganizationID, LocationID: s.SourceLocationID, AllocationID: s.AllocationID, Subject: tenantdb.ActorFrom(ctx), Mode: "safety"}, func() error {
				if err := store.VerifyShipment(tx, s); err != nil {
					return err
				}
				a, e := store.Current(tx, s.SourceTenantID, s.AllocationID)
				if e != nil {
					return e
				}
				if a.Plan.OrderID != s.OrderID || a.Plan.OrderInputSha256 != s.OrderInputSha256 || !reflect.DeepEqual(a.Plan.Product, s.Product) {
					return f.ErrUnavailable
				}
				var product catalogueProduct
				if e = tx.Where("tenant_id=? AND outlet_id=? AND id=?", s.SourceTenantID, s.SourceLocationID, s.Product.ProductID).Take(&product).Error; e != nil {
					return e
				}
				if s.Product.Version == nil || product.Version != *s.Product.Version || product.Status != "active" {
					return f.ErrConflict
				}
				var batch stockBatch
				if e = tx.Where("tenant_id=? AND outlet_id=? AND id=?", s.SourceTenantID, s.SourceLocationID, a.Plan.Input.StockID).Take(&batch).Error; e != nil {
					return e
				}
				if s.MrpMinor == nil || *s.MrpMinor != batch.BatchFields.MRPMinor {
					return f.ErrConflict
				}
				var unsafe bool
				if e = tx.Raw(`SELECT EXISTS(SELECT 1 FROM pharmacy_batch_safety_incidents WHERE tenant_id=? AND outlet_id=? AND batch_id=?)OR EXISTS(SELECT 1 FROM pharmacy_stock_batches WHERE tenant_id=? AND outlet_id=? AND id=? AND expires_on<=(clock_timestamp()AT TIME ZONE 'Asia/Kolkata')::date)`, s.SourceTenantID, s.SourceLocationID, a.Plan.Input.StockID, s.SourceTenantID, s.SourceLocationID, a.Plan.Input.StockID).Scan(&unsafe).Error; e != nil {
					return e
				}
				if unsafe {
					return f.ErrConflict
				}
				return action(ctx)
			})
		})
	})
}
