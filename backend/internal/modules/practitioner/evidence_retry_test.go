package practitioner_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/practitioner" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func failProfessionalScan(t *testing.T, s professionalEvidenceScenario) {
	t.Helper()
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: ERROR\x00"), time.Second, attachments.MaxFileBytes)
	require.NoError(t, err)
	p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
	require.NoError(t, err)
	status, err := p.Process(t.Context(), s.input)
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
}

func TestProfessionalEvidenceRetryRetainsReceiptAndFencesOldProcessing(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	failProfessionalScan(t, s)
	path := s.path + "/scan-retries/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	result := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 201, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), `"scan_attempt":2`)
	require.Contains(t, result.Body.String(), `"status":"queued"`)
	replay := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, result.Body.String(), replay.Body.String())
	status, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	require.Zero(t, s.cloud.CopyCount())
	next := s.input
	next.ScanAttempt = 2
	status, err = cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	replay = testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, result.Body.String(), replay.Body.String())
	require.Equal(t, 1, s.cloud.CopyCount())
}

func TestProfessionalRetryRejectsWrongScopeIntentPermissionAndNonfailureStates(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	path := s.path + "/scan-retries/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", body).Code, "pending work cannot be retried")
	failProfessionalScan(t, s)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", path, "reader", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "foreign", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", strings.Replace(path, s.input.RevisionID, uuid.Must(uuid.NewV7()).String(), 1), "steward", body).Code)
	for _, bad := range []string{`null`, `[]`, `{}`, `{"expected_attempt":0}`, `{"expected_attempt":1.5}`, `{"expected_attempt":9007199254740991}`, strings.Replace(body, `"expected_attempt":1`, `"expected_attempt":0`, 1), strings.TrimSuffix(body, "}") + `,"tenant_id":"forged"}`, strings.Replace(body, `"review_reference":"`, `"review_reference":"invalid`, 1)} {
		require.Equal(t, 400, testutil.Do(s.router, "PUT", path, "steward", bad).Code, bad)
	}
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", strings.Replace(body, `"expected_attempt":1`, `"expected_attempt":2`, 1)).Code)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", strings.Replace(body, `"expected_attempt":1`, `"expected_attempt":2`, 1)).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))).Code)
	next := s.input
	next.ScanAttempt = 2
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/scan-retries/"+uuid.Must(uuid.NewV7()).String(), "steward", strings.Replace(body, `"expected_attempt":1`, `"expected_attempt":2`, 1)).Code)
}

func TestProfessionalRetryAfterCopyFencesPriorSuccessfulWorkerAndCannotEraseReceipts(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	failProfessionalScan(t, s)
	id := uuid.Must(uuid.NewV7()).String()
	path := s.path + "/scan-retries/" + id
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	s.cloud.AfterCopy(func() { require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code) })
	status, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "superseded", status)
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "steward", "").Code)
	next := s.input
	next.ScanAttempt = 2
	status, err = cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	require.Equal(t, 1, s.cloud.CopyCount(), "same immutable bytes survive superseded copy")
	for _, query := range []string{"UPDATE practitioner_evidence_retries SET status='queued' WHERE tenant_id=? AND id=?", "DELETE FROM practitioner_evidence_retries WHERE tenant_id=? AND id=?"} {
		err = s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; err != nil {
				return err
			}
			return tx.Exec(query, s.input.TenantID, id).Error
		})
		require.ErrorContains(t, err, "receipt is immutable")
	}
}

func TestProfessionalConcurrentRetryRetainsOneReceiptAndOneScanRequest(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	failProfessionalScan(t, s)
	path := s.path + "/scan-retries/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	codes := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(s.router, "PUT", path, "steward", body).Code })
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 201 {
			created++
		} else {
			require.Equal(t, 200, code)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/scan-retries/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=? AND payload->'data'->>'scan_attempt'='2'", practitioner.EvidenceScanRequested).Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
}

func TestProfessionalRetryCannotAdvanceWithoutRetainedDecision(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	failProfessionalScan(t, s)
	err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE practitioner_credential_evidence SET scan_attempt=2,status='scan_pending',failure_code=NULL WHERE tenant_id=? AND id=?", s.input.TenantID, s.input.FileID).Error
	})
	require.ErrorContains(t, err, "requires retained receipt")
}

func TestProfessionalRetryRollsBackReceiptAttemptAndOutboxOnDependencyFailure(t *testing.T) {
	for _, dependency := range []string{"audit", "outbox"} {
		t.Run(dependency, func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			failProfessionalScan(t, s)
			path := s.path + "/scan-retries/" + uuid.Must(uuid.NewV7()).String()
			body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
			table, condition := "audit_events", "NEW.action='insert' AND NEW.resource_type='practitioner_evidence_retries'"
			if dependency == "outbox" {
				table = "outbox_events"
				condition = "NEW.subject='helivanta.in.practitioner.evidence_scan_requested.v1'"
			}
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retry_failure", SQL: "CREATE FUNCTION reject_retry() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF " + condition + " THEN RAISE EXCEPTION 'synthetic retry outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_retry BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION reject_retry();"}}))
			failed := testutil.Do(s.router, "PUT", path, "steward", body)
			require.Equal(t, 500, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic")
			state := testutil.Do(s.router, "GET", s.path, "steward", "")
			require.Contains(t, state.Body.String(), `"scan_attempt":1`)
			require.Contains(t, state.Body.String(), `"status":"scan_error"`)
			require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true)").Error; err != nil {
					return err
				}
				var n int64
				err := tx.Table("practitioner_evidence_retries").Count(&n).Error
				require.Zero(t, n)
				return err
			}))
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retry_recovery", SQL: "DROP TRIGGER reject_retry ON " + table + ";"}}))
			require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "steward", body).Code)
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retry_replay_audit", SQL: "CREATE FUNCTION reject_retry_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_evidence_retries' THEN RAISE EXCEPTION 'synthetic read outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_retry_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_retry_read();"}}))
			require.Equal(t, 500, testutil.Do(s.router, "PUT", path, "steward", body).Code)
		})
	}
}

func TestProfessionalStaleWorkerDatabaseVerdictCannotChangeRetriedEvidence(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	failProfessionalScan(t, s)
	body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path+"/scan-retries/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	err := workerEvidenceScope(t, s, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE practitioner_credential_evidence SET status='scan_error',failure_code='scanner_unavailable' WHERE tenant_id=? AND id=?", s.input.TenantID, s.input.FileID).Error
	})
	require.ErrorContains(t, err, "attempt is stale")
	next := s
	next.input.ScanAttempt = 2
	err = workerEvidenceScope(t, next, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE practitioner_credential_evidence SET scan_attempt=3 WHERE tenant_id=? AND id=?", s.input.TenantID, s.input.FileID).Error
	})
	require.ErrorContains(t, err, "permission denied")
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true)").Error; err != nil {
			return err
		}
		var attempt int64
		err := tx.Table("practitioner_credential_evidence").Select("scan_attempt").Where("tenant_id=? AND id=?", s.input.TenantID, s.input.FileID).Scan(&attempt).Error
		require.EqualValues(t, 2, attempt)
		return err
	}))
}

func TestProfessionalRetryNeverReopensRejectedOrRevokedEvidence(t *testing.T) {
	for _, state := range []string{"rejected", "revoked"} {
		t.Run(state, func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			if state == "rejected" {
				scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: Eicar-Test-Signature FOUND\x00"), time.Second, attachments.MaxFileBytes)
				require.NoError(t, err)
				p, err := practitioner.NewEvidenceProcessor(t.Context(), s.worker, s.files, scanner)
				require.NoError(t, err)
				status, err := p.Process(t.Context(), s.input)
				require.NoError(t, err)
				require.Equal(t, "rejected", status)
			} else {
				_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
				require.NoError(t, err)
				revoked := testutil.Do(s.router, "PUT", s.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7())))
				require.Equal(t, 201, revoked.Code, revoked.Body.String())
			}
			body := fmt.Sprintf(`{"expected_attempt":1,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
			require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/scan-retries/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
			require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "steward", "").Code)
		})
	}
}
