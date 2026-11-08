package platform

import (
	"context"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
)

type stockCancellationProvider interface {
	StockCancellation(Deps) f.StockCancellation
}
type stockCancellation struct {
	owners map[string]f.StockCancellation
}

func (s stockCancellation) ReleaseOrder(ctx context.Context, o supplierorder.SupplierOrder) error {
	owner := s.owners[o.Terms.Seller.Product]
	if owner == nil {
		return f.ErrUnavailable
	}
	return owner.ReleaseOrder(ctx, o)
}
func SupplierStockCancellation(modules []Module, deps Deps) f.StockCancellation {
	result := stockCancellation{owners: map[string]f.StockCancellation{}}
	for _, name := range []string{"supply", "pharmacy"} {
		if owner, ok := soleOwner(modules, deps.DB, name).(stockCancellationProvider); ok {
			result.owners[name] = owner.StockCancellation(deps)
		}
	}
	return result
}
