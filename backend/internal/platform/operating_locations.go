package platform

import (
	"context"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type operatingLocationProvider interface {
	OperatingLocations(*tenantdb.DB) authz.CurrentOperatingLocationGuard
}
type operatingLocations struct {
	sources map[string]authz.CurrentOperatingLocationGuard
}

func OperatingLocations(modules []Module, db *tenantdb.DB) authz.CurrentOperatingLocationGuard {
	sources := map[string]authz.CurrentOperatingLocationGuard{}
	for kind, name := range map[string]string{"supplier": "supply", "pharmacy": "pharmacy"} {
		if owner, ok := soleOwner(modules, db, name).(operatingLocationProvider); ok {
			sources[kind] = owner.OperatingLocations(db)
		}
	}
	return operatingLocations{sources: sources}
}
func (g operatingLocations) CurrentOperatingLocation(ctx context.Context, scope authz.OperatingLocationScope) (authz.OperatingLocationReference, error) {
	guard := g.sources[scope.EmployerKind]
	if guard == nil || !authz.ValidOperatingLocationScope(scope) {
		return authz.OperatingLocationReference{}, authz.ErrScopedAuthorizationUnavailable
	}
	result, err := guard.CurrentOperatingLocation(ctx, scope)
	if err != nil {
		return authz.OperatingLocationReference{}, err
	}
	if result.Scope != scope || result.Country != "IN" || result.EmployerVersion < 1 || result.LocationVersion < 1 {
		return authz.OperatingLocationReference{}, authz.ErrScopedAuthorizationUnavailable
	}
	return result, nil
}
