package pharmacy_test

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func overviewPath() string {
	return "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/stock/positions"
}

// HTTP field tags are explicit: the fixture validates the public wire contract.
type overviewPage struct {
	Data []struct {
		ID           string `json:"id"`
		OutletID     string `json:"outlet_id"`
		ProductID    string `json:"product_id"`
		Units        int64  `json:"on_hand_units"`
		ProductUnits string `json:"product_on_hand_units"`
		Value        string `json:"known_purchase_value_minor"`
		Uncosted     string `json:"uncosted_units"`
		Unit         string `json:"unit_of_issue"`
	} `json:"data"`
	Page struct {
		More bool   `json:"has_more"`
		Next string `json:"next_cursor"`
	} `json:"page"`
}

// Independent products deliberately share a SKU and name. They must never merge.
func overviewFixture(t *testing.T, r *gin.Engine, outlet, code, unit string, quantity int, cost *int, secondLocation bool) string {
	t.Helper()
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + outlet
	w := do(r, "PUT", root, "editor", fmt.Sprintf(`{"code":%q,"name":%q,"country_code":"IN","timezone":"Asia/Kolkata"}`, code, code))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "PUT", root+"/access/user-editor", "editor", `{"level":"editor"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	product := uuid.Must(uuid.NewV7()).String()
	body := strings.ReplaceAll(productBody(0, "TEST same brand"), `"unit_of_issue":"capsule"`, `"unit_of_issue":"`+unit+`"`)
	w = do(r, "PUT", root+"/catalogue/products/"+product, "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	batch := uuid.Must(uuid.NewV7()).String()
	batchBody := fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-10-31","mrp_minor":12500`, product)
	if cost != nil {
		batchBody += fmt.Sprintf(`,"purchase_cost_minor":%d`, *cost)
	}
	w = do(r, "PUT", root+"/stock/batches/"+batch, "editor", batchBody+"}")
	require.Equal(t, 200, w.Code, w.Body.String())
	n := 1
	if secondLocation {
		n = 2
	}
	for i := 0; i < n; i++ {
		location := uuid.Must(uuid.NewV7()).String()
		w = do(r, "PUT", root+"/stock/locations/"+location, "editor", fmt.Sprintf(`{"code":"TEST-Q%d","name":"TEST Quarantine"}`, i))
		require.Equal(t, 200, w.Code, w.Body.String())
		w = do(r, "PUT", root+"/stock/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", fmt.Sprintf("TEST-RECEIPT-%d", i), quantity))
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	return product
}

func TestStockOverviewScopesEveryOutletAndKeepsTotalsAcrossPages(t *testing.T) {
	r, db := catalogueHarness(t)
	// The original outlet has no grant or positions; coverage must still say partial.
	outletA, outletB := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	cost := 7
	a := overviewFixture(t, r, outletA, "TEST-A", "capsule", 3, &cost, true)
	b := overviewFixture(t, r, outletB, "TEST-B", "tablet", 11, nil, false)
	var databaseNow time.Time
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return tx.Raw("SELECT clock_timestamp()").Scan(&databaseNow).Error }))
	cutoff := url.QueryEscape(databaseNow.UTC().Format(time.RFC3339Nano))
	query := "?as_of=" + cutoff + "&limit=1"
	seen := map[string]bool{}
	for cursor := ""; ; {
		w := do(r, "GET", overviewPath()+query+cursor, "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var values []struct {
			Currency string `json:"currency"`
			Value    string `json:"known_purchase_value_minor"`
			Uncosted string `json:"uncosted_products"`
		}
		require.NoError(t, json.Unmarshal([]byte(w.Header().Get("X-Pharmacy-Stock-Valuation")), &values))
		require.Len(t, values, 1)
		require.Equal(t, "INR", values[0].Currency)
		require.Equal(t, "4", values[0].Value)
		require.Equal(t, "1", values[0].Uncosted)
		require.Equal(t, "restricted_outlets_excluded", w.Header().Get("X-Pharmacy-Stock-Coverage"))
		var page overviewPage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		row := page.Data[0]
		require.False(t, seen[row.ID])
		seen[row.ID] = true
		switch row.ProductID {
		case a:
			require.Equal(t, outletA, row.OutletID)
			require.Equal(t, "6", row.ProductUnits)
			require.Equal(t, "4", row.Value, "floor((3+3)*7/10), not separately rounded locations")
			require.Equal(t, "0", row.Uncosted)
		case b:
			require.Equal(t, outletB, row.OutletID)
			require.Equal(t, "11", row.ProductUnits)
			require.Equal(t, "0", row.Value)
			require.Equal(t, "11", row.Uncosted)
		default:
			t.Fatalf("unexpected product %s", row.ProductID)
		}
		if !page.Page.More {
			break
		}
		require.NotEmpty(t, page.Page.Next)
		cursor = "&cursor=" + url.QueryEscape(page.Page.Next)
	}
	require.Len(t, seen, 3)
	empty := do(r, "GET", overviewPath()+"?as_of="+cutoff+"&expiry=due_30", "editor", "")
	require.Equal(t, 200, empty.Code, empty.Body.String())
	require.Contains(t, empty.Body.String(), `"data":[]`)
	require.JSONEq(t, `[]`, empty.Header().Get("X-Pharmacy-Stock-Valuation"))
	// Both CSV and list reauthorize after revocation; unknown/ungranted outlet filter is 404.
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + outletB
	require.Equal(t, 200, do(r, "DELETE", root+"/access/user-editor", "editor", "").Code)
	w := do(r, "GET", overviewPath()+"?as_of="+cutoff, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), b)
	require.Equal(t, 404, do(r, "GET", overviewPath()+"?as_of="+cutoff+"&outlet_id="+outletB, "editor", "").Code)
	w = do(r, "GET", overviewPath()+".csv?as_of="+cutoff, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), b)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes())), w.Header().Get("X-Export-Sha256"))
	records, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3)
	require.Equal(t, "outlet_id", records[0][0])
	require.Equal(t, outletA, records[1][0])
	require.Equal(t, "restricted_outlets_excluded", records[1][3])
}

func TestStockOverviewDeniesInvalidScopesFiltersAndFailedDependencies(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "reader")
	query := "?as_of=2020-01-01T00:00:00Z"
	for _, suffix := range []string{"", ".csv"} {
		for token, status := range map[string]int{"unknown": 401, "denied": 403, "sibling": 404, "foreign": 404} {
			w := do(r, "GET", overviewPath()+suffix+query, token, "")
			require.Equal(t, status, w.Code, w.Body.String())
			require.Empty(t, w.Header().Get("Content-Disposition"))
		}
		for _, bad := range []string{"", "?as_of=bad", query + "&as_of=2020-01-01T00:00:00Z", query + "&expiry=bad", query + "&balance=bad", query + "&outlet_id=bad", query + "&tenant_id=other", query + "&from=1970-01-01T00:00:00Z", "?as_of=2099-01-01T00:00:00Z"} {
			w := do(r, "GET", overviewPath()+suffix+bad, "editor", "")
			require.Equal(t, 400, w.Code, bad+": "+w.Body.String())
		}
	}
	w := do(r, "GET", overviewPath()+query, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "all_accessible", w.Header().Get("X-Pharmacy-Stock-Coverage"))
	require.Contains(t, w.Body.String(), `"data":[]`)
	require.Equal(t, 200, do(r, "DELETE", strings.Split(productPath(""), "/catalogue")[0]+"/access/user-editor", "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", overviewPath()+query, "editor", "").Code, "manager permission does not replace a grant")
	grantCatalogue(t, r, "user-editor", "reader")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_overview_dependency", SQL: `ALTER TABLE pharmacy_outlet_access RENAME TO test_unavailable_access`}}))
	w = do(r, "GET", overviewPath()+query, "editor", "")
	require.GreaterOrEqual(t, w.Code, 500)
	require.NotContains(t, w.Body.String(), `"data"`)
}

func TestStockOverviewReadPoliciesDoNotPermitWritesOrUnscopedReads(t *testing.T) {
	r, db := catalogueHarness(t)
	cost := 0
	outlet := uuid.Must(uuid.NewV7()).String()
	overviewFixture(t, r, outlet, "TEST-READ", "capsule", 5, &cost, false)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("pharmacy_stock_movements").Count(&count).Error)
		require.Zero(t, count)
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-editor',true),set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_stock_overview',?,true)`, pharmaOrganization).Error; err != nil {
			return err
		}
		require.NoError(t, tx.Table("pharmacy_stock_movements").Count(&count).Error)
		require.EqualValues(t, 1, count)
		// Grant plus overview flag is read-only; app.pharmacy_outlet is still absent.
		if err := tx.Exec("SAVEPOINT denied_write").Error; err != nil {
			return err
		}
		err := tx.Exec(`INSERT INTO pharmacy_stock_locations(id,tenant_id,outlet_id,code,name) VALUES (?,?,?,'TEST-FORBIDDEN','TEST forbidden')`, uuid.Must(uuid.NewV7()), testutil.TenantA, outlet).Error
		require.Error(t, err)
		require.Contains(t, err.Error(), "row-level security")
		return tx.Exec("ROLLBACK TO SAVEPOINT denied_write").Error
	}))
}
