package supply

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type warehouseCancellation struct{ db *tenantdb.DB }

func (*Module) StockCancellation(deps platform.Deps) f.StockCancellation {
	return warehouseCancellation{db: deps.DB}
}
func (g warehouseCancellation) ReleaseOrder(ctx context.Context, o supplierorder.SupplierOrder) error {
	if g.db == nil || !g.db.GuardedCommandActive(ctx) || tenantdb.ActorFrom(ctx) != "system:supplier-fulfilment" {
		return f.ErrUnavailable
	}
	return g.db.WithTenantCommand(ctx, o.OwnerTenantID.String(), func(tx *gorm.DB) error {
		s := supplierstock.Store{Owner: supplierstock.Warehouse}
		return s.Frame(tx, supplierstock.Scope{TenantID: o.OwnerTenantID, OrganizationID: o.Terms.Seller.OrganizationID, LocationID: o.Terms.Seller.LocationID, OrderID: o.ID, Subject: "system:supplier-fulfilment", Mode: "cancel"}, func() error { return s.Cancel(tx, o) })
	})
}
