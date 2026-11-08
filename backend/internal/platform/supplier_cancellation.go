package platform

import (
	"context"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

// StockCancellation joins the order consumer transaction through the owning stock module.
type StockCancellation interface {
	ReleaseOrder(context.Context, *gorm.DB, supplierorder.SupplierOrder) error
}

type stockCancellationProvider interface {
	StockCancellation(Deps) StockCancellation
}
type stockCancellation struct {
	owners map[string]StockCancellation
}

func (s stockCancellation) ReleaseOrder(ctx context.Context, tx *gorm.DB, o supplierorder.SupplierOrder) error {
	owner := s.owners[o.Terms.Seller.Product]
	if owner == nil {
		return f.ErrUnavailable
	}
	return owner.ReleaseOrder(ctx, tx, o)
}
func SupplierStockCancellation(modules []Module, deps Deps) StockCancellation {
	result := stockCancellation{owners: map[string]StockCancellation{}}
	for _, name := range []string{"supply", "pharmacy"} {
		if owner, ok := soleOwner(modules, deps.DB, name).(stockCancellationProvider); ok {
			result.owners[name] = owner.StockCancellation(deps)
		}
	}
	return result
}
