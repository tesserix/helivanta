package authz

import "context"

const PurchaseOrderActorUse Permission = "workforce.purchase_order_actor.use"

// CurrentPurchaseOrderActorGuard verifies current reviewed Staff identity for procurement.
// It grants no order, stock, clinical or financial permission on its own.
type CurrentPurchaseOrderActorGuard interface {
	WithCurrentPurchaseOrderActor(context.Context, StaffActorScope, func(context.Context, StaffActorReference) error) error
}
