package organization_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestHospitalRegistrationRequiresOwnerObservationAndImmutableIntent(t *testing.T) {
	router, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"manager": {"organization.structure.manage", "organization.structure.read"}, "reader": {"organization.structure.read"}, "foreign": {"organization.structure.manage", "organization.structure.read"}},
		Modules: []platform.Module{organization.New()},
	})
	hospital := uuid.NewString()
	request := uuid.Must(uuid.NewV7()).String()
	path := "/v1/organization/hospitals/" + hospital + "/registrations/" + request
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if token == "foreign" {
			tenant = testutil.TenantB
		}
		req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	first := call("PUT", path, "manager", `{"reason":"Synthetic organization setup"}`)
	require.Equal(t, 200, first.Code, first.Body.String())
	var out struct {
		RequestID  string `json:"request_id"`
		HospitalID string `json:"hospital_id"`
		Status     string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &out))
	require.Equal(t, request, out.RequestID)
	require.Equal(t, hospital, out.HospitalID)
	require.Equal(t, "pending", out.Status)
	require.JSONEq(t, first.Body.String(), call("PUT", path, "manager", `{"reason":"Synthetic organization setup"}`).Body.String())
	require.Equal(t, 409, call("PUT", path, "manager", `{"reason":"Changed intent"}`).Code)
	require.Equal(t, 403, call("PUT", path, "reader", `{"reason":"Synthetic organization setup"}`).Code)
	require.Equal(t, 404, call("GET", path, "foreign", "").Code)
}

func organizationCall(router *gin.Engine, method, path, token, tenant, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
