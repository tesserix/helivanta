package practitioner_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type professionalEvidenceScenario struct {
	router     *gin.Engine
	db, worker *tenantdb.DB
	cloud      *testinfra.GCSProtocol
	files      *attachments.GCS
	input      practitionercontract.EvidenceScanInput
	path       string
	content    []byte
}

func newProfessionalEvidenceScenario(t *testing.T) professionalEvidenceScenario {
	t.Helper()
	cloud := testinfra.NewGCSProtocol(t)
	files, err := attachments.NewGCS(cloud.Client, cloud.Quarantine, cloud.Released)
	require.NoError(t, err)
	r, db, collection, _ := evidenceFixtureWithFiles(t, files)
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	file := uuid.Must(uuid.NewV7()).String()
	path := collection + "/" + file
	require.Equal(t, 201, testutil.Do(r, "PUT", path, "steward", fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, data.Len(), sha256.Sum256(data.Bytes()))).Code)
	req := httptest.NewRequest("PUT", path+"/content", bytes.NewReader(data.Bytes()))
	req.Header.Set("Authorization", "Bearer steward")
	req.Header.Set("Content-Type", "image/png")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	require.Equal(t, 202, res.Code, res.Body.String())
	var database string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw("SELECT current_database()").Scan(&database).Error }))
	worker, err := tenantdb.OpenRuntime(t.Context(), testinfra.PractitionerEvidenceWorkerDSN(t, database))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, worker.Close()) })
	segments := strings.Split(path, "/")
	return professionalEvidenceScenario{r, db, worker, cloud, files, practitionercontract.EvidenceScanInput{TenantID: testutil.TenantA, PractitionerID: segments[3], RegistrationID: segments[5], RevisionID: segments[7], FileID: file}, path, data.Bytes()}
}
func TestProfessionalEvidenceProcessorCommitsOnlyGenerationBoundAuditedRelease(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	_, err = practitioner.NewEvidenceProcessor(t.Context(), s.db, s.files, scanner)
	require.Error(t, err, "request credentials cannot mint professional-evidence verdicts")
	processor, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = processor.Process(canceled, s.input)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, s.cloud.CopyCount())
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
	metadata := testutil.Do(s.router, "GET", s.path, "steward", "")
	require.Equal(t, 200, metadata.Code, metadata.Body.String())
	require.Contains(t, metadata.Body.String(), `"status":"available"`)
	require.NotContains(t, metadata.Body.String(), "released_generation")
	require.NotContains(t, metadata.Body.String(), "engine_version")
	require.NotContains(t, metadata.Body.String(), "verified")
	owner := testutil.Do(s.router, "GET", strings.Split(s.path, "/registrations/")[0], "steward", "")
	require.Equal(t, 200, owner.Code)
	require.Contains(t, owner.Body.String(), `"verification_state":"unverified"`)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var rows []struct{ Payload json.RawMessage }
		if err := tx.Table("outbox_events").Select("payload").Where("subject=?", practitionercontract.EvidenceChanged).Find(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 1)
		require.NotContains(t, string(rows[0].Payload), "sha256")
		require.NotContains(t, string(rows[0].Payload), "image/png")
		var actor string
		err := tx.Table("audit_events").Select("actor_subject").Where("resource_type='practitioner_registration_evidence' AND resource_id=? AND 'released_generation'=ANY(changed_fields)", s.input.FileID).Scan(&actor).Error
		require.Equal(t, "service:practitioner-evidence-worker", actor)
		return err
	}))
}

func workerEvidenceScope(t *testing.T, s professionalEvidenceScenario, fn func(*gorm.DB) error) error {
	t.Helper()
	return s.worker.WithTenant(tenantdb.WithActor(t.Context(), "service:practitioner-evidence-worker"), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_evidence_owner',?,true),set_config('app.practitioner_evidence_registration',?,true),set_config('app.practitioner_evidence_revision',?,true),set_config('app.practitioner_evidence_file',?,true)", s.input.PractitionerID, s.input.RegistrationID, s.input.RevisionID, s.input.FileID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func cleanProfessionalEvidenceProcessor(t *testing.T, s professionalEvidenceScenario) *practitioner.EvidenceProcessor {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	return p
}
func TestProfessionalEvidenceWorkerCannotEscapeFileScopeOrForgeOtherAuditAndOutbox(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	p := cleanProfessionalEvidenceProcessor(t, s)
	require.NoError(t, s.worker.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("practitioner_registration_evidence").Count(&n).Error
		require.Zero(t, n)
		return err
	}))
	require.NoError(t, workerEvidenceScope(t, s, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("practitioner_registration_evidence").Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
	for _, mutate := range []func(*practitionercontract.EvidenceScanInput){
		func(in *practitionercontract.EvidenceScanInput) { in.TenantID = testutil.TenantB },
		func(in *practitionercontract.EvidenceScanInput) { in.PractitionerID = uuid.Must(uuid.NewV7()).String() },
		func(in *practitionercontract.EvidenceScanInput) { in.RegistrationID = uuid.Must(uuid.NewV7()).String() },
		func(in *practitionercontract.EvidenceScanInput) { in.RevisionID = uuid.Must(uuid.NewV7()).String() },
		func(in *practitionercontract.EvidenceScanInput) { in.FileID = uuid.Must(uuid.NewV7()).String() },
	} {
		wrong := s
		mutate(&wrong.input)
		require.NoError(t, workerEvidenceScope(t, wrong, func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error)
			var n int64
			err := tx.Table("practitioner_registration_evidence").Count(&n).Error
			require.Zero(t, n)
			return err
		}))
	}
	for _, query := range []string{"SELECT given_name FROM practitioner_records", "SELECT registration_number FROM practitioner_registrations", "SELECT subject FROM practitioner_steward_appointments", "SELECT created_by FROM practitioner_registration_evidence", "SELECT payload FROM outbox_events"} {
		err := workerEvidenceScope(t, s, func(tx *gorm.DB) error { return tx.Exec(query).Error })
		require.Error(t, err)
		require.Contains(t, err.Error(), "permission denied")
	}
	forged := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE practitioner_registration_evidence SET status='rejected',failure_code='malware_detected' WHERE tenant_id=? AND id=?", s.input.TenantID, s.input.FileID).Error
	})
	require.Error(t, forged)
	require.Contains(t, forged.Error(), "scoped worker identity")
	status, err := p.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	require.NoError(t, workerEvidenceScope(t, s, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Select("id").Count(&n).Error
		require.EqualValues(t, 1, n, "worker sees only its file lifecycle IDs, not the scan-request event")
		return err
	}))
	for _, resource := range []string{"practitioner_records", "audit_events"} {
		err := workerEvidenceScope(t, s, func(tx *gorm.DB) error {
			return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,'service:practitioner-evidence-worker','read',?,?)", s.input.TenantID, resource, s.input.FileID).Error
		})
		require.Error(t, err)
	}
	payload := fmt.Sprintf(`{"event_id":"%s","type":"%s","version":1,"tenant_id":"%s","occurred_at":"2026-10-26T00:00:00Z","data":{"practitioner_id":"%s","registration_id":"%s","revision_id":"%s","file_id":"%s","status":"available","private_content":"blocked"}}`, uuid.NewString(), practitionercontract.EvidenceChanged, s.input.TenantID, s.input.PractitionerID, s.input.RegistrationID, s.input.RevisionID, s.input.FileID)
	err = workerEvidenceScope(t, s, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO outbox_events(id,tenant_id,subject,payload) VALUES(?,?,?,?::jsonb)", uuid.New(), s.input.TenantID, practitionercontract.EvidenceChanged, payload).Error
	})
	require.Error(t, err, "worker cannot add protected data to the lifecycle envelope")
}
func TestProfessionalEvidenceProcessingRejectsMalwareUnsupportedAndChangedBytes(t *testing.T) {
	for _, kind := range []string{"malware", "unsupported", "integrity"} {
		t.Run(kind, func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			reply := "stream: OK\x00"
			expected := "integrity_mismatch"
			switch kind {
			case "malware":
				reply = "stream: Eicar-Test-Signature FOUND\x00"
				expected = "malware_detected"
			case "unsupported":
				s.cloud.CorruptQuarantine([]byte("not a passive image"))
				expected = "unsupported_content"
			case "integrity":
				var modified bytes.Buffer
				img := image.NewRGBA(image.Rect(0, 0, 2, 2))
				img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
				require.NoError(t, png.Encode(&modified, img))
				s.cloud.CorruptQuarantine(modified.Bytes())
			}
			scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, reply), time.Second, attachments.MaxFileBytes)
			require.NoError(t, err)
			p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
			require.NoError(t, err)
			for range 2 {
				state, err := p.Process(t.Context(), s.input)
				require.NoError(t, err)
				require.Equal(t, "rejected", state)
			}
			require.Zero(t, s.cloud.CopyCount())
			metadata := testutil.Do(s.router, "GET", s.path, "steward", "")
			require.Equal(t, 200, metadata.Code)
			require.Contains(t, metadata.Body.String(), `"failure_code":"`+expected+`"`)
		})
	}
}
func TestProfessionalEvidenceTemporaryFailuresStayUnavailableAndRecoverSameGeneration(t *testing.T) {
	for _, dependency := range []string{"storage", "scanner"} {
		t.Run(dependency, func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			reply := "stream: ERROR\x00"
			if dependency == "storage" {
				reply = "stream: OK\x00"
				s.cloud.DenyReads(true)
			}
			scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, reply), time.Second, attachments.MaxFileBytes)
			require.NoError(t, err)
			p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
			require.NoError(t, err)
			status, err := p.Process(t.Context(), s.input)
			require.Error(t, err)
			require.Equal(t, "scan_error", status)
			require.Zero(t, s.cloud.CopyCount())
			metadata := testutil.Do(s.router, "GET", s.path, "steward", "")
			require.Equal(t, 200, metadata.Code)
			require.Contains(t, metadata.Body.String(), `"failure_code":"`+dependency+`_unavailable"`)
			s.cloud.DenyReads(false)
			p = cleanProfessionalEvidenceProcessor(t, s)
			status, err = p.Process(t.Context(), s.input)
			require.NoError(t, err)
			require.Equal(t, "available", status)
			require.Equal(t, 1, s.cloud.CopyCount())
		})
	}
}
func TestProfessionalEvidenceCopyReceiptReconcilesWhenAuditOrOutboxCommitFails(t *testing.T) {
	for _, dependency := range []string{"audit", "outbox"} {
		t.Run(dependency, func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			p := cleanProfessionalEvidenceProcessor(t, s)
			table, condition := "audit_events", "NEW.action='update' AND NEW.resource_type='practitioner_registration_evidence' AND 'released_generation'=ANY(NEW.changed_fields)"
			if dependency == "outbox" {
				table = "outbox_events"
				condition = "NEW.subject='" + practitionercontract.EvidenceChanged + "'"
			}
			s.cloud.AfterCopy(func() {
				require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_professional_copy_commit_failure", SQL: "CREATE FUNCTION reject_professional_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF " + condition + " THEN RAISE EXCEPTION 'synthetic commit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_professional_commit BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION reject_professional_commit();"}}))
			})
			_, err := p.Process(t.Context(), s.input)
			require.Error(t, err)
			require.Equal(t, 1, s.cloud.CopyCount())
			metadata := testutil.Do(s.router, "GET", s.path, "steward", "")
			require.Equal(t, 200, metadata.Code)
			require.Contains(t, metadata.Body.String(), `"status":"scan_pending"`)
			require.NotContains(t, metadata.Body.String(), "released_generation")
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_professional_copy_commit_recovery", SQL: "DROP TRIGGER reject_professional_commit ON " + table + ";"}}))
			s.cloud.AfterCopy(nil)
			state, err := p.Process(t.Context(), s.input)
			require.NoError(t, err)
			require.Equal(t, "available", state)
			require.Equal(t, 1, s.cloud.CopyCount(), "recovery verifies the retained destination instead of replacing it")
		})
	}
}

func TestProfessionalEvidenceConcurrentProcessingCreatesOneRetainedReleaseAndEvent(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	p := cleanProfessionalEvidenceProcessor(t, s)
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	statuses := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			status, err := p.Process(t.Context(), s.input)
			if err != nil {
				failures <- err
			}
			statuses <- status
		})
	}
	wg.Wait()
	close(failures)
	close(statuses)
	for err := range failures {
		require.NoError(t, err)
	}
	for status := range statuses {
		require.Equal(t, "available", status)
	}
	require.Equal(t, 1, s.cloud.CopyCount())
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=?", practitionercontract.EvidenceChanged).Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
	for _, mutate := range []func(*practitionercontract.EvidenceScanInput){func(in *practitionercontract.EvidenceScanInput) { in.TenantID = "" }, func(in *practitionercontract.EvidenceScanInput) { in.FileID = uuid.NewString() }, func(in *practitionercontract.EvidenceScanInput) { in.RevisionID = "../../file" }, func(in *practitionercontract.EvidenceScanInput) {
		in.PractitionerID = strings.ToUpper(in.PractitionerID)
	}} {
		bad := s.input
		mutate(&bad)
		_, err := p.Process(t.Context(), bad)
		require.ErrorIs(t, err, practitioner.ErrEvidenceNotReady)
	}
}
