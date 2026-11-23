package workforce

import (
	"context"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type stocktakeActorGuard struct{ actors *staffActorGuard }

// StocktakeActors exposes the separate hospital physical-count eligibility purpose.
func (*Module) StocktakeActors(db *tenantdb.DB) authz.CurrentStocktakeActorGuard {
	return &stocktakeActorGuard{actors: &staffActorGuard{db: db}}
}

func (g *stocktakeActorGuard) WithCurrentStocktakeActor(ctx context.Context, scope authz.StaffActorScope, action func(context.Context, authz.StaffActorReference) error) error {
	if g == nil || g.actors == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.actors.withCurrentEmployeeActor(ctx, scope, authz.StocktakeActorUse, action)
}
