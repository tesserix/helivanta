package workforce_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestStaffActorScopesRequireAccountIntentAndCurrentDependencies(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"coarse": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"coarse": {}},
		Modules: []platform.Module{workforce.New()},
	})
	for _, tc := range []struct {
		name, path string
		headers    http.Header
		status     int
		code       string
	}{
		{"missing", "", nil, 409, "session_changed"},
		{"missing subject", "", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}}, 409, "session_changed"},
		{"changed subject", "", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"other-account"}}, 409, "session_changed"},
		{"repeated subject", "", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-coarse", "user-coarse"}}, 409, "session_changed"},
		{"unavailable", "", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-coarse"}}, 503, "scoped_authorization_unavailable"},
		{"unbounded", "?limit=21", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-coarse"}}, 400, ""},
		{"unknown query", "?hospital=guessed", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-coarse"}}, 400, "invalid_request"},
		{"repeated limit", "?limit=1&limit=2", http.Header{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-coarse"}}, 400, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/workforce/staff-actor-scopes"+tc.path, nil)
			if tc.headers != nil {
				req.Header = tc.headers.Clone()
			}
			req.Header.Set("Authorization", "Bearer coarse")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.code != "" {
				require.Contains(t, w.Body.String(), `"error":"`+tc.code+`"`)
			}
			require.NotContains(t, w.Body.String(), `"hospital_id"`)
			require.NotContains(t, w.Body.String(), `"staff_id"`)
		})
	}
}
