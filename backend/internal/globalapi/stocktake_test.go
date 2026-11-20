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
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

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
