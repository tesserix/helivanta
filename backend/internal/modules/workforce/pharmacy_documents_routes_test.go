package workforce_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const pharmacyCredentialID, pharmacyQualificationID = "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"

// pharmacyDocumentKeys is the pharmacy file shape: the hospital view's visible fields with pharmacy_id for hospital_id.
var pharmacyDocumentKeys = []string{"category", "content_type", "created_at", "expires_at", "failure_code", "filename", "id", "pharmacy_id", "record_id", "record_kind", "record_version", "scan_attempt", "scanned_at", "sha256", "size", "staff_id", "status", "updated_at", "uploaded_at"}

type pharmacyDocuments struct {
	*establishmentHarness
	pharmacy, staff string
	files           *attachments.GCS
	worker          *tenantdb.DB
	content         []byte
}

// newPharmacyDocuments enrolls one pharmacist; "admin" manages the pharmacy and the hospital, "directory" manages the pharmacy without the document permission.
func newPharmacyDocuments(t *testing.T, storage bool) *pharmacyDocuments {
	t.Helper()
	var files *attachments.GCS
	if storage {
		cloud := testinfra.NewGCSProtocol(t)
		var err error
		files, err = attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
		require.NoError(t, err)
	}
	manage := []authz.Permission{workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}
	module := workforce.New()
	router, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "directory": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{"admin": manage, "foreign": manage, "reader": {workforce.PermDocumentManage}, "outsider": {workforce.PermDocumentManage},
			"directory": {workforce.PermHospitalManage, workforce.PermEnrollmentManage}},
		Modules: []platform.Module{module}, Documents: files,
	})
	h := &establishmentHarness{router: router, db: db, bus: bus, module: module, hospital: uuid.Must(uuid.NewV7()).String()}
	require.Equal(t, 200, testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital, "admin", `{"name":"Synthetic Document Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(router, "PUT", "/v1/workforce/hospitals/"+h.hospital+"/access/user-admin", "admin", `{"level":"manager"}`).Code)
	pharmacy := h.pharmacy(t, "independent")
	grant(t, h, pharmacy, "user-admin", "manager")
	grant(t, h, pharmacy, "user-reader", "reader")
	grant(t, h, pharmacy, "user-directory", "manager")
	staff := uuid.Must(uuid.NewV7()).String()
	w := pharmacyRequest(h, "admin", "PUT", enrollmentPath(pharmacy, staff), string(mustJSON(t, pharmacyEnrollmentInput(0, "active"))))
	require.Equal(t, 200, w.Code, w.Body.String())
	var picture bytes.Buffer
	require.NoError(t, png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	return &pharmacyDocuments{establishmentHarness: h, pharmacy: pharmacy, staff: staff, files: files, worker: worker, content: picture.Bytes()}
}

func (d *pharmacyDocuments) path(pharmacy, staff, rest string) string {
	return pharmacy + "/staff/" + staff + rest
}

func (d *pharmacyDocuments) intent(kind, record, category string) string {
	digest := sha256.Sum256(d.content)
	return `{"record_kind":"` + kind + `","record_id":"` + record + `","category":"` + category + `","filename":"synthetic-evidence.png","content_type":"image/png","size":` + strconv.Itoa(len(d.content)) + `,"sha256":"` + hex.EncodeToString(digest[:]) + `"}`
}

func (d *pharmacyDocuments) upload(token, path string) *httptest.ResponseRecorder {
	return pharmacyContent(d.establishmentHarness, token, "PUT", path, "image/png", bytes.NewReader(d.content))
}

// stored registers and uploads one enrollment file under the harness pharmacist.
func (d *pharmacyDocuments) stored(t *testing.T) string {
	t.Helper()
	file := uuid.Must(uuid.NewV7()).String()
	path := d.path(d.pharmacy, d.staff, "/documents/"+file)
	w := pharmacyRequest(d.establishmentHarness, "admin", "PUT", path, d.intent("enrollment", d.staff, "identity_evidence"))
	require.Equal(t, 201, w.Code, w.Body.String())
	w = d.upload("admin", path+"/content")
	require.Equal(t, 202, w.Code, w.Body.String())
	return file
}

func (d *pharmacyDocuments) scan(t *testing.T, verdict, file string, attempt int64) (string, error) {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, verdict), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), d.worker, d.files, scanner)
	require.NoError(t, err)
	return processor.Process(t.Context(), workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: d.pharmacy, StaffID: d.staff, FileID: file, ScanAttempt: attempt})
}

// pharmacyReceipt decodes a pharmacy file response, which names its establishment pharmacy_id and never hospital_id.
func pharmacyReceipt(t *testing.T, w *httptest.ResponseRecorder, pharmacy string) map[string]any {
	t.Helper()
	var row map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &row), w.Body.String())
	require.Equal(t, pharmacyDocumentKeys, sortedKeys(row))
	require.Equal(t, pharmacy, row["pharmacy_id"])
	return row
}

func TestPharmacyManagerKeepsStaffDocuments(t *testing.T) {
	d := newPharmacyDocuments(t, true)
	h := d.establishmentHarness

	records := pharmacyRequest(h, "admin", "GET", d.path(d.pharmacy, d.staff, "/document-records"), "")
	require.Equal(t, 200, records.Code, records.Body.String())
	require.JSONEq(t, `{"uploads_available":true,"records":[
		{"record_kind":"enrollment","record_id":"`+d.staff+`","label":"Staff enrollment","categories":["employment_evidence","identity_evidence"]},
		{"record_kind":"credential","record_id":"`+pharmacyCredentialID+`","label":"registration · Synthetic pharmacy council","categories":["credential_evidence"]},
		{"record_kind":"qualification","record_id":"`+pharmacyQualificationID+`","label":"Synthetic B.Pharm · Synthetic institute","categories":["qualification_evidence"]}]}`, records.Body.String())

	file := uuid.Must(uuid.NewV7()).String()
	path := d.path(d.pharmacy, d.staff, "/documents/"+file)
	intent := d.intent("enrollment", d.staff, "identity_evidence")
	created := pharmacyRequest(h, "admin", "PUT", path, intent)
	require.Equal(t, 201, created.Code, created.Body.String())
	receipt := pharmacyReceipt(t, created, d.pharmacy)
	require.Equal(t, "upload_pending", receipt["status"])
	require.Equal(t, d.staff, receipt["staff_id"])
	replay := pharmacyRequest(h, "admin", "PUT", path, intent)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	changed := pharmacyRequest(h, "admin", "PUT", path, d.intent("enrollment", d.staff, "employment_evidence"))
	require.Equal(t, 409, changed.Code, changed.Body.String())

	uploaded := d.upload("admin", path+"/content")
	require.Equal(t, 202, uploaded.Code, uploaded.Body.String())
	require.Equal(t, "scan_pending", pharmacyReceipt(t, uploaded, d.pharmacy)["status"])
	again := d.upload("admin", path+"/content")
	require.Equal(t, 200, again.Code, again.Body.String())
	require.JSONEq(t, uploaded.Body.String(), again.Body.String())
	var requests []struct{ Payload string }
	require.NoError(t, h.tx(t, d.pharmacy, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Select("payload").Where("subject=?", workforce.DocumentScanRequested).Find(&requests).Error
	}))
	require.Len(t, requests, 1)
	var event events.Event
	require.NoError(t, json.Unmarshal([]byte(requests[0].Payload), &event))
	var scan workforcecontract.DocumentScanRequestedData
	require.NoError(t, json.Unmarshal(event.Data, &scan))
	require.Equal(t, workforcecontract.DocumentScanRequestedData{HospitalID: d.pharmacy, StaffID: d.staff, FileID: file, ScanAttempt: 1}, scan, "the scan request names the owning establishment row")

	early := pharmacyRequest(h, "admin", "GET", path+"/content", "")
	require.Equal(t, 409, early.Code, early.Body.String())
	status, err := d.scan(t, "stream: OK\x00", file, 1)
	require.NoError(t, err)
	require.Equal(t, "available", status)

	listed := pharmacyRequest(h, "admin", "GET", d.path(d.pharmacy, d.staff, "/documents"), "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	var page struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, pharmacyDocumentKeys, sortedKeys(page.Data[0]))
	require.Equal(t, d.pharmacy, page.Data[0]["pharmacy_id"])
	require.Equal(t, "available", page.Data[0]["status"])

	downloaded := pharmacyRequest(h, "admin", "GET", path+"/content", "")
	require.Equal(t, 200, downloaded.Code, downloaded.Body.String())
	require.Equal(t, d.content, downloaded.Body.Bytes())
	require.Equal(t, "private, no-store", downloaded.Header().Get("Cache-Control"))
	require.EqualValues(t, 1, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=? AND resource_id=? AND action='workforce.document.downloaded' AND changes->>'file_id'=?", d.pharmacy, d.staff, file))

	for kind, record := range map[string]string{"credential": pharmacyCredentialID, "qualification": pharmacyQualificationID} {
		w := pharmacyRequest(h, "admin", "PUT", d.path(d.pharmacy, d.staff, "/documents/"+uuid.Must(uuid.NewV7()).String()), d.intent(kind, record, kind+"_evidence"))
		require.Equal(t, 201, w.Code, w.Body.String())
		require.Equal(t, record, pharmacyReceipt(t, w, d.pharmacy)["record_id"])
	}

	revoked := pharmacyRequest(h, "admin", "PUT", path+"/revocation", "")
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Equal(t, "revoked", pharmacyReceipt(t, revoked, d.pharmacy)["status"])
	gone := pharmacyRequest(h, "admin", "GET", path+"/content", "")
	require.Equal(t, 409, gone.Code, gone.Body.String())

	failed := d.stored(t)
	status, err = d.scan(t, "stream: ERROR\x00", failed, 1)
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	retryPath := d.path(d.pharmacy, d.staff, "/documents/"+failed+"/retries/2")
	for _, expected := range []int{202, 200} {
		w := pharmacyRequest(h, "admin", "PUT", retryPath, "")
		require.Equal(t, expected, w.Code, w.Body.String())
		retried := pharmacyReceipt(t, w, d.pharmacy)
		require.Equal(t, float64(2), retried["scan_attempt"])
		require.Equal(t, "scan_pending", retried["status"])
	}
	require.EqualValues(t, 0, h.count(t, d.pharmacy, "workforce_documents", "hospital_id<>?", d.pharmacy))
}

func TestPharmacyDocumentsRefuseCallersOutsideTheirAuthority(t *testing.T) {
	d := newPharmacyDocuments(t, true)
	h := d.establishmentHarness
	file := d.stored(t)

	other := h.pharmacy(t, "independent")
	grant(t, h, other, "user-admin", "manager")
	elsewhere := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, pharmacyRequest(h, "admin", "PUT", enrollmentPath(other, elsewhere), string(mustJSON(t, pharmacyEnrollmentInput(0, "active")))).Code)
	sibling := uuid.Must(uuid.NewV7()).String()
	siblingInput := pharmacyEnrollmentInput(0, "active")
	siblingInput["staff"].(map[string]any)["employee_code"] = "SYN-PH-SIBLING"
	for _, rows := range []string{"addresses", "emergency_contacts", "credentials", "qualifications"} {
		siblingInput[rows] = []any{}
	}
	w := pharmacyRequest(h, "admin", "PUT", enrollmentPath(d.pharmacy, sibling), string(mustJSON(t, siblingInput)))
	require.Equal(t, 200, w.Code, w.Body.String())
	supplier, supplierStaff := h.supplierStaff(t)
	hospitalStaff, hospitalFile := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	hospitalPath := "/v1/workforce/hospitals/" + h.hospital + "/staff/" + hospitalStaff
	hospitalCredential := uuid.Must(uuid.NewV7()).String()
	w = testutil.Do(h.router, "PUT", hospitalPath+"/enrollment", "admin", `{"expected_version":0,"staff":{"employee_code":"SYN-HOSPITAL-DOC","display_name":"Synthetic nurse","job_category":"nurse","employment_status":"active"},"credentials":[{"id":"`+hospitalCredential+`","type":"registration","number":"SYN-NURSE-1","issuer":"Synthetic nursing council"}]}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(h.router, "PUT", hospitalPath+"/documents/"+hospitalFile, "admin", d.intent("enrollment", hospitalStaff, "identity_evidence"))
	require.Equal(t, 201, w.Code, w.Body.String())

	documentsBefore := h.count(t, d.pharmacy, "workforce_documents", "true")
	auditBefore := h.count(t, d.pharmacy, "workforce_audit", "hospital_id=?", d.pharmacy)
	routes := []struct {
		name, method, suffix string
		file                 bool
	}{
		{"record choices", "GET", "/document-records", false},
		{"list", "GET", "/documents", false},
		{"intent", "PUT", "/documents/{file}", true},
		{"upload", "PUT", "/documents/{file}/content", true},
		{"download", "GET", "/documents/{file}/content", true},
		{"revocation", "PUT", "/documents/{file}/revocation", true},
		{"retry", "PUT", "/documents/{file}/retries/2", true},
	}
	// send gives the intent route a body naming the path's own Staff record, so refusal is never a body mismatch.
	send := func(token, method, path, staff string) *httptest.ResponseRecorder {
		if strings.HasSuffix(path, "/content") && method == "PUT" {
			return d.upload(token, path)
		}
		body := ""
		if method == "PUT" && strings.Count(path, "/documents/") == 1 && !strings.Contains(path[strings.Index(path, "/documents/")+len("/documents/"):], "/") {
			body = d.intent("enrollment", staff, "identity_evidence")
		}
		return pharmacyRequest(h, token, method, path, body)
	}
	// fresh marks a file ID unused in the path's establishment: the intent route may claim it, every other file route must refuse.
	cases := map[string]struct {
		token, pharmacy, staff, file string
		fileOnly, fresh              bool
		status                       int
	}{
		"a reader":                       {"reader", d.pharmacy, d.staff, file, false, false, 404},
		"no access row":                  {"outsider", d.pharmacy, d.staff, file, false, false, 404},
		"no document permission":         {"directory", d.pharmacy, d.staff, file, false, false, 403},
		"a hospital ID":                  {"admin", h.hospital, hospitalStaff, hospitalFile, false, false, 404},
		"a supplier ID":                  {"admin", supplier, supplierStaff, file, false, false, 404},
		"an unregistered pharmacy":       {"admin", uuid.Must(uuid.NewV7()).String(), d.staff, file, false, false, 404},
		"a foreign tenant":               {"foreign", d.pharmacy, d.staff, file, false, false, 404},
		"a missing Staff record":         {"admin", d.pharmacy, uuid.Must(uuid.NewV7()).String(), file, false, false, 404},
		"another pharmacy's Staff":       {"admin", d.pharmacy, elsewhere, file, false, false, 404},
		"a hospital Staff record":        {"admin", d.pharmacy, hospitalStaff, hospitalFile, false, false, 404},
		"a sibling's file":               {"admin", d.pharmacy, sibling, file, true, false, 404},
		"a hospital file":                {"admin", d.pharmacy, d.staff, hospitalFile, true, true, 404},
		"a file of another Staff record": {"admin", other, elsewhere, file, true, true, 404},
	}
	for name, call := range cases {
		for _, route := range routes {
			if call.fileOnly && !route.file || call.fresh && route.name == "intent" {
				continue
			}
			t.Run(route.name+" "+name, func(t *testing.T) {
				path := d.path(call.pharmacy, call.staff, strings.ReplaceAll(route.suffix, "{file}", call.file))
				w := send(call.token, route.method, path, call.staff)
				require.Equal(t, call.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "synthetic-evidence.png")
			})
		}
	}
	for _, route := range routes {
		path := d.path(d.pharmacy, d.staff, strings.ReplaceAll(route.suffix, "{file}", file))
		t.Run(route.name+" the HR intake view", func(t *testing.T) {
			w := send("admin", route.method, path+"?hr_intake=v1", d.staff)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
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
	fresh := func() string { return "/documents/" + uuid.Must(uuid.NewV7()).String() }
	for name, call := range map[string]struct{ staff, credential string }{
		"a hospital credential":  {d.staff, hospitalCredential},
		"a sibling's credential": {sibling, pharmacyCredentialID},
	} {
		t.Run("intent binding "+name, func(t *testing.T) {
			w := pharmacyRequest(h, "admin", "PUT", d.path(d.pharmacy, call.staff, fresh()), d.intent("credential", call.credential, "credential_evidence"))
			require.Equal(t, 404, w.Code, w.Body.String())
		})
	}
	t.Run("hospital intent binding a pharmacy credential", func(t *testing.T) {
		w := testutil.Do(h.router, "PUT", hospitalPath+fresh(), "admin", d.intent("credential", pharmacyCredentialID, "credential_evidence"))
		require.Equal(t, 404, w.Code, w.Body.String())
	})
	// A pharmacy file ID is fresh under a hospital Staff record, so only its read routes are checked there.
	for _, target := range []struct {
		name, path string
		intent     bool
	}{
		{"a pharmacy ID", "/v1/workforce/hospitals/" + d.pharmacy + "/staff/" + d.staff + "/documents/" + file, true},
		{"a pharmacy Staff record", hospitalPath[:strings.LastIndex(hospitalPath, "/")+1] + d.staff + "/documents/" + file, true},
		{"a pharmacy file", hospitalPath + "/documents/" + file, false},
	} {
		for _, suffix := range []string{"", "/content", "/classification"} {
			if suffix == "" && !target.intent {
				continue
			}
			t.Run("hospital family on "+target.name+suffix, func(t *testing.T) {
				w := testutil.Do(h.router, "GET", target.path+suffix, "admin", "")
				if suffix == "" {
					w = testutil.Do(h.router, "PUT", target.path, "admin", d.intent("enrollment", d.staff, "identity_evidence"))
				}
				require.Equal(t, 404, w.Code, w.Body.String())
			})
		}
	}

	require.Equal(t, documentsBefore, h.count(t, d.pharmacy, "workforce_documents", "true"))
	require.Equal(t, auditBefore, h.count(t, d.pharmacy, "workforce_audit", "hospital_id=?", d.pharmacy))
	require.EqualValues(t, 1, h.count(t, d.pharmacy, "workforce_documents", "hospital_id=? AND id=? AND status='scan_pending' AND scan_attempt=1", d.pharmacy, file))
}

func TestPharmacyDocumentsWithoutStorageWriteNoIntent(t *testing.T) {
	d := newPharmacyDocuments(t, false)
	h := d.establishmentHarness
	records := pharmacyRequest(h, "admin", "GET", d.path(d.pharmacy, d.staff, "/document-records"), "")
	require.Equal(t, 200, records.Code, records.Body.String())
	require.Contains(t, records.Body.String(), `"uploads_available":false`)
	path := d.path(d.pharmacy, d.staff, "/documents/"+uuid.Must(uuid.NewV7()).String())
	for _, call := range []struct{ method, path, body string }{
		{"PUT", path, d.intent("enrollment", d.staff, "identity_evidence")},
		{"GET", path + "/content", ""},
		{"PUT", path + "/retries/2", ""},
	} {
		w := pharmacyRequest(h, "admin", call.method, call.path, call.body)
		require.Equal(t, 503, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "documents_unavailable")
	}
	require.EqualValues(t, 0, h.count(t, d.pharmacy, "workforce_documents", "true"))
}
