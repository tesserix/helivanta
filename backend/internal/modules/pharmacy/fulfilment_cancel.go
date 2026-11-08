package pharmacy

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type outletCancellation struct{}

func (*Module) StockCancellation(deps platform.Deps) platform.StockCancellation {
	return outletCancellation{}
}
func (g outletCancellation) ReleaseOrder(ctx context.Context, tx *gorm.DB, o supplierorder.SupplierOrder) error {
	if tx == nil || tenantdb.ActorFrom(ctx) != "system:supplier-fulfilment" {
		return f.ErrUnavailable
	}
	s := supplierstock.Store{Owner: supplierstock.Outlet}
	return s.Frame(tx, supplierstock.Scope{TenantID: o.OwnerTenantID, OrganizationID: o.Terms.Seller.OrganizationID, LocationID: o.Terms.Seller.LocationID, OrderID: o.ID, Subject: "system:supplier-fulfilment", Mode: "cancel"}, func() error { return s.Cancel(tx, o) })
}
