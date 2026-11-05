package pharmacy_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1060: CSV downloads of sales by item, receipts and refunds, and sales and stock.

// rupees writes minor units as a float would, to check the exact writer independently.
func rupees(minor int64) string { return strconv.FormatFloat(float64(minor)/100, 'f', 2, 64) }

// csvRecords requires the byte-order mark and returns each row by its header.
func csvRecords(t *testing.T, res *httptest.ResponseRecorder) []map[string]string {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	body := res.Body.Bytes()
	require.True(t, bytes.HasPrefix(body, []byte("\xef\xbb\xbf")), "a byte-order mark, so Excel reads UTF-8")
	require.Contains(t, string(body), "\r\n")
	rows, err := csv.NewReader(bytes.NewReader(body[3:])).ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, rows, "a header at least")
	out := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		m := map[string]string{}
		for i, name := range rows[0] {
			m[name] = row[i]
		}
		out = append(out, m)
	}
	return out
}

func captureAudit(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &logs
}

func exportEvents(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var e map[string]any
		if len(line) == 0 || json.Unmarshal(line, &e) != nil || e["audit_event"] != "pharmacy.report.exported" {
			continue
		}
		events = append(events, e)
	}
	return events
}

func TestSalesByItemCSV(t *testing.T) {
	w := newItemWorld(t)
	_, page, out := reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-02")
	require.Len(t, page.Data, 2, out)
	logs := captureAudit(t)

	res := do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-01&to=2026-11-02"), "pharmacist", "")
	rows := csvRecords(t, res)
	require.Len(t, rows, 2)
	for i, j := range page.Data {
		r := rows[i]
		require.Equal(t, "2026-11-01", r["period_from"])
		require.Equal(t, "2026-11-02", r["period_to"])
		require.Equal(t, j.ProductID, r["product_id"], "the same order as the pages")
		require.Equal(t, j.SKU, r["sku"])
		require.Equal(t, strconv.Itoa(j.PackSize), r["pack_size"])
		require.Equal(t, "INR", r["currency"])
		require.Equal(t, strconv.FormatInt(j.UnitsSold, 10), r["units_sold"])
		require.Equal(t, rupees(j.SalesMinor), r["sales"])
		require.Equal(t, rupees(j.DiscountMinor), r["discount"])
		require.Equal(t, rupees(j.SalesTaxable), r["sales_taxable"])
		require.Equal(t, rupees(j.SalesCGST), r["sales_cgst"])
		require.Equal(t, rupees(j.SalesSGST), r["sales_sgst"])
		require.Equal(t, strconv.FormatInt(j.UnitsReturned, 10), r["units_returned"])
		require.Equal(t, rupees(j.CreditsMinor), r["credits"])
		require.Equal(t, rupees(j.CreditsTaxable), r["credits_taxable"])
		require.Equal(t, rupees(j.CreditsCGST), r["credits_cgst"])
		require.Equal(t, rupees(j.CreditsSGST), r["credits_sgst"])
		require.Equal(t, strconv.FormatInt(j.NetUnits, 10), r["net_units"])
		require.Equal(t, rupees(j.NetMinor), r["net"])
	}
	require.Equal(t, "125.00", rows[0]["mrp_low"])
	require.Equal(t, "130.00", rows[0]["mrp_high"])

	require.Equal(t, "text/csv; charset=utf-8", res.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", res.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", res.Header().Get("X-Content-Type-Options"))
	require.Equal(t, fmt.Sprintf(`attachment; filename="pharmacy-items-%s-2026-11-01-2026-11-02.csv"`, pharmaOutlet), res.Header().Get("Content-Disposition"))
	require.Equal(t, "2", res.Header().Get("X-Export-Row-Count"))
	digest := fmt.Sprintf("%x", sha256.Sum256(res.Body.Bytes()))
	require.Equal(t, digest, res.Header().Get("X-Export-Sha256"))

	events := exportEvents(t, logs)
	require.Len(t, events, 1)
	e := events[0]
	require.Equal(t, "items", e["report"])
	require.Equal(t, testutil.TenantA, e["tenant_id"])
	require.Equal(t, "user-pharmacist", e["subject"])
	require.Equal(t, pharmaOutlet, e["outlet_id"])
	require.Equal(t, "2026-11-01", e["from"])
	require.Equal(t, "all", e["product_id"])
	require.EqualValues(t, 2, e["row_count"])
	require.Equal(t, digest, e["sha256"])
	require.Equal(t, res.Header().Get("X-Request-ID"), e["request_id"])

	// The day of the return alone: a negative net, written exactly.
	rows = csvRecords(t, do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-02&to=2026-11-02"), "pharmacist", ""))
	require.Len(t, rows, 1)
	require.Equal(t, "-"+rupees(w.note.TotalMinor), rows[0]["net"])
	require.Equal(t, "0.00", rows[0]["sales"])
	require.Empty(t, rows[0]["mrp_low"], "no sale, no MRP charged")

	// The filters apply, and an empty period is a header alone.
	rows = csvRecords(t, do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-01&to=2026-11-02&product_id="+w.q), "pharmacist", ""))
	require.Len(t, rows, 1)
	require.Equal(t, w.q, rows[0]["product_id"])
	rows = csvRecords(t, do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-01&to=2026-11-02&lot_number=%20TEST-NO-SUCH-LOT%20"), "pharmacist", ""))
	require.Empty(t, rows)
	events = exportEvents(t, logs)
	require.Equal(t, "TEST-NO-SUCH-LOT", events[len(events)-1]["lot_number"], "the lot the file was made with, trimmed")
	empty := do(w.r, "GET", licencePath("/reports/items.csv?from=2026-11-05&to=2026-11-05"), "pharmacist", "")
	require.Empty(t, csvRecords(t, empty))
	require.True(t, strings.HasPrefix(empty.Body.String(), "\ufeffperiod_from,period_to,product_id,"), empty.Body.String())
}

func TestReceiptsCSV(t *testing.T) {
	w := newItemWorld(t)
	_, page, out := reportPage[receiptRow](t, w.saleWorld, "pharmacist", "/reports/receipts?from=2026-11-01&to=2026-11-02")
	require.Len(t, page.Data, 5, out)
	rows := csvRecords(t, do(w.r, "GET", licencePath("/reports/receipts.csv?from=2026-11-01&to=2026-11-02"), "clerk", ""))
	require.Len(t, rows, len(page.Data))
	for i, e := range page.Data {
		r := rows[i]
		require.Equal(t, e.ID, r["id"])
		require.Equal(t, e.Day, r["day"])
		require.Equal(t, e.Direction, r["direction"])
		require.Equal(t, e.DocumentID, r["document_id"])
		require.Equal(t, e.Document, r["document_number"])
		require.Equal(t, e.Kind, r["kind"])
		if e.Reference != nil {
			require.Equal(t, *e.Reference, r["reference"])
		} else {
			require.Empty(t, r["reference"])
		}
		require.Equal(t, rupees(e.AmountMinor), r["amount"])
		require.Equal(t, "INR", r["currency"])
		_, err := time.Parse(time.RFC3339Nano, r["posted_at_utc"])
		require.NoError(t, err)
	}
	rows = csvRecords(t, do(w.r, "GET", licencePath("/reports/receipts.csv?from=2026-11-01&to=2026-11-02&kind=card"), "clerk", ""))
	require.Len(t, rows, 1)
	require.Equal(t, "TEST-CARD-1", rows[0]["reference"])
}

func TestStockSalesCSV(t *testing.T) {
	w := newItemWorld(t)
	_, page, out := reportPage[stockSalesRow](t, w.saleWorld, "stocker", "/reports/stock-sales?from=2026-11-01&to=2026-11-02")
	require.Len(t, page.Data, 2, out)
	rows := csvRecords(t, do(w.r, "GET", licencePath("/reports/stock-sales.csv?from=2026-11-01&to=2026-11-02"), "stocker", ""))
	require.Len(t, rows, 2)
	for i, s := range page.Data {
		r := rows[i]
		require.Equal(t, "2026-11-02", r["stock_on"], "the outlet's today")
		require.Equal(t, s.ProductID, r["product_id"])
		require.Equal(t, s.SKU, r["sku"])
		require.Equal(t, strconv.FormatInt(s.UnitsSold, 10), r["units_sold"])
		require.Equal(t, strconv.FormatInt(s.UnitsReturned, 10), r["units_returned"])
		require.Equal(t, strconv.FormatInt(s.OnHand, 10), r["on_hand_units"])
		require.Equal(t, strconv.FormatInt(s.Sellable, 10), r["sellable_units"])
	}

	// A name a spreadsheet would run is written as text.
	id := uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(productBody(0, `=HYPERLINK("https://example.test")`), `"TEST-AMOX-500"`, `"TEST-FORMULA"`, 1)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", productPath(id), "editor", body).Code)
	receive(t, w.r, id, w.quarantine, "2028-06-30", `,"mrp_minor":12500`)
	rows = csvRecords(t, do(w.r, "GET", licencePath("/reports/stock-sales.csv?from=2026-11-01&to=2026-11-02"), "stocker", ""))
	require.Len(t, rows, 3)
	require.Equal(t, `'=HYPERLINK("https://example.test")`, rows[2]["brand_name"])
}

func TestReportCSVRefuses(t *testing.T) {
	w := newItemWorld(t)
	logs := captureAudit(t)
	period := "from=2026-11-01&to=2026-11-02"
	refused := func(t *testing.T, res *httptest.ResponseRecorder, code int, what string) {
		t.Helper()
		require.Equal(t, code, res.Code, what+": "+res.Body.String())
		require.Empty(t, res.Header().Get("Content-Disposition"), what)
	}
	for _, bad := range []string{"", "from=2026-11-01", period + "&from=2026-11-01", period + "&product_id=", period + "&kind=cash", period + "&cursor=x", period + "&limit=10",
		period + "&prescriber_registration=MMC%2F1", period + "&unused=1", period + "&broken=%ZZ", period + "&subcategory_id=" + uuid.Must(uuid.NewV7()).String()} {
		refused(t, do(w.r, "GET", licencePath("/reports/items.csv?"+bad), "pharmacist", ""), http.StatusBadRequest, "items "+bad)
	}
	for _, bad := range []string{period + "&product_id=" + w.p, period + "&kind=cheque", period + "&lot_number=x"} {
		refused(t, do(w.r, "GET", licencePath("/reports/receipts.csv?"+bad), "pharmacist", ""), http.StatusBadRequest, "receipts "+bad)
	}
	for _, bad := range []string{period + "&product_id=" + w.p, period + "&lot_number=x", period + "&kind=cash"} {
		refused(t, do(w.r, "GET", licencePath("/reports/stock-sales.csv?"+bad), "stocker", ""), http.StatusBadRequest, "stock-sales "+bad)
	}
	for path, who := range map[string]string{"items": "stocker", "receipts": "stocker", "stock-sales": "owner"} {
		refused(t, do(w.r, "GET", licencePath("/reports/"+path+".csv?"+period), who, ""), http.StatusForbidden, path+" "+who)
		refused(t, do(w.r, "GET", licencePath("/reports/"+path+".csv?"+period), "foreign", ""), http.StatusNotFound, path+" foreign")
	}

	// More rows than the bound is refused whole, never cut off.
	pharmacy.SetReportExportRows(t, 1)
	res := do(w.r, "GET", licencePath("/reports/items.csv?"+period), "pharmacist", "")
	refused(t, res, http.StatusRequestEntityTooLarge, "over the bound")
	require.Contains(t, res.Body.String(), "report_export_too_large")
	require.Equal(t, http.StatusOK, do(w.r, "GET", licencePath("/reports/items.csv?"+period+"&product_id="+w.q), "pharmacist", "").Code, "at the bound")
	require.Len(t, exportEvents(t, logs), 1, "only the file sent is recorded")
}

func TestMajorUnits(t *testing.T) {
	for _, tc := range []struct {
		minor  int64
		digits int
		want   string
	}{{0, 2, "0.00"}, {5, 2, "0.05"}, {-5, 2, "-0.05"}, {123450, 2, "1234.50"}, {-1250, 2, "-12.50"}, {-100, 2, "-1.00"}, {42, 0, "42"}, {-42, 0, "-42"}, {1234, 3, "1.234"}, {-9223372036854775808, 2, "-92233720368547758.08"}} {
		require.Equal(t, tc.want, pharmacy.MajorUnits(tc.minor, tc.digits), tc)
	}
	got, err := pharmacy.Amount(-1250, "INR")
	require.NoError(t, err)
	require.Equal(t, "-12.50", got)
	_, err = pharmacy.Amount(1250, "USD")
	require.Error(t, err, "an undeclared currency is never guessed at two digits")
}
