package supplierorder

import (
	"context"
	"errors"

	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

var ErrUnavailable = errors.New("supplier order authority unavailable")

// CurrentOrderProducts holds the exact source owner's current product identity and units.
// Callers separately hold current actor authority. These facts do not reserve stock,
// promise availability or approve a financial document.
type CurrentOrderProducts interface {
	WithCurrentOrderProducts(context.Context, suppliertrade.Endpoint, []SupplierOrderLine, func(context.Context) error) error
}

// ValidateProductLines checks bounded owner references and units, never source eligibility.
func ValidateProductLines(lines []SupplierOrderLine, owner string) error {
	if len(lines) < 1 || len(lines) > maximumLines {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if !recordID(line.ID) || seen[line.ID.String()] || !productValid(line.Product, owner) || line.QuantityUnits < 1 || line.QuantityUnits > 1000000000 || !suppliertrade.Text(line.UnitCode, 100) {
			return ErrInvalid
		}
		seen[line.ID.String()] = true
	}
	return nil
}
