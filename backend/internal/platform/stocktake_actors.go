package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type stocktakeActorProvider interface {
	StocktakeActors(*tenantdb.DB) authz.CurrentStocktakeActorGuard
}

// StocktakeActors resolves Workforce's current and historical physical-count identity boundary.
func StocktakeActors(modules []Module, db *tenantdb.DB) authz.HospitalStocktakeActorGuard {
	owner, ok := soleOwner(modules, db, "workforce").(stocktakeActorProvider)
	if !ok {
		return nil
	}
	guard, ok := owner.StocktakeActors(db).(authz.HospitalStocktakeActorGuard)
	if !ok {
		return nil
	}
	return guard
}

type wardIndentActorProvider interface {
	WardIndentActors(*tenantdb.DB) authz.CurrentWardIndentActorGuard
}

// WardIndentActors resolves Workforce's current ward indent identity boundary.
func WardIndentActors(modules []Module, db *tenantdb.DB) authz.CurrentWardIndentActorGuard {
	owner, ok := soleOwner(modules, db, "workforce").(wardIndentActorProvider)
	if !ok {
		return nil
	}
	return owner.WardIndentActors(db)
}
