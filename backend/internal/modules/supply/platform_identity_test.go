package supply_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestOwnedOrganisationIdentityRequiresConfiguredAuthorityAndPrivateAccess(t *testing.T) {
	r, _ := partyHarness(t)
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(partyBody(0, "TEST Owned Distributor"), `"counterparty"`, `"owned_organisation"`, 1)
	w := testutil.Do(r, "PUT", root, "owner", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, "GET", root+"/platform-identity", "owner", "")
	require.Equal(t, 503, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"organization_identity_unavailable"`)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	for _, token := range []string{"foreign", "auditor", "manager"} {
		w = testutil.Do(r, "GET", root+"/platform-identity", token, "")
		require.Equal(t, 404, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "TEST Owned Distributor")
	}
}

func activeIdentityFacts() map[string]any {
	return map[string]any{"schema_version": 1, "tenant_id": testutil.TenantA, "product_key": "TEST-product", "organization_id": "TEST-org", "project_id": "TEST-project", "grant_id": "TEST-grant", "state": "active", "verified_at": time.Now().UTC(), "source_revisions": map[string]string{"organization": "101", "metadata": "102", "project": "103", "grant": "104"}}
}

func platformIdentityHarness(t *testing.T, handler http.HandlerFunc) (*gin.Engine, *tenantdb.DB) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	identity, err := organizationidentity.New(server.URL, "TEST-product", server.Client(), identityTestCredential("TEST-machine-token"))
	require.NoError(t, err)
	permissions := []authz.Permission{supply.PermPartyRead, supply.PermPartyWrite, supply.PermOrganizationManage, supply.PermAccessManage}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Roles: accessMembership{}, OrganizationIdentity: identity, Tokens: map[string]string{"owner": testutil.TenantA, "manager": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": permissions, "manager": permissions, "foreign": permissions}, Modules: []platform.Module{supply.New()}})
	return r, db
}

func TestOwnedOrganisationIdentityRechecksAccessAndVersionAfterSourceIO(t *testing.T) {
	for _, change := range []string{"access_revoked", "profile_changed"} {
		t.Run(change, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			r, db := platformIdentityHarness(t, func(w http.ResponseWriter, request *http.Request) {
				close(entered)
				select {
				case <-release:
					_ = json.NewEncoder(w).Encode(activeIdentityFacts())
				case <-request.Context().Done():
				}
			})
			root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
			owned := func(version int, name string) string {
				return strings.Replace(partyBody(version, name), `"counterparty"`, `"owned_organisation"`, 1)
			}
			require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", owned(0, "TEST Identity Race")).Code)
			require.Equal(t, 200, testutil.Do(r, "PUT", root+"/access/user-manager/administrator", "owner", "").Code)
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			request := httptest.NewRequest(http.MethodGet, root+"/platform-identity", nil).WithContext(ctx)
			request.Header.Set("Authorization", "Bearer owner")
			completed := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); r.ServeHTTP(w, request); completed <- w }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("identity verification did not reach the source")
			}
			method, path, body, expectedMutation, expectedResult := http.MethodDelete, root+"/access/user-owner/administrator", "", 204, 404
			if change == "profile_changed" {
				method, path, body, expectedMutation, expectedResult = http.MethodPut, root, owned(1, "TEST Changed While Verifying"), 200, 409
			}
			mutationCtx, stop := context.WithTimeout(t.Context(), time.Second)
			mutation := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(mutationCtx)
			mutation.Header.Set("Authorization", "Bearer manager")
			mutation.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			r.ServeHTTP(result, mutation)
			stop()
			close(release)
			require.Equal(t, expectedMutation, result.Code, result.Body.String(), "source I/O must not hold database locks")
			select {
			case response := <-completed:
				require.Equal(t, expectedResult, response.Code, response.Body.String())
				require.NotContains(t, response.Body.String(), "TEST-org")
			case <-ctx.Done():
				t.Fatal("identity response did not complete")
			}
			var count int64
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Table("audit_events").Where("tenant_id=? AND action='platform_identity_read'", testutil.TenantA).Count(&count).Error
			}))
			require.Zero(t, count, "a denied observation must not be recorded as a successful identity read")
		})
	}
}

func TestOwnedOrganisationIdentityRejectsInactiveAndUncertainSources(t *testing.T) {
	for _, state := range []string{"inactive", "unknown"} {
		t.Run(state, func(t *testing.T) {
			r, _ := platformIdentityHarness(t, func(w http.ResponseWriter, r *http.Request) {
				facts := activeIdentityFacts()
				facts["state"] = state
				_ = json.NewEncoder(w).Encode(facts)
			})
			root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
			body := strings.Replace(partyBody(0, "TEST Inactive Identity"), `"counterparty"`, `"owned_organisation"`, 1)
			require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", body).Code)
			result := testutil.Do(r, "GET", root+"/platform-identity", "owner", "")
			expected := 503
			if state == "inactive" {
				expected = 409
			}
			require.Equal(t, expected, result.Code, result.Body.String())
			require.NotContains(t, result.Body.String(), "TEST-org")
		})
	}
}

type identityTestCredential string

func (v identityTestCredential) Token(context.Context) (string, error) { return string(v), nil }

func TestOwnedOrganisationIdentityUsesOnlySessionTenantAndMinimizedFreshFacts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/orgs/"+testutil.TenantA+"/identity" || r.Header.Get("Authorization") != "Bearer TEST-machine-token" {
			t.Error("identity request was not bound to the session and dedicated machine")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"schema_version": 1, "tenant_id": testutil.TenantA, "product_key": "TEST-product", "organization_id": "TEST-org", "project_id": "TEST-project", "grant_id": "TEST-grant", "state": "active", "verified_at": time.Now().UTC(), "source_revisions": map[string]string{"organization": "101", "metadata": "102", "project": "103", "grant": "104"}, "privateBilling": "PRIVATE_NOT_FOR_BROWSER"})
	}))
	defer server.Close()
	identity, err := organizationidentity.New(server.URL, "TEST-product", server.Client(), identityTestCredential("TEST-machine-token"))
	require.NoError(t, err)
	permissions := []authz.Permission{supply.PermPartyRead, supply.PermPartyWrite, supply.PermOrganizationManage}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Roles: accessMembership{}, OrganizationIdentity: identity, Tokens: map[string]string{"owner": testutil.TenantA, "foreign": testutil.TenantB, "other": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": permissions, "foreign": permissions, "other": permissions}, Modules: []platform.Module{supply.New()}})
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	body := strings.Replace(partyBody(0, "TEST Canonical Owner"), `"counterparty"`, `"owned_organisation"`, 1)
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", body).Code)
	for _, token := range []string{"foreign", "other"} {
		w := testutil.Do(r, "GET", root+"/platform-identity", token, "")
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	require.Zero(t, calls.Load(), "private denial must happen before any source lookup")
	for range 2 {
		w := testutil.Do(r, "GET", root+"/platform-identity", "owner", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"party_version":1`)
		require.Contains(t, w.Body.String(), `"trading_approved":false`)
		require.Contains(t, w.Body.String(), `"organization_id":"TEST-org"`)
		require.NotContains(t, w.Body.String(), "PRIVATE_NOT_FOR_BROWSER")
		require.NotContains(t, w.Body.String(), "TEST-machine-token")
	}
	require.Equal(t, int32(2), calls.Load(), "live observations must not be cached")
	w := testutil.Do(r, "GET", root+"/platform-identity?tenant_id="+testutil.TenantB, "owner", "")
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Equal(t, int32(2), calls.Load())
}
