package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

// #1060: sales by item, and the receipts and refunds register.

type itemRow struct {
	ProductID      string `json:"product_id"`
	SKU            string `json:"sku"`
	PackSize       int    `json:"pack_size"`
	UnitOfIssue    string `json:"unit_of_issue"`
	Currency       string `json:"currency"`
	UnitsSold      int64  `json:"units_sold"`
	SalesMinor     int64  `json:"sales_minor"`
	DiscountMinor  int64  `json:"discount_minor"`
	SalesTaxable   int64  `json:"sales_taxable_minor"`
	SalesCGST      int64  `json:"sales_cgst_minor"`
	SalesSGST      int64  `json:"sales_sgst_minor"`
	MRPLow         *int64 `json:"mrp_low_minor"`
	MRPHigh        *int64 `json:"mrp_high_minor"`
	UnitsReturned  int64  `json:"units_returned"`
	CreditsMinor   int64  `json:"credits_minor"`
	CreditsTaxable int64  `json:"credits_taxable_minor"`
	CreditsCGST    int64  `json:"credits_cgst_minor"`
	CreditsSGST    int64  `json:"credits_sgst_minor"`
	NetUnits       int64  `json:"net_units"`
	NetMinor       int64  `json:"net_minor"`
}

type receiptRow struct {
	ID          string  `json:"id"`
	Day         string  `json:"day"`
	Direction   string  `json:"direction"`
	Document    string  `json:"document_number"`
	DocumentID  string  `json:"document_id"`
	Kind        string  `json:"kind"`
	Reference   *string `json:"reference"`
	AmountMinor int64   `json:"amount_minor"`
	Currency    string  `json:"currency"`
}

type page[T any] struct {
	Data []T `json:"data"`
	Page struct {
		NextCursor *string `json:"next_cursor"`
	} `json:"page"`
}

func reportPage[T any](t *testing.T, w saleWorld, who, path string) (int, page[T], string) {
	t.Helper()
	res := do(w.r, "GET", licencePath(path), who, "")
	var body page[T]
	if res.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	}
	return res.Code, body, res.Body.String()
}

// itemWorld sells two products on 1 November, one of them from two batches
// at different MRPs, and takes back two units on 2 November.
type itemWorld struct {
	saleWorld
	p, q             string
	first, second    saleResp
	third            saleResp
	note             creditResp
	firstID, thirdID string
}

func newItemWorld(t *testing.T) itemWorld {
	t.Helper()
	w := itemWorld{saleWorld: newSaleWorld(t, true)}
	w.p = w.product(t, "TEST-PCM-500", "none")
	w.q = w.product(t, "TEST-ORS-200", "none")
	w.shelve(t, w.p, "2028-06-30", 12500, 5)
	w.shelve(t, w.p, "2028-12-31", 13000, 10)
	w.shelve(t, w.q, "2028-06-30", 12500, 10)
	var code int
	var out string
	w.firstID = uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+w.firstID), "pharmacist", cashSale(w.p, 5, 6250, 7000))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &w.first))
	code, w.second, out = w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"upi","amount_minor":2600,"reference":"TEST-UPI-7"}]}`, w.p))
	require.Equal(t, http.StatusCreated, code, out)
	w.thirdID = uuid.Must(uuid.NewV7()).String()
	res = do(w.r, "PUT", salePath("/"+w.thirdID), "pharmacist",
		fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":3}],"tenders":[{"kind":"cash","amount_minor":1000,"tendered_minor":1000},{"kind":"card","amount_minor":2750,"reference":"TEST-CARD-1"}]}`, w.q))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &w.third))
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC) })
	code, w.note, out = w.returnUnits(t, returnBody(w.firstID, 2, w.quarantine, 2500))
	require.Equal(t, http.StatusCreated, code, out)
	return w
}

func TestSalesByItemSumsEachProduct(t *testing.T) {
	w := newItemWorld(t)
	code, body, out := reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-02")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 2, out)
	p, q := body.Data[0], body.Data[1]
	require.Equal(t, w.p, p.ProductID, "oldest product first")
	require.Equal(t, "TEST-PCM-500", p.SKU)
	require.Equal(t, "INR", p.Currency)
	require.Equal(t, 10, p.PackSize)
	require.EqualValues(t, 7, p.UnitsSold)
	require.Equal(t, w.first.TotalMinor+w.second.TotalMinor, p.SalesMinor)
	require.Equal(t, w.first.TaxableMinor+w.second.TaxableMinor, p.SalesTaxable)
	require.Equal(t, w.first.CGSTMinor+w.second.CGSTMinor, p.SalesCGST)
	require.Equal(t, w.first.SGSTMinor+w.second.SGSTMinor, p.SalesSGST)
	require.Equal(t, []int64{12500, 13000}, []int64{*p.MRPLow, *p.MRPHigh}, "both batches' MRPs per pack")
	require.EqualValues(t, 2, p.UnitsReturned)
	require.Equal(t, w.note.TotalMinor, p.CreditsMinor)
	require.Equal(t, w.note.TaxableMinor, p.CreditsTaxable)
	require.Equal(t, w.note.CGSTMinor, p.CreditsCGST)
	require.Equal(t, w.note.CGSTMinor, p.CreditsSGST)
	require.EqualValues(t, 5, p.NetUnits)
	require.Equal(t, p.SalesMinor-p.CreditsMinor, p.NetMinor)
	require.Equal(t, w.q, q.ProductID)
	require.EqualValues(t, 3, q.UnitsSold)
	require.Equal(t, w.third.TotalMinor, q.SalesMinor)
	require.Zero(t, q.UnitsReturned)

	// The items add up to the daily report's sales and credits.
	_, days, out := dailyReport(t, w.saleWorld, "pharmacist", "?from=2026-11-01&to=2026-11-02")
	var sales, credits int64
	for _, d := range days {
		sales += d.SalesMinor
		credits += d.CreditsMinor
	}
	require.Equal(t, sales, p.SalesMinor+q.SalesMinor, out)
	require.Equal(t, credits, p.CreditsMinor+q.CreditsMinor, out)

	// A return in the period of a sale before it lists the product, sold nothing.
	code, body, out = reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-02&to=2026-11-02")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1, out)
	require.Zero(t, body.Data[0].UnitsSold)
	require.Nil(t, body.Data[0].MRPLow, "no sale, no MRP charged")
	require.EqualValues(t, 2, body.Data[0].UnitsReturned)
	require.Equal(t, -w.note.TotalMinor, body.Data[0].NetMinor)

	// The return counts on its own day, not its sale's.
	code, body, out = reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-01")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 2, out)
	require.Zero(t, body.Data[0].UnitsReturned)
	require.Zero(t, body.Data[0].CreditsMinor)

	code, body, out = reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-02&product_id="+w.q)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1, out)
	require.Equal(t, w.q, body.Data[0].ProductID)

	code, body, out = reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-02&limit=1")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1)
	require.NotNil(t, body.Page.NextCursor)
	code, body, out = reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-02&limit=1&cursor="+url.QueryEscape(*body.Page.NextCursor))
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1)
	require.Equal(t, w.q, body.Data[0].ProductID, "the second page follows the first")
}

func TestSalesByItemCountsDiscounts(t *testing.T) {
	w := newDiscountWorld(t)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	w.ceiling(t, 1000)
	code, out := w.sellAs(t, "manager", discountSale(p, 10, 1000, 11250, "TEST senior citizen"))
	require.Equal(t, http.StatusCreated, code, out)
	code, body, out := reportPage[itemRow](t, w, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-01")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1, out)
	require.EqualValues(t, 1250, body.Data[0].DiscountMinor)
	require.EqualValues(t, 11250, body.Data[0].SalesMinor, "charged, after the discount")
}

func TestSalesByItemLeavesOutServiceCharges(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	code, _, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 7500, delivery))
	require.Equal(t, http.StatusCreated, code, out)
	code, body, out := reportPage[itemRow](t, w, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-01")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1, out)
	require.EqualValues(t, 2500, body.Data[0].SalesMinor, "the delivery charge is not the medicine's")
}

func TestTheReceiptsRegisterListsEachPayment(t *testing.T) {
	w := newItemWorld(t)
	code, body, out := reportPage[receiptRow](t, w.saleWorld, "pharmacist", "/reports/receipts?from=2026-11-01&to=2026-11-02")
	require.Equal(t, http.StatusOK, code, out)
	type entry struct {
		day, direction, document, kind string
		amount                         int64
	}
	got := []entry{}
	for _, r := range body.Data {
		require.Equal(t, "INR", r.Currency)
		got = append(got, entry{r.Day, r.Direction, r.Document, r.Kind, r.AmountMinor})
	}
	require.ElementsMatch(t, []entry{
		{"2026-11-01", "receipt", w.first.InvoiceNumber, "cash", 6250},
		{"2026-11-01", "receipt", w.second.InvoiceNumber, "upi", 2600},
		{"2026-11-01", "receipt", w.third.InvoiceNumber, "cash", 1000},
		{"2026-11-01", "receipt", w.third.InvoiceNumber, "card", 2750},
		{"2026-11-02", "refund", w.note.CreditNoteNumber, "cash", 2500},
	}, got, "cash is what was applied, not what was handed over")
	require.Equal(t, "refund", body.Data[len(body.Data)-1].Direction, "oldest first")
	require.Equal(t, w.firstID, body.Data[0].DocumentID)
	for _, r := range body.Data {
		if r.Kind == "upi" {
			require.Equal(t, "TEST-UPI-7", *r.Reference)
		}
		if r.Kind == "cash" {
			require.Nil(t, r.Reference)
		}
	}

	// Per day and kind it agrees with the daily report.
	_, days, _ := dailyReport(t, w.saleWorld, "pharmacist", "?from=2026-11-01&to=2026-11-02")
	sum := map[string]int64{}
	for _, r := range body.Data {
		sum[r.Day+r.Direction+r.Kind] += r.AmountMinor
	}
	for _, d := range days {
		require.Equal(t, d.CashMinor, sum[d.Day+"receiptcash"], d.Day)
		require.Equal(t, d.UPIMinor, sum[d.Day+"receiptupi"], d.Day)
		require.Equal(t, d.CardMinor, sum[d.Day+"receiptcard"], d.Day)
		require.Equal(t, d.CashRefundMinor, sum[d.Day+"refundcash"], d.Day)
	}

	code, body, out = reportPage[receiptRow](t, w.saleWorld, "pharmacist", "/reports/receipts?from=2026-11-01&to=2026-11-02&kind=cash")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 3, out)
	code, body, out = reportPage[receiptRow](t, w.saleWorld, "pharmacist", "/reports/receipts?from=2026-11-01&to=2026-11-02&kind=upi")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 1, "the kind narrows refunds as well as receipts")

	// Walking single-row pages yields every row once, even where two
	// tenders share their sale's time.
	seen := map[string]bool{}
	path := "/reports/receipts?from=2026-11-01&to=2026-11-02&limit=1"
	for i := 0; i < 10; i++ {
		code, body, out = reportPage[receiptRow](t, w.saleWorld, "pharmacist", path)
		require.Equal(t, http.StatusOK, code, out)
		for _, r := range body.Data {
			require.False(t, seen[r.ID], "row repeated across pages")
			seen[r.ID] = true
		}
		if body.Page.NextCursor == nil {
			break
		}
		path = "/reports/receipts?from=2026-11-01&to=2026-11-02&limit=1&cursor=" + url.QueryEscape(*body.Page.NextCursor)
	}
	require.Len(t, seen, 5)
}

func TestTheSalesReportsAreBoundedAndScoped(t *testing.T) {
	w := newSaleWorld(t, true)
	for _, path := range []string{"/reports/items", "/reports/receipts"} {
		code, body, out := reportPage[itemRow](t, w, "clerk", path+"?from=2026-11-01&to=2026-11-01")
		require.Equal(t, http.StatusOK, code, out)
		require.Empty(t, body.Data)
		require.Contains(t, out, `"data":[]`)
		require.Equal(t, http.StatusNotFound, do(w.r, "GET", licencePath(path+"?from=2026-11-01&to=2026-11-01"), "foreign", "").Code, path)
		require.Equal(t, http.StatusForbidden, do(w.r, "GET", licencePath(path+"?from=2026-11-01&to=2026-11-01"), "stocker", "").Code, path)
		for _, bad := range []string{"", "?from=2026-11-02&to=2026-11-01", "?from=2025-10-31&to=2026-11-01"} {
			require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath(path+bad), "owner", "").Code, path+bad)
		}
	}
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/reports/items?from=2026-11-01&to=2026-11-01&product_id=x"), "owner", "").Code)
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", licencePath("/reports/receipts?from=2026-11-01&to=2026-11-01&kind=cheque"), "owner", "").Code)
}
