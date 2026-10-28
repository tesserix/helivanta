package globalapi

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestWorkforceGlobalAPIAuthority(t *testing.T) {
	ctx := t.Context()
	fgaURL, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	fgaProxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(fgaURL))
	t.Cleanup(fgaProxy.Close)
	fga, err := authz.NewClient(ctx, fgaProxy.URL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	module := workforce.New()
	registry := platform.NewRegistry()
	require.NoError(t, registry.Register(module))
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, platform.ReconcileTenant(ctx, registry, fga, tenant))
		require.NoError(t, fga.GrantTenantRole(ctx, tenant, authz.RoleTenantAdmin))
		require.NoError(t, fga.GrantTenantRole(ctx, tenant, authz.RoleStaffMember))
	}
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "admin", authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "reader", authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "reviewer", authz.RoleTenantAdmin))
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantB, "foreign", authz.RoleTenantAdmin))
	signer, verifier := testutil.NewSessionSignerForTest(t)
	owner, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Verifier: authn.NewSessionVerifier(verifier), Membership: fga, Resolver: fga, Roles: fga,
		Modules: []platform.Module{module},
	})
	upstream := httptest.NewServer(owner)
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"workforce": upstream.URL}})
	require.NoError(t, err)
	tokens := map[string]string{}
	for subject, tenant := range map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB} {
		tokens[subject], err = signer.Mint(subject, tenant, time.Now(), time.Now().Add(time.Minute))
		require.NoError(t, err)
	}
	call := func(method, path, subject, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tokens[subject])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", testutil.TenantA)
		req.Header.Set("X-User-ID", "admin")
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		require.Equal(t, status, response.Code, "%s %s: %s", method, path, response.Body.String())
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		return response
	}
	hospital := "/v1/workforce/hospitals/" + uuid.NewString()
	sibling := "/v1/workforce/hospitals/" + uuid.NewString()
	call("PUT", hospital, "admin", `{"name":"Synthetic Gateway Hospital"}`, http.StatusOK)
	call("PUT", sibling, "admin", `{"name":"Synthetic Sibling Hospital"}`, http.StatusOK)
	staff := hospital + "/staff/" + uuid.NewString()
	body := `{"employee_code":"GW-01","display_name":"Synthetic Gateway Staff","job_category":"nurse","employment_status":"active"}`
	call("PUT", staff, "admin", body, http.StatusNotFound)
	call("PUT", hospital+"/access/admin", "admin", `{"level":"manager"}`, http.StatusOK)
	created := call("PUT", staff, "admin", body, http.StatusOK)
	replayed := call("PUT", staff, "admin", body, http.StatusOK)
	require.JSONEq(t, created.Body.String(), replayed.Body.String())
	require.Contains(t, call("GET", staff, "admin", "", http.StatusOK).Body.String(), "Synthetic Gateway Staff")
	empty := call("GET", sibling+"/staff", "admin", "", http.StatusOK)
	require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, empty.Body.String())
	call("GET", sibling+"/staff/"+uuid.NewString(), "admin", "", http.StatusNotFound)
	call("GET", staff, "foreign", "", http.StatusNotFound)
	call("GET", staff, "", "", http.StatusUnauthorized)
	call("PUT", hospital+"/access/reader", "admin", `{"level":"reader"}`, http.StatusOK)
	call("GET", staff, "reader", "", http.StatusOK)
	call("PUT", staff, "reader", body, http.StatusNotFound)
	call("PUT", hospital, "reader", `{"name":"Forbidden Rename"}`, http.StatusForbidden)

	link := staff + "/login-links/" + uuid.Must(uuid.NewV7()).String()
	proposal := `{"subject":"reader","reason":"Verified synthetic account"}`
	call("PUT", hospital+"/access/reader", "admin", `{"level":"manager"}`, http.StatusOK)
	call("PUT", link, "reader", proposal, http.StatusForbidden)
	call("PUT", link, "foreign", proposal, http.StatusNotFound)
	firstLink := call("PUT", link, "admin", proposal, http.StatusOK)
	replayedLink := call("PUT", link, "admin", proposal, http.StatusOK)
	require.JSONEq(t, firstLink.Body.String(), replayedLink.Body.String())
	decision := `{"expected_version":1,"decision":"approve","reason":"Independent synthetic review"}`
	call("POST", link+"/decision", "admin", decision, http.StatusForbidden)
	call("POST", link+"/decision", "reviewer", decision, http.StatusNotFound)
	call("PUT", hospital+"/access/reviewer", "admin", `{"level":"manager"}`, http.StatusOK)
	approved := call("POST", link+"/decision", "reviewer", decision, http.StatusOK)
	require.Contains(t, approved.Body.String(), `"status":"active"`)
	call("GET", link+"/history", "reader", "", http.StatusForbidden)
	call("GET", link+"/history", "foreign", "", http.StatusNotFound)
	call("PUT", staff, "admin", strings.Replace(body, `"active"`, `"inactive"`, 1), http.StatusOK)
	history := call("GET", link+"/history", "reviewer", "", http.StatusOK)
	require.Contains(t, history.Body.String(), `"status":"revoked"`)
	require.Contains(t, history.Body.String(), `"version":3`)
	call("DELETE", hospital+"/access/reviewer", "admin", "", http.StatusNoContent)
	call("GET", link+"/history", "reviewer", "", http.StatusNotFound)
	call("DELETE", hospital+"/access/reader", "admin", "", http.StatusNoContent)
	call("GET", staff, "reader", "", http.StatusNotFound)
	require.NoError(t, fga.RevokeRole(ctx, testutil.TenantA, "admin", authz.RoleTenantAdmin))
	call("GET", staff, "admin", "", http.StatusForbidden)
	require.NoError(t, fga.GrantRole(ctx, testutil.TenantA, "admin", authz.RoleTenantAdmin))
	call("GET", staff, "admin", "", http.StatusOK)
	fgaProxy.Close()
	unavailable := call("GET", staff, "admin", "", http.StatusServiceUnavailable)
	require.Contains(t, unavailable.Body.String(), `"error":"authz_unavailable"`)
	require.NotContains(t, unavailable.Body.String(), "Synthetic Gateway Staff")
	upstream.Close()
	failedOwner := call("GET", staff, "admin", "", http.StatusBadGateway)
	require.Contains(t, failedOwner.Body.String(), `"error":"upstream_unavailable"`)
}
