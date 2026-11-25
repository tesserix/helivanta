package workforce

import (
	"context"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type wardIndentActorGuard struct{ actors *staffActorGuard }

// WardIndentActors exposes the separate hospital ward indent eligibility purpose.
func (*Module) WardIndentActors(db *tenantdb.DB) authz.CurrentWardIndentActorGuard {
	return &wardIndentActorGuard{actors: &staffActorGuard{db: db}}
}

func (g *wardIndentActorGuard) WithCurrentWardIndentActor(ctx context.Context, scope authz.StaffActorScope, action func(context.Context, authz.StaffActorReference) error) error {
	if g == nil || g.actors == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.actors.withCurrentEmployeeActor(ctx, scope, authz.WardIndentActorUse, action)
}
