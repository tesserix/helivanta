package suppliertrade

import (
	"context"
	"errors"
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
