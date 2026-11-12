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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"                           //nolint:depguard // external test importing its own owner
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own published contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type licenceEvidenceScenario struct {
	r                   *gin.Engine
	db                  *tenantdb.DB
	files               *attachments.GCS
	cloud               *testinfra.GCSProtocol
	licence, path, body string
	data                []byte
}

// Existing sale/licence fixtures use the same owner upload and restricted scan
// path as new declarations; no available verdicts are inserted into tables.
func releaseLicenceEvidenceForTest(t *testing.T, r *gin.Engine, db *tenantdb.DB, files *attachments.GCS, licence, token string) {
	t.Helper()
	var page bytes.Buffer
	require.NoError(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := page.Bytes()
	digest := sha256.Sum256(data)
	s := licenceEvidenceScenario{r: r, db: db, files: files, licence: licence, path: licencePath("/licences/" + licence + "/evidence/" + uuid.Must(uuid.NewV7()).String()), data: data}
	body := fmt.Sprintf(`{"filename":"TEST-licence.png","content_type":"image/png","size":%d,"sha256":"%s"}`, len(data), hex.EncodeToString(digest[:]))
	w := do(r, "PUT", s.path, token, body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	w = s.upload(token, data)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	status, err := s.processor(t, "stream: OK\x00").Process(t.Context(), s.input())
	require.NoError(t, err)
	require.Equal(t, "available", status)
}

func newLicenceEvidenceScenario(t *testing.T) licenceEvidenceScenario {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	permissions := append(append([]authz.Permission{}, licencePerms...), pharmacy.PermLicenceEvidenceRead)
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"editor": testutil.TenantA, "owner": testutil.TenantA, "reader": testutil.TenantA, "clerk": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"editor": permissions, "owner": permissions, "reader": permissions, "foreign": permissions, "clerk": {pharmacy.PermCatalogueRead}},
		Modules: []platform.Module{pharmacy.New()}, Documents: files, PharmacyEvidenceEnabled: true,
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-owner", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	licence := uuid.Must(uuid.NewV7()).String()
	created := do(r, "PUT", licencePath("/licences/"+licence), "editor", licenceBody("2026-01-01", "2030-12-31"))
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var page bytes.Buffer
	require.NoError(t, png.Encode(&page, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := page.Bytes()
	digest := sha256.Sum256(data)
	body := fmt.Sprintf(`{"filename":"TEST-licence.png","content_type":"image/png","size":%d,"sha256":"%s"}`, len(data), hex.EncodeToString(digest[:]))
	return licenceEvidenceScenario{r: r, db: db, files: files, cloud: cloud, licence: licence, path: licencePath("/licences/" + licence + "/evidence/" + uuid.Must(uuid.NewV7()).String()), body: body, data: data}
}

func (s licenceEvidenceScenario) upload(token string, data []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, s.path+"/content", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, r)
	return w
}

func (s licenceEvidenceScenario) input() pharmacycontract.LicenceEvidenceScanInput {
	return pharmacycontract.LicenceEvidenceScanInput{TenantID: testutil.TenantA, OutletID: pharmaOutlet, LicenceID: s.licence, FileID: s.path[strings.LastIndex(s.path, "/")+1:], ScanAttempt: 1}
}

func (s licenceEvidenceScenario) worker(t *testing.T) *tenantdb.DB {
	t.Helper()
	var database string
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PharmacyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	return worker
}

func (s licenceEvidenceScenario) processor(t *testing.T, reply string) *pharmacy.LicenceEvidenceProcessor {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, reply), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := pharmacy.NewLicenceEvidenceProcessor(t.Context(), s.worker(t), s.files, scanner)
	require.NoError(t, err)
	return processor
}

func (s licenceEvidenceScenario) outbox(t *testing.T, subject string) []json.RawMessage {
	t.Helper()
	var payloads []json.RawMessage
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", subject).Order("created_at").Pluck("payload", &payloads).Error
	}))
	return payloads
}

func TestLicenceEvidenceHasOneImmutableScopedSlot(t *testing.T) {
	s := newLicenceEvidenceScenario(t)
	created := do(s.r, "PUT", s.path, "editor", s.body)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.NotContains(t, created.Body.String(), "sha256")
	require.Equal(t, http.StatusOK, do(s.r, "PUT", s.path, "editor", s.body).Code)
	require.Equal(t, http.StatusConflict, do(s.r, "PUT", s.path, "editor", strings.Replace(s.body, "TEST-licence.png", "TEST-changed.png", 1)).Code)
	second := s.path[:strings.LastIndex(s.path, "/")+1] + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusConflict, do(s.r, "PUT", second, "editor", s.body).Code)
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "editor", "").Code)
	require.Equal(t, http.StatusNotFound, do(s.r, "PUT", s.path, "reader", s.body).Code)
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "foreign", "").Code)
	require.Equal(t, http.StatusForbidden, do(s.r, "GET", s.path+"/download", "clerk", "").Code)
	list := do(s.r, "GET", licencePath("/licences/"+s.licence+"/evidence"), "reader", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), "TEST-licence.png")
}

func TestLicenceEvidenceScansThroughTheOwnWorkerAndAuditsDownload(t *testing.T) {
	s := newLicenceEvidenceScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "editor", s.body).Code)
	require.Equal(t, http.StatusBadRequest, s.upload("editor", []byte("different bytes")).Code)
	uploaded := s.upload("editor", s.data)
	require.Equal(t, http.StatusAccepted, uploaded.Code, uploaded.Body.String())
	status, err := s.processor(t, "stream: OK\x00").Process(t.Context(), s.input())
	require.NoError(t, err)
	require.Equal(t, "available", status)
	download := do(s.r, "GET", s.path+"/download", "reader", "")
	require.Equal(t, http.StatusOK, download.Code, download.Body.String())
	require.Equal(t, s.data, download.Body.Bytes())
	require.Equal(t, "private, no-store", download.Header().Get("Cache-Control"))
	var reads int64
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("audit_events").Where("resource_type='pharmacy_licence_evidence' AND resource_id=? AND action='read' AND actor_subject='user-reader'", s.input().FileID).Count(&reads).Error
	}))
	require.EqualValues(t, 1, reads)
	var payloads []json.RawMessage
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", pharmacycontract.SubjectLicenceEvidenceChanged).Pluck("payload", &payloads).Error
	}))
	require.Len(t, payloads, 1)
	require.NotContains(t, string(payloads[0]), "TEST-licence.png")
	require.NotContains(t, string(payloads[0]), "TEST-DL")
}

func TestLicenceEvidenceRejectsMalwareAndRetainsTheOriginal(t *testing.T) {
	s := newLicenceEvidenceScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "editor", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("editor", s.data).Code)
	status, err := s.processor(t, "stream: Eicar-Test-Signature FOUND\x00").Process(t.Context(), s.input())
	require.NoError(t, err)
	require.Equal(t, "rejected", status)
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "reader", "").Code)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", s.path+"/retry/2", "editor", "").Code)
	_, err = pharmacy.NewLicenceEvidenceProcessor(t.Context(), s.db, s.files, mustLicenceScanner(t))
	require.Error(t, err, "the API credential must never act as the scanner")
}

func TestLicenceEvidenceRetryPinsTheAttemptAndRefusesForgedRelease(t *testing.T) {
	s := newLicenceEvidenceScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "editor", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("editor", s.data).Code)
	status, err := s.processor(t, "stream: scanner offline ERROR\x00").Process(t.Context(), s.input())
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	require.Equal(t, http.StatusNotFound, do(s.r, "GET", s.path+"/download", "reader", "").Code)

	err = s.db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE pharmacy_licence_evidence SET status='available',released_generation=1,engine_version='TEST',definition_version=1,definitions_updated_at=clock_timestamp(),scanned_at=clock_timestamp(),failure_code=NULL WHERE id=?`, s.input().FileID).Error
	})
	require.Error(t, err, "the application credential cannot manufacture a clean verdict")
	retry := do(s.r, "POST", s.path+"/retry/2", "editor", "")
	require.Equal(t, http.StatusAccepted, retry.Code, retry.Body.String())
	require.Equal(t, http.StatusOK, do(s.r, "POST", s.path+"/retry/2", "editor", "").Code)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", s.path+"/retry/4", "editor", "").Code)
	processor := s.processor(t, "stream: OK\x00")
	status, err = processor.Process(t.Context(), s.input())
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	input := s.input()
	input.ScanAttempt = 2
	status, err = processor.Process(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	wrong := input
	wrong.LicenceID = uuid.Must(uuid.NewV7()).String()
	_, err = processor.Process(t.Context(), wrong)
	require.Error(t, err, "a worker input cannot release evidence for another declaration")
	wrong = input
	wrong.TenantID = testutil.TenantB
	_, err = processor.Process(t.Context(), wrong)
	require.Error(t, err, "an outlet ID is not authority in another tenant")
}

func TestLicenceReviewWaitsForReleasedEvidenceAndPinsIt(t *testing.T) {
	s := newLicenceEvidenceScenario(t)
	reviewPath := licencePath("/licences/" + s.licence + "/review")
	body := `{"decision":"approved","reason":"TEST checked the original and retained image"}`
	absent := do(s.r, "POST", reviewPath, "owner", body)
	require.Equal(t, http.StatusConflict, absent.Code, absent.Body.String())
	require.Contains(t, absent.Body.String(), "licence_evidence_required")
	err := s.db.WithTenant(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-owner',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_outlet_licence_reviews(tenant_id,outlet_id,licence_id,decision,reason,reviewed_by) VALUES(?,?,?,'approved','TEST direct SQL bypass','user-owner')`, testutil.TenantA, pharmaOutlet, s.licence).Error
	})
	require.ErrorContains(t, err, "clean licence evidence required")
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "editor", s.body).Code)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", reviewPath, "owner", body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("editor", s.data).Code)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", reviewPath, "owner", body).Code)
	_, err = s.processor(t, "stream: OK\x00").Process(t.Context(), s.input())
	require.NoError(t, err)
	approved := do(s.r, "POST", reviewPath, "owner", body)
	require.Equal(t, http.StatusOK, approved.Code, approved.Body.String())
	require.Contains(t, approved.Body.String(), `"reviewed_evidence_file_id":"`+s.input().FileID+`"`)
	require.Equal(t, http.StatusConflict, do(s.r, "POST", reviewPath, "owner", body).Code, "a completed review remains immutable")
}

func TestConcurrentLicenceReviewsRetainOneDecisionAndExactEvidence(t *testing.T) {
	s := newLicenceEvidenceScenario(t)
	require.Equal(t, http.StatusCreated, do(s.r, "PUT", s.path, "editor", s.body).Code)
	require.Equal(t, http.StatusAccepted, s.upload("editor", s.data).Code)
	_, err := s.processor(t, "stream: OK\x00").Process(t.Context(), s.input())
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for _, decision := range []string{"approved", "rejected"} {
		group.Go(func() {
			<-start
			results <- do(s.r, "POST", licencePath("/licences/"+s.licence+"/review"), "owner", fmt.Sprintf(`{"decision":%q,"reason":"TEST independent concurrent review"}`, decision))
		})
	}
	close(start)
	group.Wait()
	close(results)
	statuses := []int{}
	for result := range results {
		statuses = append(statuses, result.Code)
		if result.Code == http.StatusOK {
			require.Contains(t, result.Body.String(), `"reviewed_evidence_file_id":"`+s.input().FileID+`"`)
		}
	}
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, statuses)
}

type licenceModuleBeforeEvidenceReview struct{ *pharmacy.Module }

func (m licenceModuleBeforeEvidenceReview) Migrations() []tenantdb.Migration {
	var result []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID != "0046_pharmacy_licence_evidence_review" {
			result = append(result, migration)
		}
	}
	return result
}

func TestLicenceReviewMigrationPreservesHistoricalAuthorityAndDecision(t *testing.T) {
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"editor": testutil.TenantA, "owner": testutil.TenantA, "reader": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"editor": licencePerms, "owner": licencePerms, "reader": licencePerms},
		Modules: []platform.Module{licenceModuleBeforeEvidenceReview{pharmacy.New()}},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-owner", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	licence := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_outlet_licences(id,tenant_id,outlet_id,licence_kind,form_label,licence_number,issuing_authority,valid_from,valid_until,pharmacist_name,pharmacist_registration,intent_sha256,entered_by) VALUES(?,?,?,'retail','TEST Form','TEST historic licence','TEST Controller','2026-01-01','2030-12-31','TEST Pharmacist','TEST Registration',?,'user-editor')`, licence, testutil.TenantA, pharmaOutlet, strings.Repeat("a", 64)).Error
	}))
	// Execute the old owner's write before the new migration is applied. This is
	// a retained historical decision, not a fabricated clean evidence verdict.
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "user-owner"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-owner',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_outlet_licence_reviews(tenant_id,outlet_id,licence_id,decision,reason,reviewed_by) VALUES(?,?,?,'approved','TEST historical independent decision','user-owner')`, testutil.TenantA, pharmaOutlet, licence).Error
	}))
	var gate []tenantdb.Migration
	for _, migration := range pharmacy.New().Migrations() {
		if migration.ID == "0046_pharmacy_licence_evidence_review" {
			gate = append(gate, migration)
		}
	}
	require.Len(t, gate, 1)
	require.NoError(t, db.Migrate(t.Context(), gate))
	require.True(t, licenceStatus(t, r).InForce)
	list := do(r, "GET", licencePath("/licences"), "reader", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"reviewed_evidence_file_id":null`)
	require.Contains(t, list.Body.String(), "TEST historical independent decision")
	require.Equal(t, http.StatusConflict, do(r, "POST", licencePath("/licences/"+licence+"/review"), "owner", `{"decision":"rejected","reason":"TEST cannot rewrite history"}`).Code)
}

func mustLicenceScanner(t *testing.T) *attachments.ClamAV {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	return scanner
}
