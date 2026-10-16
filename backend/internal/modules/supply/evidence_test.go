package supply_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own module and data-only contract
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"image"
	"image/png"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module and data-only contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type evidenceScenario struct {
	r                         *gin.Engine
	db                        *tenantdb.DB
	bus                       *events.Bus
	cloud                     *testinfra.GCSProtocol
	files                     *attachments.GCS
	root, licence, path, body string
	data                      []byte
}

func newEvidenceScenario(t *testing.T) evidenceScenario {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	permissions := []authz.Permission{supply.PermPartyRead, supply.PermPartyWrite, supply.PermAccessManage, supply.PermPremiseRead, supply.PermPremiseWrite, supply.PermLicenseRead, supply.PermLicenseWrite, authz.Permission("supply.evidence.read"), authz.Permission("supply.evidence.write")}
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"owner": testutil.TenantA, "auditor": testutil.TenantA, "foreign": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"owner": permissions, "auditor": permissions, "foreign": permissions},
		Modules: []platform.Module{supply.New()}, Documents: files,
	})
	root := "/v1/supply/parties/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root, "owner", partyBody(0, "TEST Evidence Supplier")).Code)
	premise := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", root+"/premises/"+premise, "owner", premiseBody(0, "IN")).Code)
	licence := root + "/licenses/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, testutil.Do(r, "PUT", licence, "owner", licenseBody(0, premise)).Code)
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	data := imageBytes.Bytes()
	digest := sha256.Sum256(data)
	body := fmt.Sprintf(`{"expected_license_version":1,"filename":"TEST-licence.png","content_type":"image/png","size":%d,"sha256":"%s"}`, len(data), hex.EncodeToString(digest[:]))
	return evidenceScenario{r: r, db: db, bus: bus, cloud: cloud, files: files, root: root, licence: licence, path: licence + "/evidence/" + uuid.Must(uuid.NewV7()).String(), body: body, data: data}
}

func TestLicenceEvidenceIntentsArePrivatePinnedAndExactlyReplayable(t *testing.T) {
	s := newEvidenceScenario(t)
	created := testutil.Do(s.r, "PUT", s.path, "owner", s.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	var saved map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &saved))
	require.Equal(t, "upload_pending", saved["status"])
	require.EqualValues(t, 1, saved["license_version"])
	require.NotContains(t, saved, "source_generation")
	require.NotContains(t, saved, "object_key")
	for range 3 {
		replay := testutil.Do(s.r, "PUT", s.path, "owner", s.body)
		require.Equal(t, 200, replay.Code, replay.Body.String())
		require.JSONEq(t, created.Body.String(), replay.Body.String())
	}
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.licence+"/evidence", "foreign", "").Code)
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.licence+"/evidence", "auditor", "").Code)
	require.Equal(t, 200, testutil.Do(s.r, "PUT", s.root+"/access/user-auditor/auditor", "owner", "").Code)
	read := testutil.Do(s.r, "GET", s.licence+"/evidence", "auditor", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), "TEST-licence.png")
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		err := tx.Table("audit_events").Where("resource_type='supply_licenses' AND action='evidence_list' AND actor_subject='user-auditor'").Count(&count).Error
		require.EqualValues(t, 1, count, "reading protected evidence metadata must be audited without filenames")
		return err
	}))
	require.Equal(t, 404, testutil.Do(s.r, "PUT", s.licence+"/evidence/"+uuid.Must(uuid.NewV7()).String(), "auditor", s.body).Code)
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.path+"/download", "owner", "").Code, "unscanned bytes must never be downloadable")
}

func uploadEvidence(s evidenceScenario, path, token, contentType string, data []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", path, bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, r)
	return w
}

func TestLicenceEvidenceUploadsValidateBytesBeforeDurableQuarantine(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
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
			w := uploadEvidence(s, s.path+"/content", "owner", tc.contentType, tc.data)
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	require.Equal(t, 404, uploadEvidence(s, s.path+"/content", "foreign", "image/png", s.data).Code)
	w := uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"scan_pending"`)
	require.NotContains(t, w.Body.String(), "generation")
	again := uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data)
	require.Equal(t, 200, again.Code, again.Body.String())
	require.JSONEq(t, w.Body.String(), again.Body.String())
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.path+"/download", "owner", "").Code)
	require.NoError(t, s.db.WithTenant(tenantdb.WithActor(t.Context(), "test"), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.supply.evidence_scan_requested.v1").Count(&n).Error
		require.EqualValues(t, 1, n, "exact replay must not enqueue another scan")
		return err
	}))
}

func TestLicenceEvidenceUploadPreservesChronologyAcrossClockCorrections(t *testing.T) {
	s := newEvidenceScenario(t)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_supply_creation_clock", SQL: `ALTER TABLE supply_license_evidence ALTER COLUMN created_at SET DEFAULT clock_timestamp()+interval '2 minutes'`}}))
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	first := uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data)
	require.Equal(t, 202, first.Code, first.Body.String())
	var saved struct {
		CreatedAt  time.Time `json:"created_at"`
		UploadedAt time.Time `json:"uploaded_at"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &saved))
	require.False(t, saved.UploadedAt.Before(saved.CreatedAt))
	replay := uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
}

func evidenceWorker(t *testing.T, s evidenceScenario) *tenantdb.DB {
	t.Helper()
	var database string
	require.NoError(t, s.db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.SupplyEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	return worker
}
func evidenceScanInput(s evidenceScenario) supplycontract.EvidenceScanInput {
	return supplycontract.EvidenceScanInput{TenantID: testutil.TenantA, PartyID: s.root[len("/v1/supply/parties/"):], LicenseID: s.licence[len(s.root+"/licenses/"):], FileID: s.path[len(s.licence+"/evidence/"):], ScanAttempt: 1}
}
func TestLicenceEvidenceWorkerReleasesOnlyCleanPinnedBytesAndAllowsRevocableAuditedDownload(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	_, err = supply.NewEvidenceProcessor(t.Context(), s.db, s.files, scanner)
	require.Error(t, err, "API credentials must not process evidence")
	worker := evidenceWorker(t, s)
	processor, err := supply.NewEvidenceProcessor(t.Context(), worker, s.files, scanner)
	require.NoError(t, err)
	in := evidenceScanInput(s)
	foreign := in
	foreign.TenantID = testutil.TenantB
	_, err = processor.Process(t.Context(), foreign)
	require.Error(t, err)
	require.Zero(t, s.cloud.CopyCount())
	for range 3 {
		status, err := processor.Process(t.Context(), in)
		require.NoError(t, err)
		require.Equal(t, "available", status)
	}
	require.Equal(t, 1, s.cloud.CopyCount())
	download := testutil.Do(s.r, "GET", s.path+"/download", "owner", "")
	require.Equal(t, 200, download.Code, download.Body.String())
	require.Equal(t, s.data, download.Body.Bytes())
	require.Equal(t, "image/png", download.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", download.Header().Get("X-Content-Type-Options"))
	require.Contains(t, download.Header().Get("Content-Disposition"), "attachment")
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.path+"/download", "foreign", "").Code)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", supplycontract.SubjectEvidenceChanged).Count(&n).Error)
		require.EqualValues(t, 1, n)
		require.NoError(t, tx.Table("audit_events").Where("resource_type='supply_license_evidence' AND resource_id=? AND action='read'", in.FileID).Count(&n).Error)
		require.EqualValues(t, 1, n)
		return nil
	}))
	require.Equal(t, 200, testutil.Do(s.r, "POST", s.path+"/revoke", "owner", "").Code)
	require.Equal(t, 200, testutil.Do(s.r, "POST", s.path+"/revoke", "owner", "").Code)
	require.Equal(t, 404, testutil.Do(s.r, "GET", s.path+"/download", "owner", "").Code)
}

func evidenceWorkerScope(t *testing.T, worker *tenantdb.DB, in supplycontract.EvidenceScanInput, fn func(*gorm.DB) error) error {
	t.Helper()
	return worker.WithTenant(tenantdb.WithActor(t.Context(), "service:supply-evidence-worker"), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_license',?,true),set_config('app.supply_file',?,true)`, in.PartyID, in.LicenseID, in.FileID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func TestLicenceEvidenceWorkerCannotReadClinicalDataOrForgeEvents(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	worker := evidenceWorker(t, s)
	in := evidenceScanInput(s)
	for _, query := range []string{
		`SELECT filename FROM supply_license_evidence`,
		`SELECT * FROM patients`,
		`SELECT * FROM workforce_staff`,
		`SELECT * FROM supply_licenses`,
		`SELECT * FROM audit_events`,
		`SELECT payload FROM outbox_events`,
	} {
		t.Run(query, func(t *testing.T) {
			require.Error(t, evidenceWorkerScope(t, worker, in, func(tx *gorm.DB) error { var rows []map[string]any; return tx.Raw(query).Scan(&rows).Error }))
		})
	}
	for _, tc := range []struct{ name, action, resource, fields string }{
		{"clinical resource", "update", "patients", "status"},
		{"read event", "read", "supply_license_evidence", "status"},
		{"arbitrary field", "update", "supply_license_evidence", "filename"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, evidenceWorkerScope(t, worker, in, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields) VALUES(?,'service:supply-evidence-worker',?,?,?,ARRAY[?]::text[])`, in.TenantID, tc.action, tc.resource, in.FileID, tc.fields).Error
			}))
		})
	}
	require.Error(t, evidenceWorkerScope(t, worker, in, func(tx *gorm.DB) error {
		return events.Enqueue(tx, "helivanta.in.patient.registered.v1", events.Event{TenantID: in.TenantID, Type: "helivanta.in.patient.registered.v1", Version: 1, Data: json.RawMessage(`{"name":"TEST private payload"}`)})
	}))
	require.Error(t, evidenceWorkerScope(t, worker, in, func(tx *gorm.DB) error {
		data := fmt.Sprintf(`{"party_id":%q,"license_id":%q,"file_id":%q,"scan_attempt":1,"status":"scan_pending","filename":"TEST extra"}`, in.PartyID, in.LicenseID, in.FileID)
		return events.Enqueue(tx, supplycontract.SubjectEvidenceChanged, events.Event{TenantID: in.TenantID, Type: supplycontract.SubjectEvidenceChanged, Version: 1, Data: json.RawMessage(data)})
	}))
	require.Error(t, evidenceWorkerScope(t, worker, in, func(tx *gorm.DB) error {
		data := fmt.Sprintf(`{"party_id":%q,"license_id":%q,"file_id":%q,"scan_attempt":1,"status":"available"}`, in.PartyID, in.LicenseID, in.FileID)
		return events.Enqueue(tx, supplycontract.SubjectEvidenceChanged, events.Event{TenantID: in.TenantID, Type: supplycontract.SubjectEvidenceChanged, Version: 1, Data: json.RawMessage(data)})
	}))
	// Even a forged service actor/GUC cannot turn the API identity into the worker.
	require.NoError(t, s.db.WithTenant(tenantdb.WithActor(t.Context(), "service:supply-evidence-worker"), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_license',?,true),set_config('app.supply_party_creator','true',true),set_config('app.supply_access_admin','true',true)`, in.PartyID, in.LicenseID).Error; err != nil {
			return err
		}
		result := tx.Exec(`UPDATE supply_license_evidence SET status='available',released_generation=1,engine_version='TEST',definition_version=1,definitions_updated_at=now(),scanned_at=now() WHERE id=?`, in.FileID)
		require.True(t, result.Error != nil || result.RowsAffected == 0, "spoofing cannot change an evidence verdict")
		return nil
	}))
}

func TestLicenceEvidenceWorkerCannotSmuggleContentInEventEnvelope(t *testing.T) {
	s := newEvidenceScenario(t)
	require.Equal(t, 201, testutil.Do(s.r, "PUT", s.path, "owner", s.body).Code)
	require.Equal(t, 202, uploadEvidence(s, s.path+"/content", "owner", "image/png", s.data).Code)
	worker := evidenceWorker(t, s)
	in := evidenceScanInput(s)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := supply.NewEvidenceProcessor(t.Context(), worker, s.files, scanner)
	require.NoError(t, err)
	_, err = processor.Process(t.Context(), in)
	require.NoError(t, err)
	for _, tc := range []struct {
		name  string
		patch func(map[string]any)
	}{
		{"timestamp content", func(payload map[string]any) { payload["occurred_at"] = "TEST private document content" }},
		{"string version", func(payload map[string]any) { payload["version"] = "1" }},
		{"string attempt", func(payload map[string]any) { payload["data"].(map[string]any)["scan_attempt"] = "1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.Must(uuid.NewV7()).String()
			payload := map[string]any{"event_id": id, "type": supplycontract.SubjectEvidenceChanged, "version": 1, "tenant_id": in.TenantID, "occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "data": map[string]any{"party_id": in.PartyID, "license_id": in.LicenseID, "file_id": in.FileID, "scan_attempt": 1, "status": "available"}}
			tc.patch(payload)
			encoded, err := json.Marshal(payload)
			require.NoError(t, err)
			require.Error(t, evidenceWorkerScope(t, worker, in, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO outbox_events(id,tenant_id,subject,payload) VALUES(?,?,?,?::jsonb)`, id, in.TenantID, supplycontract.SubjectEvidenceChanged, string(encoded)).Error
			}))
		})
	}
}
