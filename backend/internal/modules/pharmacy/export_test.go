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

// WithinCeiling and TaxInside expose the #1371 ceiling cap.
var (
	WithinCeiling = withinCeiling
	TaxInside     = taxInside
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

// SetRxRegisterExportRows lowers the prescription register export bound for one test.
func SetRxRegisterExportRows(t interface{ Cleanup(func()) }, n int) {
	old := rxRegisterExportRows
	rxRegisterExportRows = n
	t.Cleanup(func() { rxRegisterExportRows = old })
}

// SetSummaryCreditNotes lowers the credit notes a sale summary lists for one test.
func SetSummaryCreditNotes(t interface{ Cleanup(func()) }, n int) {
	was := summaryCreditNotes
	summaryCreditNotes = n
	t.Cleanup(func() { summaryCreditNotes = was })
}

// SetSummaryMidRead runs fn between a sale summary's line read and the rest,
// for one test: where a concurrent return would land.
func SetSummaryMidRead(t interface{ Cleanup(func()) }, fn func()) {
	was := summaryMidRead
	summaryMidRead = fn
	t.Cleanup(func() { summaryMidRead = was })
}
