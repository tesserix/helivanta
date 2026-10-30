package pharmacy_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"                           //nolint:depguard // external test importing its own module
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type imageScenario struct {
	prescriptionWorld
	cloud          *testinfra.GCSProtocol
	rx, path, body string
	data           []byte
}

func newImageScenario(t *testing.T) imageScenario {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	w := newPrescriptionWorld(t, files)
	rx := w.record(t)
	return imageScenarioForPrescription(t, w, cloud, rx)
}

func imageScenarioForPrescription(t *testing.T, w prescriptionWorld, cloud *testinfra.GCSProtocol, rx string) imageScenario {
	t.Helper()
	var page bytes.Buffer
	require.NoError(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := page.Bytes()
	digest := sha256.Sum256(data)
	body := fmt.Sprintf(`{"filename":"TEST-prescription-page-1.png","content_type":"image/png","size":%d,"sha256":"%s"}`, len(data), hex.EncodeToString(digest[:]))
	return imageScenario{prescriptionWorld: w, cloud: cloud, rx: rx, path: prescriptionPath("/" + rx + "/images/" + uuid.Must(uuid.NewV7()).String()), body: body, data: data}
}

func (s imageScenario) upload(token, contentType string, data []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", s.path+"/content", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, r)
	return w
}

func (s imageScenario) input() pharmacycontract.PrescriptionImageScanInput {
	return pharmacycontract.PrescriptionImageScanInput{TenantID: testutil.TenantA, OutletID: pharmaOutlet, PrescriptionID: s.rx, FileID: s.path[strings.LastIndex(s.path, "/")+1:], ScanAttempt: 1}
}

func (s imageScenario) worker(t *testing.T) *tenantdb.DB {
	t.Helper()
	var database string
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PharmacyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	return worker
}

func (s imageScenario) processor(t *testing.T, reply string) *pharmacy.PrescriptionImageProcessor {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, reply), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := pharmacy.NewPrescriptionImageProcessor(t.Context(), s.worker(t), s.files, scanner)
	require.NoError(t, err)
	return processor
}

func (s imageScenario) outbox(t *testing.T, subject string) []json.RawMessage {
	t.Helper()
	var payloads []json.RawMessage
	require.NoError(t, s.db.WithTenant(tenantdb.WithActor(t.Context(), "test"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", subject).Order("created_at").Pluck("payload", &payloads).Error
	}))
	return payloads
}

func TestPrescriptionImageIntentsArePinnedAndExactlyReplayable(t *testing.T) {
	s := newImageScenario(t)
	created := do(s.r, "PUT", s.path, "pharmacist", s.body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"status":"upload_pending"`)
	require.NotContains(t, created.Body.String(), "sha256", "the checksum is not echoed")
	require.Equal(t, http.StatusOK, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	changed := strings.Replace(s.body, "TEST-prescription-page-1.png", "TEST-other.png", 1)
	require.Equal(t, http.StatusConflict, do(s.r, "PUT", s.path, "pharmacist", changed).Code, "a replay cannot rename the page")

	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "pharmacist", "").Code, "unscanned bytes are never downloadable")
	require.Equal(t, http.StatusNotFound, do(s.r, "PUT", s.path, "viewer", s.body).Code, "a reader grant cannot add pages")
	require.Equal(t, http.StatusForbidden, do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "owner", "").Code)
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "foreign", "").Code)
	list := do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "viewer", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), "TEST-prescription-page-1.png")

	unknown := prescriptionPath("/" + uuid.Must(uuid.NewV7()).String() + "/images/" + uuid.Must(uuid.NewV7()).String())
	require.Equal(t, http.StatusNotFound, do(s.r, "PUT", unknown, "pharmacist", s.body).Code, "a page needs a recorded prescription")
	for _, bad := range []string{
		strings.Replace(s.body, "TEST-prescription-page-1.png", "../TEST.png", 1),
		strings.Replace(s.body, "image/png", "application/pdf", 1),
		strings.Replace(s.body, `"sha256":"`, `"sha256":"G`, 1),
	} {
		require.Equal(t, http.StatusBadRequest, do(s.r, "PUT", prescriptionPath("/"+s.rx+"/images/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", bad).Code, bad)
	}
}

func TestPrescriptionImagesNeedPrivateStorage(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	rx := w.record(t)
	res := do(w.r, "PUT", prescriptionPath("/"+rx+"/images/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", `{"filename":"TEST.png","content_type":"image/png","size":10,"sha256":"`+strings.Repeat("a", 64)+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code, res.Body.String())
}

func TestPrescriptionImageUploadValidatesBytesBeforeQuarantine(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	var writes atomic.Int32
	s.cloud.AfterQuarantineWrite(func() { writes.Add(1) })
	for _, tc := range []struct {
		name, contentType string
		data              []byte
	}{
		{"wrong MIME", "application/octet-stream", s.data},
		{"MIME parameters", "image/png;charset=utf-8", s.data},
		{"short bytes", "image/png", s.data[:len(s.data)-1]},
		{"long bytes", "image/png", append(append([]byte{}, s.data...), 0)},
		{"wrong checksum", "image/png", bytes.Repeat([]byte{'x'}, len(s.data))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, http.StatusBadRequest, s.upload("pharmacist", tc.contentType, tc.data).Code)
		})
	}
	require.Zero(t, writes.Load(), "rejected bytes never reach quarantine")
	require.Equal(t, http.StatusNotFound, s.upload("foreign", "image/png", s.data).Code)
	require.Equal(t, http.StatusNotFound, s.upload("viewer", "image/png", s.data).Code)

	first := s.upload("pharmacist", "image/png", s.data)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"status":"scan_pending"`)
	require.NotContains(t, first.Body.String(), "generation")
	again := s.upload("pharmacist", "image/png", s.data)
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	require.JSONEq(t, first.Body.String(), again.Body.String())
	require.EqualValues(t, 1, writes.Load())
	require.Len(t, s.outbox(t, pharmacycontract.SubjectPrescriptionImageScanRequested), 1, "an exact replay does not queue another scan")
}

func TestTheOutboxCarriesNoPatientOrPrescriberDetails(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("pharmacist", "image/png", s.data).Code)
	payloads := s.outbox(t, pharmacycontract.SubjectPrescriptionImageScanRequested)
	require.Len(t, payloads, 1)
	var envelope struct{ Data map[string]any }
	require.NoError(t, json.Unmarshal(payloads[0], &envelope))
	keys := make([]string, 0, len(envelope.Data))
	for k := range envelope.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	require.Equal(t, []string{"file_id", "outlet_id", "prescription_id", "scan_attempt"}, keys)
	require.NotContains(t, string(payloads[0]), "TEST Ravi Kumar")
	require.NotContains(t, string(payloads[0]), "MMC/2011/04567")
}

func TestTheWorkerReleasesOnlyCleanPinnedBytesAndDownloadsAreAudited(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("pharmacist", "image/png", s.data).Code)
	processor := s.processor(t, "stream: OK\x00")
	in := s.input()
	foreign := in
	foreign.TenantID = testutil.TenantB
	_, err := processor.Process(t.Context(), foreign)
	require.Error(t, err, "the worker finds nothing outside the tenant it is given")
	otherOutlet := in
	otherOutlet.OutletID = uuid.Must(uuid.NewV7()).String()
	_, err = processor.Process(t.Context(), otherOutlet)
	require.Error(t, err)
	require.Zero(t, s.cloud.CopyCount())
	for range 3 {
		status, err := processor.Process(t.Context(), in)
		require.NoError(t, err)
		require.Equal(t, "available", status)
	}
	require.Equal(t, 1, s.cloud.CopyCount(), "a repeated activity promotes once")

	download := do(s.r, "GET", s.path+"/download", "viewer", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.Equal(t, s.data, download.Body.Bytes())
	require.Equal(t, "image/png", download.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", download.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", download.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "default-src 'none'; sandbox", download.Header().Get("Content-Security-Policy"))
	require.Contains(t, download.Header().Get("Content-Disposition"), "attachment")
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "foreign", "").Code)
	require.Equal(t, http.StatusForbidden, do(s.r, "GET", s.path+"/download", "owner", "").Code)

	changed := s.outbox(t, pharmacycontract.SubjectPrescriptionImageChanged)
	require.Len(t, changed, 1)
	var envelope struct{ Data struct{ Status string } }
	require.NoError(t, json.Unmarshal(changed[0], &envelope))
	require.Equal(t, "available", envelope.Data.Status)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("audit_events").Where("resource_type='pharmacy_prescription_images' AND resource_id=? AND action='read' AND actor_subject='user-viewer'", in.FileID).Count(&n).Error)
		require.EqualValues(t, 1, n, "every download is audited")
		return nil
	}))
}

func TestAnInfectedPageIsRejectedAndNeverDownloadable(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("pharmacist", "image/png", s.data).Code)
	status, err := s.processor(t, "stream: Eicar-Test-Signature FOUND\x00").Process(t.Context(), s.input())
	require.NoError(t, err)
	require.Equal(t, "rejected", status)
	require.Zero(t, s.cloud.CopyCount())
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "pharmacist", "").Code)
	page := do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "pharmacist", "")
	require.Contains(t, page.Body.String(), `"failure_code":"malware_detected"`)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", s.path+"/retry/2", "pharmacist", "").Code, "a rejected page is not retried")
}

func TestAScanErrorRetriesOnlyOnTheNextAttempt(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("pharmacist", "image/png", s.data).Code)
	status, err := s.processor(t, "UNKNOWN COMMAND\x00").Process(t.Context(), s.input())
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", s.path+"/retry/3", "pharmacist", "").Code)
	require.Equal(t, http.StatusBadRequest, do(s.r, "POST", s.path+"/retry/02", "pharmacist", "").Code)
	require.Equal(t, http.StatusNotFound, do(s.r, "POST", s.path+"/retry/2", "viewer", "").Code)
	retry := do(s.r, "POST", s.path+"/retry/2", "pharmacist", "")
	require.Equal(t, http.StatusAccepted, retry.Code, retry.Body.String())
	require.Equal(t, http.StatusOK, do(s.r, "POST", s.path+"/retry/2", "pharmacist", "").Code, "the same attempt replays")
	require.Len(t, s.outbox(t, pharmacycontract.SubjectPrescriptionImageScanRequested), 2)
	next := s.input()
	next.ScanAttempt = 2
	status, err = s.processor(t, "stream: OK\x00").Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status)
}

func workerScope(t *testing.T, worker *tenantdb.DB, in pharmacycontract.PrescriptionImageScanInput, fn func(*gorm.DB) error) error {
	t.Helper()
	return worker.WithTenant(tenantdb.WithActor(t.Context(), "service:pharmacy-evidence-worker"), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_prescription',?,true),set_config('app.pharmacy_file',?,true)`, in.OutletID, in.PrescriptionID, in.FileID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func TestTheWorkerIdentityReachesOnlyItsOneFile(t *testing.T) {
	s := newImageScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "pharmacist", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("pharmacist", "image/png", s.data).Code)
	other := prescriptionPath("/" + s.rx + "/images/" + uuid.Must(uuid.NewV7()).String())
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", other, "pharmacist", s.body).Code)

	_, err := pharmacy.NewPrescriptionImageProcessor(t.Context(), s.db, s.files, nil)
	require.Error(t, err)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	_, err = pharmacy.NewPrescriptionImageProcessor(t.Context(), s.db, s.files, scanner)
	require.Error(t, err, "the API identity cannot process prescription images")

	worker := s.worker(t)
	in := s.input()
	require.NoError(t, workerScope(t, worker, in, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Raw(`SELECT count(*) FROM pharmacy_prescription_images`).Scan(&n).Error)
		require.EqualValues(t, 1, n, "the worker sees exactly the file its session names")
		return nil
	}))
	for _, query := range []string{
		`SELECT filename FROM pharmacy_prescription_images`,
		`SELECT uploader_subject FROM pharmacy_prescription_images`,
		`SELECT * FROM pharmacy_prescriptions`,
		`SELECT * FROM pharmacy_outlets`,
		`SELECT * FROM patients`,
		`SELECT * FROM audit_events`,
		`SELECT payload FROM outbox_events`,
	} {
		t.Run(query, func(t *testing.T) {
			require.Error(t, workerScope(t, worker, in, func(tx *gorm.DB) error { var rows []map[string]any; return tx.Raw(query).Scan(&rows).Error }))
		})
	}
	for _, tc := range []struct{ name, action, resource, fields string }{
		{"another table", "update", "pharmacy_prescriptions", "status"},
		{"read event", "read", "pharmacy_prescription_images", "status"},
		{"identity field", "update", "pharmacy_prescription_images", "filename"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, workerScope(t, worker, in, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields) VALUES(?,'service:pharmacy-evidence-worker',?,?,?,ARRAY[?]::text[])`, in.TenantID, tc.action, tc.resource, in.FileID, tc.fields).Error
			}))
		})
	}
	require.Error(t, workerScope(t, worker, in, func(tx *gorm.DB) error {
		data := fmt.Sprintf(`{"outlet_id":%q,"prescription_id":%q,"file_id":%q,"scan_attempt":1,"status":"scan_pending","patient_name":"TEST extra"}`, in.OutletID, in.PrescriptionID, in.FileID)
		return events.Enqueue(tx, pharmacycontract.SubjectPrescriptionImageChanged, events.Event{TenantID: in.TenantID, Type: pharmacycontract.SubjectPrescriptionImageChanged, Version: 1, Data: json.RawMessage(data)})
	}), "the worker cannot add fields to its event")
	require.Error(t, workerScope(t, worker, in, func(tx *gorm.DB) error {
		data := fmt.Sprintf(`{"outlet_id":%q,"prescription_id":%q,"file_id":%q,"scan_attempt":1,"status":"available"}`, in.OutletID, in.PrescriptionID, in.FileID)
		return events.Enqueue(tx, pharmacycontract.SubjectPrescriptionImageChanged, events.Event{TenantID: in.TenantID, Type: pharmacycontract.SubjectPrescriptionImageChanged, Version: 1, Data: json.RawMessage(data)})
	}), "the worker cannot announce a status the row does not hold")
	for _, column := range []string{"filename='TEST.png'", "sha256=repeat('b',64)", "source_generation=99", "uploader_subject='TEST'"} {
		require.Error(t, workerScope(t, worker, in, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE pharmacy_prescription_images SET `+column+` WHERE id=?`, in.FileID).Error
		}), "the worker cannot rewrite %s", column)
	}

	// A forged actor and session settings cannot turn the API identity into the worker.
	err = s.db.WithTenant(tenantdb.WithActor(t.Context(), "service:pharmacy-evidence-worker"), in.TenantID, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.pharmacy_subject','user-pharmacist',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_prescription',?,true),set_config('app.pharmacy_file',?,true)`, in.OutletID, pharmaOrganization, in.PrescriptionID, in.FileID).Error)
		return tx.Exec(`UPDATE pharmacy_prescription_images SET status='available',released_generation=1,engine_version='TEST',definition_version=1,definitions_updated_at=now(),scanned_at=now() WHERE id=?`, in.FileID).Error
	})
	require.Error(t, err, "spoofing cannot set a verdict")
	page := do(s.r, "GET", prescriptionPath("/"+s.rx+"/images"), "pharmacist", "")
	require.Contains(t, page.Body.String(), `"status":"scan_pending"`)
}

// Private storage alone is not enough: without the worker rolled out (#1406), a
// page could never be scanned, so it is refused rather than stranded.
func TestPrescriptionImagesWaitForTheWorkerRollout(t *testing.T) {
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	w := newPrescriptionWorldWith(t, files, false)
	rx := w.record(t)
	res := do(w.r, "PUT", prescriptionPath("/"+rx+"/images/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", `{"filename":"TEST.png","content_type":"image/png","size":10,"sha256":"`+strings.Repeat("a", 64)+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code, res.Body.String())
}
