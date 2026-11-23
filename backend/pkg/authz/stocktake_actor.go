package authz

import "context"

// CurrentStocktakeActorGuard verifies hospital Staff for physical inventory work.
// Its source evidence grants no count, review, dispensing or financial authority.
type CurrentStocktakeActorGuard interface {
	WithCurrentStocktakeActor(context.Context, StaffActorScope, func(context.Context, StaffActorReference) error) error
}
