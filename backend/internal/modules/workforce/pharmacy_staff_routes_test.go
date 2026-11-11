package workforce_test

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

const pharmacyStaffBody = `{"employee_code":"SYN-PH-1","display_name":"Synthetic pharmacist","job_category":"pharmacist","employment_status":"active","expected_version":%d,"directory_context_version":"v1"}`

// newPharmacyStaffHarness registers one hospital and one pharmacy; only "admin" and "foreign" hold workforce.hospital.manage.
func newPharmacyStaffHarness(t *testing.T) (*establishmentHarness, string) {
	t.Helper()
	module := workforce.New()
	router, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"admin": {workforce.PermHospitalManage}, "foreign": {workforce.PermHospitalManage}},
		Modules: []platform.Module{module},
	})
	h := &establishmentHarness{router: router, db: db, bus: bus, module: module, hospital: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "admin", `{"name":"Synthetic Establishment Hospital"}`).Code)
	return h, h.pharmacy(t, "independent")
}

func pharmacyRequest(h *establishmentHarness, token, method, path, body string) *httptest.ResponseRecorder {
	return pharmacyContent(h, token, method, path, "application/json", strings.NewReader(body))
}

// pharmacyContent sends a pharmacy-family request in the caller's own session, with any body and content type.
func pharmacyContent(h *establishmentHarness, token, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, strings.TrimSuffix("/v1/workforce/pharmacies/"+path, "/"), body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	tenant := testutil.TenantA
	if token == "foreign" {
		tenant = testutil.TenantB
	}
	req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

func grant(t *testing.T, h *establishmentHarness, pharmacy, subject, level string) {
	t.Helper()
	w := pharmacyRequest(h, "admin", "PUT", pharmacy+"/access/"+subject, `{"level":"`+level+`"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, `{"pharmacy_id":"`+pharmacy+`","subject":"`+subject+`","level":"`+level+`"}`, w.Body.String())
}

func pharmacyAccess(t *testing.T, h *establishmentHarness, pharmacy string) []string {
	t.Helper()
	var rows []string
	require.NoError(t, h.tx(t, pharmacy, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT subject||':'||level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? ORDER BY subject`, testutil.TenantA, pharmacy).Scan(&rows).Error
	}))
	return rows
}

func TestPharmacyAccessIsGrantedAndRevokedByATenantAdministrator(t *testing.T) {
	h, pharmacy := newPharmacyStaffHarness(t)
	grant(t, h, pharmacy, "user-reader", "reader")
	grant(t, h, pharmacy, "user-reader", "manager")
	require.Equal(t, []string{"user-reader:manager"}, pharmacyAccess(t, h, pharmacy))

	refused := map[string]struct {
		token, method, path string
		status              int
	}{
		"without workforce.hospital.manage": {"reader", "PUT", pharmacy + "/access/user-outsider", 403},
		"revoke without the permission":     {"reader", "DELETE", pharmacy + "/access/user-reader", 403},
		"a hospital ID":                     {"admin", "PUT", h.hospital + "/access/user-outsider", 404},
		"an unregistered pharmacy":          {"admin", "PUT", uuid.Must(uuid.NewV7()).String() + "/access/user-outsider", 404},
		"a foreign-tenant pharmacy":         {"foreign", "PUT", pharmacy + "/access/user-outsider", 404},
		"a foreign-tenant revoke":           {"foreign", "DELETE", pharmacy + "/access/user-reader", 404},
		"a blank subject":                   {"admin", "PUT", pharmacy + "/access/%20", 400},
	}
	for name, call := range refused {
		t.Run(name, func(t *testing.T) {
			w := pharmacyRequest(h, call.token, call.method, call.path, `{"level":"manager"}`)
			require.Equal(t, call.status, w.Code, w.Body.String())
		})
	}
	require.Equal(t, []string{"user-reader:manager"}, pharmacyAccess(t, h, pharmacy))
	require.Zero(t, h.accessRows(t, h.hospital), "a hospital ID on the pharmacy family must not grant hospital access")

	w := pharmacyRequest(h, "admin", "DELETE", pharmacy+"/access/user-reader", "")
	require.Equal(t, 204, w.Code, w.Body.String())
	require.Empty(t, pharmacyAccess(t, h, pharmacy))
}

func TestPharmacyManagerKeepsTheStaffDirectory(t *testing.T) {
	h, pharmacy := newPharmacyStaffHarness(t)
	grant(t, h, pharmacy, "user-admin", "manager")
	staff := uuid.Must(uuid.NewV7()).String()
	path := pharmacy + "/staff/" + staff + "?directory_context=v1"

	created := pharmacyRequest(h, "admin", "PUT", path, strings.Replace(pharmacyStaffBody, "%d", "0", 1))
	require.Equal(t, 200, created.Code, created.Body.String())
	var record map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &record))
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	require.Equal(t, []string{"created_at", "directory_context_version", "display_name", "employee_code", "employment_status", "id", "job_category", "pharmacy_id", "updated_at", "version"}, keys)
	require.Equal(t, staff, record["id"])
	require.Equal(t, pharmacy, record["pharmacy_id"])
	require.Equal(t, float64(1), record["version"])

	read := pharmacyRequest(h, "admin", "GET", path, "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())

	edited := pharmacyRequest(h, "admin", "PUT", path, strings.Replace(strings.Replace(pharmacyStaffBody, "%d", "1", 1), "Synthetic pharmacist", "Synthetic chief pharmacist", 1))
	require.Equal(t, 200, edited.Code, edited.Body.String())
	require.NoError(t, json.Unmarshal(edited.Body.Bytes(), &record))
	require.Equal(t, "Synthetic chief pharmacist", record["display_name"])
	require.Equal(t, float64(2), record["version"])

	stale := pharmacyRequest(h, "admin", "PUT", path, strings.Replace(pharmacyStaffBody, "%d", "1", 1))
	require.Equal(t, 409, stale.Code, stale.Body.String())
	require.Contains(t, stale.Body.String(), "staff_version_conflict")

	list := pharmacyRequest(h, "admin", "GET", pharmacy+"/staff", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	var page struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, pharmacy, page.Data[0]["pharmacy_id"])
	require.Equal(t, "Synthetic chief pharmacist", page.Data[0]["display_name"])
	require.NotContains(t, page.Data[0], "hospital_id")
	require.NotContains(t, page.Data[0], "version")
}

func TestPharmacyStaffDirectoryRefusesCallersOutsideItsAccess(t *testing.T) {
	h, pharmacy := newPharmacyStaffHarness(t)
	grant(t, h, pharmacy, "user-admin", "manager")
	grant(t, h, pharmacy, "user-reader", "reader")
	staff := uuid.Must(uuid.NewV7()).String()
	path := pharmacy + "/staff/" + staff + "?directory_context=v1"
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", path, strings.Replace(pharmacyStaffBody, "%d", "0", 1)).Code)

	for name, call := range map[string]struct {
		token, method, path, body string
		status                    int
	}{
		"a reader reads":                   {"reader", "GET", path, "", 200},
		"a reader lists":                   {"reader", "GET", pharmacy + "/staff", "", 200},
		"a reader cannot write":            {"reader", "PUT", path, strings.Replace(pharmacyStaffBody, "%d", "1", 1), 404},
		"no access row reads":              {"outsider", "GET", path, "", 404},
		"no access row lists":              {"outsider", "GET", pharmacy + "/staff", "", 404},
		"no access row writes":             {"outsider", "PUT", path, strings.Replace(pharmacyStaffBody, "%d", "1", 1), 404},
		"a foreign tenant reads":           {"foreign", "GET", path, "", 404},
		"a foreign tenant lists":           {"foreign", "GET", pharmacy + "/staff", "", 404},
		"a foreign tenant writes":          {"foreign", "PUT", path, strings.Replace(pharmacyStaffBody, "%d", "1", 1), 404},
		"a hospital ID reads":              {"admin", "GET", h.hospital + "/staff/" + staff + "?directory_context=v1", "", 404},
		"a hospital ID lists":              {"admin", "GET", h.hospital + "/staff", "", 404},
		"the HR intake view reads":         {"admin", "GET", path + "&hr_intake=v1", "", 400},
		"the HR intake view lists":         {"admin", "GET", pharmacy + "/staff?hr_intake=v1", "", 400},
		"the HR intake view writes":        {"admin", "PUT", path + "&hr_intake=v1", strings.Replace(pharmacyStaffBody, "%d", "1", 1), 400},
		"an unversioned read":              {"admin", "GET", pharmacy + "/staff/" + staff, "", 400},
		"an unversioned write":             {"admin", "PUT", pharmacy + "/staff/" + staff, `{"employee_code":"SYN-PH-1","display_name":"Synthetic pharmacist","job_category":"pharmacist","employment_status":"inactive"}`, 400},
		"a write without expected_version": {"admin", "PUT", path, `{"employee_code":"SYN-PH-1","display_name":"Synthetic pharmacist","job_category":"pharmacist","employment_status":"inactive","directory_context_version":"v1"}`, 400},
	} {
		t.Run(name, func(t *testing.T) {
			w := pharmacyRequest(h, call.token, call.method, call.path, call.body)
			require.Equal(t, call.status, w.Code, w.Body.String())
		})
	}
	var rows []string
	require.NoError(t, h.tx(t, pharmacy, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT employee_code||':'||display_name||':'||employment_status||':'||version FROM workforce_staff WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, pharmacy).Scan(&rows).Error
	}))
	require.Equal(t, []string{"SYN-PH-1:Synthetic pharmacist:active:1"}, rows, "a refused request must not change the record")
}

func TestPharmacyStaffSourcesListOnlyTheCallersPharmacies(t *testing.T) {
	h, pharmacy := newPharmacyStaffHarness(t)
	chain := h.pharmacy(t, "chain")
	h.pharmacy(t, "independent")
	grant(t, h, pharmacy, "user-admin", "manager")
	grant(t, h, chain, "user-reader", "reader")
	require.Equal(t, 200, testutil.Do(h.router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/access/user-admin", "admin", `{"level":"manager"}`).Code)

	for token, want := range map[string][]string{
		"admin":    {pharmacy + ":manager"},
		"reader":   {chain + ":reader"},
		"outsider": {},
		"foreign":  {},
	} {
		t.Run(token, func(t *testing.T) {
			w := pharmacyRequest(h, token, "GET", "", "")
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			var page struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
			got := []string{}
			for _, row := range page.Data {
				keys := make([]string, 0, len(row))
				for key := range row {
					keys = append(keys, key)
				}
				slices.Sort(keys)
				require.Equal(t, []string{"access_level", "created_at", "name", "pharmacy_id"}, keys)
				require.Equal(t, "Synthetic Employer Pharmacy", row["name"])
				got = append(got, row["pharmacy_id"].(string)+":"+row["access_level"].(string))
			}
			require.Equal(t, want, got)
		})
	}
}
