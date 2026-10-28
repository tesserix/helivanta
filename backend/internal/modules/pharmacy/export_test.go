package pharmacy

import (
	"testing"
	"time"
)

// SetClock pins the module clock for a test.
func SetClock(t testing.TB, now func() time.Time) {
	t.Helper()
	prev := clock
	clock = now
	t.Cleanup(func() { clock = prev })
}

// PriceLine and FinancialYear expose the sale arithmetic (#1386 B-1).
var (
	PriceLine     = priceLine
	FinancialYear = financialYear
)

// DiscountLine exposes the B-3 discount rounding.
var DiscountLine = discountLine

// CreditStep exposes the C-1 return arithmetic.
var CreditStep = creditStep

// SetH1ExportRows lowers the H1 register export bound for one test.
func SetH1ExportRows(t interface{ Cleanup(func()) }, n int) {
	old := h1ExportRows
	h1ExportRows = n
	t.Cleanup(func() { h1ExportRows = old })
}
