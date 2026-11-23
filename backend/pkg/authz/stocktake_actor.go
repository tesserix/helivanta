package authz

import "context"

// CurrentStocktakeActorGuard verifies hospital Staff for physical inventory work.
// Its source evidence grants no count, review, dispensing or financial authority.
type CurrentStocktakeActorGuard interface {
	WithCurrentStocktakeActor(context.Context, StaffActorScope, func(context.Context, StaffActorReference) error) error
}

// StocktakeActorObservation is retained Workforce source data, not current access.
type StocktakeActorObservation struct {
	Subject   string
	Reference StaffActorReference
}

// StocktakeActorHistoryGuard verifies historical hospital identity and independence
// under the reviewer's held stocktake operation and current Staff source authority.
type StocktakeActorHistoryGuard interface {
	WithStocktakeActorHistoryReview(context.Context, StaffActorScope, StaffActorReference, StocktakeActorObservation, func(context.Context) error) error
}

// HospitalStocktakeActorGuard keeps current eligibility and historical independence under one owner.
type HospitalStocktakeActorGuard interface {
	CurrentStocktakeActorGuard
	StocktakeActorHistoryGuard
}
