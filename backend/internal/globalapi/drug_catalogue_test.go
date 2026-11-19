package globalapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/stockproduct"
)

func TestOutletMedicineImportThroughGlobalAPIPinsIdentityAndRejectsStaleOrUnauthorizedCommands(t *testing.T) {
	perms := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage}
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"editor": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"editor": perms, "reader": perms, "foreign": perms},
		Modules: []platform.Module{drugmaster.New(), pharmacy.New()},
	})
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"pharmacy": server.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string, want int) string {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		require.Equal(t, want, w.Code, w.Body.String())
		return w.Body.String()
	}
	organization, outlet, product := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	root := "/v1/pharmacy/pharmacies/" + organization.String()
	call("PUT", root, "editor", `{"code":"TEST-MASTER","name":"TEST Independent Pharmacy","ownership_type":"independent"}`, 200)
	root += "/outlets/" + outlet.String()
	call("PUT", root, "editor", `{"code":"TEST-MAIN","name":"TEST Main Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`, 200)
	call("PUT", root+"/access/user-editor", "editor", `{"level":"editor"}`, 200)
	call("PUT", root+"/access/user-reader", "editor", `{"level":"reader"}`, 200)
	search := root + "/catalogue/drugmaster?q=Test"
	call("GET", search, "foreign", "", 404)
	var empty struct {
		Data []stockproduct.CatalogueProduct
	}
	require.NoError(t, json.Unmarshal([]byte(call("GET", search, "editor", "", 200)), &empty))
	require.Empty(t, empty.Data)
	publishStockProduct(t, db, "active")
	var found struct {
		Data []stockproduct.CatalogueProduct
	}
	require.NoError(t, json.Unmarshal([]byte(call("GET", search, "editor", "", 200)), &found))
	require.Len(t, found.Data, 1)
	source := found.Data[0]
	command := fmt.Sprintf(`{"version_id":%q,"currency":"INR","mrp_minor":12500}`, source.VersionID)
	path := root + "/catalogue/products/" + product.String()
	call("PUT", path+"/from-drugmaster", "reader", command, 404)
	call("PUT", path+"/from-drugmaster", "foreign", command, 404)
	first := call("PUT", path+"/from-drugmaster", "editor", command, 200)
	require.JSONEq(t, first, call("PUT", path+"/from-drugmaster", "editor", command, 200))
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(first), &result))
	link := result["drug_master"].(map[string]any)
	require.Equal(t, source.VersionID.String(), link["version_id"])
	require.Equal(t, source.FormulationVersionID.String(), link["formulation_version_id"])
	require.Equal(t, "30041010", link["hsn_code"])
	require.Equal(t, "H", link["regulatory_class"])
	call("PUT", root+"/catalogue/products/"+uuid.Must(uuid.NewV7()).String()+"/from-drugmaster", "editor", command, 409)
	// Canonical identity stays pinned while commercial edits remain possible.
	for _, key := range []string{"id", "outlet_id", "version", "regulatory_status", "created_at", "updated_at", "tax", "drug_master"} {
		delete(result, key)
	}
	result["expected_version"] = 1
	result["mrp_minor"] = 13000
	body, err := json.Marshal(result)
	require.NoError(t, err)
	call("PUT", path, "editor", string(body), 200)
	result["expected_version"] = 2
	result["brand_name"] = "TEST altered identity"
	body, err = json.Marshal(result)
	require.NoError(t, err)
	call("PUT", path, "editor", string(body), 409)
	call("PUT", path+"/from-drugmaster", "editor", strings.TrimSuffix(command, "}")+`,"brand_name":"injected"}`, 400)
	publishStockProduct(t, db, "recalled")
	require.JSONEq(t, first, call("PUT", path+"/from-drugmaster", "editor", command, 200), "uncertain import replays original receipt even after commercial edits and recall")
	call("PUT", root+"/catalogue/products/"+uuid.Must(uuid.NewV7()).String()+"/from-drugmaster", "editor", command, 409)
	require.NoError(t, json.Unmarshal([]byte(call("GET", search, "editor", "", 200)), &empty))
	require.Empty(t, empty.Data)
	retained := call("GET", path, "editor", "", 200)
	require.Contains(t, retained, source.VersionID.String(), "source refresh must not rewrite historical identity")
}
