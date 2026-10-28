package privacy_test

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/privacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func privacyExpectedTenant(r *gin.Engine, method, path, token, body string, values []string) *httptest.ResponseRecorder {
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

func TestPrivacyExpectedTenantRejectsEveryRegisteredRouteWithoutSideEffects(t *testing.T) {
	var perms []authz.Permission
	for _, g := range privacy.New().Permissions() {
		perms = append(perms, g.Permission)
	}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": perms}, Modules: []platform.Module{privacy.New()}})
	counts := func() (n [5]int64) {
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			for i, table := range []string{"privacy_officers", "privacy_requests", "privacy_commands", "audit_events", "outbox_events"} {
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
		if !strings.HasPrefix(route.Path, "/v1/privacy/") {
			continue
		}
		checked++
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			path := parameter.ReplaceAllString(route.Path, "0199e000-0000-7000-8000-000000000099")
			w := privacyExpectedTenant(r, route.Method, path, "owner", `{}`, []string{testutil.TenantB})
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			require.NotContains(t, w.Body.String(), testutil.TenantA)
			require.NotContains(t, w.Body.String(), testutil.TenantB)
		})
	}
	require.Equal(t, 13, checked)
	require.Equal(t, before, counts())
}

func TestPrivacyExpectedTenantThroughGlobalAPIKeepsRightsRequestInVerifiedWorkspace(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{ProductionOwner: true, Tokens: map[string]string{"admin": testutil.TenantA, "officer": testutil.TenantA, "patient": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"admin": {privacy.PermOfficerManage}}, Modules: []platform.Module{privacy.New()}})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	routed := gin.New()
	routed.NoRoute(func(c *gin.Context) {
		c.Request.Header.Set("X-Tenant-ID", testutil.TenantB)
		gateway.ServeHTTP(c.Writer, c.Request)
	})
	r = routed
	officerPath := "/v1/privacy/officers/" + uuid.Must(uuid.NewV7()).String()
	nomination := fmt.Sprintf(`{"subject":"user-officer","display_name":"Synthetic Officer","contact_email":"privacy@example.invalid","review_reference":%q,"valid_until":"2099-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 201, testutil.Do(r, "PUT", officerPath, "admin", nomination).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", officerPath+"/accept", "officer", `{}`).Code)
	path := "/v1/privacy/requests/" + uuid.Must(uuid.NewV7()).String()
	body := `{"kind":"access","description":"Synthetic workspace regression request"}`
	for _, values := range [][]string{{testutil.TenantB}, {""}, {"invalid"}, {testutil.TenantA, testutil.TenantA}, {testutil.TenantA + "," + testutil.TenantB}} {
		denied := privacyExpectedTenant(r, "PUT", path, "patient", body, values)
		require.Equal(t, 409, denied.Code, denied.Body.String())
		require.Equal(t, "no-store", denied.Header().Get("Cache-Control"))
		require.Equal(t, 404, testutil.Do(r, "GET", path, "patient", "").Code)
	}
	created := privacyExpectedTenant(r, "PUT", path, "patient", body, []string{testutil.TenantA})
	require.Equal(t, 201, created.Code, created.Body.String())
	replay := privacyExpectedTenant(r, "PUT", path, "patient", body, nil)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	require.Equal(t, 404, privacyExpectedTenant(r, "GET", path, "other", "", []string{testutil.TenantB}).Code)
	require.Equal(t, 401, privacyExpectedTenant(r, "GET", path, "unknown", "", []string{testutil.TenantA}).Code)
}
