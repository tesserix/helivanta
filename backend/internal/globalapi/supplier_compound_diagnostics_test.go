package globalapi

import (
	"context"
	"testing"

	"github.com/tesserix/helivanta/pkg/authz"
)

type observedCompoundPartnerScopes struct {
	authz.PartnerScopeAuthorizer
	t *testing.T
}

func (a observedCompoundPartnerScopes) WithPartnerScope(ctx context.Context, request authz.PartnerScopeRequest, action func(context.Context, authz.PartnerScopeGrant) error) error {
	if action == nil {
		return a.PartnerScopeAuthorizer.WithPartnerScope(ctx, request, nil)
	}
	entered := false
	err := a.PartnerScopeAuthorizer.WithPartnerScope(ctx, request, func(current context.Context, grant authz.PartnerScopeGrant) error {
		entered = true
		return action(current, grant)
	})
	if err != nil {
		a.t.Logf("compound partner scope: consumer_entered=%t error=%v", entered, err)
	}
	return err
}
