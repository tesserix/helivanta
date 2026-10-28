package pharmacy_test

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func pharmacyRequestWithExpectedTenant(r *gin.Engine, method, path, token, body string, expected []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for _, value := range expected {
		req.Header.Add("X-Helivanta-Expected-Tenant", value)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPharmacyStockExportRejectsStaleExpectedTenant(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	path := strings.Split(productPath(""), "/catalogue")[0] + "/stock/positions.csv?as_of=2026-10-01T00:00:00Z"
	w := pharmacyRequestWithExpectedTenant(r, "GET", path, "editor", "", []string{testutil.TenantB})

	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"session_changed"`)
	require.Empty(t, w.Header().Get("Content-Disposition"))
	require.NotContains(t, w.Body.String(), testutil.TenantA)
	require.NotContains(t, w.Body.String(), testutil.TenantB)
}

func TestPharmacyExpectedTenantRejectsEveryRouteFamilyWithoutSideEffects(t *testing.T) {
	var permissions []authz.Permission
	for _, grant := range pharmacy.New().Permissions() {
		permissions = append(permissions, grant.Permission)
	}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"editor": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"editor": permissions},
		Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	counts := func() (result [3]int64) {
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			for i, table := range []string{"audit_events", "outbox_events", "pharmacy_medications"} {
				if err := tx.Table(table).Count(&result[i]).Error; err != nil {
					return err
				}
			}
			return nil
		}))
		return result
	}
	before := counts()
	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	parameter := regexp.MustCompile(`:[A-Za-z_][A-Za-z0-9_]*`)
	checked := make(map[string]bool)
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/v1/pharmacy/") {
			continue
		}
		checked[route.Method+" "+route.Path] = true
		path := parameter.ReplaceAllStringFunc(route.Path, func(name string) string {
			switch name {
			case ":pharmacy":
				return pharmaOrganization
			case ":outlet":
				return pharmaOutlet
			case ":subject":
				return "user-reader"
			default:
				return "0199e000-0000-7000-8000-000000000099"
			}
		})
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			w := pharmacyRequestWithExpectedTenant(r, route.Method, path, "editor", `{}`, []string{testutil.TenantB})
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			require.Empty(t, w.Header().Get("Content-Disposition"))
			require.NotContains(t, w.Body.String(), testutil.TenantA)
			require.NotContains(t, w.Body.String(), testutil.TenantB)
		})
	}
	for _, route := range []string{"GET /v1/pharmacy/configuration", "GET /v1/pharmacy/insights", "POST /v1/pharmacy/medications", "POST /v1/pharmacy/dispenses/:id/dispense", "GET /v1/pharmacy/pharmacies/:pharmacy/outlets/:outlet/stock/positions.csv"} {
		require.Contains(t, checked, route)
	}
	t.Logf("verified %d declared pharmacy routes", len(checked))
	require.Equal(t, before, counts())
	require.NotContains(t, logs.String(), "pharmacy.stock.exported")
}

func TestPharmacyExpectedTenantHeaderFormsAndVerifiedSessionIsolation(t *testing.T) {
	r, _ := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	root := strings.Split(productPath(""), "/catalogue")[0]
	path := root + "/stock/positions.csv?as_of=2026-10-01T00:00:00Z"
	for _, expected := range [][]string{nil, {testutil.TenantA}, {"urn:uuid:" + testutil.TenantA}, {"{" + testutil.TenantA + "}"}} {
		w := pharmacyRequestWithExpectedTenant(r, "GET", path, "editor", "", expected)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "0", w.Header().Get("X-Export-Row-Count"))
	}
	for _, expected := range [][]string{{""}, {" "}, {"not-a-tenant"}, {uuid.Nil.String()}, {testutil.TenantB}, {testutil.TenantA, testutil.TenantA}, {testutil.TenantA, testutil.TenantB}, {testutil.TenantA + "," + testutil.TenantA}} {
		w := pharmacyRequestWithExpectedTenant(r, "GET", path, "editor", "", expected)
		require.Equal(t, 409, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.Contains(t, w.Body.String(), `"error":"session_changed"`)
		require.Empty(t, w.Header().Get("Content-Disposition"))
	}
	for _, tc := range []struct {
		token, expected string
		status          int
	}{
		{"foreign", testutil.TenantA, 409},
		{"foreign", testutil.TenantB, 404},
		{"sibling", testutil.TenantA, 404},
		{"unknown", testutil.TenantA, 401},
		{"denied", testutil.TenantB, 403},
		{"reader", testutil.TenantA, 200},
	} {
		w := pharmacyRequestWithExpectedTenant(r, "GET", path, tc.token, "", []string{tc.expected})
		require.Equal(t, tc.status, w.Code, w.Body.String())
	}
	require.Equal(t, 200, do(r, "DELETE", root+"/access/user-reader", "editor", "").Code)
	require.Equal(t, 404, pharmacyRequestWithExpectedTenant(r, "GET", path, "reader", "", []string{testutil.TenantA}).Code)
}

func TestPharmacyExpectedTenantPreventsValidWritesAndPreservesReplay(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	counts := func() (result [3]int64) {
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			for i, table := range []string{"audit_events", "outbox_events", "pharmacy_catalogue_revisions"} {
				if err := tx.Table(table).Count(&result[i]).Error; err != nil {
					return err
				}
			}
			return nil
		}))
		return result
	}
	before := counts()
	path := productPath(uuid.Must(uuid.NewV7()).String())
	body := productBody(0, "TEST expected tenant replay")
	w := pharmacyRequestWithExpectedTenant(r, "PUT", path, "editor", body, []string{testutil.TenantB})
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"session_changed"`)
	require.Equal(t, before, counts())
	require.Equal(t, 404, do(r, "GET", path, "editor", "").Code)
	w = pharmacyRequestWithExpectedTenant(r, "PUT", path, "editor", body, []string{testutil.TenantA})
	require.Equal(t, 200, w.Code, w.Body.String())
	after := counts()
	require.Greater(t, after[0], before[0])
	require.Greater(t, after[1], before[1])
	replay := pharmacyRequestWithExpectedTenant(r, "PUT", path, "editor", body, nil)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, w.Body.String(), replay.Body.String())
	require.Equal(t, after, counts())
}
