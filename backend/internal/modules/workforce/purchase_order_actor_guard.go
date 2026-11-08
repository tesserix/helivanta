package workforce

import (
	"context"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type purchaseOrderActorGuard struct{ actors *staffActorGuard }

// PurchaseOrderActors exposes the separately scoped procurement eligibility purpose.
func (*Module) PurchaseOrderActors(db *tenantdb.DB) authz.CurrentPurchaseOrderActorGuard {
	return &purchaseOrderActorGuard{actors: &staffActorGuard{db: db}}
}
func (g *purchaseOrderActorGuard) WithCurrentPurchaseOrderActor(ctx context.Context, scope authz.StaffActorScope, action func(context.Context, authz.StaffActorReference) error) error {
	if g == nil || g.actors == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.actors.withCurrentEmployeeActor(ctx, scope, authz.PurchaseOrderActorUse, action)
}
