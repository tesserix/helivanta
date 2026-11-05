package pharmacy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestDepartmentBindingCannotUseCoarsePermissionWithoutCurrentOwners(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"coarse": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"coarse": {"pharmacy.staff_binding.prepare"}},
		Modules: []platform.Module{pharmacy.New()},
	})
	path := "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000011/hospitals/11111111-1111-4111-8111-111111111112/staff-department-bindings/0199e000-0000-7000-8000-000000000012"
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"department_id":"0199e000-0000-7000-8000-000000000013","department_revision_id":"0199e000-0000-7000-8000-000000000014","reason":"Synthetic relationship preparation"}`))
	req.Header.Set("Authorization", "Bearer coarse")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	req.Header.Set("X-Helivanta-Expected-Subject", "user-coarse")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"scoped_authorization_unavailable"`)
	require.NotContains(t, w.Body.String(), `"status":"pending"`)
}

func TestDepartmentBindingRejectsMissingOrChangedAccountIntent(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"coarse": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"coarse": {"pharmacy.staff_binding.prepare"}},
		Modules: []platform.Module{pharmacy.New()},
	})
	path := "/v1/pharmacy/pharmacies/0199e000-0000-7000-8000-000000000010/outlets/0199e000-0000-7000-8000-000000000011/hospitals/11111111-1111-4111-8111-111111111112/staff-department-bindings/0199e000-0000-7000-8000-000000000012"
	for _, headers := range []http.Header{
		{},
		{"X-Helivanta-Expected-Tenant": {testutil.TenantA}},
		{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-other"}},
		{"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-coarse", "user-coarse"}},
	} {
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"department_id":"0199e000-0000-7000-8000-000000000013","department_revision_id":"0199e000-0000-7000-8000-000000000014","reason":"Synthetic account intent precondition"}`))
		req.Header = headers
		req.Header.Set("Authorization", "Bearer coarse")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"error":"session_changed"`)
	}
}
