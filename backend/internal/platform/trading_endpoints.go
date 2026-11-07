package platform

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type tradingEndpointProvider interface {
	TradingEndpoints(*tenantdb.DB, authz.DepartmentReferenceGuard) suppliertrade.CurrentEndpointGuard
}
type tradingEndpointOwners struct {
	owners map[string]suppliertrade.CurrentEndpointGuard
}

func TradingEndpoints(modules []Module, db *tenantdb.DB, departments authz.DepartmentReferenceGuard) suppliertrade.CurrentEndpointGuard {
	owners := map[string]suppliertrade.CurrentEndpointGuard{}
	for _, product := range []string{"supply", "pharmacy"} {
		provider, ok := soleOwner(modules, db, product).(tradingEndpointProvider)
		if ok {
			owners[product] = provider.TradingEndpoints(db, departments)
		}
	}
	return tradingEndpointOwners{owners: owners}
}
func (g tradingEndpointOwners) WithCurrentTradingEndpoint(ctx context.Context, e suppliertrade.Endpoint, category, operation string, action func(context.Context, suppliertrade.EndpointReference) error) error {
	if !suppliertrade.ValidEndpointUse(e, category, operation) {
		return suppliertrade.ErrIneligible
	}
	owner := g.owners[e.Product]
	if owner == nil || action == nil {
		return suppliertrade.ErrUnavailable
	}
	return owner.WithCurrentTradingEndpoint(ctx, e, category, operation, func(ctx context.Context, ref suppliertrade.EndpointReference) error {
		if ref.Category != category || ref.Operation != operation || !suppliertrade.EndpointEqual(e, ref.Endpoint) || ref.LicenceReviewID == uuid.Nil || ref.LicenceReviewVersion < 1 || ref.PolicyVersion == uuid.Nil {
			return suppliertrade.ErrUnavailable
		}
		return action(ctx, ref)
	})
}

func (g tradingEndpointOwners) WithTradingAdministration(ctx context.Context, p authn.Principal, e suppliertrade.Endpoint, write bool, action func(context.Context) error) error {
	owner, ok := g.owners[e.Product].(suppliertrade.AdministrationGuard)
	if !ok || owner == nil {
		return suppliertrade.ErrUnavailable
	}
	return owner.WithTradingAdministration(ctx, p, e, write, action)
}
