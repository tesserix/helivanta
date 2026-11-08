package globalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func publishStockProduct(t *testing.T, db *tenantdb.DB, status string) {
	t.Helper()
	var name string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&name).Error }))
	stager, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugStagerDSN(t, name))
	require.NoError(t, err)
	defer func() { require.NoError(t, stager.Close()) }()
	approver, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugApproverDSN(t, name))
	require.NoError(t, err)
	defer func() { require.NoError(t, approver.Close()) }()
	from := "2020-01-01"
	if status != "active" {
		location, err := time.LoadLocation("Asia/Kolkata")
		require.NoError(t, err)
		from = time.Now().In(location).Format(time.DateOnly)
	}
	raw := []byte(fmt.Sprintf(`{"country":"IN","source":{"name":"TEST-source","licence_ref":"TEST-LICENCE","version":%q},"molecules":[{"inn_name":"Testmolecule"}],"formulations":[{"code":"TEST-CAPSULE","valid_from":"2020-01-01","label":"TEST 500 mg capsule","dose_form":"capsule","route":"oral","regulatory_class":"H","ndps":false,"teleconsult_prescribable":true,"ingredients":[{"molecule":"Testmolecule","strength_value":500,"strength_unit":"mg"}]}],"products":[{"code":"TEST-PACK","formulation":"TEST-CAPSULE","valid_from":%q,"brand_name":"TEST Stock","manufacturer":"TEST Manufacturer","pack_size":10,"unit_of_issue":"capsule","gtin":"8901234567890","hsn_code":"30041010","status":%q}]}`, status, from, status))
	id, err := drugmaster.Stage(t.Context(), stager, raw, "TEST-catalogue-author")
	require.NoError(t, err)
	require.NoError(t, drugmaster.Decide(t.Context(), approver, id, raw, "TEST-catalogue-reviewer", true, "TEST independently reviewed source"))
}

func TestWholesaleIdentityCheckThroughGlobalAPIUsesCurrentCatalogueOwner(t *testing.T) {
	perms := []authz.Permission{supply.PermPartyWrite, supply.PermOrganizationManage, supply.PermPremiseWrite, supply.PermOperatingWrite, supply.PermOperatingRead, supply.PermAccessManage, supply.PermStockReview, drugmaster.PermCatalogueRead}
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "foreign": testutil.TenantB, "reviewer": testutil.TenantA}, Roles: stockQualityMembership{}, Perms: map[string][]authz.Permission{"owner": perms, "foreign": perms, "reviewer": perms}, Modules: []platform.Module{drugmaster.New(), supply.New()}})
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"supply": server.URL, "platform": server.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string, status int) string {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		return response.Body.String()
	}
	publishStockProduct(t, db, "active")
	var product struct {
		VersionID uuid.UUID `json:"version_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(call("GET", "/v1/drugmaster/products/by-gtin/8901234567890", "owner", "", 200)), &product))
	party, premise, receipt, check := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	root := "/v1/supply/parties/" + party.String()
	call("PUT", root, "owner", `{"expected_version":0,"code":"TEST-STOCK","record_kind":"owned_organisation","legal_name":"TEST independent distributor","trading_name":"TEST Trade","business_kind":"distributor","country_code":"IN","jurisdiction":"IN-MH","registration_number":"TEST-REG-001","corporate_group":"","website":"","default_credit_days":30,"contacts":[],"addresses":[],"tax_registrations":[]}`, 200)
	call("PUT", root+"/premises/"+premise.String(), "owner", `{"expected_version":0,"code":"TEST-PUNE","name":"TEST Pune warehouse","kind":"warehouse","line1":"TEST Building","line2":"","city":"Pune","region":"IN-MH","postal_code":"411001","country_code":"IN","time_zone":"Asia/Kolkata","status":"active"}`, 200)

	scope := authz.OperatingLocationScope{TenantID: testutil.TenantA, EmployerKind: "supplier", EmployerID: party, LocationID: premise}
	guard := supply.New().OperatingLocations(db)
	_, unguarded := guard.CurrentOperatingLocation(t.Context(), scope)
	require.ErrorIs(t, unguarded, authz.ErrScopedAuthorizationUnavailable)
	var location authz.OperatingLocationReference
	checkLocation := func(request authz.OperatingLocationScope) error {
		return db.WithGuardedTenantCommand(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			var err error
			location, err = guard.CurrentOperatingLocation(ctx, request)
			return err
		})
	}
	require.NoError(t, checkLocation(scope))
	require.Equal(t, scope, location.Scope)
	require.Equal(t, int64(1), location.EmployerVersion)
	require.Equal(t, int64(1), location.LocationVersion)
	require.Equal(t, "IN", location.Country)
	wrong := scope
	wrong.EmployerKind = "pharmacy"
	require.ErrorIs(t, checkLocation(wrong), authz.ErrScopedCommandDenied)
	wrong = scope
	wrong.EmployerID = uuid.Must(uuid.NewV7())
	require.ErrorIs(t, checkLocation(wrong), authz.ErrScopedCommandDenied)
	require.Zero(t, location)
	stock := root + "/operating-locations/" + premise.String() + "/stock/receipts/" + receipt.String()
	call("PUT", stock, "owner", fmt.Sprintf(`{"product_version_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-12-31","unit_code":"pack","quantity_units":100,"source_reference":"TEST-GRN:1","reason":"TEST counted 100 packs"}`, product.VersionID), http.StatusCreated)
	path := stock + "/identity-checks/" + check.String()
	input := fmt.Sprintf(`{"expected_version":0,"product_version_id":%q,"unit_kind":"pack","expected_pack_size":10,"expected_base_units":1000,"reason":"TEST physical pack and label match"}`, product.VersionID)
	first := call("PUT", path, "owner", input, 201)
	require.Contains(t, first, `"quantity_base_units":1000`)
	require.Contains(t, first, `"unit_of_issue":"capsule"`)
	require.Contains(t, first, `"regulatory_class":"H"`)
	require.JSONEq(t, first, call("GET", path, "owner", "", 200))
	require.JSONEq(t, first, call("PUT", path, "owner", input, 200))
	call("GET", path, "foreign", "", 404)

	grantPath := root + "/grants/" + uuid.Must(uuid.NewV7()).String()
	grantBody := fmt.Sprintf(`{"expected_version":0,"subject":"user-reviewer","role":"inventory","premise_id":%q,"valid_from":%q,"expires_at":%q,"status":"active","reason":"TEST exact warehouse reviewer"}`, premise, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339))
	call("PUT", grantPath, "owner", grantBody, 200)
	qualityPath := stock + "/quality-decisions/" + uuid.Must(uuid.NewV7()).String()
	qualityInput := fmt.Sprintf(`{"expected_version":0,"identity_check_id":%q,"identity_version":1,"decision":"release","reason":"TEST independent seal, storage and expiry inspection"}`, check)
	call("PUT", qualityPath, "owner", qualityInput, 409)
	released := call("PUT", qualityPath, "reviewer", qualityInput, 201)
	require.Contains(t, released, `"released_base_units":1000`)
	require.Contains(t, released, `"status":"released"`)
	require.JSONEq(t, released, call("PUT", qualityPath, "reviewer", qualityInput, 200))
	require.JSONEq(t, released, call("GET", qualityPath, "owner", "", 200))
	call("GET", qualityPath, "foreign", "", 404)
	call("PUT", qualityPath, "reviewer", strings.Replace(qualityInput, "TEST independent", "TEST altered", 1), 409)
	inspection := call("GET", stock+"/inspection", "owner", "", 200)
	require.Contains(t, inspection, `"quality":`)
	require.Contains(t, inspection, `"identity":`)
	call("GET", stock+"/inspection", "foreign", "", 404)
	holdPath := stock + "/quality-decisions/" + uuid.Must(uuid.NewV7()).String()
	holdInput := strings.Replace(strings.Replace(qualityInput, `"expected_version":0`, `"expected_version":1`, 1), `"decision":"release"`, `"decision":"hold"`, 1)
	held := call("PUT", holdPath, "owner", holdInput, 201)
	require.Contains(t, held, `"status":"held"`)
	require.Contains(t, held, `"released_base_units":0`)
	require.JSONEq(t, held, call("GET", holdPath, "reviewer", "", 200))
	stalePath := stock + "/quality-decisions/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", stalePath, "reviewer", qualityInput, 409)
	call("GET", stalePath, "reviewer", "", 404)

	for _, failure := range []struct{ name, table, condition string }{
		{"audit", "audit_events", "resource_type<>'supply_stock_quality_decisions'"},
		{"outbox", "outbox_events", "subject<>'helivanta.in.supply.stock_quality_recorded.v1'"},
	} {
		require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_quality_" + failure.name, SQL: "ALTER TABLE " + failure.table + " ADD CONSTRAINT test_quality_failure CHECK(" + failure.condition + ")NOT VALID"}}))
		failedPath := stock + "/quality-decisions/" + uuid.Must(uuid.NewV7()).String()
		call("PUT", failedPath, "reviewer", strings.Replace(qualityInput, `"expected_version":0`, `"expected_version":2`, 1), 503)
		call("GET", failedPath, "reviewer", "", 404)
		unchanged := call("GET", stock+"/inspection", "owner", "", 200)
		require.Contains(t, unchanged, `"status":"held"`)
		require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_quality_" + failure.name + "_restore", SQL: "ALTER TABLE " + failure.table + " DROP CONSTRAINT test_quality_failure"}}))
	}
	publishStockProduct(t, db, "recalled")
	require.JSONEq(t, released, call("GET", qualityPath, "reviewer", "", 200))
	call("PUT", stock+"/quality-decisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", strings.Replace(qualityInput, `"expected_version":0`, `"expected_version":2`, 1), 409)
	// A recall cannot prevent an attributable safety hold.
	call("PUT", stock+"/quality-decisions/"+uuid.Must(uuid.NewV7()).String(), "owner", strings.Replace(holdInput, `"expected_version":1`, `"expected_version":2`, 1), 201)
	call("PUT", grantPath, "owner", strings.Replace(strings.Replace(grantBody, `"expected_version":0`, `"expected_version":1`, 1), `"status":"active"`, `"status":"revoked"`, 1), 200)
	call("GET", qualityPath, "reviewer", "", 404)
	call("GET", stock+"/inspection", "reviewer", "", 404)
	require.JSONEq(t, first, call("GET", path, "owner", "", 200))
	call("PUT", stock+"/identity-checks/"+uuid.Must(uuid.NewV7()).String(), "owner", strings.Replace(input, `"expected_version":0`, `"expected_version":1`, 1), 409)
}

type stockQualityMembership struct{}

func (stockQualityMembership) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	if subject == "user-owner" || subject == "user-reviewer" {
		return []authz.RoleBinding{{TenantID: testutil.TenantA, Role: authz.RoleSupplyInventory}}, nil
	}
	return nil, nil
}
