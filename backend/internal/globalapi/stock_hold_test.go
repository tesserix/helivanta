package globalapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestStockHoldThroughGlobalAPIPreservesReservationsAndRecovery(t *testing.T) {
	perms := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermStockRead, pharmacy.PermStockReceive, pharmacy.PermStockCorrect, pharmacy.PermStockRelease, pharmacy.PermStockQuarantine, "pharmacy.tax.manage", "pharmacy.schedule.declare", "pharmacy.schedule.review"}
	owner, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA, "sibling": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": perms, "reviewer": perms, "sibling": perms, "foreign": perms}, Modules: []platform.Module{pharmacy.New()}})
	fixture := seedSupplierStock(t, owner, db, "pharmacy")
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 15 * time.Second, Upstreams: map[string]string{"pharmacy": server.URL}})
	require.NoError(t, err)
	root := "/v1/pharmacy/pharmacies/" + fixture.organization.String() + "/outlets/" + fixture.location.String()
	request := func(method, path, actor, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if actor == "foreign" {
			tenant = testutil.TenantB
		}
		req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		return response
	}
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
		_, _, err := store.Create(tx, allocation, order, input, actor, adapter.Snapshot)
		return err
	}))
	body := func(units int) string {
		return fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"quarantine_location_id":%q,"quantity_units":%d,"reason":"TEST quarantine physical units","physical_disposition":"moved_to_quarantine"}`, fixture.stock, *fixture.bucket, fixture.quarantine, units)
	}
	paths := []string{root + "/stock/holds/" + uuid.Must(uuid.NewV7()).String(), root + "/stock/holds/" + uuid.Must(uuid.NewV7()).String()}
	var wg sync.WaitGroup
	results := make([]*httptest.ResponseRecorder, 2)
	for i := range paths {
		wg.Go(func() { results[i] = request("PUT", paths[i], "owner", body(30)) })
	}
	wg.Wait()
	var accepted int
	if results[0].Code != 200 {
		accepted = 1
	}
	rejected := 1 - accepted
	require.Equal(t, 200, results[accepted].Code, results[accepted].Body.String())
	require.Equal(t, 409, results[rejected].Code, results[rejected].Body.String())
	require.Equal(t, 404, request("GET", paths[rejected], "owner", "").Code)
	original := results[accepted].Body.String()
	recovered := request("GET", paths[accepted], "owner", "")
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, original, recovered.Body.String())
	require.Contains(t, recovered.Header().Get("Via"), "helivanta-global-api")
	require.Equal(t, "private, no-store", recovered.Header().Get("Cache-Control"))
	for _, other := range []string{"sibling", "foreign"} {
		require.Equal(t, 404, request("GET", paths[accepted], other, "").Code)
		require.Equal(t, 404, request("PUT", paths[accepted], other, body(30)).Code)
	}
	require.NoError(t, run(func(tx *gorm.DB) error {
		held, err := store.Held(tx, order.OwnerTenantID, fixture.location, fixture.stock, *fixture.bucket)
		require.NoError(t, err)
		require.EqualValues(t, 60, held)
		_, _, err = store.Command(tx, order.OwnerTenantID, allocation, uuid.Must(uuid.NewV7()), f.AllocationDecisionInput{ExpectedVersion: 1, Decision: "release", QuantityUnits: 60, Reason: "TEST source released reservation"}, actor, adapter)
		return err
	}))
	held := request("PUT", paths[rejected], "owner", body(70))
	require.Equal(t, 200, held.Code, held.Body.String())
	balance := request("GET", root+"/stock/balance?location_id="+fixture.bucket.String()+"&batch_id="+fixture.stock.String(), "owner", "")
	require.Equal(t, 200, balance.Code, balance.Body.String())
	var units struct {
		OnHand int64 `json:"on_hand_units"`
	}
	require.NoError(t, json.Unmarshal(balance.Body.Bytes(), &units))
	require.Zero(t, units.OnHand)
	replay := request("PUT", paths[accepted], "owner", body(30))
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, original, replay.Body.String())
}
