package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func TestQuarantineReceiptIsIdempotentAndBalanceIsNotDispensable(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet
	product := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", productPath(product), "editor", productBody(0, "TEST Receiving product")).Code)
	location := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", root+"/stock/locations/"+location, "editor", `{"code":"TEST-QUARANTINE","name":"TEST Receiving quarantine"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	batch := uuid.Must(uuid.NewV7()).String()
	batchBody := fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT-001","expires_on":"2030-10-31","manufactured_on":"2026-10-01"}`, product)
	w = do(r, "PUT", root+"/stock/batches/"+batch, "editor", batchBody)
	require.Equal(t, 200, w.Code, w.Body.String())
	movement := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":100,"reason":"TEST opening receipt","source_reference":"TEST-GRN-001-L1"}`, location, batch)
	path := root + "/stock/movements/" + movement
	w = do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	replay := do(r, "PUT", path, "editor", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	require.Equal(t, 409, do(r, "PUT", path, "editor", strings.Replace(body, "100", "101", 1)).Code)
	balance := do(r, "GET", root+"/stock/balance?location_id="+location+"&batch_id="+batch, "editor", "")
	require.Equal(t, 200, balance.Code, balance.Body.String())
	var result struct {
		OnHandUnits      int64  `json:"on_hand_units"`
		DispensableUnits int64  `json:"dispensable_units"`
		StockStatus      string `json:"stock_status"`
	}
	require.NoError(t, json.Unmarshal(balance.Body.Bytes(), &result))
	require.EqualValues(t, 100, result.OnHandUnits)
	require.Zero(t, result.DispensableUnits)
	require.Equal(t, "quarantine", result.StockStatus)
}

func stockFixture(t *testing.T, r *gin.Engine) (product, location, batch string) {
	t.Helper()
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet
	product = uuid.Must(uuid.NewV7()).String()
	location = uuid.Must(uuid.NewV7()).String()
	batch = uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", productPath(product), "editor", productBody(0, "TEST Quarantine product")).Code)
	require.Equal(t, 200, do(r, "PUT", root+"/stock/locations/"+location, "editor", `{"code":"TEST-Q","name":"TEST Quarantine"}`).Code)
	w := do(r, "PUT", root+"/stock/batches/"+batch, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-10-31"}`, product))
	require.Equal(t, 200, w.Code, w.Body.String())
	return product, location, batch
}

func stockBody(location, batch, kind, source string, qty int) string {
	return fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":%q,"quantity_units":%d,"reason":"TEST receiving reconciliation","source_reference":%q}`, location, batch, kind, qty, source)
}

func TestQuarantineCorrectionsSerializeAndCannotOverdraw(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	_, location, batch := stockFixture(t, r)
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock"
	require.Equal(t, 200, do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-RECEIPT", 10)).Code)
	const workers = 8
	codes := make(chan int, workers)
	start := make(chan struct{})
	for i := range workers {
		go func() {
			<-start
			codes <- do(r, "PUT", root+"/corrections/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "correction", fmt.Sprintf("TEST-CORRECTION-%d", i), -3)).Code
		}()
	}
	close(start)
	winners, conflicts := 0, 0
	for range workers {
		switch code := <-codes; code {
		case 200:
			winners++
		case 409:
			conflicts++
		default:
			t.Fatalf("unexpected %d", code)
		}
	}
	require.Equal(t, 3, winners)
	require.Equal(t, 5, conflicts)
	w := do(r, "GET", root+"/balance?location_id="+location+"&batch_id="+batch, "editor", "")
	require.Contains(t, w.Body.String(), `"on_hand_units":1`)
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject) VALUES (?,?,?,?,?,'correction',-2,'TEST raw correction','TEST-RAW','user-editor')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, location, batch).Error
	}))
	require.Equal(t, 409, do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-RECEIPT", 10)).Code)
	for _, table := range []string{"pharmacy_stock_movements", "pharmacy_stock_locations", "pharmacy_stock_batches"} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec("DELETE FROM " + table).Error }))
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			var count int64
			err := tx.Table(table).Count(&count).Error
			require.Zero(t, count)
			return err
		}))
	}
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", "helivanta.in.pharmacy.stock_movement_recorded.v1").Count(&count).Error
	}))
	require.EqualValues(t, 4, count)
}

func TestBatchIdentityDatesAndStockedProductUnitsAreProtected(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	product, location, batch := stockFixture(t, r)
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock"
	body := fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-10-31"}`, product)
	require.Equal(t, 200, do(r, "PUT", root+"/batches/"+batch, "editor", body).Code)
	require.Equal(t, 409, do(r, "PUT", root+"/batches/"+batch, "editor", strings.Replace(body, "2030-10-31", "2030-11-30", 1)).Code)
	require.Equal(t, 409, do(r, "PUT", root+"/batches/"+uuid.Must(uuid.NewV7()).String(), "editor", body).Code)
	for _, bad := range []string{
		strings.Replace(body, "2030-10-31", "2030-02-30", 1),
		strings.Replace(body, "2030-10-31", "2030-10", 1),
		strings.Replace(body, `"expires_on":`, `"manufactured_on":"2031-01-01","expires_on":`, 1),
	} {
		require.Equal(t, 400, do(r, "PUT", root+"/batches/"+uuid.Must(uuid.NewV7()).String(), "editor", bad).Code)
	}
	updated := strings.Replace(productBody(1, "TEST Quarantine product"), `"pack_size":10`, `"pack_size":20`, 1)
	w := do(r, "PUT", productPath(product), "editor", updated)
	require.Equal(t, 409, w.Code, w.Body.String())
	for _, token := range []string{"sibling", "foreign"} {
		for _, collection := range []string{"locations", "batches", "movements"} {
			require.Equal(t, 404, do(r, "GET", root+"/"+collection, token, "").Code)
		}
	}
	require.Equal(t, 400, do(r, "GET", root+"/balance?location_id=bad&batch_id="+batch, "editor", "").Code)
	require.Equal(t, 404, do(r, "GET", root+"/balance?location_id="+location+"&batch_id="+uuid.Must(uuid.NewV7()).String(), "editor", "").Code)
	for _, kind := range []string{"correction", "dispense"} {
		require.Equal(t, 400, do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, kind, "TEST-FORGED", -1)).Code)
	}
	require.Equal(t, 403, do(r, "PUT", root+"/corrections/"+uuid.Must(uuid.NewV7()).String(), "reader", stockBody(location, batch, "correction", "TEST-DENIED", -1)).Code)
	w = do(r, "GET", root+"/batches?product_id="+product, "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"expires_on":"2030-10-31"`)
}
