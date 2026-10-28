package patientmaster_test

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func patientExpectedTenant(r *gin.Engine, method, path, token, body string, expected []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for _, v := range expected {
		req.Header.Add("X-Helivanta-Expected-Tenant", v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPatientExpectedTenantRejectsEveryRegisteredRouteWithoutSideEffects(t *testing.T) {
	var perms []authz.Permission
	for _, g := range patientmaster.New().Permissions() {
		perms = append(perms, g.Permission)
	}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": perms}, Modules: []platform.Module{patientmaster.New()}})
	counts := func() (n [3]int64) {
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			for i, table := range []string{"patients", "audit_events", "outbox_events"} {
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
	checked := map[string]bool{}
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/v1/patients") {
			continue
		}
		checked[route.Method+" "+route.Path] = true
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			path := parameter.ReplaceAllString(route.Path, "0199e000-0000-7000-8000-000000000099") + "?hospital_id=" + testHospital
			w := patientExpectedTenant(r, route.Method, path, "owner", `{}`, []string{testutil.TenantB})
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			require.NotContains(t, w.Body.String(), testutil.TenantA)
			require.NotContains(t, w.Body.String(), testutil.TenantB)
		})
	}
	for _, path := range []string{"POST /v1/patients", "POST /v1/patients/search", "PUT /v1/patients/hospitals/:hospital/access/:subject", "POST /v1/patients/clinical-consents/:consent/decide", "PUT /v1/patients/my/clinical-consents/:consent"} {
		require.Contains(t, checked, path)
	}
	require.Equal(t, before, counts())
}

func TestPatientExpectedTenantThroughGlobalAPIBlocksRegistrationWithoutChangingScope(t *testing.T) {
	owner, _, _ := setupHTTP(t)
	r := consentAuthorityGateway(t, owner)
	path := "/v1/patients?hospital_id=" + testHospital
	body := registerBody("SyntheticWorkspace", "Safety", "1990-01-01", "", false, "")
	denied := patientExpectedTenant(r, "POST", path, "tokA", body, []string{testutil.TenantB})
	require.Equal(t, 409, denied.Code, denied.Body.String())
	created := patientExpectedTenant(r, "POST", path, "tokA", body, []string{testutil.TenantA})
	require.Equal(t, 201, created.Code, created.Body.String())
	id := registeredID(t, created)
	detail := "/v1/patients/" + id + "?hospital_id=" + testHospital
	require.Equal(t, 200, patientExpectedTenant(r, "GET", detail, "tokA", "", nil).Code)
	require.Equal(t, 409, patientExpectedTenant(r, "GET", detail, "tokA", "", []string{"", testutil.TenantA}).Code)
	require.Equal(t, 404, patientExpectedTenant(r, "GET", detail, "tokB", "", []string{testutil.TenantB}).Code)
}
