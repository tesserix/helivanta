package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLocationPersistsCanonicalIndependentStorageAddress(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock"
	id := uuid.Must(uuid.NewV7()).String()
	body := `{"code":"TEST-BIN-1","name":"TEST quarantine","storage_address":{"zone_code":" test-z1 ","aisle_code":"","rack_code":"","shelf_code":" test-s1 "}}`
	w := do(r, "PUT", root+"/locations/"+id, "editor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	var result struct {
		StorageAddress map[string]string `json:"storage_address"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, map[string]string{"zone_code": "TEST-Z1", "aisle_code": "", "rack_code": "", "shelf_code": "TEST-S1"}, result.StorageAddress)
	replay := do(r, "PUT", root+"/locations/"+id, "editor", strings.ReplaceAll(strings.ReplaceAll(body, " test-z1 ", "TEST-Z1"), " test-s1 ", "TEST-S1"))
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	read := do(r, "GET", root+"/locations", "editor", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), `"shelf_code":"TEST-S1"`)
	product, _, batch := stockFixture(t, r)
	movement := do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(id, batch, "receipt", "TEST-ADDRESS-RECEIPT", 5))
	require.Equal(t, 200, movement.Code, movement.Body.String())
	var recorded struct {
		CreatedAt time.Time `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(movement.Body.Bytes(), &recorded))
	positions := do(r, "GET", root+"/positions?product_id="+product+"&as_of="+url.QueryEscape(recorded.CreatedAt.Format(time.RFC3339Nano)), "editor", "")
	require.Equal(t, 200, positions.Code, positions.Body.String())
	var rows struct {
		Data []struct {
			StorageAddress map[string]string `json:"storage_address"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(positions.Body.Bytes(), &rows))
	require.Len(t, rows.Data, 1)
	require.Equal(t, result.StorageAddress, rows.Data[0].StorageAddress, positions.Body.String())
}

func TestLocationStorageAddressGuardsIdentityAndConcurrentReplay(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock"
	id := uuid.Must(uuid.NewV7()).String()
	body := `{"code":"TEST-ADDRESS","name":"TEST quarantine","storage_address":{"zone_code":"TEST-Z1","aisle_code":"TEST-A1","rack_code":"TEST-R1","shelf_code":"TEST-S1"}}`
	path := root + "/locations/" + id
	start := make(chan struct{})
	codes := make(chan int, 8)
	for range 8 {
		go func() { <-start; codes <- do(r, "PUT", path, "editor", body).Code }()
	}
	close(start)
	for range 8 {
		require.Equal(t, 200, <-codes)
	}
	sharedShelf := do(r, "PUT", root+"/locations/"+uuid.Must(uuid.NewV7()).String(), "editor", strings.Replace(body, "TEST-ADDRESS", "TEST-SECOND-BIN", 1))
	require.Equal(t, 200, sharedShelf.Code, sharedShelf.Body.String())
	for _, key := range []string{"zone_code", "aisle_code", "rack_code", "shelf_code"} {
		var changed map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &changed))
		changed["storage_address"].(map[string]any)[key] = "TEST-CHANGED"
		payload, err := json.Marshal(changed)
		require.NoError(t, err)
		require.Equal(t, 409, do(r, "PUT", path, "editor", string(payload)).Code, key)
	}
	for _, token := range []string{"reader", "sibling", "foreign"} {
		require.Equal(t, 404, do(r, "PUT", path, token, body).Code, token)
	}
	for _, token := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, do(r, "GET", root+"/locations", token, "").Code)
	}
	require.Equal(t, 401, do(r, "PUT", path, "unknown", body).Code)
	read := do(r, "GET", root+"/locations", "reader", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
	require.Contains(t, read.Body.String(), `"shelf_code":"TEST-S1"`)
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("audit_events").Where("resource_type=? AND resource_id=? AND actor_subject=?", "pharmacy_stock_locations", id, "user-editor").Count(&count).Error
		require.EqualValues(t, 1, count)
		return err
	}))
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_stock_locations SET shelf_code='TEST-CHANGED' WHERE id=?`, id).Error
	}))
	require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec(`DELETE FROM pharmacy_stock_locations WHERE id=?`, id).Error }))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("pharmacy_stock_locations").Where("id=?", id).Count(&count).Error
		require.Zero(t, count)
		return err
	}))
}

func TestLocationStorageAddressRejectsMalformedAndAcceptsBoundaries(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock/locations/"
	for _, value := range []string{"A/B", "A B", "<script>", "é", "药", strings.Repeat("A", 33), "_R1"} {
		for _, key := range []string{"zone_code", "aisle_code", "rack_code", "shelf_code"} {
			body, err := json.Marshal(map[string]any{"code": "TEST-BAD", "name": "TEST invalid", "storage_address": map[string]string{key: value}})
			require.NoError(t, err)
			w := do(r, "PUT", root+uuid.Must(uuid.NewV7()).String(), "editor", string(body))
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO pharmacy_stock_locations(id,tenant_id,outlet_id,code,name,`+key+`) VALUES (?,?,?,'TEST-RAW','TEST raw',?)`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, value).Error
			}))
		}
	}
	for i, address := range []string{``, `,"storage_address":null`, `,"storage_address":{}`, `,"storage_address":{"zone_code":"","aisle_code":"","rack_code":"","shelf_code":"` + strings.Repeat("A", 32) + `"}`} {
		w := do(r, "PUT", root+uuid.Must(uuid.NewV7()).String(), "editor", fmt.Sprintf(`{"code":"TEST-BOUNDARY-%d","name":"TEST boundary"%s}`, i, address))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"zone_code":""`)
	}
	for _, address := range []string{`{"unexpected":"X"}`, `[]`, `"rack"`, `{"rack_code":3}`} {
		w := do(r, "PUT", root+uuid.Must(uuid.NewV7()).String(), "editor", `{"code":"TEST-BAD","name":"TEST bad","storage_address":`+address+`}`)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}

type pharmacyBeforeStorageAddress struct{ *pharmacy.Module }

func (m pharmacyBeforeStorageAddress) Migrations() []tenantdb.Migration {
	migrations := m.Module.Migrations()
	return migrations[:len(migrations)-1]
}

func TestStorageAddressMigrationPreservesLegacyRowsAndReplay(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"editor": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"editor": {"pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.stock.read", "pharmacy.stock.receive"}},
		Modules: []platform.Module{pharmacyBeforeStorageAddress{pharmacy.New()}},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_locations(id,tenant_id,outlet_id,code,name) VALUES (?,?,?,'TEST-LEGACY','TEST previous schema')`, id, testutil.TenantA, pharmaOutlet).Error
	}))
	migrations := pharmacy.New().Migrations()
	require.NoError(t, db.Migrate(t.Context(), migrations))
	require.NoError(t, db.Migrate(t.Context(), migrations))
	path := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock/locations/" + id
	w := do(r, "PUT", path, "editor", `{"code":"TEST-LEGACY","name":"TEST previous schema"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"storage_address":{"zone_code":"","aisle_code":"","rack_code":"","shelf_code":""}`)
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("audit_events").Where("resource_type=? AND resource_id=?", "pharmacy_stock_locations", id).Count(&count).Error
		require.EqualValues(t, 1, count)
		return err
	}))
}
