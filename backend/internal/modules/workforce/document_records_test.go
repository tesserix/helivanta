package workforce_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestDocumentRecordChoicesAreScopedAndDoNotRequireEnrollmentPermission(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "documents": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}, "documents": {workforce.PermDocumentManage}, "reader": {workforce.PermDocumentManage}, "foreign": {workforce.PermDocumentManage}},
		Modules: []platform.Module{workforce.New()},
	})
	hospital, staff, credential, qualification := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "admin", `{"name":"Synthetic Evidence Hospital"}`).Code)
	for token, level := range map[string]string{"admin": "manager", "documents": "manager", "reader": "reader"} {
		require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-"+token, "admin", `{"level":"`+level+`"}`).Code)
	}
	path := base + "/staff/" + staff
	body := `{"expected_version":0,"staff":{"employee_code":"DOC-CHOICE-001","display_name":"Synthetic Evidence Staff","job_category":"nurse","employment_status":"active"},"profile":{"engagement_type":"employee","personal_email":"private-staff@example.invalid","blood_group":"AB-"},"credentials":[{"id":"` + credential + `","type":"Synthetic registration","number":"SYN-PRIVATE-LICENSE","issuer":"Synthetic board"}],"qualifications":[{"id":"` + qualification + `","qualification":"Synthetic nursing certificate","institution":"Synthetic school","completion_year":2020}]}`
	require.Equal(t, 200, testutil.Do(r, "PUT", path+"/enrollment", "admin", body).Code)
	require.Equal(t, 403, testutil.Do(r, "GET", path+"/enrollment", "documents", "").Code)
	response := testutil.Do(r, "GET", path+"/document-records", "documents", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var choices struct {
		UploadsAvailable bool `json:"uploads_available"`
		Records          []struct {
			RecordKind string   `json:"record_kind"`
			RecordID   string   `json:"record_id"`
			Label      string   `json:"label"`
			Categories []string `json:"categories"`
		} `json:"records"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &choices))
	require.False(t, choices.UploadsAvailable)
	require.Len(t, choices.Records, 3)
	require.Equal(t, staff, choices.Records[0].RecordID)
	require.Equal(t, []string{"employment_evidence", "identity_evidence"}, choices.Records[0].Categories)
	require.Equal(t, credential, choices.Records[1].RecordID)
	require.Equal(t, "Synthetic registration · Synthetic board", choices.Records[1].Label)
	require.Equal(t, []string{"credential_evidence"}, choices.Records[1].Categories)
	require.Equal(t, qualification, choices.Records[2].RecordID)
	require.Equal(t, "Synthetic nursing certificate · Synthetic school", choices.Records[2].Label)
	for _, private := range []string{"private-staff@example.invalid", "SYN-PRIVATE-LICENSE", "blood_group", "AB-"} {
		require.NotContains(t, response.Body.String(), private)
	}
	for _, token := range []string{"reader", "foreign"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path+"/document-records", token, "").Code)
	}
	require.Equal(t, 404, testutil.Do(r, "GET", base+"/staff/"+uuid.NewString()+"/document-records", "documents", "").Code)
}

func TestDocumentRequestsCannotCrossAnExpectedTenantBoundary(t *testing.T) {
	s := newDocumentScenario(t)
	records := "/v1/workforce/hospitals/" + s.input.HospitalID + "/staff/" + s.input.StaffID + "/document-records"
	for _, route := range []struct{ method, path string }{
		{"GET", records}, {"GET", strings.TrimSuffix(s.path, "/"+s.input.FileID)},
		{"PUT", s.path}, {"PUT", s.path + "/content"}, {"GET", s.path + "/content"},
		{"PUT", s.path + "/revocation"}, {"PUT", s.path + "/retries/2"},
	} {
		request := httptest.NewRequest(route.method, route.path, nil)
		request.Header.Set("Authorization", "Bearer manager")
		request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
		response := httptest.NewRecorder()
		s.router.ServeHTTP(response, request)
		require.Equal(t, 409, response.Code, route.method+" "+route.path+": "+response.Body.String())
	}
	request := httptest.NewRequest("GET", records, nil)
	request.Header.Set("Authorization", "Bearer manager")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
}
