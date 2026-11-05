package pharmacy_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
)

// #988 slice 2: sales by item and sales and stock, by item category.

// categoryReportWorld sells P uncategorised on 1 November, as Antibiotics ›
// Oral on 2 November and as Analgesics on 4 November; Q is Analgesics from
// 2 November. On 5 November two units of the 2 November sale come back.
type categoryReportWorld struct {
	saleWorld
	p, q                    string
	antibiotics, oral, anal string
}

func onDay(t *testing.T, day int) {
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, day, 6, 30, 0, 0, time.UTC) })
}

func (w categoryReportWorld) categorise(t *testing.T, product, category, sub, from string) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", productCategoryPath(product, "/"+id), "editor", declareCategory(category, sub, from))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	res = do(w.r, "POST", productCategoryPath(product, "/"+id+"/review"), "pharmacist", `{"decision":"approved","reason":"checked the pack"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
}

func newCategoryReportWorld(t *testing.T) categoryReportWorld {
	t.Helper()
	w := categoryReportWorld{saleWorld: newSaleWorld(t, true)}
	w.p = w.product(t, "TEST-PCM-500", "none")
	w.q = w.product(t, "TEST-ORS-200", "none")
	w.shelve(t, w.p, "2028-06-30", 12500, 20)
	w.shelve(t, w.q, "2028-06-30", 12500, 10)
	w.antibiotics, w.oral, w.anal = uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	for _, c := range [][2]string{{w.antibiotics, category("ANTI", "Antibiotics")}, {w.anal, category("ANAL", "Analgesics")}, {w.oral, subcategory(w.antibiotics, "ORAL", "Oral")}} {
		res := do(w.r, "PUT", categoryPath("/"+c[0]), "editor", c[1])
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	}
	sell := func(product string, units int) string {
		id := uuid.Must(uuid.NewV7()).String()
		res := do(w.r, "PUT", salePath("/"+id), "pharmacist", cashSale(product, units, units*1250, units*1250))
		require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
		return id
	}
	sell(w.p, 1)
	w.categorise(t, w.p, w.antibiotics, w.oral, "2026-11-02")
	w.categorise(t, w.q, w.anal, "", "2026-11-02")
	onDay(t, 2)
	asOral := sell(w.p, 3)
	sell(w.q, 2)
	w.categorise(t, w.p, w.anal, "", "2026-11-04")
	onDay(t, 4)
	sell(w.p, 4)
	onDay(t, 5)
	code, _, out := w.returnUnits(t, returnBody(asOral, 2, w.quarantine, 2500))
	require.Equal(t, http.StatusCreated, code, out)
	return w
}

func (w categoryReportWorld) items(t *testing.T, filter string) []itemRow {
	t.Helper()
	code, body, out := reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-05"+filter)
	require.Equal(t, http.StatusOK, code, out)
	return body.Data
}

type soldReturned struct {
	product        string
	sold, returned int64
}

func itemTotals(rows []itemRow) []soldReturned {
	out := []soldReturned{}
	for _, r := range rows {
		out = append(out, soldReturned{r.ProductID, r.UnitsSold, r.UnitsReturned})
	}
	return out
}

func TestSalesByItemFiltersByTheCategoryOnTheSaleDate(t *testing.T) {
	w := newCategoryReportWorld(t)
	require.Equal(t, []soldReturned{{w.p, 8, 2}, {w.q, 2, 0}}, itemTotals(w.items(t, "")), "no filter is every sale")

	// The 2 November sale was Antibiotics, and so is its return on 5
	// November, after the product had moved to Analgesics.
	anti := w.items(t, "&category_id="+w.antibiotics)
	require.Equal(t, []soldReturned{{w.p, 3, 2}}, itemTotals(anti))
	require.EqualValues(t, 3*1250, anti[0].SalesMinor)
	require.EqualValues(t, 1, anti[0].NetUnits)
	require.EqualValues(t, 1250, anti[0].NetMinor)
	require.Equal(t, itemTotals(anti), itemTotals(w.items(t, fmt.Sprintf("&category_id=%s&subcategory_id=%s", w.antibiotics, w.oral))), "the subcategory narrows to the same sale")

	require.Equal(t, []soldReturned{{w.p, 4, 0}, {w.q, 2, 0}}, itemTotals(w.items(t, "&category_id="+w.anal)), "each sale where it was made")
	require.Empty(t, w.items(t, fmt.Sprintf("&category_id=%s&subcategory_id=%s", w.anal, w.oral)), "another category's subcategory matches nothing")
	require.Empty(t, w.items(t, "&category_id="+uuid.NewString()), "not a category here")

	for _, bad := range []string{"&subcategory_id=" + w.oral, "&category_id=x", "&category_id=" + w.antibiotics + "&subcategory_id=x"} {
		res := do(w.r, "GET", licencePath("/reports/items?from=2026-11-01&to=2026-11-05"+bad), "pharmacist", "")
		require.Equal(t, http.StatusBadRequest, res.Code, bad)
	}
}

func TestTheSalesStockReportFiltersByTodaysCategory(t *testing.T) {
	w := newCategoryReportWorld(t)
	stock := func(filter string) []stockSalesRow {
		code, body, out := reportPage[stockSalesRow](t, w.saleWorld, "stocker", "/reports/stock-sales?from=2026-11-01&to=2026-11-05"+filter)
		require.Equal(t, http.StatusOK, code, out)
		return body.Data
	}
	all := stock("")
	require.Len(t, all, 2)
	// P is Analgesics today: listed there, with only its Analgesics sales,
	// beside all of its stock.
	anal := stock("&category_id=" + w.anal)
	require.Len(t, anal, 2)
	require.Equal(t, stockSalesRow{ProductID: w.p, SKU: "TEST-PCM-500", UnitsSold: 4, OnHand: all[0].OnHand, Sellable: all[0].Sellable}, anal[0])
	require.Equal(t, all[1], anal[1], "Q was only ever Analgesics")
	require.EqualValues(t, 20-8, all[0].Sellable, "the 2 returned units went to quarantine")
	require.Empty(t, stock("&category_id="+w.antibiotics), "nothing is Antibiotics today")
	res := do(w.r, "GET", licencePath("/reports/stock-sales?from=2026-11-01&to=2026-11-05&subcategory_id="+w.oral), "stocker", "")
	require.Equal(t, http.StatusBadRequest, res.Code)
}
