package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type pharmacyEmployerProvider interface {
	PharmacyEmployers(*tenantdb.DB) authz.PharmacyEmployerGuard
}

// PharmacyEmployers binds only the registered Pharma owner's employer guard.
func PharmacyEmployers(modules []Module, db *tenantdb.DB) authz.PharmacyEmployerGuard {
	provider, ok := soleOwner(modules, db, "pharmacy").(pharmacyEmployerProvider)
	if !ok {
		return nil
	}
	return provider.PharmacyEmployers(db)
}
