package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type purchaseOrderActorProvider interface {
	PurchaseOrderActors(*tenantdb.DB) authz.CurrentPurchaseOrderActorGuard
}

// PurchaseOrderActors resolves only the Workforce-owned procurement actor boundary.
func PurchaseOrderActors(modules []Module, db *tenantdb.DB) authz.CurrentPurchaseOrderActorGuard {
	owner, ok := soleOwner(modules, db, "workforce").(purchaseOrderActorProvider)
	if !ok {
		return nil
	}
	return owner.PurchaseOrderActors(db)
}
