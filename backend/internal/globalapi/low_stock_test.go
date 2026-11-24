package globalapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/release"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type lowStockGatewayFixture struct {
	handler                                  http.Handler
	db                                       *tenantdb.DB
	pharmacy, outlet, sibling, product, root string
	tokens                                   map[string]string
	call                                     func(string, string, string, string, int) *httptest.ResponseRecorder
}

func newLowStockGatewayFixture(t *testing.T, ownership string) lowStockGatewayFixture {
	t.Helper()
	policy, err := release.DevelopmentFixture("dev", "http://auth.tesserix.localhost:20080", "true", "team_accounts", "pharmacy_setup", "pharmacy_low_stock")
	require.NoError(t, err)
	require.NoError(t, policy.Refresh(t.Context()))
	go policy.Run(t.Context())
	var enforce atomic.Bool
	admin := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermCatalogueWrite, pharmacy.PermAccessManage, pharmacy.PermStockRead}
	reader := []authz.Permission{pharmacy.PermCatalogueRead, pharmacy.PermStockRead}
	tenants := map[string]string{"owner": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB, "denied": testutil.TenantA}
	signer, verifier := testutil.NewSessionSignerForTest(t)
	owner, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:        tenants,
		Perms:         map[string][]authz.Permission{"owner": admin, "reader": reader, "outsider": reader, "foreign": reader, "denied": {pharmacy.PermCatalogueRead}},
		Verifier:      authn.NewSessionVerifier(verifier),
		Modules:       []platform.Module{iam.New(nil), pharmacy.New()},
		ReleasePolicy: policy,
		ReleaseAdmission: func(method, path string) gin.HandlerFunc {
			admission := policy.Admission(method, path)
			return func(c *gin.Context) {
				if enforce.Load() {
					admission(c)
					return
				}
				c.Next()
			}
		},
	})
	upstream := httptest.NewServer(owner)
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: 10 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "pharmacy": upstream.URL}})
	require.NoError(t, err)
	f := lowStockGatewayFixture{handler: gateway, db: db, pharmacy: v7(), outlet: v7(), sibling: v7(), product: v7(), tokens: map[string]string{}}
	for actor, tenant := range tenants {
		f.tokens[actor], err = signer.Mint("user-"+actor, tenant, time.Now(), time.Now().Add(15*time.Minute), "")
		require.NoError(t, err)
	}
	f.root = "/v1/pharmacy/pharmacies/" + f.pharmacy + "/outlets/" + f.outlet
	f.call = func(method, path, actor, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+f.tokens[actor])
		request.Header.Set("Content-Type", "application/json")
		if tenants[actor] != "" {
			request.Header.Set("X-Helivanta-Expected-Tenant", tenants[actor])
		}
		request.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
		result := httptest.NewRecorder()
		gateway.ServeHTTP(result, request)
		require.Equal(t, status, result.Code, "%s %s: %s", method, path, result.Body.String())
		return result
	}
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy
	f.call("PUT", root, "owner", fmt.Sprintf(`{"code":"TEST-LOW-STOCK","name":"TEST Low-stock pharmacy","ownership_type":%q}`, ownership), 200)
	for i, outlet := range []string{f.outlet, f.sibling} {
		path := root + "/outlets/" + outlet
		f.call("PUT", path, "owner", fmt.Sprintf(`{"code":"TEST-LOW-%d","name":"TEST Low-stock outlet %d","country_code":"IN","timezone":"Asia/Kolkata"}`, i, i), 200)
		f.call("PUT", path+"/access/user-owner", "owner", `{"level":"editor"}`, 200)
	}
	f.call("PUT", f.root+"/access/user-reader", "owner", `{"level":"reader"}`, 200)
	f.call("PUT", f.root+"/catalogue/products/"+f.product, "owner", receiverProduct, 200)
	// Fixture preparation precedes release admission. Every subsequent business request uses the real narrow policy.
	enforce.Store(true)
	return f
}

func TestLowStockThroughGlobalAPI(t *testing.T) {
	for _, ownership := range []string{"independent", "hospital_owned"} {
		t.Run(ownership, func(t *testing.T) {
			f := newLowStockGatewayFixture(t, ownership)
			product := f.root + "/catalogue/products/" + f.product
			f.call("GET", f.root+"/catalogue/products", "owner", "", 404)
			f.call("PUT", product, "owner", receiverProduct, 404)
			f.call("GET", f.root+"/stock/movements", "owner", "", 404)
			f.call("GET", f.root+"/overview", "owner", "", 404)
			choices := f.call("GET", f.root+"/catalogue/reorder-products", "reader", "", 200)
			require.Contains(t, choices.Body.String(), "TEST Amoxicillin")
			command := product + "/reorder-levels/" + v7()
			first := f.call("PUT", command, "owner", `{"minimum_units":0,"expected_version":0,"product_version":1}`, 200)
			require.JSONEq(t, first.Body.String(), f.call("PUT", command, "owner", `{"minimum_units":0,"expected_version":0,"product_version":1}`, 200).Body.String())
			summary := f.call("GET", f.root+"/stock/low-products/overview", "reader", "", 200)
			var count struct {
				Low int `json:"low_stock"`
			}
			require.NoError(t, json.Unmarshal(summary.Body.Bytes(), &count))
			require.Equal(t, 1, count.Low)
			require.Contains(t, f.call("GET", f.root+"/stock/low-products", "reader", "", 200).Body.String(), `"available_units":"0"`)
			f.call("PUT", product+"/reorder-levels/"+v7(), "reader", `{"minimum_units":1,"expected_version":1,"product_version":1}`, 403)
			for _, actor := range []string{"outsider", "foreign"} {
				f.call("GET", f.root+"/stock/low-products/overview", actor, "", 404)
				f.call("GET", product+"/reorder-level", actor, "", 404)
			}
			f.call("GET", f.root+"/stock/low-products", "denied", "", 403)
			f.call("GET", f.root+"/stock/low-products", "unknown", "", 401)
			changed := httptest.NewRequest("GET", f.root+"/stock/low-products/overview", nil)
			changed.Header.Set("Authorization", "Bearer "+f.tokens["owner"])
			changed.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
			refused := httptest.NewRecorder()
			f.handler.ServeHTTP(refused, changed)
			require.Equal(t, 409, refused.Code, refused.Body.String())
			require.Contains(t, refused.Body.String(), "session_changed")
			require.NotContains(t, refused.Body.String(), "low_stock")
			sibling := strings.Replace(f.root, f.outlet, f.sibling, 1)
			f.call("GET", sibling+"/stock/low-products", "reader", "", 404)
			f.call("PUT", product+"/reorder-levels/"+v7(), "owner", `{"minimum_units":null,"expected_version":1,"product_version":1}`, 200)
			require.JSONEq(t, first.Body.String(), f.call("GET", command, "owner", "", 200).Body.String())
			require.NoError(t, json.Unmarshal(f.call("GET", f.root+"/stock/low-products/overview", "reader", "", 200).Body.Bytes(), &count))
			require.Zero(t, count.Low)
		})
	}
}
