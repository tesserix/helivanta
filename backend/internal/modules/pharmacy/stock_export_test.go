package pharmacy_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStockExportReconcilesRecordedQuantityAtTheExplicitCutoff(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	root := strings.Split(productPath(""), "/catalogue")[0] + "/stock"
	w := do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-EXPORT-RECEIPT", 25))
	require.Equal(t, 200, w.Code, w.Body.String())
	var receipt struct {
		CreatedAt time.Time `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	cutoff := receipt.CreatedAt.UTC().Format(time.RFC3339Nano)
	w = do(r, "GET", root+"/positions.csv?as_of="+url.QueryEscape(cutoff), "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "text/csv; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Contains(t, w.Header().Get("Content-Disposition"), "attachment; filename=\"pharmacy-stock-")
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	values := map[string]string{}
	for i, name := range rows[0] {
		values[name] = rows[1][i]
	}
	require.Equal(t, cutoff, values["snapshot_as_of_utc"])
	require.Equal(t, location, values["location_id"])
	require.Equal(t, batch, values["batch_id"])
	require.Equal(t, "TEST-AMOX-500", values["sku"])
	require.Equal(t, "capsule", values["unit_of_issue"])
	require.Equal(t, "Asia/Kolkata", values["expiry_time_zone"])
	require.Equal(t, "quarantine", values["stock_status"])
	require.Equal(t, "25", values["on_hand_units"])
	require.Equal(t, "25", values["receipt_units"])
	require.Equal(t, "0", values["correction_units"])
	require.Equal(t, "0", values["dispensable_units"])
}

func TestStockExportRejectsForeignRevokedAndInvalidScopes(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	_, location, _ := stockFixture(t, r)
	root := strings.Split(productPath(""), "/catalogue")[0] + "/stock"
	query := "as_of=2026-10-01T00:00:00Z"
	for actor, status := range map[string]int{"editor": 200, "reader": 200, "foreign": 404, "sibling": 404, "denied": 403, "unknown": 401} {
		w := do(r, "GET", root+"/positions.csv?"+query, actor, "")
		require.Equal(t, status, w.Code, actor+": "+w.Body.String())
		if status != 200 {
			require.Empty(t, w.Header().Get("Content-Disposition"))
		}
	}
	for _, bad := range []string{"", "as_of=bad", "as_of=2099-01-01T00:00:00Z", "as_of=2020-01-01T00:00:00.000000001Z", query + "&as_of=2020-01-01T00:00:00Z", query + "&expiry=unknown", query + "&expiry=all&expiry=expired", query + "&balance=no", query + "&location_id=no", query + "&product_id=", query + "&limit=100", query + "&cursor=no", query + "&from=2020-01-01T00:00:00Z", query + "&tenant_id=other", query + "&unused=1", query + "&broken=%ZZ"} {
		w := do(r, "GET", root+"/positions.csv?"+bad, "editor", "")
		require.Equal(t, 400, w.Code, bad)
		require.Empty(t, w.Header().Get("Content-Disposition"))
	}
	for _, reference := range []string{"location_id", "product_id"} {
		require.Equal(t, 404, do(r, "GET", root+"/positions.csv?"+query+"&"+reference+"="+uuid.Must(uuid.NewV7()).String(), "editor", "").Code)
	}
	wrongPharmacy := strings.Replace(root, pharmaOrganization, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, 404, do(r, "GET", wrongPharmacy+"/positions.csv?"+query, "editor", "").Code)
	w := do(r, "GET", root+"/positions.csv?"+query+"&location_id="+location+"&expiry=due_30&balance=on_hand", "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "0", w.Header().Get("X-Export-Row-Count"))
	require.Equal(t, 200, do(r, "DELETE", strings.TrimSuffix(root, "/stock")+"/access/user-reader", "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", root+"/positions.csv?"+query, "reader", "").Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_export_auth_failure", SQL: `ALTER TABLE pharmacy_outlet_access RENAME TO test_unavailable_pharmacy_access`}}))
	w = do(r, "GET", root+"/positions.csv?"+query, "editor", "")
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "test_unavailable")
	require.Empty(t, w.Header().Get("Content-Disposition"))
}

func TestStockExportIncludesUnloadedPagesAndRefusesRowOverflow(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, _, batch := stockFixture(t, r)
	seed := func(first, last int) {
		t.Helper()
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','user-editor',true),set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO pharmacy_stock_locations(id,tenant_id,outlet_id,code,name) SELECT ('0199e0ab-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,?,?,('TEST-EXPORT-'||n),'TEST export location' FROM generate_series(?::integer,?::integer) n`, testutil.TenantA, pharmaOutlet, first, last).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at) SELECT ('0199e0ac-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,?,?,('0199e0ab-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,?,'receipt',1,'TEST export row bound','TEST-EXPORT-'||n,'user-editor','2026-01-01'::timestamptz FROM generate_series(?::integer,?::integer) n`, testutil.TenantA, pharmaOutlet, batch, first, last).Error
		}))
	}
	seed(1, 1000)
	root := strings.Split(productPath(""), "/catalogue")[0] + "/stock"
	path := root + "/positions.csv?as_of=2026-10-01T00:00:00Z"
	page := do(r, "GET", root+"/positions?as_of=2026-10-01T00:00:00Z", "editor", "")
	require.Equal(t, 200, page.Code, page.Body.String())
	var listed struct {
		Data []json.RawMessage
		Page struct {
			HasMore bool `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(page.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 50)
	require.True(t, listed.Page.HasMore)
	w := do(r, "GET", path, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 1001)
	require.Equal(t, "1000", w.Header().Get("X-Export-Row-Count"))
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		require.False(t, seen[row[1]])
		seen[row[1]] = true
	}
	seed(1001, 1001)
	w = do(r, "GET", path, "editor", "")
	require.Equal(t, 413, w.Code, w.Body.String())
	require.Empty(t, w.Header().Get("Content-Disposition"))
	require.NotContains(t, w.Body.String(), "TEST-EXPORT-")
	require.Contains(t, w.Body.String(), "stock_export_too_large")
	w = do(r, "GET", path+"&location_id=0199e0ab-0000-7000-8000-000000000001", "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "1", w.Header().Get("X-Export-Row-Count"))
}

func TestStockExportAuditsOnlySuccessfulGenerationWithoutCellValues(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	root := strings.Split(productPath(""), "/catalogue")[0] + "/stock"
	w := do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-EXPORT-AUDIT", 5))
	require.Equal(t, 200, w.Code, w.Body.String())
	var receipt struct {
		CreatedAt time.Time `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	path := root + "/positions.csv?as_of=" + url.QueryEscape(receipt.CreatedAt.Format(time.RFC3339Nano))
	require.Equal(t, 404, do(r, "GET", path, "foreign", "").Code)
	require.Empty(t, logs.String())
	w = do(r, "GET", path, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &event))
	require.Equal(t, "pharmacy.stock.exported", event["audit_event"])
	require.Equal(t, testutil.TenantA, event["tenant_id"])
	require.Equal(t, "user-editor", event["subject"])
	require.Equal(t, pharmaOrganization, event["pharmacy_id"])
	require.Equal(t, pharmaOutlet, event["outlet_id"])
	require.Equal(t, w.Header().Get("X-Request-ID"), event["request_id"])
	require.Equal(t, w.Header().Get("X-Export-Sha256"), event["sha256"])
	require.Equal(t, float64(1), event["row_count"])
	require.NotContains(t, logs.String(), "TEST Quarantine")
	require.NotContains(t, logs.String(), "TEST-EXPORT-AUDIT")
}

func TestStockExportEscapesFormulaTextAndPreservesSignedNumericTotals(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, _, batch := stockFixture(t, r)
	name := `=HYPERLINK("https://invalid.example","TEST")`
	w := do(r, "PUT", productPath(product), "editor", productBody(1, name))
	require.Equal(t, 200, w.Code, w.Body.String())
	root := strings.Split(productPath(""), "/catalogue")[0] + "/stock"
	location := uuid.Must(uuid.NewV7()).String()
	w = do(r, "PUT", root+"/locations/"+location, "editor", `{"code":"TEST-CSV","name":"TEST \"Cabinet\", north","storage_address":{"zone_code":"TEST-Z","shelf_code":"TEST-S"}}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-EXPORT-R", 10))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "PUT", root+"/corrections/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "correction", "TEST-EXPORT-C", -4))
	require.Equal(t, 200, w.Code, w.Body.String())
	var correction struct {
		CreatedAt time.Time `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &correction))
	w = do(r, "GET", root+"/positions.csv?as_of="+url.QueryEscape(correction.CreatedAt.Format(time.RFC3339Nano)), "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	values := map[string]string{}
	for i, key := range rows[0] {
		values[key] = rows[1][i]
	}
	require.Equal(t, "'"+name, values["brand_name"])
	require.Equal(t, `TEST "Cabinet", north`, values["location_name"])
	require.Equal(t, "TEST-Z", values["zone_code"])
	require.Equal(t, "", values["rack_code"])
	require.Equal(t, "TEST-S", values["shelf_code"])
	require.Equal(t, "6", values["on_hand_units"])
	require.Equal(t, "-4", values["correction_units"])
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes())), w.Header().Get("X-Export-Sha256"))
}
