package globalapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStocktakePreservesSupplierReservations(t *testing.T) {
	perms := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermStockCorrect, pharmacy.PermStockRelease, "pharmacy.tax.manage", "pharmacy.schedule.declare", "pharmacy.schedule.review"}
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": perms, "reviewer": perms}, Modules: []platform.Module{pharmacy.New()}})
	fixture := seedSupplierStock(t, r, db, "pharmacy")
	root := "/v1/pharmacy/pharmacies/" + fixture.organization.String() + "/outlets/" + fixture.location.String()
	call := func(method, path, actor, body string, status int) string {
		t.Helper()
		response := testutil.Do(r, method, path, actor, body)
		require.Equal(t, status, response.Code, response.Body.String())
		return response.Body.String()
	}
	path := root + "/stock/stocktakes/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", path, "owner", fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST reservation-aware count"}`, fixture.quarantine), 200)
	var lines struct {
		Data []struct {
			ID         string
			LocationID string `json:"location_id"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(call("GET", path+"/lines", "owner", "", 200)), &lines))
	require.Len(t, lines.Data, 2)
	entries := make([]map[string]any, 0, 2)
	for _, line := range lines.Data {
		units := 0
		if line.LocationID == fixture.bucket.String() {
			units = 50
		}
		entries = append(entries, map[string]any{"line_id": line.ID, "counted_units": units})
	}
	count, err := json.Marshal(map[string]any{"expected_version": 0, "counts": entries, "reason": "TEST physical shelf shortage"})
	require.NoError(t, err)
	call("PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "owner", string(count), 200)
	order, input, actor := sourceOrder(t, "pharmacy", fixture)
	store := supplierstock.Store{Owner: supplierstock.Outlet}
	adapter := pharmacy.SupplierStock(platform.Deps{DB: db, Bus: bus})
	scope := supplierstock.Scope{TenantID: order.OwnerTenantID, OrganizationID: fixture.organization, LocationID: fixture.location, StaffID: actor.StaffID, Subject: actor.Subject, Mode: "operator"}
	run := func(action func(*gorm.DB) error) error {
		return db.WithTenant(tenantdb.WithActor(t.Context(), actor.Subject), order.OwnerTenantID.String(), func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_permission','pharmacy.stock.fulfil',true)`, actor.Subject, fixture.organization.String(), fixture.location.String()).Error; err != nil {
				return err
			}
			return store.Frame(tx, scope, func() error { return action(tx) })
		})
	}
	allocation := uuid.Must(uuid.NewV7())
	require.NoError(t, run(func(tx *gorm.DB) error {
		_, _, err := store.Create(tx, allocation, order, input, actor, func(tx *gorm.DB, plan f.AllocationPlan) (f.StockEvidence, error) {
			evidence, err := adapter.Snapshot(tx, plan)
			require.NoError(t, err)
			require.Equal(t, "2030-12-31", evidence.ExpiresOn)
			require.EqualValues(t, 100, evidence.AvailableUnits)
			require.EqualValues(t, 10, evidence.PackSize)
			return evidence, err
		})
		return err
	}))
	// This is a native purpose-policy check; the reviewed gateway tests separately prove actual IAM admission.
	reviewPurpose := func(action func(*gorm.DB) error) error {
		return db.WithTenant(tenantdb.WithActor(t.Context(), "user-reviewed-stock-only"), order.OwnerTenantID.String(), func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-reviewed-stock-only',true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_permission','pharmacy.stocktake.review',true)`, fixture.organization.String(), fixture.location.String()).Error; err != nil {
				return err
			}
			return action(tx)
		})
	}
	require.NoError(t, reviewPurpose(func(tx *gorm.DB) error {
		var held int64
		if err := tx.Raw(`SELECT pharmacy_supplier_reserved(?,?,?,?)`, order.OwnerTenantID, fixture.location, fixture.bucket, fixture.stock).Scan(&held).Error; err != nil {
			return err
		}
		require.EqualValues(t, 60, held, "reviewed corrections must see holds without legacy outlet access")
		return nil
	}))
	for _, location := range []uuid.UUID{fixture.quarantine, *fixture.bucket} {
		err = reviewPurpose(func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
 VALUES(?,?,?,?,?,'correction',1,'TEST reject general correction','TEST-unlinked-reviewed-correction','user-reviewed-stock-only')`, order.OwnerTenantID, fixture.location, uuid.Must(uuid.NewV7()), location, fixture.stock).Error
		})
		var refusal *pgconn.PgError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, "pharmacy_stocktake_movement", refusal.ConstraintName, "reviewed rows must pass complete statement-level movement evidence")
	}
	call("PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "owner", `{"decision":"submit","expected_version":1,"reason":"TEST all shelves counted"}`, 200)
	approvalPath := path + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	approval := `{"decision":"approve","expected_version":2,"extra_units_disposition":"no_extra_units","reason":"TEST independent reservation check"}`
	call("PUT", approvalPath, "reviewer", approval, 409)
	balance := func() int64 {
		t.Helper()
		var result struct {
			OnHandUnits int64 `json:"on_hand_units"`
		}
		require.NoError(t, json.Unmarshal([]byte(call("GET", root+"/stock/balance?location_id="+fixture.bucket.String()+"&batch_id="+fixture.stock.String(), "owner", "", 200)), &result))
		return result.OnHandUnits
	}
	require.EqualValues(t, 100, balance())
	require.NoError(t, run(func(tx *gorm.DB) error {
		held, err := store.Held(tx, order.OwnerTenantID, fixture.location, fixture.stock, *fixture.bucket)
		if err != nil {
			return err
		}
		require.EqualValues(t, 60, held)
		_, _, err = store.Command(tx, order.OwnerTenantID, allocation, uuid.Must(uuid.NewV7()), f.AllocationDecisionInput{ExpectedVersion: 1, Decision: "release", QuantityUnits: 60, Reason: "TEST source released reservation"}, actor, adapter)
		return err
	}))
	call("PUT", approvalPath, "reviewer", approval, 200)
	require.EqualValues(t, 50, balance())
}

func TestStocktakeThroughGlobalAPIPreservesScopeAndRegisterEvidence(t *testing.T) {
	write := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermStockCorrect}
	owner, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"editor": testutil.TenantA, "reviewer": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"editor": write, "reviewer": write, "outsider": write, "foreign": write, "reader": {pharmacy.PermCatalogueRead, pharmacy.PermStockRead}},
		Modules: []platform.Module{pharmacy.New()},
	})
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 15 * time.Second, Upstreams: map[string]string{"pharmacy": server.URL}})
	require.NoError(t, err)
	call := func(method, path, actor, body string, status int) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+actor)
		request.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if actor == "foreign" {
			tenant = testutil.TenantB
		}
		request.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		return response
	}
	organization, outlet := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	root := "/v1/pharmacy/pharmacies/" + organization
	call("PUT", root, "editor", `{"code":"TEST-GATEWAY","name":"TEST Gateway Pharmacy","ownership_type":"independent"}`, 200)
	root += "/outlets/" + outlet
	call("PUT", root, "editor", `{"code":"TEST-MAIN","name":"TEST Main Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`, 200)
	for _, actor := range []string{"editor", "reviewer"} {
		call("PUT", root+"/access/user-"+actor, "editor", `{"level":"editor"}`, 200)
	}
	call("PUT", root+"/access/user-reader", "editor", `{"level":"reader"}`, 200)
	product, location, batch := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	call("PUT", root+"/catalogue/products/"+product, "editor", `{"expected_version":0,"sku":"TEST-GATEWAY-500","brand_name":"TEST Gateway Product","generic_name":"TEST ingredient","dosage_form":"capsule","route":"oral","manufacturer":"TEST Manufacturer","marketing_company":"TEST Marketing","ingredients":[{"name":"TEST ingredient","strength":"500","unit":"mg"}],"pack_size":10,"unit_of_issue":"capsule","currency":"INR","mrp_minor":12500,"status":"active"}`, 200)
	call("PUT", root+"/stock/locations/"+location, "editor", `{"code":"TEST-Q","name":"TEST Quarantine"}`, 200)
	call("PUT", root+"/stock/batches/"+batch, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-GATEWAY-LOT","expires_on":"2030-10-31","mrp_minor":12500}`, product), 200)
	call("PUT", root+"/stock/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":10,"reason":"TEST opening","source_reference":"TEST-GATEWAY-GRN"}`, location, batch), 200)
	id := uuid.Must(uuid.NewV7()).String()
	path := root + "/stock/stocktakes/" + id
	call("PUT", path, "editor", fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST gateway stocktake"}`, location), 200)
	for _, actor := range []string{"outsider", "foreign"} {
		call("GET", path, actor, "", 404)
	}
	lines := call("GET", path+"/lines", "reader", "", 200)
	var buckets struct{ Data []struct{ ID string } }
	require.NoError(t, json.Unmarshal(lines.Body.Bytes(), &buckets))
	require.Len(t, buckets.Data, 1)
	countPath := path + "/counts/" + uuid.Must(uuid.NewV7()).String()
	countInput := fmt.Sprintf(`{"expected_version":0,"counts":[{"line_id":%q,"counted_units":9}],"reason":"TEST physical count"}`, buckets.Data[0].ID)
	call("PUT", countPath, "reader", countInput, 403)
	saved := call("PUT", countPath, "editor", countInput, 200)
	recovered := call("GET", countPath, "reader", "", 200)
	require.JSONEq(t, saved.Body.String(), recovered.Body.String())
	call("PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"decision":"submit","expected_version":1,"reason":"TEST all counted"}`, 200)
	approvalPath := path + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	approvalInput := `{"decision":"approve","expected_version":2,"extra_units_disposition":"no_extra_units","reason":"TEST independent review"}`
	call("PUT", approvalPath, "editor", approvalInput, 409)
	approved := call("PUT", approvalPath, "reviewer", approvalInput, 200)
	require.JSONEq(t, approved.Body.String(), call("PUT", approvalPath, "reviewer", approvalInput, 200).Body.String())
	export := call("GET", path+"/register.csv", "reader", "", 200)
	require.Equal(t, "private, no-store", export.Header().Get("Cache-Control"))
	require.Len(t, export.Header().Values("Cache-Control"), 1)
	require.Equal(t, testutil.TenantA, export.Header().Get("X-Stocktake-Tenant-Id"))
	require.Equal(t, outlet, export.Header().Get("X-Stocktake-Outlet-Id"))
	require.Equal(t, id, export.Header().Get("X-Stocktake-Id"))
	require.Equal(t, "3", export.Header().Get("X-Stocktake-Version"))
	require.Equal(t, "1", export.Header().Get("X-Export-Row-Count"))
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(export.Body.Bytes())), export.Header().Get("X-Export-Sha256"))
	require.Contains(t, export.Header().Get("Content-Disposition"), "stocktake-1-register.csv")
	require.Contains(t, export.Header().Get("Via"), "helivanta-global-api")
	balance := call("GET", root+"/stock/balance?location_id="+location+"&batch_id="+batch, "reader", "", 200)
	var position struct {
		OnHandUnits int64 `json:"on_hand_units"`
	}
	require.NoError(t, json.Unmarshal(balance.Body.Bytes(), &position))
	require.EqualValues(t, 9, position.OnHandUnits)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer editor")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
	mismatch := httptest.NewRecorder()
	gateway.ServeHTTP(mismatch, request)
	require.Equal(t, 409, mismatch.Code, mismatch.Body.String())
}
