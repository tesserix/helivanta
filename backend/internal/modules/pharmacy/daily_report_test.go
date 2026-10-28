package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

// #1386 C-3 (#1060): the daily sales report.

type dailyDay struct {
	Day              string `json:"day"`
	Currency         string `json:"currency"`
	Sales            int64  `json:"sales"`
	SalesMinor       int64  `json:"sales_minor"`
	SalesTaxable     int64  `json:"sales_taxable_minor"`
	SalesCGST        int64  `json:"sales_cgst_minor"`
	SalesSGST        int64  `json:"sales_sgst_minor"`
	DiscountMinor    int64  `json:"discount_minor"`
	UnitsSold        int64  `json:"units_sold"`
	CreditNotes      int64  `json:"credit_notes"`
	CreditsMinor     int64  `json:"credits_minor"`
	CreditsTaxable   int64  `json:"credits_taxable_minor"`
	CreditsCGST      int64  `json:"credits_cgst_minor"`
	CreditsSGST      int64  `json:"credits_sgst_minor"`
	UnitsReturned    int64  `json:"units_returned"`
	NetMinor         int64  `json:"net_minor"`
	CashMinor        int64  `json:"cash_minor"`
	UPIMinor         int64  `json:"upi_minor"`
	UPIVerifiedMinor int64  `json:"upi_verified_minor"`
	CardMinor        int64  `json:"card_minor"`
	CashRefundMinor  int64  `json:"cash_refund_minor"`
	UPIRefundMinor   int64  `json:"upi_refund_minor"`
	CardRefundMinor  int64  `json:"card_refund_minor"`
}

func dailyReport(t *testing.T, w saleWorld, who, period string) (int, []dailyDay, string) {
	t.Helper()
	res := do(w.r, "GET", licencePath("/reports/daily"+period), who, "")
	var body struct {
		Days []dailyDay `json:"days"`
	}
	if res.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	}
	return res.Code, body.Days, res.Body.String()
}

func TestTheDailyReportSumsEachOutletDay(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	// 10 units in cash with 500 change, and 2 units by UPI, on 1 November.
	code, cash, out := w.sell(t, cashSale(p, 10, tenUnits, tenUnits+500))
	require.Equal(t, http.StatusCreated, code, out)
	var sale struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sale))
	code, upi, out := w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"upi","amount_minor":2500,"reference":"TEST-UPI-1"}]}`, p))
	require.Equal(t, http.StatusCreated, code, out)
	// 4 of the cash units come back on 2 November.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC) })
	code, note, out := w.returnUnits(t, returnBody(sale.ID, 4, w.quarantine, 5000))
	require.Equal(t, http.StatusCreated, code, out)

	code, days, out := dailyReport(t, w, "owner", "?from=2026-10-31&to=2026-11-03")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, days, 2, "a day without activity has no row")
	first, second := days[0], days[1]
	require.Equal(t, "2026-11-01", first.Day)
	require.Equal(t, "INR", first.Currency)
	require.EqualValues(t, 2, first.Sales)
	require.Equal(t, cash.TotalMinor+upi.TotalMinor, first.SalesMinor)
	require.Equal(t, cash.TaxableMinor+upi.TaxableMinor, first.SalesTaxable)
	require.Equal(t, cash.CGSTMinor+upi.CGSTMinor, first.SalesCGST)
	require.Equal(t, cash.SGSTMinor+upi.SGSTMinor, first.SalesSGST)
	require.EqualValues(t, 12, first.UnitsSold)
	require.EqualValues(t, tenUnits, first.CashMinor, "change handed back is not income")
	require.EqualValues(t, 2500, first.UPIMinor)
	require.Zero(t, first.CardMinor)
	require.Zero(t, first.CreditNotes, "the return counts on its own day, not its sale's")
	require.Equal(t, first.SalesMinor, first.NetMinor)

	require.Equal(t, "2026-11-02", second.Day)
	require.Zero(t, second.Sales)
	require.EqualValues(t, 1, second.CreditNotes)
	require.Equal(t, note.TotalMinor, second.CreditsMinor)
	require.Equal(t, note.TaxableMinor, second.CreditsTaxable)
	require.Equal(t, note.CGSTMinor, second.CreditsCGST)
	require.Equal(t, note.CGSTMinor, second.CreditsSGST)
	require.EqualValues(t, 4, second.UnitsReturned)
	require.EqualValues(t, 5000, second.CashRefundMinor)
	require.Equal(t, -note.TotalMinor, second.NetMinor)
}

func TestTheDailyReportCountsDiscounts(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1000)
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, "TEST senior citizen"))
	require.Equal(t, http.StatusCreated, code, out)
	_, days, out := dailyReport(t, w, "pharmacist", "?from=2026-11-01&to=2026-11-01")
	require.Len(t, days, 1, out)
	require.EqualValues(t, 1250, days[0].DiscountMinor, "the amount taken off the line prices")
	require.EqualValues(t, 11250, days[0].SalesMinor)
}

func TestTheDailyReportIsBoundedAndScoped(t *testing.T) {
	w := newSaleWorld(t, true)
	code, days, out := dailyReport(t, w, "clerk", "?from=2026-11-01&to=2026-11-01")
	require.Equal(t, http.StatusOK, code, out)
	require.Empty(t, days)
	require.Contains(t, out, `"days":[]`, "an empty period is an empty list, not a missing one")
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", licencePath("/reports/daily?from=2026-11-01&to=2026-11-01"), "foreign", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", licencePath("/reports/daily?from=2026-11-01&to=2026-11-01"), "stocker", "").Code,
		"stock and catalogue readers do not see the takings")
	for _, bad := range []string{"", "?from=2026-11-01", "?from=2026-11-02&to=2026-11-01", "?from=2025-10-31&to=2026-11-01"} {
		code, _, _ := dailyReport(t, w, "owner", bad)
		require.Equal(t, http.StatusBadRequest, code, bad)
	}
	code, _, _ = dailyReport(t, w, "owner", "?from=2025-11-01&to=2026-11-01")
	require.Equal(t, http.StatusOK, code, "366 days inclusive is the longest period")
}
