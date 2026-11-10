package pharmacy

import (
	"errors"
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

// SetReadRecordFault makes every audited read's record fail, for one test.
func SetReadRecordFault(t interface{ Cleanup(func()) }) {
	was := readRecordFault
	readRecordFault = func() error { return errors.New("TEST audit store unavailable") }
	t.Cleanup(func() { readRecordFault = was })
}

// SetReportExportRows lowers the report CSV bound for one test.
func SetReportExportRows(t interface{ Cleanup(func()) }, n int) {
	old := reportExportRows
	reportExportRows = n
	t.Cleanup(func() { reportExportRows = old })
}

// MajorUnits and Amount expose the report CSV's money.
var (
	MajorUnits = majorUnits
	Amount     = amount
)

// StockMovementKinds exposes the reconciled movement kinds (#1638).
var StockMovementKinds = stockMovementKinds
