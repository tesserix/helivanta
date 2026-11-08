package globalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestSupplierGrantDraftThroughActualOwnerAndGraph(t *testing.T) {
	ctx := t.Context()
	address, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(address))
	t.Cleanup(proxy.Close)
	graph, err := authz.NewClient(ctx, proxy.URL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	modules := []platform.Module{iam.New(nil), pharmacy.New()}
	registry := platform.NewRegistry()
	for _, module := range modules {
		require.NoError(t, registry.Register(module))
	}
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, platform.ReconcileTenant(ctx, registry, graph, tenant))
		require.NoError(t, graph.GrantTenantRole(ctx, tenant, authz.RoleTenantAdmin))
	}
	require.NoError(t, graph.GrantRole(ctx, testutil.TenantA, "supplier-admin", authz.RoleTenantAdmin))
	require.NoError(t, graph.GrantRole(ctx, testutil.TenantB, "other-admin", authz.RoleTenantAdmin))
	signer, verifier := testutil.NewSessionSignerForTest(t)
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Verifier: authn.NewSessionVerifier(verifier), Membership: graph, Resolver: graph, Roles: graph, Writer: graph, SessionSigner: signer, SessionTTL: testutil.TestSessionTTL, Modules: modules})
	for _, principal := range []struct{ tenant, subject string }{{testutil.TenantA, "supplier-admin"}, {testutil.TenantB, "other-admin"}} {
		require.NoError(t, db.WithTenant(ctx, principal.tenant, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,?,'tenant_admin')`, principal.tenant, principal.subject).Error
		}))
	}
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": server.URL, "pharmacy": server.URL}})
	require.NoError(t, err)
	token, err := signer.Mint("supplier-admin", testutil.TenantA, time.Now(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	other, err := signer.Mint("other-admin", testutil.TenantB, time.Now(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	call := func(method, path, credential, tenant, subject, body string, status int) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		request.Header.Set("X-Helivanta-Expected-Subject", subject)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		return response
	}
	pharmacyID, outletID := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	call("PUT", "/v1/pharmacy/pharmacies/"+pharmacyID, token, testutil.TenantA, "supplier-admin", `{"code":"QA-SUPPLIER","name":"Synthetic independent supplier","ownership_type":"independent"}`, 200)
	call("PUT", "/v1/pharmacy/pharmacies/"+pharmacyID+"/outlets/"+outletID, token, testutil.TenantA, "supplier-admin", `{"code":"QA-PUNE","name":"Synthetic Pune supplying outlet","country_code":"IN","timezone":"Asia/Kolkata"}`, 200)
	draftID := uuid.Must(uuid.NewV7()).String()
	input := fmt.Sprintf(`{"owner_product":"pharmacy","owner_organization_id":%q,"source_location_id":%q,"relationship_id":%q,"relationship_version":1,"recipient_kind":"subject","recipient_tenant_id":%q,"recipient_hospital_id":%q,"recipient_subject":"synthetic-hospital-buyer","template_key":"partner.catalogue_viewer.v1","source_jurisdiction":"IN-MH","destination_jurisdiction":"IN-OD","valid_from":%q,"expires_at":%q,"reason":"Synthetic exact receiving-party intent"}`, pharmacyID, outletID, uuid.Must(uuid.NewV7()).String(), testutil.TenantB, uuid.NewString(), time.Now().UTC().Format(time.RFC3339), time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	path := "/v1/iam/partner-grant-drafts/" + draftID
	first := call("PUT", path, token, testutil.TenantA, "supplier-admin", input, 200)
	require.JSONEq(t, first.Body.String(), call("GET", path, token, testutil.TenantA, "supplier-admin", "", 200).Body.String())
	call("GET", path, other, testutil.TenantB, "other-admin", "", 404)
	call("GET", path, token, testutil.TenantB, "supplier-admin", "", 409)
	assertSupplierGrantDraftBrowser(t, gateway, token, pharmacyID, outletID)
	proxy.Close()
	unavailable := call("GET", path, token, testutil.TenantA, "supplier-admin", "", 503)
	require.NotContains(t, unavailable.Body.String(), "Synthetic exact receiving-party intent")
}

func assertSupplierGrantDraftBrowser(t *testing.T, gateway http.Handler, token, pharmacyID, outletID string) {
	t.Helper()
	app := os.Getenv("HELIVANTA_SUPPLIER_GRANT_BROWSER_URL")
	if app == "" {
		return
	}
	server := httptest.NewServer(gateway)
	defer server.Close()
	raw, err := json.Marshal(map[string]string{"app_url": app, "gateway_url": server.URL, "token": token, "tenant_id": testutil.TenantA, "subject": "supplier-admin", "pharmacy_id": pharmacyID, "outlet_id": outletID, "recipient_tenant_id": testutil.TenantB, "hospital_id": uuid.NewString(), "group_id": uuid.Must(uuid.NewV7()).String(), "relationship_id": uuid.Must(uuid.NewV7()).String()})
	require.NoError(t, err)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts/verify-supplier-grant-owner.mjs"))
	command.Dir = root
	command.Stdin = bytes.NewReader(raw)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "supplier access browser acceptance: %s", output)
	t.Log(string(output))
}
