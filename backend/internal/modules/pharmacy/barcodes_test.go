package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func barcodePath(gtin string) string {
	return strings.TrimSuffix(productPath(""), "products/") + "barcodes/" + gtin
}

func TestBarcodeScopeValidationAndAudit(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", productPath(id), "editor", productBody(0, "TEST Barcode scope")).Code)
	body := fmt.Sprintf(`{"product_id":%q}`, id)
	for _, tc := range []struct {
		token string
		code  int
	}{{"foreign", 404}, {"sibling", 404}, {"denied", 403}, {"unknown", 401}} {
		for _, method := range []string{"GET", "PUT"} {
			w := do(r, method, barcodePath("4006381333931"), tc.token, body)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "barcode_unknown")
		}
	}
	require.Equal(t, 404, do(r, "PUT", barcodePath("4006381333931"), "reader", body).Code)
	for _, code := range []string{"4006381333932", "400638133393", "abc", "4006381333931?q=all"} {
		require.Equal(t, 400, do(r, "GET", barcodePath(code), "editor", "").Code)
		require.Equal(t, 400, do(r, "PUT", barcodePath(code), "editor", body).Code)
	}
	for _, bad := range []string{`{}`, `{"product_id":null}`, `{"product_id":"bad"}`, body + `{}`, strings.TrimSuffix(body, "}") + `,"tenant_id":"foreign"}`} {
		require.Equal(t, 400, do(r, "PUT", barcodePath("4006381333931"), "editor", bad).Code)
	}
	foreignOutlet := strings.Replace(barcodePath("4006381333931"), pharmaOutlet, "0199e000-0000-7000-8000-000000000002", 1)
	require.Equal(t, 404, do(r, "PUT", foreignOutlet, "editor", body).Code)
	for range 2 {
		require.Equal(t, 200, do(r, "PUT", barcodePath("4006381333931"), "editor", body).Code)
	}
	require.Equal(t, 200, do(r, "GET", barcodePath("4006381333931"), "reader", "").Code)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_product_barcodes' AND action='insert'`).Scan(&count).Error
	}))
	require.Equal(t, int64(1), count, "a replay must not insert another link or audit write")
	require.Equal(t, 200, do(r, "DELETE", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/outlets/"+pharmaOutlet+"/access/user-reader", "editor", "").Code)
	w := do(r, "GET", barcodePath("4006381333931"), "reader", "")
	require.Equal(t, 404, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "barcode_unknown")
}

func TestConcurrentBarcodeLinkKeepsOneProduct(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	ids := []string{uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()}
	for i, id := range ids {
		body := strings.ReplaceAll(productBody(0, "TEST Concurrent"), "TEST-AMOX-500", fmt.Sprintf("TEST-%d", i))
		require.Equal(t, 200, do(r, "PUT", productPath(id), "editor", body).Code)
	}
	codes := make(chan int, 2)
	start := make(chan struct{})
	for _, id := range ids {
		go func() {
			<-start
			codes <- do(r, "PUT", barcodePath("4006381333931"), "editor", fmt.Sprintf(`{"product_id":%q}`, id)).Code
		}()
	}
	close(start)
	a, b := <-codes, <-codes
	require.ElementsMatch(t, []int{200, 409}, []int{a, b})
	w := do(r, "GET", barcodePath("04006381333931"), "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var product struct{ ID string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &product))
	require.Contains(t, ids, product.ID)
}

func TestBarcodeLinksStayInsideEachOutletAndCannotBypassRLS(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	otherOutlet := uuid.Must(uuid.NewV7()).String()
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + otherOutlet
	w := do(r, "PUT", root, "editor", `{"code":"TEST-SECOND","name":"TEST Second Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 200, do(r, "PUT", root+"/access/user-editor", "editor", `{"level":"editor"}`).Code)
	ids := []string{uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()}
	paths := []string{productPath(ids[0]), strings.Replace(productPath(ids[1]), pharmaOutlet, otherOutlet, 1)}
	barcodes := []string{barcodePath("4006381333931"), strings.Replace(barcodePath("4006381333931"), pharmaOutlet, otherOutlet, 1)}
	for _, path := range paths {
		require.Equal(t, 200, do(r, "PUT", path, "editor", productBody(0, "TEST Scoped pack")).Code)
	}
	require.Equal(t, 404, do(r, "PUT", barcodes[0], "editor", fmt.Sprintf(`{"product_id":%q}`, ids[1])).Code)
	for i, path := range barcodes {
		require.Equal(t, 200, do(r, "PUT", path, "editor", fmt.Sprintf(`{"product_id":%q}`, ids[i])).Code)
		w = do(r, "GET", path, "editor", "")
		var product struct{ ID string }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &product))
		require.Equal(t, ids[i], product.ID)
	}
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pharmacy_product_barcodes`).Scan(&count).Error
	}))
	require.Zero(t, count, "a tenant context alone does not grant outlet access")
	err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_product_barcodes(id,tenant_id,outlet_id,gtin,product_id,actor_subject)VALUES(?,?,?,?,?,'user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, "00036000291452", ids[0]).Error
	})
	var pg *pgconn.PgError
	require.ErrorAs(t, err, &pg)
	require.Equal(t, "42501", pg.Code)
}

func TestBarcodeLinkResolvesCanonicalGTINAndRejectsReassignment(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", productPath(id), "editor", productBody(0, "TEST Barcode product"))
	require.Equal(t, 200, w.Code, w.Body.String())
	body := fmt.Sprintf(`{"product_id":%q}`, id)
	w = do(r, "GET", barcodePath("4006381333931"), "editor", "")
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "barcode_unknown")
	w = do(r, "PUT", barcodePath("4006381333931"), "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, code := range []string{"4006381333931", "04006381333931"} {
		w = do(r, "GET", barcodePath(code), "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var product map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &product))
		require.Equal(t, id, product["id"])
	}
	w = do(r, "PUT", barcodePath("04006381333931"), "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	other := uuid.Must(uuid.NewV7()).String()
	w = do(r, "PUT", productPath(other), "editor", strings.ReplaceAll(productBody(0, "TEST Other"), "TEST-AMOX-500", "TEST-OTHER"))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "PUT", barcodePath("4006381333931"), "editor", fmt.Sprintf(`{"product_id":%q}`, other))
	require.Equal(t, 409, w.Code, w.Body.String())
	w = do(r, "PUT", productPath(id), "editor", strings.Replace(productBody(1, "TEST Barcode product"), `"active"`, `"withdrawn"`, 1))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "GET", barcodePath("4006381333931"), "editor", "")
	require.Equal(t, 409, w.Code, w.Body.String())
}
