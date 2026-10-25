package mediconnect_test

import (
	"fmt"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func mediconnectExpectedTenant(r *gin.Engine, method, path, token, body string, values []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for _, value := range values {
		req.Header.Add("X-Helivanta-Expected-Tenant", value)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMediConnectExpectedTenantPreventsAccountCreationAndPreservesReplay(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "peer": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
	owner := httptest.NewServer(r)
	t.Cleanup(owner.Close)
	cfg, err := globalapi.FromEnv(func(key string) string {
		switch key {
		case "HELIVANTA_ENV":
			return "dev"
		case "GLOBAL_API_UPSTREAMS":
			return fmt.Sprintf(`{"mediconnect":%q}`, owner.URL)
		default:
			return ""
		}
	})
	require.NoError(t, err)
	gateway, err := globalapi.New(cfg)
	require.NoError(t, err)
	forwarded := gin.New()
	forwarded.NoRoute(func(c *gin.Context) {
		c.Request.Header.Set("X-Tenant-ID", testutil.TenantB)
		gateway.ServeHTTP(c.Writer, c.Request)
	})
	r = forwarded
	path := "/v1/mediconnect/account"
	denied := mediconnectExpectedTenant(r, "POST", path, "owner", "{}", []string{testutil.TenantB})
	require.Equal(t, 409, denied.Code, denied.Body.String())
	require.JSONEq(t, `{"error":"session_changed","message":"Your workspace changed. Refresh the page before continuing."}`, denied.Body.String())
	require.Equal(t, "no-store", denied.Header().Get("Cache-Control"))
	require.Equal(t, 404, testutil.Do(r, "GET", path, "owner", "").Code)
	created := mediconnectExpectedTenant(r, "POST", path, "owner", "{}", []string{testutil.TenantA})
	require.Equal(t, 201, created.Code, created.Body.String())
	replay := mediconnectExpectedTenant(r, "POST", path, "owner", "{}", nil)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	require.Equal(t, 404, mediconnectExpectedTenant(r, "GET", path, "peer", "", []string{testutil.TenantA}).Code)
	require.Equal(t, 401, mediconnectExpectedTenant(r, "GET", path, "unknown", "", []string{testutil.TenantA}).Code)
}

func TestMediConnectExpectedTenantRejectsEveryRegisteredRouteWithoutSideEffects(t *testing.T) {
	var perms []authz.Permission
	for _, g := range mediconnect.New().Permissions() {
		perms = append(perms, g.Permission)
	}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": perms}, Modules: []platform.Module{mediconnect.New()}})
	counts := func() (n [3]int64) {
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			for i, table := range []string{"mediconnect_accounts", "audit_events", "outbox_events"} {
				if err := tx.Table(table).Count(&n[i]).Error; err != nil {
					return err
				}
			}
			return nil
		}))
		return
	}
	before := counts()
	parameter := regexp.MustCompile(`:[A-Za-z_][A-Za-z0-9_]*`)
	checked := 0
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/v1/mediconnect/") {
			continue
		}
		checked++
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			path := parameter.ReplaceAllString(route.Path, "0199e000-0000-7000-8000-000000000099")
			w := mediconnectExpectedTenant(r, route.Method, path, "owner", `{}`, []string{testutil.TenantB})
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			require.NotContains(t, w.Body.String(), testutil.TenantA)
			require.NotContains(t, w.Body.String(), testutil.TenantB)
		})
	}
	require.Equal(t, 17, checked)
	require.Equal(t, before, counts())
}
