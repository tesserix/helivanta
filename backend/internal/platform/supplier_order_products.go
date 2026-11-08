package platform

import (
	"context"

	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type supplierOrderProductProvider interface {
	SupplierOrderProducts(*tenantdb.DB, stockproduct.Guard) supplierorder.CurrentOrderProducts
}
type supplierOrderProductOwners struct {
	owners map[string]supplierorder.CurrentOrderProducts
}

// OrderProducts resolves one current identity/unit owner for each source stock model.
func OrderProducts(modules []Module, db *tenantdb.DB, products stockproduct.Guard) supplierorder.CurrentOrderProducts {
	owners := map[string]supplierorder.CurrentOrderProducts{}
	for _, name := range []string{"supply", "pharmacy"} {
		provider, ok := soleOwner(modules, db, name).(supplierOrderProductProvider)
		if ok {
			owners[name] = provider.SupplierOrderProducts(db, products)
		}
	}
	return supplierOrderProductOwners{owners: owners}
}
func (g supplierOrderProductOwners) WithCurrentOrderProducts(ctx context.Context, e suppliertrade.Endpoint, lines []supplierorder.SupplierOrderLine, action func(context.Context) error) error {
	if e.Product != "supply" && e.Product != "pharmacy" {
		return supplierorder.ErrIneligible
	}
	owner := g.owners[e.Product]
	if owner == nil || action == nil {
		return supplierorder.ErrUnavailable
	}
	if supplierorder.ValidateProductLines(lines, e.Product) != nil {
		return supplierorder.ErrInvalid
	}
	return owner.WithCurrentOrderProducts(ctx, e, lines, action)
}
