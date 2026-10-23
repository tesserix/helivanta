package pharmacy_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// countedTotal pages a counted list at 2 rows a page to the end. Every page
// reports the same total, and the total is the number of rows reached.
func countedTotal(t *testing.T, r *gin.Engine, path string) int {
	t.Helper()
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	n, cursor, total := 0, "", -1
	for range 50 {
		url := path + sep + "limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		w := do(r, "GET", url, "editor", "")
		require.Equal(t, http.StatusOK, w.Code, path+": "+w.Body.String())
		var page struct {
			Data []json.RawMessage
			Page struct {
				NextCursor *string `json:"next_cursor"`
				Total      *int    `json:"total"`
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.NotNil(t, page.Page.Total, path)
		if total >= 0 {
			require.Equal(t, total, *page.Page.Total, path)
		}
		total = *page.Page.Total
		n += len(page.Data)
		if page.Page.NextCursor == nil {
			require.Equal(t, total, n, path)
			return n
		}
		cursor = *page.Page.NextCursor
	}
	t.Fatalf("%s did not end", path)
	return 0
}

func seedBill(t *testing.T, r *gin.Engine, ref, due string) {
	t.Helper()
	body := strings.Replace(expenseBody(ref), `"due_on":"2026-10-31"`, `"due_on":"`+due+`"`, 1)
	w := do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
}

// #576 slice 3 D2: each tab's total is the rows its pages reach, and equals
// the overview's count for the same filter (outlet date pinned 2026-11-01).
func TestCountedPayablesTotalsMatchPaging(t *testing.T) {
	r, _ := overviewHarness(t)
	for i, due := range []string{"2026-10-20", "2026-10-25", "2026-10-31", "2026-11-01", "2026-11-08", "2026-12-01"} {
		seedBill(t, r, "TEST-BILL-"+string(rune('A'+i)), due)
	}
	require.Equal(t, 6, countedTotal(t, r, outletPath("/accounts/payables/counted")))
	overdue := countedTotal(t, r, outletPath("/accounts/payables/counted?due=overdue"))
	week := countedTotal(t, r, outletPath("/accounts/payables/counted?due=week"))
	require.Equal(t, 3, overdue)
	require.Equal(t, 2, week)
	_, got, raw := getOverview(t, r, "editor")
	require.NotNil(t, got.Bills, raw)
	require.Equal(t, got.Bills.Overdue, overdue)
	require.Equal(t, got.Bills.Week, week)
}

func TestCountedInventoryAuditTotal(t *testing.T) {
	r, _ := overviewHarness(t)
	stockFixture(t, r)
	require.Equal(t, listCount(t, r, outletPath("/accounts/inventory-audit?x=1")), countedTotal(t, r, outletPath("/accounts/inventory-audit/counted")))
}

var countedRoutes = []string{
	"/accounts/payables/counted",
	"/accounts/inventory-audit/counted",
	"/stock/safety-incidents/recent",
	"/catalogue/products/counted",
}

func TestCountedPharmacyListsRefuseWithoutAccess(t *testing.T) {
	r, _ := overviewHarness(t)
	for _, route := range countedRoutes {
		require.Equal(t, http.StatusNotFound, do(r, "GET", outletPath(route), "ungranted", "").Code, route)
		require.Equal(t, http.StatusNotFound, do(r, "GET", outletPath(route), "foreign", "").Code, route)
	}
	for _, q := range []string{"?due=soon", "?due=overdue&due=week"} {
		w := do(r, "GET", outletPath("/accounts/payables/counted"+q), "editor", "")
		require.Equal(t, http.StatusBadRequest, w.Code, q+": "+w.Body.String())
	}
}

func TestCountedPharmacyListsUnavailableIs503(t *testing.T) {
	r, db := overviewHarness(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_pharmacy_access_unavailable", SQL: `ALTER TABLE pharmacy_outlet_access RENAME TO test_unavailable_pharmacy_access`}}))
	for _, route := range countedRoutes {
		w := do(r, "GET", outletPath(route), "editor", "")
		require.Equal(t, http.StatusServiceUnavailable, w.Code, route+": "+w.Body.String())
		require.NotContains(t, w.Body.String(), "test_unavailable")
	}
}

func TestCountedRecentSafetyTotal(t *testing.T) {
	r, _ := overviewHarness(t)
	_, location, batch := stockFixture(t, r)
	for range 3 {
		w := do(r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(batch, location))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	got := countedTotal(t, r, outletPath("/stock/safety-incidents/recent"))
	require.Equal(t, 3, got)
	_, overview, raw := getOverview(t, r, "editor")
	require.NotNil(t, overview.Safety, raw)
	require.Equal(t, overview.Safety.Recent, got)
}

func TestCountedBatchSafety(t *testing.T) {
	r, _ := overviewHarness(t)
	_, location, batch := stockFixture(t, r)
	for range 3 {
		w := do(r, "PUT", stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "editor", safetyBody(batch, location))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	require.Equal(t, 3, countedTotal(t, r, outletPath("/stock/batches/"+batch+"/safety-incidents")))
	// A batch that is not this outlet's is the same 404 as a missing one.
	other := outletPath("/stock/batches/" + uuid.Must(uuid.NewV7()).String() + "/safety-incidents")
	require.Equal(t, http.StatusNotFound, do(r, "GET", other, "editor", "").Code)
	require.Equal(t, http.StatusBadRequest, do(r, "GET", outletPath("/stock/batches/not-a-uuid/safety-incidents"), "editor", "").Code)
	for _, token := range []string{"ungranted", "foreign"} {
		require.Equal(t, http.StatusNotFound, do(r, "GET", outletPath("/stock/batches/"+batch+"/safety-incidents"), token, "").Code)
	}
}

func seedProduct(t *testing.T, r *gin.Engine, sku, brand, status string) {
	t.Helper()
	body := strings.NewReplacer(`"sku":"TEST-AMOX-500"`, `"sku":"`+sku+`"`, `"status":"active"`, `"status":"`+status+`"`).Replace(productBody(0, brand))
	w := do(r, "PUT", productPath(uuid.Must(uuid.NewV7()).String()), "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
}

func TestCountedCatalogueFilters(t *testing.T) {
	r, _ := overviewHarness(t)
	seedProduct(t, r, "TEST-PARA-1", "TEST Paracip", "active")
	seedProduct(t, r, "TEST-PARA-2", "TEST Paramol", "active")
	seedProduct(t, r, "TEST-IBU-1", "TEST Ibugesic", "active")
	seedProduct(t, r, "TEST-OLD-1", "TEST Oldcin", "discontinued")
	require.Equal(t, 4, countedTotal(t, r, outletPath("/catalogue/products/counted")))
	require.Equal(t, 3, countedTotal(t, r, outletPath("/catalogue/products/counted?status=active")))
	require.Equal(t, 2, countedTotal(t, r, outletPath("/catalogue/products/counted?q=test%20para")))
	unclassified := countedTotal(t, r, outletPath("/catalogue/products/counted?tax=unclassified"))
	_, overview, raw := getOverview(t, r, "editor")
	require.NotNil(t, overview.Setup, raw)
	require.Equal(t, overview.Setup.Unclassified, unclassified)
	// Every row carries its tax state, which the badge reads.
	w := do(r, "GET", outletPath("/catalogue/products/counted"), "editor", "")
	var page struct{ Data []map[string]json.RawMessage }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 4)
	for _, row := range page.Data {
		require.Contains(t, row, "tax")
	}
	for _, q := range []string{"?status=gone", "?tax=all", "?q=" + strings.Repeat("a", 201)} {
		require.Equal(t, http.StatusBadRequest, do(r, "GET", outletPath("/catalogue/products/counted"+q), "editor", "").Code, q)
	}
}

// Fail closed: without a tax scheme every product would be "unclassified".
func TestCountedCatalogueRefusesUnclassifiedWithoutAScheme(t *testing.T) {
	r, _ := overviewHarness(t)
	base := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pkOutlet
	w := do(r, "PUT", base, "editor", `{"code":"TEST-LAHORE","name":"TEST Lahore Outlet","country_code":"PK","timezone":"Asia/Karachi"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, do(r, "PUT", base+"/access/user-editor", "editor", `{"level":"editor"}`).Code)
	require.Equal(t, http.StatusBadRequest, do(r, "GET", base+"/catalogue/products/counted?tax=unclassified", "editor", "").Code)
	require.Equal(t, http.StatusOK, do(r, "GET", base+"/catalogue/products/counted", "editor", "").Code)
}
