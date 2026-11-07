package suppliertrade

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
)

var ErrIneligible = errors.New("current trading source ineligible")
var ErrUnavailable = errors.New("current trading source unavailable")

type EndpointReference = SupplierTradeEndpointReference

// CurrentEndpointGuard checks owner facts inside the caller's guarded command.
type CurrentEndpointGuard interface {
	WithCurrentTradingEndpoint(context.Context, Endpoint, string, string, func(context.Context, EndpointReference) error) error
}

// AdministrationGuard checks resource ownership separately from local regulatory facts.
type AdministrationGuard interface {
	WithTradingAdministration(context.Context, authn.Principal, Endpoint, bool, func(context.Context) error) error
}

// RelationshipAuthorization is opaque owner evidence, not a user permission or wire token.
type RelationshipAuthorization interface{ Snapshot() Relationship }
type RelationshipScope struct {
	TenantID    string
	ID          uuid.UUID
	TermsSha256 string
	Purpose     string
}
type CurrentRelationshipGuard interface {
	VerifyRelationship(context.Context, RelationshipScope) (RelationshipAuthorization, error)
	WithVerifiedRelationship(context.Context, RelationshipAuthorization, func(context.Context, Relationship) error) error
}
