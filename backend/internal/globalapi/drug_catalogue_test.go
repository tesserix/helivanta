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
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
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
	command := fmt.Sprintf(`{"version_id":%q,"formulation_version_id":%q,"currency":"INR","mrp_minor":12500}`, source.VersionID, source.FormulationVersionID)
	path := root + "/catalogue/products/" + product.String()
	call("PUT", path+"/from-drugmaster", "reader", command, 404)
	call("PUT", path+"/from-drugmaster", "foreign", command, 404)
	call("PUT", path+"/from-drugmaster", "editor", fmt.Sprintf(`{"version_id":%q,"currency":"INR","mrp_minor":12500}`, source.VersionID), 400)
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
	// A newly published pack version can be added without rewriting the old
	// product's composition/pack evidence or reusing its local SKU.
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	stager, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugStagerDSN(t, database))
	require.NoError(t, err)
	defer func() { require.NoError(t, stager.Close()) }()
	approver, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugApproverDSN(t, database))
	require.NoError(t, err)
	defer func() { require.NoError(t, approver.Close()) }()
	location, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	update := []byte(fmt.Sprintf(`{"country":"IN","source":{"name":"TEST-source","licence_ref":"TEST-LICENCE","version":"TEST-pack-v2"},"products":[{"code":"TEST-PACK","formulation":"TEST-CAPSULE","valid_from":%q,"brand_name":"TEST Stock","manufacturer":"TEST Manufacturer","pack_size":20,"unit_of_issue":"capsule","gtin":"8901234567890","hsn_code":"30041010","status":"active"}]}`, time.Now().In(location).Format(time.DateOnly)))
	release, err := drugmaster.Stage(t.Context(), stager, update, "TEST-catalogue-author")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), approver, release, update, "TEST-catalogue-reviewer", true, "TEST revised pack independently reviewed"))
	require.NoError(t, json.Unmarshal([]byte(call("GET", search, "editor", "", 200)), &found))
	require.Len(t, found.Data, 1)
	require.NotEqual(t, source.VersionID, found.Data[0].VersionID)
	nextCommand := fmt.Sprintf(`{"version_id":%q,"formulation_version_id":%q,"currency":"INR","mrp_minor":25000}`, found.Data[0].VersionID, found.Data[0].FormulationVersionID)
	revised := call("PUT", root+"/catalogue/products/"+uuid.Must(uuid.NewV7()).String()+"/from-drugmaster", "editor", nextCommand, 200)
	require.Contains(t, revised, `"pack_size":20`)
	require.Contains(t, call("GET", path, "editor", "", 200), `"pack_size":10`)
	// A formulation-only update leaves the product version unchanged. A
	// selection made before it must fail, rather than substituting new facts.
	selected := found.Data[0]
	changedForm := []byte(fmt.Sprintf(`{"country":"IN","source":{"name":"TEST-source","licence_ref":"TEST-LICENCE","version":"TEST-formulation-v3"},"formulations":[{"code":"TEST-CAPSULE","valid_from":%q,"label":"TEST 500mg capsule","dose_form":"capsule","route":"oral","regulatory_class":"H1","ndps":false,"teleconsult_prescribable":false,"ingredients":[{"molecule":"Testmolecule","strength_value":500,"strength_unit":"mg"}]}]}`, time.Now().In(location).Format(time.DateOnly)))
	changed, err := drugmaster.Stage(t.Context(), stager, changedForm, "TEST-catalogue-author")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), approver, changed, changedForm, "TEST-catalogue-reviewer", true, "TEST formulation independently reviewed"))
	require.NoError(t, json.Unmarshal([]byte(call("GET", search, "editor", "", 200)), &found))
	require.Len(t, found.Data, 1)
	require.Equal(t, selected.VersionID, found.Data[0].VersionID)
	require.NotEqual(t, selected.FormulationVersionID, found.Data[0].FormulationVersionID)
	call("PUT", root+"/catalogue/products/"+uuid.Must(uuid.NewV7()).String()+"/from-drugmaster", "editor", nextCommand, 409)
	formCommand := fmt.Sprintf(`{"version_id":%q,"formulation_version_id":%q,"currency":"INR","mrp_minor":25000}`, found.Data[0].VersionID, found.Data[0].FormulationVersionID)
	updatedForm := call("PUT", root+"/catalogue/products/"+uuid.Must(uuid.NewV7()).String()+"/from-drugmaster", "editor", formCommand, 200)
	require.Contains(t, updatedForm, `"regulatory_class":"H1"`)
	publishStockProduct(t, db, "recalled")
	require.JSONEq(t, first, call("PUT", path+"/from-drugmaster", "editor", command, 200), "uncertain import replays original receipt even after commercial edits and recall")
	call("PUT", root+"/catalogue/products/"+uuid.Must(uuid.NewV7()).String()+"/from-drugmaster", "editor", command, 409)
	require.NoError(t, json.Unmarshal([]byte(call("GET", search, "editor", "", 200)), &empty))
	require.Empty(t, empty.Data)
	retained := call("GET", path, "editor", "", 200)
	require.Contains(t, retained, source.VersionID.String(), "source refresh must not rewrite historical identity")
}
