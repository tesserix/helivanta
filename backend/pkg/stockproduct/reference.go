// Package stockproduct defines the drug master's current stock-reference boundary.
package stockproduct

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

var ErrIneligible = errors.New("stock product version is not current and active")
var ErrUnavailable = errors.New("stock product authority is unavailable")

// Guard admits a local owner operation while the published source remains current.
type Guard interface {
	WithCurrentStockProduct(context.Context, uuid.UUID, func(context.Context, Reference) error) error
}

// CommandGuard holds publication authority on the existing owner command through its commit.
type CommandGuard interface {
	WithCurrentStockProductInCommand(context.Context, string, uuid.UUID, func(context.Context, Reference) error) error
}

// BarcodeLookup returns only the published stock-purpose reference, never clinical catalogue data.
type BarcodeLookup interface {
	LookupStockProduct(context.Context, string) (Reference, error)
}

var ErrInvalidBarcode = errors.New("invalid stock product barcode")
