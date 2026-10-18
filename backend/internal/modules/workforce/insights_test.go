package workforce_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestStaffInsightsRequireHospitalGrantAndReflectRevocation(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}},
		Modules: []platform.Module{workforce.New()},
	})
	base := "/v1/workforce/hospitals/" + uuid.NewString()
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-reader", "admin", `{"level":"manager"}`).Code)
	for _, category := range []string{"doctor", "doctor", "nurse"} {
		body := `{"employee_code":"` + uuid.NewString() + `","display_name":"Private name","job_category":"` + category + `","employment_status":"active"}`
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/staff/"+uuid.NewString(), "reader", body).Code)
	}
	path := base + "/insights"
	w := testutil.Do(r, "GET", path, "reader", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var body struct {
		Counts []struct {
			Category string `json:"category"`
			Status   string `json:"status"`
			Count    int    `json:"count"`
		} `json:"counts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Counts, 2)
	require.Equal(t, "doctor", body.Counts[0].Category)
	require.Equal(t, 2, body.Counts[0].Count)
	require.NotContains(t, w.Body.String(), "Private name")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	for _, token := range []string{"admin", "other"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path, token, "").Code)
	}
	require.Equal(t, 401, testutil.Do(r, "GET", path, "", "").Code)
	require.Equal(t, 204, testutil.Do(r, "DELETE", base+"/access/user-reader", "admin", "").Code)
	require.Equal(t, 404, testutil.Do(r, "GET", path, "reader", "").Code)
}
