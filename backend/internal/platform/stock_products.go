package platform

import (
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type stockProductProvider interface {
	StockProducts(*tenantdb.DB) stockproduct.Guard
}

// StockProducts binds the current catalogue source only from the single drug master.
func StockProducts(modules []Module, db *tenantdb.DB) stockproduct.Guard {
	owner, ok := soleOwner(modules, db, "drugmaster").(stockProductProvider)
	if !ok {
		return nil
	}
	return owner.StockProducts(db)
}
