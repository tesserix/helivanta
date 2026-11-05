package pharmacy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/csvsafe"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// CSV files of the pharmacy reports (#1060). Each is its report's own query
// in one transaction, all or an error: a file cut off at a bound would read
// as the whole period. A file that names or points to a person (a patient's
// purchases, a prescriber's sales, a worklist with an admission sale) writes
// its audit_events row in that transaction after the whole file is built:
// no record, no file, and a refused file is never recorded as disclosed.
// Every file sent is also recorded in the audit stream.

// reportExportRows bounds a file; a var only so a test can reach the bound.
var reportExportRows = 5000

const reportExportBytes = 8 * 1024 * 1024

var errReportTooLarge = errors.New("report export exceeds its row limit")

// reportScope is what a file was read for: its period, the outlet's today,
// and each parameter's value as applied.
type reportScope struct {
	since, until, today string
	applied             func(name string) string
}

// reportFile is a report's CSV: its name, the parameters it takes besides
// from and to, its header, its row and its read.
type reportFile[T any] struct {
	name   string
	params []string
	header []string
	row    func(T, reportScope) ([]string, error)
	// read reads at most limit+1 rows and runs inTx on them inside the read's
	// transaction, before any audit row; false means it has responded.
	read func(c *gin.Context, h *catalogueHandlers, limit int, inTx func([]T, reportScope) error) bool
}

// fromReport reads a file through its report's pages' query, audited as an
// export.
func fromReport[T any](r salesReport[T]) func(*gin.Context, *catalogueHandlers, int, func([]T, reportScope) error) bool {
	return func(c *gin.Context, h *catalogueHandlers, limit int, inTx func([]T, reportScope) error) bool {
		_, _, ok := r.read(c, h, pagination.Params{Limit: limit}, "report_export", func(rows []T, f salesReportFilter) error {
			return inTx(rows, reportScope{since: f.since, until: f.until, today: f.today, applied: f.applied})
		})
		return ok
	}
}

// reportReadError answers a failed report read; a file over its bounds is
// 413, never a cut-off file.
func reportReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errReportTooLarge):
		respond.Error(c, http.StatusRequestEntityTooLarge, "report_export_too_large", fmt.Sprintf("the report has more than %d rows; choose a shorter period or a narrower filter", reportExportRows))
	case errors.Is(err, csvsafe.ErrTooLarge):
		respond.Error(c, http.StatusRequestEntityTooLarge, "report_export_too_large", "the report exceeds 8 MiB; choose a shorter period or a narrower filter")
	default:
		stockError(c, err)
	}
}

// exportReport refuses any parameter the report does not take, among them
// cursor and limit.
func exportReport[T any](h *catalogueHandlers, f reportFile[T]) gin.HandlerFunc {
	allowed := map[string]bool{"from": true, "to": true}
	for _, name := range f.params {
		allowed[name] = true
	}
	return func(c *gin.Context) {
		p, outlet, ok := catalogueScope(c)
		if !ok {
			return
		}
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid report filters")
			return
		}
		for name, values := range query {
			if !allowed[name] {
				respond.Error(c, http.StatusBadRequest, "invalid_request", "this report's CSV takes from, to and "+strings.Join(f.params, ", "))
				return
			}
			if len(values) != 1 || values[0] == "" {
				respond.Error(c, http.StatusBadRequest, "invalid_request", "report filters must be nonempty and occur once")
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		var data []byte
		var rows int
		var scope reportScope
		if !f.read(c, h, reportExportRows, func(read []T, s reportScope) error {
			if len(read) > reportExportRows {
				return errReportTooLarge
			}
			var err error
			data, err = encodeReport(read, s, f)
			rows, scope = len(read), s
			return err
		}) {
			return
		}

		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		logged := []any{"report", f.name, "tenant_id", p.TenantID, "subject", p.Subject, "pharmacy_id", currentPharmacy(ctx), "outlet_id", outlet}
		for _, name := range append([]string{"from", "to"}, f.params...) {
			logged = append(logged, name, scope.applied(name))
		}
		logged = append(logged, "row_count", rows, "sha256", digest)
		audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "pharmacy.report.exported", "report CSV generated", logged...)
		c.Header("Cache-Control", "private, no-store")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-%s-%s-%s-%s.csv"`, f.name, outlet, scope.since, scope.until))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Export-Row-Count", strconv.Itoa(rows))
		c.Header("X-Export-Sha256", digest)
		c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
	}
}

// applied is the value of a report parameter as the query used it (a lot
// trimmed, an id parsed), or "all"; the audit log records what was exported.
func (f salesReportFilter) applied(name string) string {
	text := map[string]*string{"kind": f.kind, "lot_number": f.lot, "prescriber_registration": f.registration, "prescriber_council": f.council}[name]
	id := map[string]*uuid.UUID{"product_id": f.product, "category_id": f.category, "subcategory_id": f.subcategory, "patient_id": f.patient}[name]
	switch {
	case name == "from":
		return f.since
	case name == "to":
		return f.until
	case text != nil:
		return *text
	case id != nil:
		return id.String()
	}
	return "all"
}

func encodeReport[T any](rows []T, scope reportScope, f reportFile[T]) ([]byte, error) {
	w := csvsafe.NewExcelWriter(reportExportBytes)
	if err := w.Write(append([]string{"period_from", "period_to"}, f.header...)); err != nil {
		return nil, err
	}
	for _, row := range rows {
		cells, err := f.row(row, scope)
		if err != nil {
			return nil, err
		}
		if err := w.Write(append([]string{scope.since, scope.until}, cells...)); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}

// majorUnits writes minor units exactly in the major unit: -1250 with two
// digits is -12.50. The sign is split from the formatted digits, never
// negated, so the most negative amount cannot overflow.
func majorUnits(minor int64, digits int) string {
	sign, s := "", strconv.FormatInt(minor, 10)
	if minor < 0 {
		sign, s = "-", s[1:]
	}
	if digits == 0 {
		return sign + s
	}
	if len(s) <= digits {
		s = strings.Repeat("0", digits-len(s)+1) + s
	}
	return sign + s[:len(s)-digits] + "." + s[len(s)-digits:]
}

// amount is minor units of currency in its major unit. A currency no country
// profile declares fails: guessing its digits could misstate the amount a
// hundredfold.
func amount(minor int64, currency string) (string, error) {
	digits, ok := countryprofile.CurrencyMinorDigits(currency)
	if !ok {
		return "", fmt.Errorf("no country profile declares the minor digits of %q", currency)
	}
	return majorUnits(minor, digits), nil
}

// amounts writes each of values in currency; a nil value is blank.
func amounts(currency string, values ...*int64) ([]string, error) {
	out := make([]string, len(values))
	for i, v := range values {
		if v == nil {
			continue
		}
		var err error
		if out[i], err = amount(*v, currency); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func units(n int64) string { return strconv.FormatInt(n, 10) }

var itemSalesFile = reportFile[itemSalesRow]{
	name:   "items",
	params: []string{"product_id", "category_id", "subcategory_id", "lot_number", "prescriber_registration", "prescriber_council"},
	read:   fromReport(itemSalesReport),
	header: []string{"product_id", "sku", "brand_name", "generic_name", "manufacturer", "pack_size", "unit_of_issue", "currency",
		"units_sold", "sales", "discount", "sales_taxable", "sales_cgst", "sales_sgst", "mrp_low", "mrp_high",
		"units_returned", "credits", "credits_taxable", "credits_cgst", "credits_sgst", "net_units", "net"},
	row: func(r itemSalesRow, _ reportScope) ([]string, error) {
		money, err := amounts(r.Currency, &r.SalesMinor, &r.DiscountMinor, &r.SalesTaxable, &r.SalesCGST, &r.SalesSGST, r.MRPLow, r.MRPHigh,
			&r.CreditsMinor, &r.CreditsTaxable, &r.CreditsCGST, &r.CreditsSGST, &r.NetMinor)
		if err != nil {
			return nil, err
		}
		return []string{r.ProductID.String(), csvsafe.Text(r.SKU), csvsafe.Text(r.BrandName), csvsafe.Text(r.GenericName), csvsafe.Text(r.Manufacturer),
			strconv.Itoa(r.PackSize), csvsafe.Text(r.UnitOfIssue), r.Currency,
			units(r.UnitsSold), money[0], money[1], money[2], money[3], money[4], money[5], money[6],
			units(r.UnitsReturned), money[7], money[8], money[9], money[10], units(r.NetUnits), money[11]}, nil
	},
}

var receiptsFile = reportFile[receiptEntry]{
	name:   "receipts",
	params: []string{"kind"},
	read:   fromReport(receiptsReport),
	header: []string{"id", "day", "posted_at_utc", "direction", "document_id", "document_number", "kind", "reference", "currency", "amount"},
	row: func(e receiptEntry, _ reportScope) ([]string, error) {
		money, err := amount(e.AmountMinor, e.Currency)
		if err != nil {
			return nil, err
		}
		reference := ""
		if e.Reference != nil {
			reference = csvsafe.Text(*e.Reference)
		}
		return []string{e.ID.String(), e.Day, e.PostedAt.UTC().Format(time.RFC3339Nano), e.Direction, e.DocumentID.String(), csvsafe.Text(e.DocumentNumber),
			e.Kind, reference, e.Currency, money}, nil
	},
}

var stockSalesFile = reportFile[stockSalesRow]{
	name:   "stock-sales",
	params: []string{"category_id", "subcategory_id"},
	read:   fromReport(stockSalesReport),
	header: []string{"stock_on", "product_id", "sku", "brand_name", "generic_name", "pack_size", "unit_of_issue", "units_sold", "units_returned", "on_hand_units", "sellable_units"},
	row: func(r stockSalesRow, f reportScope) ([]string, error) {
		return []string{f.today, r.ProductID.String(), csvsafe.Text(r.SKU), csvsafe.Text(r.BrandName), csvsafe.Text(r.GenericName), strconv.Itoa(r.PackSize),
			csvsafe.Text(r.UnitOfIssue), units(r.UnitsSold), units(r.UnitsReturned), units(r.OnHand), units(r.Sellable)}, nil
	},
}

func optional(v *string) string {
	if v == nil {
		return ""
	}
	return csvsafe.Text(*v)
}

var patientSalesFile = reportFile[patientSaleLine]{
	name:   "patient-sales",
	params: []string{"patient_id"},
	header: []string{"patient_id", "sale_date", "sale_id", "invoice_number", "line_number", "admission_bed_code", "product_id", "sku", "brand_name", "generic_name",
		"pack_size", "unit_of_issue", "batch_id", "lot_number", "expires_on", "quantity_units", "currency", "mrp", "discount", "amount", "returned_units"},
	read: fromReport(patientSalesReport),
	row: func(l patientSaleLine, s reportScope) ([]string, error) {
		money, err := amounts(l.Currency, &l.MRPMinor, &l.DiscountMinor, &l.AmountMinor)
		if err != nil {
			return nil, err
		}
		return []string{s.applied("patient_id"), l.SaleDate, l.SaleID.String(), csvsafe.Text(l.InvoiceNumber), strconv.Itoa(l.LineNumber), optional(l.BedCode),
			l.ProductID.String(), csvsafe.Text(l.SKU), csvsafe.Text(l.BrandName), csvsafe.Text(l.GenericName), strconv.Itoa(l.PackSize), csvsafe.Text(l.UnitOfIssue),
			l.BatchID.String(), csvsafe.Text(l.LotNumber), l.ExpiresOn, units(l.Units), l.Currency, money[0], money[1], money[2], units(l.ReturnedUnits)}, nil
	},
}

// The worklist file needs a period: one invoice is the sale summary. Its
// rows carry what the worklist carries, so no name, phone or patient id.
var salesWorklistFile = reportFile[saleWorkRow]{
	name:   "sales-worklist",
	params: []string{"customer", "linkage"},
	header: []string{"id", "invoice_number", "sale_date", "posted_at_utc", "customer", "admission_bed_code", "linkage", "currency",
		"total", "discount", "units", "credited", "net", "admission_outstanding"},
	read: func(c *gin.Context, h *catalogueHandlers, limit int, inTx func([]saleWorkRow, reportScope) error) bool {
		_, ok := h.readSalesWorklist(c, pagination.Params{Limit: limit}, "list_export", inTx)
		return ok
	},
	row: func(r saleWorkRow, _ reportScope) ([]string, error) {
		money, err := amounts(r.Currency, &r.TotalMinor, &r.DiscountMinor, &r.CreditedMinor, &r.NetMinor, &r.AdmissionOutstanding)
		if err != nil {
			return nil, err
		}
		return []string{r.ID.String(), csvsafe.Text(r.InvoiceNumber), r.SaleDate, r.CreatedAt.UTC().Format(time.RFC3339Nano), r.Customer, optional(r.BedCode), r.Linkage,
			r.Currency, money[0], money[1], units(r.Units), money[2], money[3], money[4]}, nil
	},
}

// The daily report holds totals only: its file, like its page, is not
// audited as a read.
var dailyReportFile = reportFile[dailyRow]{
	name:   "daily",
	params: nil,
	header: []string{"day", "currency", "sales", "sales_amount", "sales_taxable", "sales_cgst", "sales_sgst", "discount", "units_sold",
		"credit_notes", "credits", "credits_taxable", "credits_cgst", "credits_sgst", "units_returned", "net",
		"cash", "upi", "upi_verified", "card", "cash_refunds", "upi_refunds", "card_refunds", "admission_account", "admission_account_refunds",
		"service_charges", "service_charges_taxable", "service_charge_credits", "service_charge_credits_taxable"},
	read: func(c *gin.Context, h *catalogueHandlers, _ int, inTx func([]dailyRow, reportScope) error) bool {
		p, outlet, ok := catalogueScope(c)
		if !ok {
			return false
		}
		from, to, ok := outletPeriod(c, "the daily report")
		if !ok {
			return false
		}
		scope := reportScope{since: from, until: to, applied: func(name string) string {
			return map[string]string{"from": from, "to": to}[name]
		}}
		err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
			var rows []dailyRow
			if err := tx.Raw(dailyTotals, map[string]any{"tenant": p.TenantID, "outlet": outlet, "since": from, "until": to}).Scan(&rows).Error; err != nil {
				return err
			}
			return inTx(rows, scope)
		})
		if err != nil {
			reportReadError(c, err)
			return false
		}
		return true
	},
	row: func(d dailyRow, _ reportScope) ([]string, error) {
		money, err := amounts(d.Currency, &d.SalesMinor, &d.SalesTaxable, &d.SalesCGST, &d.SalesSGST, &d.DiscountMinor,
			&d.CreditsMinor, &d.CreditsTaxable, &d.CreditsCGST, &d.CreditsSGST, &d.NetMinor,
			&d.CashMinor, &d.UPIMinor, &d.UPIVerifiedMinor, &d.CardMinor, &d.CashRefundMinor, &d.UPIRefundMinor, &d.CardRefundMinor,
			&d.AdmissionAccountMinor, &d.AdmissionAccountRefundMinor,
			&d.ServiceChargesMinor, &d.ServiceChargesTaxableMinor, &d.ServiceChargeCreditsMinor, &d.ServiceChargeCreditsTaxableMinor)
		if err != nil {
			return nil, err
		}
		row := append([]string{d.Day, d.Currency, units(d.Sales)}, money[0:5]...)
		row = append(row, units(d.UnitsSold), units(d.CreditNotes))
		row = append(row, money[5:9]...)
		row = append(row, units(d.UnitsReturned))
		return append(row, money[9:]...), nil
	},
}
