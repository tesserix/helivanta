package workforce_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

const employmentLifecycle = "?lifecycle=withdrawal-v1"

// The pharmacy employment views are the hospital views with pharmacy_id for hospital_id.
var (
	pharmacyEmploymentSourceKeys   = []string{"employment_epoch", "pharmacy_id", "staff_id", "staff_version"}
	pharmacyEmploymentDraftKeys    = slices.Sorted(slices.Values([]string{"actor_subject", "contracting_party", "created_at", "duration", "employment_epoch", "engagement_kind", "evidence_reference", "id", "pharmacy_id", "proposed_by_subject", "reason", "revision_id", "sponsor_id", "staff_id", "staff_version", "starts_at", "status", "updated_at", "version"}))
	pharmacyEmploymentEvidenceKeys = slices.Sorted(slices.Values([]string{"actor_subject", "content_type", "created_at", "document_id", "document_record_version", "employment_epoch", "filename", "id", "period_id", "period_revision_id", "period_version", "pharmacy_id", "proposed_by_subject", "reason", "released_generation", "revision_id", "scan_attempt", "scanned_at", "sha256", "size", "source_generation", "staff_id", "staff_version", "status", "updated_at", "version"}))
)

// newPharmacyEmployment gives "reader" and "outsider" both employment permissions, so only the access row refuses them; "documents" is a manager without the enrollment permission.
func newPharmacyEmployment(t *testing.T) *pharmacyDocuments {
	t.Helper()
	both := []authz.Permission{workforce.PermEnrollmentManage, workforce.PermDocumentManage}
	d := newPharmacyDocumentsWith(t, true, map[string][]authz.Permission{"reader": both, "outsider": both, "documents": {workforce.PermDocumentManage}})
	grant(t, d.establishmentHarness, d.pharmacy, "user-documents", "manager")
	return d
}

// enrollPharmacist enrolls another active pharmacist with no credential or qualification rows.
func (d *pharmacyDocuments) enrollPharmacist(t *testing.T, pharmacy, code string) string {
	t.Helper()
	staff := uuid.Must(uuid.NewV7()).String()
	input := pharmacyEnrollmentInput(0, "active")
	input["staff"].(map[string]any)["employee_code"] = code
	for _, rows := range []string{"addresses", "emergency_contacts", "credentials", "qualifications"} {
		input[rows] = []any{}
	}
	w := pharmacyRequest(d.establishmentHarness, "admin", "PUT", enrollmentPath(pharmacy, staff), string(mustJSON(t, input)))
	require.Equal(t, 200, w.Code, w.Body.String())
	return staff
}

// employmentTerms is an employee draft body quoting the pharmacy Staff record's current source.
func (d *pharmacyDocuments) employmentTerms(t *testing.T, staff string, expected int64, changes map[string]any) string {
	t.Helper()
	source := pharmacyView(t, pharmacyRequest(d.establishmentHarness, "admin", "GET", d.path(d.pharmacy, staff, "/employment-source"), ""), pharmacyEmploymentSourceKeys, d.pharmacy)
	body := map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": expected, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"],
		"engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-12-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic pharmacy appointment", "evidence_reference": "SYN-PHARMACY-TERMS"}
	return string(mustJSON(t, with(body, changes)))
}

// employmentDraft saves version 1 of a fresh draft for the harness pharmacist and returns its path.
func (d *pharmacyDocuments) employmentDraft(t *testing.T) string {
	t.Helper()
	path := d.path(d.pharmacy, d.staff, "/employment-periods/"+uuid.Must(uuid.NewV7()).String())
	pharmacyView(t, pharmacyRequest(d.establishmentHarness, "admin", "PUT", path, d.employmentTerms(t, d.staff, 0, nil)), pharmacyEmploymentDraftKeys, d.pharmacy)
	return path
}

// employmentEvidenceFile registers and uploads one employment evidence file, and releases it unless asked not to.
func (d *pharmacyDocuments) employmentEvidenceFile(t *testing.T, category string, release bool) string {
	t.Helper()
	file := uuid.Must(uuid.NewV7()).String()
	path := d.path(d.pharmacy, d.staff, "/documents/"+file)
	w := pharmacyRequest(d.establishmentHarness, "admin", "PUT", path, d.intent("enrollment", d.staff, category))
	require.Equal(t, 201, w.Code, w.Body.String())
	w = d.upload("admin", path+"/content")
	require.Equal(t, 202, w.Code, w.Body.String())
	if release {
		state, err := d.scan(t, "stream: OK\x00", file, 1)
		require.NoError(t, err)
		require.Equal(t, "available", state)
	}
	return file
}

func pharmacyView(t *testing.T, w *httptest.ResponseRecorder, keys []string, pharmacy string) map[string]any {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row), w.Body.String())
	require.Equal(t, keys, sortedKeys(row))
	require.Equal(t, pharmacy, row["pharmacy_id"])
	return row
}

func pharmacyPage(t *testing.T, w *httptest.ResponseRecorder, keys []string, pharmacy string) []map[string]any {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct{ Data []map[string]any }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page), w.Body.String())
	for _, row := range page.Data {
		require.Equal(t, keys, sortedKeys(row))
		require.Equal(t, pharmacy, row["pharmacy_id"])
	}
	return page.Data
}

func versions(rows []map[string]any) []float64 {
	out := make([]float64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row["version"].(float64))
	}
	return out
}

func TestPharmacyManagerKeepsEmploymentTerms(t *testing.T) {
	d := newPharmacyEmployment(t)
	h := d.establishmentHarness
	sponsor := d.enrollPharmacist(t, d.pharmacy, "SYN-PH-SPONSOR")

	source := pharmacyView(t, pharmacyRequest(h, "admin", "GET", d.path(d.pharmacy, d.staff, "/employment-source"), ""), pharmacyEmploymentSourceKeys, d.pharmacy)
	require.Equal(t, d.staff, source["staff_id"])
	require.EqualValues(t, 1, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.employment_source.read'", d.pharmacy, d.staff))

	period := uuid.Must(uuid.NewV7()).String()
	path := d.path(d.pharmacy, d.staff, "/employment-periods/"+period)
	first := d.employmentTerms(t, d.staff, 0, nil)
	created := pharmacyView(t, pharmacyRequest(h, "admin", "PUT", path, first), pharmacyEmploymentDraftKeys, d.pharmacy)
	require.Equal(t, []any{period, d.staff, float64(1), "draft", "user-admin", "employee", ""}, []any{created["id"], created["staff_id"], created["version"], created["status"], created["proposed_by_subject"], created["engagement_kind"], created["sponsor_id"]})

	second := d.employmentTerms(t, d.staff, 1, map[string]any{"engagement_kind": "contractor", "contracting_party": "Synthetic Pharmacy Agency", "sponsor_id": sponsor, "reason": "Synthetic contracted pharmacist"})
	saved := pharmacyRequest(h, "admin", "PUT", path, second)
	revised := pharmacyView(t, saved, pharmacyEmploymentDraftKeys, d.pharmacy)
	require.Equal(t, []any{float64(2), "contractor", sponsor, "Synthetic Pharmacy Agency"}, []any{revised["version"], revised["engagement_kind"], revised["sponsor_id"], revised["contracting_party"]})
	replay := pharmacyRequest(h, "admin", "PUT", path, second)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())
	receipt := pharmacyRequest(h, "admin", "PUT", path, first)
	require.Equal(t, 200, receipt.Code, receipt.Body.String())
	require.Equal(t, float64(1), pharmacyView(t, receipt, pharmacyEmploymentDraftKeys, d.pharmacy)["version"], "the first command's receipt returns its own revision")
	stale := pharmacyRequest(h, "admin", "PUT", path, d.employmentTerms(t, d.staff, 1, nil))
	require.Equal(t, 409, stale.Code, stale.Body.String())
	require.Contains(t, stale.Body.String(), "employment_draft_conflict")

	current := pharmacyRequest(h, "admin", "GET", path+employmentLifecycle, "")
	require.JSONEq(t, saved.Body.String(), current.Body.String())
	drafts := pharmacyPage(t, pharmacyRequest(h, "admin", "GET", d.path(d.pharmacy, d.staff, "/employment-periods"+employmentLifecycle), ""), pharmacyEmploymentDraftKeys, d.pharmacy)
	require.Equal(t, []float64{2}, versions(drafts))
	history := pharmacyPage(t, pharmacyRequest(h, "admin", "GET", path+"/history"+employmentLifecycle, ""), pharmacyEmploymentDraftKeys, d.pharmacy)
	require.Equal(t, []float64{2, 1}, versions(history))

	withdrawal := fmt.Sprintf(`{"command_id":%q,"expected_version":2,"reason":"Synthetic withdrawn terms"}`, uuid.Must(uuid.NewV7()).String())
	withdrawn := pharmacyView(t, pharmacyRequest(h, "admin", "POST", path+"/withdraw", withdrawal), pharmacyEmploymentDraftKeys, d.pharmacy)
	require.Equal(t, []any{"withdrawn", float64(3), sponsor}, []any{withdrawn["status"], withdrawn["version"], withdrawn["sponsor_id"]}, "withdrawal preserves the terms")
	require.Equal(t, "withdrawn", pharmacyView(t, pharmacyRequest(h, "admin", "GET", path+employmentLifecycle, ""), pharmacyEmploymentDraftKeys, d.pharmacy)["status"])
	reopened := pharmacyRequest(h, "admin", "PUT", path, d.employmentTerms(t, d.staff, 3, nil))
	require.Equal(t, 409, reopened.Code, reopened.Body.String())

	for action, want := range map[string]int64{"workforce.employment_period.draft.saved": 2, "workforce.employment_period.receipt.read": 2, "workforce.employment_period.read": 2, "workforce.employment_period.history.read": 1, "workforce.employment_period.withdrawn": 1} {
		require.Equal(t, want, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action=?", d.pharmacy, period, action), action)
	}
	require.EqualValues(t, 1, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.employment_period.list'", d.pharmacy, d.staff))
	require.EqualValues(t, 3, h.count(t, d.pharmacy, "workforce_employment_period_history", "hospital_id=? AND id=?", d.pharmacy, period))
}

func TestPharmacyManagerBindsEmploymentEvidence(t *testing.T) {
	d := newPharmacyEmployment(t)
	h := d.establishmentHarness
	period := d.employmentDraft(t)
	root := period + "/revisions/1/evidence"
	binding := uuid.Must(uuid.NewV7()).String()
	path := root + "/" + binding
	attach := func(file string) string {
		return fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic pharmacy contract"}`, uuid.Must(uuid.NewV7()).String(), file)
	}

	unscanned := d.employmentEvidenceFile(t, "employment_evidence", false)
	w := pharmacyRequest(h, "admin", "PUT", path, attach(unscanned))
	require.Equal(t, 409, w.Code, w.Body.String(), "unreleased bytes cannot become employment evidence")
	identity := d.employmentEvidenceFile(t, "identity_evidence", true)
	w = pharmacyRequest(h, "admin", "PUT", path, attach(identity))
	require.Equal(t, 409, w.Code, w.Body.String(), "only the employment evidence category binds")

	file := d.employmentEvidenceFile(t, "employment_evidence", true)
	body := attach(file)
	saved := pharmacyRequest(h, "admin", "PUT", path, body)
	attached := pharmacyView(t, saved, pharmacyEmploymentEvidenceKeys, d.pharmacy)
	require.Equal(t, []any{binding, "attached", float64(1), file, float64(1), "synthetic-evidence.png"}, []any{attached["id"], attached["status"], attached["version"], attached["document_id"], attached["period_version"], attached["filename"]})
	require.NotEmpty(t, attached["released_generation"])
	replay := pharmacyRequest(h, "admin", "PUT", path, body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())

	require.Len(t, pharmacyPage(t, pharmacyRequest(h, "admin", "GET", root, ""), pharmacyEmploymentEvidenceKeys, d.pharmacy), 1)
	require.JSONEq(t, saved.Body.String(), pharmacyRequest(h, "admin", "GET", path, "").Body.String())
	content := pharmacyRequest(h, "admin", "GET", path+"/content", "")
	require.Equal(t, 200, content.Code, content.Body.String())
	require.Equal(t, d.content, content.Body.Bytes())
	require.Contains(t, content.Header().Get("Cache-Control"), "no-store")
	// sendDocument authorizes before reading the bytes and again before releasing them.
	require.EqualValues(t, 2, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.employment_evidence.disclosure.authorized'", d.pharmacy, binding))
	require.EqualValues(t, 1, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND action='workforce.employment_evidence.downloaded' AND changes->>'file_id'=?", d.pharmacy, file))

	withdrawal := fmt.Sprintf(`{"command_id":%q,"expected_version":1,"reason":"Synthetic superseded contract"}`, uuid.Must(uuid.NewV7()).String())
	withdrawn := pharmacyView(t, pharmacyRequest(h, "admin", "POST", path+"/withdraw", withdrawal), pharmacyEmploymentEvidenceKeys, d.pharmacy)
	require.Equal(t, []any{"withdrawn", float64(2)}, []any{withdrawn["status"], withdrawn["version"]})
	require.Equal(t, []float64{2, 1}, versions(pharmacyPage(t, pharmacyRequest(h, "admin", "GET", path+"/history", ""), pharmacyEmploymentEvidenceKeys, d.pharmacy)))
	gone := pharmacyRequest(h, "admin", "GET", path+"/content", "")
	require.Equal(t, 404, gone.Code, gone.Body.String())
	require.NotEqual(t, d.content, gone.Body.Bytes())
	for action, want := range map[string]int64{"workforce.employment_evidence.attached": 1, "workforce.employment_evidence.withdrawn": 1} {
		require.Equal(t, want, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action=?", d.pharmacy, binding, action), action)
	}
}

func TestPharmacyEmploymentRefusesCallersOutsideTheirAuthority(t *testing.T) {
	d := newPharmacyEmployment(t)
	h := d.establishmentHarness
	periodPath := d.employmentDraft(t)
	period := periodPath[strings.LastIndex(periodPath, "/")+1:]
	file := d.employmentEvidenceFile(t, "employment_evidence", true)
	binding := uuid.Must(uuid.NewV7()).String()
	w := pharmacyRequest(h, "admin", "PUT", periodPath+"/revisions/1/evidence/"+binding, fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic contract"}`, uuid.Must(uuid.NewV7()).String(), file))
	require.Equal(t, 200, w.Code, w.Body.String())

	sibling := d.enrollPharmacist(t, d.pharmacy, "SYN-PH-SIBLING")
	other := h.pharmacy(t, "independent")
	grant(t, h, other, "user-admin", "manager")
	elsewhere := d.enrollPharmacist(t, other, "SYN-PH-ELSEWHERE")
	supplier, supplierStaff := h.supplierStaff(t)
	hospitalStaff := uuid.Must(uuid.NewV7()).String()
	hospitalPath := "/v1/workforce/hospitals/" + h.hospital + "/staff/" + hospitalStaff
	w = testutil.Do(h.router, "PUT", hospitalPath+"/enrollment", "admin", `{"expected_version":0,"staff":{"employee_code":"SYN-HOSPITAL-TERMS","display_name":"Synthetic nurse","job_category":"nurse","employment_status":"active"}}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(h.router, "GET", hospitalPath+"/employment-source", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var hospitalSource map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &hospitalSource))
	hospitalTerms := func(changes map[string]any) string {
		return string(mustJSON(t, with(map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "expected_version": 0, "staff_version": hospitalSource["staff_version"], "employment_epoch": hospitalSource["employment_epoch"],
			"engagement_kind": "employee", "duration": "ongoing", "starts_at": "2026-12-01T00:00:00Z", "contracting_party": "", "sponsor_id": "", "reason": "Synthetic hospital appointment", "evidence_reference": "SYN-HOSPITAL-TERMS"}, changes)))
	}
	hospitalPeriod := uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(h.router, "PUT", hospitalPath+"/employment-periods/"+hospitalPeriod, "admin", hospitalTerms(nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	var terms map[string]any
	require.NoError(t, json.Unmarshal([]byte(d.employmentTerms(t, d.staff, 0, nil)), &terms))

	// bindingRows reads under the evidence view, without which RLS hides every binding row.
	bindingRows := func() int64 {
		var n int64
		require.NoError(t, h.tx(t, d.pharmacy, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_employment_evidence','read',true)`).Error; err != nil {
				return err
			}
			return tx.Table("workforce_employment_evidence_history").Count(&n).Error
		}))
		return n
	}
	drafts := h.count(t, d.pharmacy, "workforce_employment_period_history", "true")
	bindings := bindingRows()
	require.EqualValues(t, 1, drafts, "the pharmacy draft revision is visible to the count")
	require.EqualValues(t, 1, bindings, "the attached binding is visible to the count")
	auditBefore := h.count(t, d.pharmacy, "workforce_audit", "hospital_id=?", d.pharmacy)
	evidence := "/employment-periods/{period}/revisions/1/evidence"
	routes := []struct {
		name, method, suffix, query string
		period, evidence            bool
	}{
		{"source", "GET", "/employment-source", "", false, false},
		{"list", "GET", "/employment-periods", employmentLifecycle, false, false},
		{"save", "PUT", "/employment-periods/{period}", "", true, false},
		{"read", "GET", "/employment-periods/{period}", employmentLifecycle, true, false},
		{"withdraw", "POST", "/employment-periods/{period}/withdraw", "", true, false},
		{"history", "GET", "/employment-periods/{period}/history", employmentLifecycle, true, false},
		{"evidence list", "GET", evidence, "", true, true},
		{"attach", "PUT", evidence + "/{binding}", "", true, true},
		{"evidence read", "GET", evidence + "/{binding}", "", true, true},
		{"evidence withdraw", "POST", evidence + "/{binding}/withdraw", "", true, true},
		{"evidence history", "GET", evidence + "/{binding}/history", "", true, true},
		{"evidence content", "GET", evidence + "/{binding}/content", "", true, true},
	}
	// send gives each command a fresh, valid body, so a refusal is never a validation answer.
	send := func(token, method, path string) *httptest.ResponseRecorder {
		body := ""
		switch {
		case method == "PUT" && strings.Contains(path, "/evidence/"):
			body = fmt.Sprintf(`{"command_id":%q,"document_id":%q,"reason":"Synthetic refused evidence"}`, uuid.Must(uuid.NewV7()).String(), file)
		case method == "PUT":
			body = string(mustJSON(t, with(terms, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String()})))
		case method == "POST":
			body = fmt.Sprintf(`{"command_id":%q,"expected_version":1,"reason":"Synthetic refused withdrawal"}`, uuid.Must(uuid.NewV7()).String())
		}
		return pharmacyRequest(h, token, method, path, body)
	}
	target := func(route struct {
		name, method, suffix, query string
		period, evidence            bool
	}, pharmacy, staff, period string) string {
		suffix := strings.ReplaceAll(strings.ReplaceAll(route.suffix, "{period}", period), "{binding}", binding)
		return d.path(pharmacy, staff, suffix) + route.query
	}
	// periodOnly cases name an existing period of another record, so the routes that need no period, and the creating save, are skipped.
	cases := map[string]struct {
		token, pharmacy, staff, period string
		evidenceOnly, periodOnly       bool
		status                         int
	}{
		"a reader":                 {"reader", d.pharmacy, d.staff, period, false, false, 404},
		"no access row":            {"outsider", d.pharmacy, d.staff, period, false, false, 404},
		"no enrollment permission": {"documents", d.pharmacy, d.staff, period, false, false, 403},
		"no document permission":   {"directory", d.pharmacy, d.staff, period, true, false, 403},
		"a hospital ID":            {"admin", h.hospital, hospitalStaff, hospitalPeriod, false, false, 404},
		"a supplier ID":            {"admin", supplier, supplierStaff, period, false, false, 404},
		"an unregistered pharmacy": {"admin", uuid.Must(uuid.NewV7()).String(), d.staff, period, false, false, 404},
		"a foreign tenant":         {"foreign", d.pharmacy, d.staff, period, false, false, 404},
		"a missing Staff record":   {"admin", d.pharmacy, uuid.Must(uuid.NewV7()).String(), period, false, false, 404},
		"another pharmacy's Staff": {"admin", d.pharmacy, elsewhere, period, false, false, 404},
		"a hospital Staff record":  {"admin", d.pharmacy, hospitalStaff, hospitalPeriod, false, false, 404},
		"a sibling's period":       {"admin", d.pharmacy, sibling, period, false, true, 404},
		"a hospital period":        {"admin", d.pharmacy, d.staff, hospitalPeriod, false, true, 404},
		"a missing period":         {"admin", d.pharmacy, d.staff, uuid.Must(uuid.NewV7()).String(), false, true, 404},
	}
	for name, call := range cases {
		for _, route := range routes {
			if call.evidenceOnly && !route.evidence || call.periodOnly && (!route.period || route.name == "save") {
				continue
			}
			t.Run(route.name+" "+name, func(t *testing.T) {
				w := send(call.token, route.method, target(route, call.pharmacy, call.staff, call.period))
				require.Equal(t, call.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "Synthetic pharmacy appointment")
				require.NotContains(t, w.Body.String(), "synthetic-evidence.png")
			})
		}
	}
	for _, route := range routes {
		path := target(route, d.pharmacy, d.staff, period)
		t.Run(route.name+" HR intake view", func(t *testing.T) {
			intake := "?hr_intake=v1"
			if route.query != "" {
				intake = "&hr_intake=v1"
			}
			w := send("admin", route.method, path+intake)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
		if route.query != "" {
			bare := strings.TrimSuffix(path, route.query)
			for name, query := range map[string]string{"no lifecycle view": "", "a repeated lifecycle view": employmentLifecycle + "&lifecycle=withdrawal-v1", "an unknown lifecycle view": "?lifecycle=withdrawal-v2"} {
				t.Run(route.name+" "+name, func(t *testing.T) {
					w := send("admin", route.method, bare+query)
					require.Equal(t, 400, w.Code, w.Body.String())
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
			t.Run(route.name+" "+name, func(t *testing.T) {
				req := httptest.NewRequest(route.method, "/v1/workforce/pharmacies/"+path, strings.NewReader(""))
				req.Header.Set("Authorization", "Bearer admin")
				for header, values := range headers {
					for _, value := range values {
						req.Header.Add(header, value)
					}
				}
				w := httptest.NewRecorder()
				h.router.ServeHTTP(w, req)
				require.Equal(t, 409, w.Code, w.Body.String())
			})
		}
	}
	t.Run("missing revision", func(t *testing.T) {
		for _, suffix := range []string{"", "/" + binding, "/" + binding + "/history", "/" + binding + "/content"} {
			w := pharmacyRequest(h, "admin", "GET", periodPath+"/revisions/2/evidence"+suffix, "")
			require.Equal(t, 404, w.Code, "%s: %s", suffix, w.Body.String())
		}
	})

	for name, sponsor := range map[string]string{"a hospital Staff sponsor": hospitalStaff, "another pharmacy's sponsor": elsewhere, "a supplier sponsor": supplierStaff, "a missing sponsor": uuid.Must(uuid.NewV7()).String()} {
		t.Run(name, func(t *testing.T) {
			path := d.path(d.pharmacy, d.staff, "/employment-periods/"+uuid.Must(uuid.NewV7()).String())
			w := pharmacyRequest(h, "admin", "PUT", path, string(mustJSON(t, with(terms, map[string]any{"command_id": uuid.Must(uuid.NewV7()).String(), "engagement_kind": "agency_worker", "contracting_party": "Synthetic Agency", "sponsor_id": sponsor}))))
			require.Equal(t, 404, w.Code, w.Body.String())
		})
	}
	t.Run("hospital draft naming a pharmacy sponsor", func(t *testing.T) {
		w := testutil.Do(h.router, "PUT", hospitalPath+"/employment-periods/"+uuid.Must(uuid.NewV7()).String(), "admin", hospitalTerms(map[string]any{"engagement_kind": "contractor", "contracting_party": "Synthetic Agency", "sponsor_id": d.staff}))
		require.Equal(t, 404, w.Code, w.Body.String())
	})
	pharmacyEvidence := "/employment-periods/" + period + "/revisions/1/evidence/" + binding
	for name, path := range map[string]string{
		"a pharmacy ID":               "/v1/workforce/hospitals/" + d.pharmacy + "/staff/" + d.staff + "/employment-source",
		"a pharmacy Staff record":     "/v1/workforce/hospitals/" + h.hospital + "/staff/" + d.staff + "/employment-source",
		"a pharmacy period":           hospitalPath + "/employment-periods/" + period,
		"pharmacy evidence":           "/v1/workforce/hospitals/" + d.pharmacy + "/staff/" + d.staff + pharmacyEvidence,
		"pharmacy evidence content":   "/v1/workforce/hospitals/" + d.pharmacy + "/staff/" + d.staff + pharmacyEvidence + "/content",
		"pharmacy evidence by its ID": hospitalPath + "/employment-periods/" + hospitalPeriod + "/revisions/1/evidence/" + binding,
	} {
		t.Run("hospital family on "+name, func(t *testing.T) {
			w := testutil.Do(h.router, "GET", path, "admin", "")
			require.Equal(t, 404, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "Synthetic pharmacy appointment")
		})
	}

	require.Equal(t, drafts, h.count(t, d.pharmacy, "workforce_employment_period_history", "true"), "a refused command writes no draft revision")
	require.Equal(t, bindings, bindingRows(), "a refused command writes no binding")
	require.Equal(t, auditBefore, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=?", d.pharmacy), "a refused request writes no audit row")
}
