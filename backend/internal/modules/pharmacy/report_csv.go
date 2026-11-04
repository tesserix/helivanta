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

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/csvsafe"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// CSV files of sales by item, receipts and refunds, and sales and stock
// (#1060). Each is its report's own query in one transaction, all or an
// error: a file cut off at a bound would read as the whole period. These
// reports hold no patient, prescriber or buyer, so a download is recorded in
// the audit stream, as the stock export is, and writes no audit_events row.

// reportExportRows bounds a file; a var only so a test can reach the bound.
var reportExportRows = 5000

const reportExportBytes = 8 * 1024 * 1024

// reportFile is a report's CSV: its name, the parameters it takes besides
// from and to, its header and its row.
type reportFile[T any] struct {
	name   string
	params []string
	header []string
	row    func(T, salesReportFilter) ([]string, error)
}

// exportReport refuses any parameter the report does not take, among them
// cursor, limit and the prescriber filter: sales by a prescriber are
// personal, and their file needs a disclosure audit this one does not write.
func exportReport[T any](h *catalogueHandlers, r salesReport[T], f reportFile[T]) gin.HandlerFunc {
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
		rows, filter, ok := r.read(c, h, pagination.Params{Limit: reportExportRows})
		if !ok {
			return
		}
		if len(rows) > reportExportRows {
			respond.Error(c, http.StatusRequestEntityTooLarge, "report_export_too_large", fmt.Sprintf("the report has more than %d rows; choose a shorter period or a narrower filter", reportExportRows))
			return
		}
		data, err := encodeReport(rows, filter, f)
		switch {
		case errors.Is(err, csvsafe.ErrTooLarge):
			respond.Error(c, http.StatusRequestEntityTooLarge, "report_export_too_large", "the report exceeds 8 MiB; choose a shorter period or a narrower filter")
			return
		case err != nil:
			respond.InternalErr(c, err, "could not write the report CSV")
			return
		}

		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		logged := []any{"report", f.name, "tenant_id", p.TenantID, "subject", p.Subject, "pharmacy_id", currentPharmacy(ctx), "outlet_id", outlet}
		for _, name := range append([]string{"from", "to"}, f.params...) {
			v := query.Get(name)
			if v == "" {
				v = "all"
			}
			logged = append(logged, name, v)
		}
		logged = append(logged, "row_count", len(rows), "sha256", digest)
		audit.Log(ctx, requestid.Logger(c), slog.LevelInfo, "pharmacy.report.exported", "report CSV generated", logged...)
		c.Header("Cache-Control", "private, no-store")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-%s-%s-%s-%s.csv"`, f.name, outlet, filter.since, filter.until))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Export-Row-Count", strconv.Itoa(len(rows)))
		c.Header("X-Export-Sha256", digest)
		c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
	}
}

func encodeReport[T any](rows []T, filter salesReportFilter, f reportFile[T]) ([]byte, error) {
	w := csvsafe.NewExcelWriter(reportExportBytes)
	if err := w.Write(append([]string{"period_from", "period_to"}, f.header...)); err != nil {
		return nil, err
	}
	for _, row := range rows {
		cells, err := f.row(row, filter)
		if err != nil {
			return nil, err
		}
		if err := w.Write(append([]string{filter.since, filter.until}, cells...)); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}

// majorUnits writes minor units exactly in the major unit: -1250 with two
// digits is -12.50. The magnitude is unsigned so the most negative amount
// cannot overflow.
func majorUnits(minor int64, digits int) string {
	sign, magnitude := "", uint64(minor)
	if minor < 0 {
		sign, magnitude = "-", -magnitude
	}
	s := strconv.FormatUint(magnitude, 10)
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
	params: []string{"product_id", "category_id", "subcategory_id", "lot_number"},
	header: []string{"product_id", "sku", "brand_name", "generic_name", "manufacturer", "pack_size", "unit_of_issue", "currency",
		"units_sold", "sales", "discount", "sales_taxable", "sales_cgst", "sales_sgst", "mrp_low", "mrp_high",
		"units_returned", "credits", "credits_taxable", "credits_cgst", "credits_sgst", "net_units", "net"},
	row: func(r itemSalesRow, _ salesReportFilter) ([]string, error) {
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
	header: []string{"id", "day", "posted_at_utc", "direction", "document_id", "document_number", "kind", "reference", "currency", "amount"},
	row: func(e receiptEntry, _ salesReportFilter) ([]string, error) {
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
	header: []string{"stock_on", "product_id", "sku", "brand_name", "generic_name", "pack_size", "unit_of_issue", "units_sold", "units_returned", "on_hand_units", "sellable_units"},
	row: func(r stockSalesRow, f salesReportFilter) ([]string, error) {
		return []string{f.today, r.ProductID.String(), csvsafe.Text(r.SKU), csvsafe.Text(r.BrandName), csvsafe.Text(r.GenericName), strconv.Itoa(r.PackSize),
			csvsafe.Text(r.UnitOfIssue), units(r.UnitsSold), units(r.UnitsReturned), units(r.OnHand), units(r.Sellable)}, nil
	},
}
