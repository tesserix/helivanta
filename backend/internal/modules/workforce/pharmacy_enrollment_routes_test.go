package workforce_test

import (
	"encoding/json"
	"maps"
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

// newPharmacyEnrollmentHarness registers a hospital and a pharmacy; "admin" manages both, "directory" manages the pharmacy without the permission.
func newPharmacyEnrollmentHarness(t *testing.T) (*establishmentHarness, string) {
	t.Helper()
	module := workforce.New()
	manage := []authz.Permission{workforce.PermHospitalManage, workforce.PermEnrollmentManage}
	router, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"admin": manage, "foreign": manage,
			"reader": {workforce.PermEnrollmentManage}, "outsider": {workforce.PermEnrollmentManage}},
		Modules: []platform.Module{module},
	})
	h := &establishmentHarness{router: router, db: db, bus: bus, module: module, hospital: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "admin", `{"name":"Synthetic Enrollment Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	pharmacy := h.pharmacy(t, "independent")
	grant(t, h, pharmacy, "user-admin", "manager")
	grant(t, h, pharmacy, "user-reader", "reader")
	grant(t, h, pharmacy, "user-directory", "manager")
	return h, pharmacy
}

func pharmacyEnrollmentInput(version int64, status string) map[string]any {
	return map[string]any{
		"expected_version": version, "request_context_version": "v1",
		"staff":              map[string]any{"employee_code": "SYN-PH-ENR", "display_name": "Synthetic pharmacist", "job_category": "pharmacist", "employment_status": status},
		"profile":            map[string]any{"preferred_name": "Synthetic", "personal_email": "pharmacist@example.invalid", "engagement_type": "employee", "department": "Dispensary", "designation": "Pharmacist", "start_date": "2024-02-01"},
		"addresses":          []any{map[string]any{"id": "11111111-1111-4111-8111-111111111111", "use": "current", "line1": "1 Synthetic Lane", "city": "Pune", "country": "IN"}},
		"emergency_contacts": []any{map[string]any{"id": "22222222-2222-4222-8222-222222222222", "name": "Synthetic contact", "relationship": "partner", "phone": "+919800000002", "priority": 1}},
		"credentials":        []any{map[string]any{"id": "33333333-3333-4333-8333-333333333333", "type": "registration", "number": "SYN-PCI-1", "issuer": "Synthetic pharmacy council"}},
		"qualifications":     []any{map[string]any{"id": "44444444-4444-4444-8444-444444444444", "qualification": "Synthetic B.Pharm", "institution": "Synthetic institute", "country": "IN", "completion_year": 2018}},
		"experiences":        []any{},
	}
}

func withProfile(input map[string]any, field string, value any) map[string]any {
	profile := maps.Clone(input["profile"].(map[string]any))
	profile[field] = value
	changed := maps.Clone(input)
	changed["profile"] = profile
	return changed
}

func enrollmentPath(establishment, staff string) string {
	return establishment + "/staff/" + staff + "/enrollment?request_context=v1"
}

func sortedKeys(row map[string]any) []string {
	keys := slices.Collect(maps.Keys(row))
	slices.Sort(keys)
	return keys
}

func (h *establishmentHarness) count(t *testing.T, establishment, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, h.tx(t, establishment, func(tx *gorm.DB) error {
		return tx.Table(table).Where(where, args...).Count(&n).Error
	}))
	return n
}

func TestPharmacyManagerKeepsAStaffEnrollment(t *testing.T) {
	h, pharmacy := newPharmacyEnrollmentHarness(t)
	staff := uuid.Must(uuid.NewV7()).String()
	path := enrollmentPath(pharmacy, staff)
	eventsBefore := h.count(t, pharmacy, "outbox_events", "subject=?", workforce.EnrollmentChanged)

	created := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, pharmacyEnrollmentInput(0, "active"))))
	require.Equal(t, 200, created.Code, created.Body.String())
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	var saved map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &saved))
	require.Equal(t, []string{"addresses", "credentials", "emergency_contacts", "experiences", "profile", "qualifications", "request_context_version", "staff", "version"}, sortedKeys(saved))
	record := saved["staff"].(map[string]any)
	require.Equal(t, []string{"created_at", "display_name", "employee_code", "employment_status", "id", "job_category", "pharmacy_id", "updated_at"}, sortedKeys(record))
	require.Equal(t, pharmacy, record["pharmacy_id"])
	require.Equal(t, staff, record["id"])
	require.Equal(t, float64(1), saved["version"])
	require.Equal(t, "pharmacist@example.invalid", saved["profile"].(map[string]any)["personal_email"])
	require.Len(t, saved["credentials"], 1)
	for _, absent := range []string{"hospital_id", "masters_version"} {
		require.NotContains(t, created.Body.String(), absent)
	}

	read := pharmacyRequest(h, "admin", "GET", path, "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Equal(t, "no-store", read.Header().Get("Cache-Control"))
	require.JSONEq(t, created.Body.String(), read.Body.String())

	replay := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, pharmacyEnrollmentInput(0, "active"))))
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())

	updated := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, withProfile(pharmacyEnrollmentInput(1, "active"), "designation", "Synthetic chief pharmacist"))))
	require.Equal(t, 200, updated.Code, updated.Body.String())
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &saved))
	require.Equal(t, float64(2), saved["version"])
	require.Equal(t, "Synthetic chief pharmacist", saved["profile"].(map[string]any)["designation"])

	stale := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, withProfile(pharmacyEnrollmentInput(1, "active"), "designation", "Synthetic stale"))))
	require.Equal(t, 409, stale.Code, stale.Body.String())

	require.Equal(t, eventsBefore, h.count(t, pharmacy, "outbox_events", "subject=?", workforce.EnrollmentChanged), "the hospital enrollment event never carries a pharmacy")
	require.EqualValues(t, 2, h.count(t, pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.enrollment.changed'", pharmacy, staff))
	require.EqualValues(t, 1, h.count(t, pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.enrollment.read'", pharmacy, staff))
	require.EqualValues(t, 1, h.count(t, pharmacy, "workforce_enrollment_credentials", "hospital_id=? AND staff_id=?", pharmacy, staff))
}

func TestPharmacyEnrollmentRefusesCallersOutsideItsAuthority(t *testing.T) {
	h, pharmacy := newPharmacyEnrollmentHarness(t)
	staff := uuid.Must(uuid.NewV7()).String()
	path := enrollmentPath(pharmacy, staff)
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)
	other := h.pharmacy(t, "independent")
	grant(t, h, other, "user-admin", "manager")
	elsewhere := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", enrollmentPath(other, elsewhere), string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)
	supplier := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.tx(t, supplier, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,supplier_source_version) VALUES(?,?,'Synthetic Employer Supplier','supplier',1)`, testutil.TenantA, supplier).Error
	}))
	auditBefore := h.count(t, pharmacy, "workforce_audit", "hospital_id=?", pharmacy)
	update := string(mustJSON(t, withProfile(pharmacyEnrollmentInput(1, "inactive"), "designation", "Synthetic refused")))
	cases := map[string]struct {
		token, path string
		status      int
	}{
		"a reader":                   {"reader", path, 404},
		"no access row":              {"outsider", path, 404},
		"no enrollment permission":   {"directory", path, 403},
		"a hospital ID":              {"admin", enrollmentPath(h.hospital, staff), 404},
		"a supplier ID":              {"admin", enrollmentPath(supplier, staff), 404},
		"an unregistered pharmacy":   {"admin", enrollmentPath(uuid.Must(uuid.NewV7()).String(), staff), 404},
		"a foreign tenant":           {"foreign", path, 404},
		"a missing Staff record":     {"admin", enrollmentPath(pharmacy, uuid.Must(uuid.NewV7()).String()), 404},
		"another pharmacy's Staff":   {"admin", enrollmentPath(pharmacy, elsewhere), 404},
		"the HR intake view":         {"admin", path + "&hr_intake=v1", 400},
		"profile masters":            {"admin", path + "&profile_masters=v1", 400},
		"qualification masters":      {"admin", path + "&qualification_masters=v1", 400},
		"contact masters":            {"admin", path + "&contact_masters=v1", 400},
		"an unversioned view":        {"admin", pharmacy + "/staff/" + staff + "/enrollment", 400},
		"a repeated request context": {"admin", path + "&request_context=v1", 400},
	}
	for name, call := range cases {
		for _, method := range []string{"GET", "PUT"} {
			t.Run(method+" "+name, func(t *testing.T) {
				body := ""
				if method == "PUT" {
					body = update
				}
				w := pharmacyRequest(h, call.token, method, call.path, body)
				require.Equal(t, call.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "pharmacist@example.invalid")
			})
		}
	}
	for name, headers := range map[string]map[string][]string{
		"a changed subject":   {"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-other"}},
		"a missing subject":   {"X-Helivanta-Expected-Tenant": {testutil.TenantA}},
		"a missing tenant":    {"X-Helivanta-Expected-Subject": {"user-admin"}},
		"a repeated subject":  {"X-Helivanta-Expected-Tenant": {testutil.TenantA}, "X-Helivanta-Expected-Subject": {"user-admin", "user-admin"}},
		"no expected headers": {},
	} {
		for _, method := range []string{"GET", "PUT"} {
			t.Run(method+" "+name, func(t *testing.T) {
				body := ""
				if method == "PUT" {
					body = update
				}
				req := httptest.NewRequest(method, "/v1/workforce/pharmacies/"+path, strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer admin")
				req.Header.Set("Content-Type", "application/json")
				for header, values := range headers {
					for _, value := range values {
						req.Header.Add(header, value)
					}
				}
				w := httptest.NewRecorder()
				h.router.ServeHTTP(w, req)
				require.Equal(t, 409, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			})
		}
	}
	unversioned := pharmacyEnrollmentInput(1, "inactive")
	delete(unversioned, "request_context_version")
	w := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, unversioned)))
	require.Equal(t, 400, w.Code, w.Body.String())

	require.Equal(t, auditBefore, h.count(t, pharmacy, "workforce_audit", "hospital_id=?", pharmacy), "a refused request writes no audit row")
	require.EqualValues(t, 1, h.count(t, pharmacy, "workforce_staff", "hospital_id=? AND id=? AND version=1 AND employment_status='active'", pharmacy, staff))
	require.Zero(t, h.count(t, pharmacy, "workforce_enrollment_profile", "hospital_id=? AND designation='Synthetic refused'", pharmacy))
}

func TestPharmacyEnrollmentHasNoMastersAndAPharmacyReportingLine(t *testing.T) {
	h, pharmacy := newPharmacyEnrollmentHarness(t)
	lead, pharmacist := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", enrollmentPath(pharmacy, lead), string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)
	hospitalStaff := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(h.router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/staff/"+hospitalStaff+"/enrollment", "admin",
		`{"expected_version":0,"staff":{"employee_code":"SYN-HOSP-1","display_name":"Synthetic nurse","job_category":"nurse","employment_status":"active"},"profile":{},"addresses":[],"emergency_contacts":[],"credentials":[],"qualifications":[],"experiences":[]}`).Code)
	input := pharmacyEnrollmentInput(0, "active")
	input["staff"].(map[string]any)["employee_code"] = "SYN-PH-ENR-2"
	path := enrollmentPath(pharmacy, pharmacist)

	for name, call := range map[string]struct {
		body   map[string]any
		status int
		code   string
	}{
		"a profile master ID":         {withProfile(input, "department_revision_id", uuid.Must(uuid.NewV7()).String()), 404, ""},
		"a free-text contract type":   {withProfile(input, "contract_type", "Synthetic permanent"), 400, "invalid_request"},
		"a hospital Staff supervisor": {withProfile(input, "supervisor_id", hospitalStaff), 404, ""},
	} {
		t.Run(name, func(t *testing.T) {
			w := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, call.body)))
			require.Equal(t, call.status, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), call.code)
		})
	}
	qualification := maps.Clone(input)
	qualification["qualifications"] = []any{map[string]any{"id": "44444444-4444-4444-8444-444444444444", "qualification": "Synthetic B.Pharm", "institution": "Synthetic institute", "qualification_revision_id": uuid.Must(uuid.NewV7()).String()}}
	w := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, qualification)))
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Zero(t, h.count(t, pharmacy, "workforce_staff", "hospital_id=? AND id=?", pharmacy, pharmacist), "a refused create writes no Staff record")

	w = pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, withProfile(input, "supervisor_id", lead))))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = pharmacyRequest(h, "admin", "PUT", enrollmentPath(pharmacy, lead), string(mustJSON(t, withProfile(pharmacyEnrollmentInput(1, "active"), "supervisor_id", pharmacist))))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "supervisor_cycle")
}

func TestPharmacyEnrollmentInactiveRevokesTheIndependentLogin(t *testing.T) {
	h, pharmacy := newPharmacyEnrollmentHarness(t)
	staff := uuid.Must(uuid.NewV7()).String()
	path := enrollmentPath(pharmacy, staff)
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)
	link := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, h.tx(t, pharmacy, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','manage',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason,independent_staff_version) VALUES(?,?,?,?,'user-linked','pending',1,'user-admin','Synthetic link','user-admin','Synthetic link',1)`, testutil.TenantA, pharmacy, staff, link).Error
	}))
	linkEvents := h.count(t, pharmacy, "outbox_events", "subject=? AND payload::text LIKE ?", "helivanta.in.workforce.independent_login_link_changed.v1", "%"+link+"%")

	w := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, pharmacyEnrollmentInput(1, "inactive"))))
	require.Equal(t, 200, w.Code, w.Body.String())
	var statuses []string
	require.NoError(t, h.tx(t, pharmacy, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT status FROM workforce_login_links WHERE tenant_id=? AND hospital_id=? AND id=?`, testutil.TenantA, pharmacy, link).Scan(&statuses).Error
	}))
	require.Equal(t, []string{"revoked"}, statuses)
	require.Equal(t, linkEvents+1, h.count(t, pharmacy, "outbox_events", "subject=? AND payload::text LIKE ?", "helivanta.in.workforce.independent_login_link_changed.v1", "%"+link+"%"))
}

func TestAPersonEmployedByAHospitalAndAPharmacyKeepsTwoEnrollments(t *testing.T) {
	h, pharmacy := newPharmacyEnrollmentHarness(t)
	staff := uuid.Must(uuid.NewV7()).String()
	hospitalPath := "/v1/workforce/hospitals/" + h.hospital + "/staff/" + staff + "/enrollment"
	hospitalEvents := h.count(t, h.hospital, "outbox_events", "subject=?", workforce.EnrollmentChanged)
	require.Equal(t, 200, testutil.Do(h.router, "PUT", hospitalPath, "admin",
		`{"expected_version":0,"staff":{"employee_code":"SYN-HOSP-2","display_name":"Synthetic dual nurse","job_category":"nurse","employment_status":"active"},"profile":{"designation":"Synthetic ward nurse"},"addresses":[],"emergency_contacts":[],"credentials":[],"qualifications":[],"experiences":[]}`).Code)
	require.Equal(t, hospitalEvents+1, h.count(t, h.hospital, "outbox_events", "subject=?", workforce.EnrollmentChanged))
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", enrollmentPath(pharmacy, staff), string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)

	hospital := testutil.Do(h.router, "GET", hospitalPath, "admin", "")
	require.Equal(t, 200, hospital.Code, hospital.Body.String())
	require.Contains(t, hospital.Body.String(), "Synthetic ward nurse")
	require.NotContains(t, hospital.Body.String(), "pharmacist@example.invalid")
	own := pharmacyRequest(h, "admin", "GET", enrollmentPath(pharmacy, staff), "")
	require.Equal(t, 200, own.Code, own.Body.String())
	require.Contains(t, own.Body.String(), "pharmacist@example.invalid")
	require.NotContains(t, own.Body.String(), "Synthetic ward nurse")
	require.Equal(t, 404, testutil.Do(h.router, "GET", "/v1/workforce/hospitals/"+pharmacy+"/staff/"+staff+"/enrollment", "admin", "").Code)
	require.Equal(t, 404, pharmacyRequest(h, "admin", "GET", enrollmentPath(h.hospital, staff), "").Code)
}
