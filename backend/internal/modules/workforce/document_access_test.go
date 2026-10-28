package workforce_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // external test imports its own module
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestDocumentDownloadRequiresVerifiedReleaseAndCurrentOwnerAuthorization(t *testing.T) {
	s := newDocumentScenario(t)
	response := testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 409, response.Code, response.Body.String())
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	response = testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, s.content, response.Body.Bytes())
	require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Contains(t, response.Header().Get("Content-Disposition"), "attachment;")
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	response = testutil.Do(s.router, "GET", s.path+"/content", "foreign", "")
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "evidence.png")
	base := "/v1/workforce/hospitals/" + s.input.HospitalID
	require.Equal(t, 204, testutil.Do(s.router, "DELETE", base+"/access/user-manager", "manager", "").Code)
	response = testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 404, response.Code, response.Body.String())
}

func TestDocumentRevocationIsIdempotentAndStopsDownloads(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	_, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	for range 2 {
		response := testutil.Do(s.router, "PUT", s.path+"/revocation", "manager", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"status":"revoked"`)
	}
	response := testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 409, response.Code, response.Body.String())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentChanged).Count(&count).Error)
		require.EqualValues(t, 2, count, "availability and exactly one revocation event")
		return nil
	}))
	response = testutil.Do(s.router, "PUT", s.path+"/revocation", "foreign", "")
	require.Equal(t, 404, response.Code, response.Body.String())
}

func TestDocumentManualRetryUsesNewAttemptAndReturnsSameReceipt(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	for i, expected := range []int{202, 200} {
		response := testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
		require.Equal(t, expected, response.Code, response.Body.String())
		var receipt map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
		require.Equal(t, float64(2), receipt["scan_attempt"])
		require.Equal(t, "scan_pending", receipt["status"])
		require.Nil(t, receipt["failure_code"], "retry clears the old verdict, delivery %d", i)
	}
	status, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	response := testutil.Do(s.router, "PUT", s.path+"/retries/3", "manager", "")
	require.Equal(t, 409, response.Code, response.Body.String())
	response = testutil.Do(s.router, "PUT", s.path+"/retries/2", "foreign", "")
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentScanRequested).Count(&count).Error)
		require.EqualValues(t, 2, count, "initial request and one manual retry")
		return nil
	}))
}

func TestDocumentDownloadReauthorizesAfterStorageRead(t *testing.T) {
	s := newDocumentScenario(t)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	_, err = processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	s.cloud.AfterReleasedRead(func() {
		base := "/v1/workforce/hospitals/" + s.input.HospitalID
		require.Equal(t, 204, testutil.Do(s.router, "DELETE", base+"/access/user-manager", "manager", "").Code)
	})
	response := testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NotEqual(t, s.content, response.Body.Bytes())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("workforce_audit").Where("action='workforce.document.downloaded'").Count(&count).Error)
		require.Zero(t, count, "no successful download audit when the final authorization denies")
		return nil
	}))
}

func TestDocumentPendingRetryRecoversExpiredWorkflowWithoutReleasingOldAttempt(t *testing.T) {
	s := newDocumentScenario(t)
	expireDocumentQueue(t, s)
	response := testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"scan_attempt":2`)
	require.Contains(t, response.Body.String(), `"status":"scan_pending"`)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "").Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/retries/3", "manager", "").Code)
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "manager", "").Code)
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	next := s.input
	next.ScanAttempt++
	status, err = processor.Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	response = testutil.Do(s.router, "GET", s.path+"/content", "manager", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, s.content, response.Body.Bytes())
}

func expireDocumentQueue(t *testing.T, s documentScenario) {
	t.Helper()
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_workforce_document_expired_queue", SQL: fmt.Sprintf(`
 SELECT set_config('app.tenant_id','%s',true),set_config('app.actor_subject','user-manager',true),set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital','%s',true),set_config('app.workforce_enrollment','authorized',true);
 ALTER TABLE workforce_documents DISABLE TRIGGER workforce_document_identity;
 DO $$ DECLARE changed bigint; BEGIN
 UPDATE workforce_documents SET updated_at=clock_timestamp()-interval '46 minutes' WHERE tenant_id='%s' AND hospital_id='%s' AND id='%s';
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'fixture must age exactly one file'; END IF;
 END $$;
 ALTER TABLE workforce_documents ENABLE TRIGGER workforce_document_identity;`, s.input.TenantID, s.input.HospitalID, s.input.TenantID, s.input.HospitalID, s.input.FileID)}}))
}

func TestDocumentPendingRetryCannotRewriteWaitingClockOrRestartEarly(t *testing.T) {
	s := newDocumentScenario(t)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "").Code)
	for _, change := range []string{
		"updated_at=clock_timestamp()-interval '46 minutes'",
		"scan_attempt=2,updated_at=clock_timestamp()-interval '46 minutes'",
	} {
		err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.actor_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, s.input.HospitalID).Error; err != nil {
				return err
			}
			return tx.Exec("UPDATE workforce_documents SET "+change+" WHERE tenant_id=? AND hospital_id=? AND id=?", s.input.TenantID, s.input.HospitalID, s.input.FileID).Error
		})
		require.Error(t, err, "an application connection cannot forge or skip the processing clock")
	}
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "").Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", s.path+"/retries/2", "foreign", "").Code)
}

func TestDocumentExpiredPendingConcurrentRetryPublishesOnce(t *testing.T) {
	s := newDocumentScenario(t)
	expireDocumentQueue(t, s)
	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "").Code }()
	}
	require.ElementsMatch(t, []int{202, 200}, []int{<-codes, <-codes})
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("outbox_events").Where("subject=?", workforce.DocumentScanRequested).Count(&count).Error)
		require.EqualValues(t, 2, count, "initial request and one recovery command")
		return nil
	}))
}

func TestDocumentExpiredPendingRetryRollsBackWhenOutboxFails(t *testing.T) {
	s := newDocumentScenario(t)
	expireDocumentQueue(t, s)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_workforce_document_recovery_outbox_failure", SQL: `
 CREATE FUNCTION document_recovery_fail_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.workforce.document_scan_requested.v1' THEN RAISE EXCEPTION 'synthetic publication failure' USING ERRCODE='57014'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER document_recovery_fail_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION document_recovery_fail_outbox();`}}))
	response := testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
	require.Equal(t, 500, response.Code, response.Body.String())
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		require.NoError(t, tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.actor_subject','user-manager',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_enrollment','authorized',true)`, s.input.HospitalID).Error)
		var unchanged bool
		require.NoError(t, tx.Raw(`SELECT scan_attempt=1 AND status='scan_pending' AND updated_at<=clock_timestamp()-interval '45 minutes' FROM workforce_documents WHERE tenant_id=? AND hospital_id=? AND id=?`, s.input.TenantID, s.input.HospitalID, s.input.FileID).Scan(&unchanged).Error)
		require.True(t, unchanged, "attempt and recovery deadline roll back together")
		return nil
	}))
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_workforce_document_recovery_outbox_restored", SQL: `DROP TRIGGER document_recovery_fail_outbox ON outbox_events; DROP FUNCTION document_recovery_fail_outbox();`}}))
	response = testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
	require.Equal(t, 202, response.Code, response.Body.String())
}

func TestDocumentExpiredPendingRetrySupersedesInFlightRelease(t *testing.T) {
	s := newDocumentScenario(t)
	expireDocumentQueue(t, s)
	codes := make(chan int, 1)
	s.cloud.AfterCopy(func() {
		s.cloud.AfterCopy(nil)
		codes <- testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "").Code
	})
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	processor, err := workforce.NewDocumentProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := processor.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, 202, <-codes)
	require.Equal(t, "superseded", status, "a completed old copy cannot commit a release verdict")
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "manager", "").Code)
	next := s.input
	next.ScanAttempt++
	status, err = processor.Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status, "the new attempt independently scans before reusing matching stored bytes")
}

func TestDocumentRecoveryMigrationCanRestorePreviousGuardWithoutErasingFile(t *testing.T) {
	s := newDocumentScenario(t)
	expireDocumentQueue(t, s)
	rollback := ""
	for _, definition := range []struct{ file, name string }{
		{"tax_declaration_evidence.sql", "workforce_document_identity"},
		{"tax_declaration_evidence.sql", "workforce_tax_evidence_frozen"},
		{"benefit_evidence.sql", "workforce_benefit_evidence_frozen"},
	} {
		source, err := os.ReadFile(definition.file)
		require.NoError(t, err)
		text := string(source)
		start := strings.Index(text, "CREATE OR REPLACE FUNCTION "+definition.name+"()")
		if start < 0 {
			start = strings.Index(text, "CREATE FUNCTION "+definition.name+"()")
		}
		require.GreaterOrEqual(t, start, 0)
		length := strings.Index(text[start:], "\nEND $$;")
		require.Greater(t, length, 0)
		rollback += strings.Replace(text[start:start+length+len("\nEND $$;")], "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1) + "\n"
	}
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_document_recovery_guard_rollback", SQL: rollback}}))
	response := testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
	require.GreaterOrEqual(t, response.Code, 400, "the earlier schema fails closed for stalled recovery")
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "manager", "").Code)
	recovery, err := os.ReadFile("document_recovery.sql")
	require.NoError(t, err)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_document_recovery_guard_restored", SQL: string(recovery)}}))
	response = testutil.Do(s.router, "PUT", s.path+"/retries/2", "manager", "")
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"scan_attempt":2`, "the file and its original attempt survive guard rollback")
}
