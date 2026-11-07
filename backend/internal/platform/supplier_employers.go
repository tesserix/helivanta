package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type supplierEmployerProvider interface {
	SupplierEmployers(*tenantdb.DB) authz.SupplierEmployerGuard
}

func SupplierEmployers(modules []Module, db *tenantdb.DB) authz.SupplierEmployerGuard {
	owner, ok := soleOwner(modules, db, "supply").(supplierEmployerProvider)
	if !ok {
		return nil
	}
	return owner.SupplierEmployers(db)
}
