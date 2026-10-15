package workforce_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type documentScenario struct {
	router     *gin.Engine
	db, worker *tenantdb.DB
	bus        *events.Bus
	cloud      *testinfra.GCSProtocol
	files      *attachments.GCS
	input      workforcecontract.DocumentScanInput
	path       string
	content    []byte
}

func newDocumentScenario(t *testing.T) documentScenario {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"manager": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"manager": {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermDocumentManage}, "foreign": {workforce.PermDocumentManage}},
		Modules: []platform.Module{workforce.New()}, Documents: files,
	})
	hospital, staff, file := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := "/v1/workforce/hospitals/" + hospital
	require.Equal(t, 200, testutil.Do(r, "PUT", base, "manager", `{"name":"Synthetic Document Hospital"}`).Code)
	require.Equal(t, 200, testutil.Do(r, "PUT", base+"/access/user-manager", "manager", `{"level":"manager"}`).Code)
	staffPath := base + "/staff/" + staff
	require.Equal(t, 200, testutil.Do(r, "PUT", staffPath, "manager", `{"employee_code":"DOCUMENT-001","display_name":"Synthetic Document Staff","job_category":"nurse","employment_status":"active"}`).Code)
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := imageBytes.Bytes()
	digest := sha256.Sum256(data)
	path := staffPath + "/documents/" + file
	intent := `{"record_kind":"enrollment","record_id":"` + staff + `","category":"employment_evidence","filename":"synthetic-evidence.png","content_type":"image/png","size":` + strconv.Itoa(len(data)) + `,"sha256":"` + hex.EncodeToString(digest[:]) + `"}`
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "manager", intent).Code)
	request := httptest.NewRequest(http.MethodPut, path+"/content", bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer manager")
	request.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, 202, response.Code, response.Body.String())
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	return documentScenario{router: r, db: db, worker: worker, bus: bus, cloud: cloud, files: files, path: path, content: data, input: workforcecontract.DocumentScanInput{TenantID: testutil.TenantA, HospitalID: hospital, StaffID: staff, FileID: file, ScanAttempt: 1}}
}

func TestDocumentProcessorReleasesOnlyScopedVerifiedBytesOnce(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	_, err = workforce.NewDocumentProcessor(t.Context(), s.db, s.files, scanner)
	require.Error(t, err, "request-role credentials cannot construct a processor")
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	foreign := s.input
	foreign.TenantID = testutil.TenantB
	_, err = processor.Process(t.Context(), foreign)
	require.Error(t, err)
	require.Zero(t, s.cloud.CopyCount())
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	for range 3 {
		status, err = processor.Process(t.Context(), s.input)
		require.NoError(t, err)
		require.Equal(t, "available", status)
	}
	require.Equal(t, 1, s.cloud.CopyCount())
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"available"`)
	require.NotContains(t, listed.Body.String(), "released_generation")
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 1, count)
		var actor string
		require.NoError(t, tx.Table("audit_events").Select("actor_subject").Where("resource_type='workforce_documents' AND resource_id=? AND 'released_generation'=ANY(changed_fields)", s.input.FileID).Scan(&actor).Error)
		require.Equal(t, "service:workforce-document-worker", actor)
		return nil
	}))
}

func TestDocumentProcessorPersistsScannerErrorAndRecoversWithoutDuplicateEvents(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	for range 2 {
		status, err := processor.Process(t.Context(), s.input)
		require.Error(t, err)
		require.Equal(t, "scan_error", status)
		require.Zero(t, s.cloud.CopyCount())
	}
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"scan_error"`)
	require.Contains(t, listed.Body.String(), `"failure_code":"scanner_unavailable"`)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
	scanner, err = attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err = workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
	listed = testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"available"`)
	require.NotContains(t, listed.Body.String(), `"failure_code":"scanner_unavailable"`)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestDocumentProcessorRejectsMalwareWithoutReleasingBytes(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: Eicar-Test-Signature FOUND\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "rejected", status)
	require.Zero(t, s.cloud.CopyCount())
	status, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "rejected", status)
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"rejected"`)
	require.Contains(t, listed.Body.String(), `"failure_code":"malware_detected"`)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestDocumentProcessorRecoversCopyCommittedBeforeDatabaseReceipt(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	closed := make(chan error, 1)
	s.cloud.AfterCopy(func() { closed <- s.worker.Close() })
	_, err = processor.Process(t.Context(), s.input)
	require.Error(t, err, "no availability receipt can commit through a closed real pool")
	require.NoError(t, <-closed)
	require.Equal(t, 1, s.cloud.CopyCount())
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"status":"scan_pending"`)
	var database string
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.DocumentWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	processor, err = workforce.NewDocumentProcessor(t.Context(), worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount(), "the retry reconciles the original copy without overwriting")
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestDocumentProcessorRejectsChangedIntentBytesBeforePromotion(t *testing.T) {
	s := newDocumentScenario(t)
	changed := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	changed.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var bytes bytes.Buffer
	require.NoError(t, png.Encode(&bytes, changed))
	s.cloud.CorruptQuarantine(bytes.Bytes())
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "rejected", status)
	require.Zero(t, s.cloud.CopyCount())
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"failure_code":"integrity_mismatch"`)
}

func TestDocumentProcessorPersistsStorageFailureAndRecoversSameAttempt(t *testing.T) {
	s := newDocumentScenario(t)
	s.cloud.DenyReads(true)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	require.Zero(t, s.cloud.CopyCount())
	listed := testutil.Do(s.router, "GET", s.path[:len(s.path)-36-1], "manager", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"failure_code":"storage_unavailable"`)
	s.cloud.DenyReads(false)
	status, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount())
}
