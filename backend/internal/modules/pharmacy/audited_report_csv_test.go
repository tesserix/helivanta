package pharmacy_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1060: audited CSV files of patient-wise sales, sales by a prescriber and
// the sales worklist, and the daily report's file.

// exports is the audit_events rows of resource with action, as action:fields.
func exports(t *testing.T, w saleWorld, resource, action string) []string {
	t.Helper()
	var rows []string
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT action||':'||array_to_string(changed_fields,',') FROM audit_events WHERE resource_type=? AND action=? ORDER BY occurred_at`, resource, action).Scan(&rows).Error
	}))
	return rows
}

func TestPatientSalesCSV(t *testing.T) {
	w := newPatientWorld(t)
	query := "?from=2026-11-01&to=2026-11-02&patient_id=" + w.patient
	_, page, out := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales"+query)
	require.Len(t, page.Data, 2, out)
	logs := captureAudit(t)

	rows := csvRecords(t, do(w.r, "GET", licencePath("/reports/patient-sales.csv"+query), "pharmacist", ""))
	require.Len(t, rows, 2)
	for i, l := range page.Data {
		r := rows[i]
		require.Equal(t, w.patient, r["patient_id"])
		require.Equal(t, l.SaleDate, r["sale_date"])
		require.Equal(t, l.InvoiceNumber, r["invoice_number"])
		require.Equal(t, strconv.Itoa(l.LineNumber), r["line_number"])
		require.Equal(t, l.LotNumber, r["lot_number"])
		require.Equal(t, l.ExpiresOn, r["expires_on"])
		require.Equal(t, strconv.FormatInt(l.Units, 10), r["quantity_units"])
		require.Equal(t, rupees(l.MRPMinor), r["mrp"])
		require.Equal(t, rupees(l.AmountMinor), r["amount"])
		require.Equal(t, strconv.FormatInt(l.ReturnedUnits, 10), r["returned_units"])
	}
	require.Equal(t, []string{"report_export:from,to,patient_id"}, exports(t, w.saleWorld, "pharmacy_patient_sales", "report_export"), "the file is a disclosure, field names only")
	events := exportEvents(t, logs)
	require.Len(t, events, 1)
	require.Equal(t, w.patient, events[0]["patient_id"])

	// More than the bound is refused and never recorded as disclosed.
	pharmacy.SetReportExportRows(t, 1)
	res := do(w.r, "GET", licencePath("/reports/patient-sales.csv"+query), "pharmacist", "")
	require.Equal(t, http.StatusRequestEntityTooLarge, res.Code, res.Body.String())
	require.Len(t, exports(t, w.saleWorld, "pharmacy_patient_sales", "report_export"), 1)
	pharmacy.SetReportExportRows(t, 5000)

	// An audit store that refuses the row: no record, no file.
	pharmacy.SetReadRecordFault(t)
	res = do(w.r, "GET", licencePath("/reports/patient-sales.csv"+query), "pharmacist", "")
	require.Equal(t, http.StatusInternalServerError, res.Code)
	require.Empty(t, res.Header().Get("Content-Disposition"))
	require.NotContains(t, res.Body.String(), w.patientSale.InvoiceNumber)
	require.Len(t, exportEvents(t, logs), 1, "no file, no exported line")

	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/reports/patient-sales.csv?from=2026-11-01&to=2026-11-02"), "pharmacist", "").Code, "never every patient")
}

func TestPrescriberSalesCSV(t *testing.T) {
	w := newLotWorld(t)
	rao := "&prescriber_registration=" + url.QueryEscape(raoRegistration) + "&prescriber_council=" + url.QueryEscape(raoCouncil)
	path := licencePath("/reports/items.csv?from=2026-11-01&to=2026-11-01" + rao)
	rows := csvRecords(t, do(w.r, "GET", path, "pharmacist", ""))
	require.Len(t, rows, 1)
	require.Equal(t, w.h, rows[0]["product_id"])
	require.Equal(t, "8", rows[0]["units_sold"], "Dr Rao's two sales only")
	require.Equal(t, []string{"report_export:from,to,prescriber_registration,prescriber_council"}, exports(t, w.saleWorld, "pharmacy_item_sales", "report_export"))
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", path, "owner", "").Code, "the owner reads sales but not prescriptions")

	// Without the prescriber the file is not personal and writes no row.
	csvRecords(t, do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-01&to=2026-11-01"), "pharmacist", ""))
	require.Len(t, exports(t, w.saleWorld, "pharmacy_item_sales", "report_export"), 1)
}

func TestWorklistCSV(t *testing.T) {
	w := newPatientWorld(t)
	query := "?from=2026-11-01&to=2026-11-02"
	_, page, out := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist"+query+"&limit=200")
	require.Len(t, page.Data, 4, out)

	rows := csvRecords(t, do(w.r, "GET", licencePath("/sales/worklist.csv"+query), "pharmacist", ""))
	require.Len(t, rows, len(page.Data))
	for i, s := range page.Data {
		r := rows[i]
		require.Equal(t, s.ID, r["id"], "newest first, as the pages")
		require.Equal(t, s.InvoiceNumber, r["invoice_number"])
		require.Equal(t, s.Customer, r["customer"])
		require.Equal(t, s.Linkage, r["linkage"])
		if s.BedCode != nil {
			require.Equal(t, *s.BedCode, r["admission_bed_code"])
		} else {
			require.Empty(t, r["admission_bed_code"])
		}
		require.Equal(t, rupees(s.TotalMinor), r["total"])
		require.Equal(t, rupees(s.NetMinor), r["net"])
		require.Equal(t, rupees(s.AdmissionDueMinor), r["admission_outstanding"])
	}
	require.Equal(t, []string{"list_export:from,to,customer,invoice,linkage"}, exports(t, w.saleWorld, "pharmacy_sales_worklist", "list_export"), "it holds the admission sale")

	// Walk-in sales alone point to no one.
	csvRecords(t, do(w.r, "GET", licencePath("/sales/worklist.csv"+query+"&customer=walk_in"), "pharmacist", ""))
	require.Len(t, exports(t, w.saleWorld, "pharmacy_sales_worklist", "list_export"), 1)

	// An admission sale past the first page is in the file, so it is recorded.
	w.shelve(t, w.drug, "2028-12-31", 12500, 60)
	for range 55 {
		code, _, out := w.sell(t, cashSale(w.drug, 1, 1250, 1250))
		require.Equal(t, http.StatusCreated, code, out)
	}
	_, first, _ := reportPage[workRow](t, w.saleWorld, "pharmacist", "/sales/worklist"+query)
	for _, s := range first.Data {
		require.NotEqual(t, "admission", s.Customer, "the admission sale is not on the first page")
	}
	rows = csvRecords(t, do(w.r, "GET", licencePath("/sales/worklist.csv"+query), "pharmacist", ""))
	require.Len(t, rows, 59)
	require.Len(t, exports(t, w.saleWorld, "pharmacy_sales_worklist", "list_export"), 2)

	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/sales/worklist.csv?invoice="+page.Data[0].InvoiceNumber), "pharmacist", "").Code, "one invoice is its summary")
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", licencePath("/sales/worklist.csv"+query), "stocker", "").Code)
}

func TestDailyReportCSV(t *testing.T) {
	w := newItemWorld(t)
	_, days, out := dailyReport(t, w.saleWorld, "pharmacist", "?from=2026-11-01&to=2026-11-02")
	require.Len(t, days, 2, out)
	rows := csvRecords(t, do(w.r, "GET", licencePath("/reports/daily.csv?from=2026-11-01&to=2026-11-02"), "clerk", ""))
	require.Len(t, rows, 2)
	for i, d := range days {
		r := rows[i]
		require.Equal(t, d.Day, r["day"])
		require.Equal(t, "INR", r["currency"])
		require.Equal(t, strconv.FormatInt(d.Sales, 10), r["sales"])
		require.Equal(t, rupees(d.SalesMinor), r["sales_amount"])
		require.Equal(t, rupees(d.SalesTaxable), r["sales_taxable"])
		require.Equal(t, rupees(d.SalesCGST), r["sales_cgst"])
		require.Equal(t, rupees(d.DiscountMinor), r["discount"])
		require.Equal(t, strconv.FormatInt(d.UnitsSold, 10), r["units_sold"])
		require.Equal(t, strconv.FormatInt(d.CreditNotes, 10), r["credit_notes"])
		require.Equal(t, rupees(d.CreditsMinor), r["credits"])
		require.Equal(t, rupees(d.CreditsSGST), r["credits_sgst"])
		require.Equal(t, strconv.FormatInt(d.UnitsReturned, 10), r["units_returned"])
		require.Equal(t, rupees(d.NetMinor), r["net"])
		require.Equal(t, rupees(d.CashMinor), r["cash"])
		require.Equal(t, rupees(d.UPIMinor), r["upi"])
		require.Equal(t, rupees(d.CardMinor), r["card"])
		require.Equal(t, rupees(d.CashRefundMinor), r["cash_refunds"])
	}
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/reports/daily.csv?from=2026-11-01&to=2026-11-02&kind=cash"), "clerk", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", licencePath("/reports/daily.csv?from=2026-11-01&to=2026-11-02"), "stocker", "").Code)
}
