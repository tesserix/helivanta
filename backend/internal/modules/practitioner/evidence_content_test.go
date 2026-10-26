package practitioner_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestProfessionalEvidencePrivateContentMatchesReleasedBytesAndSecureHeaders(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	p := cleanProfessionalEvidenceProcessor(t, s)
	status, err := p.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	got := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
	require.Equal(t, 200, got.Code, got.Body.String())
	require.Equal(t, s.content, got.Body.Bytes())
	require.Equal(t, "image/png", got.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", got.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", got.Header().Get("X-Content-Type-Options"))
	require.Contains(t, got.Header().Get("Content-Disposition"), "attachment;")
	require.Contains(t, got.Header().Get("Content-Disposition"), s.input.FileID)
	require.Equal(t, "default-src 'none'; sandbox", got.Header().Get("Content-Security-Policy"))
	require.Equal(t, 404, testutil.Do(s.router, "GET", s.path+"/content", "foreign", "").Code)
	require.Equal(t, 401, testutil.Do(s.router, "GET", s.path+"/content", "missing", "").Code)
}
func TestProfessionalEvidenceRevocationRetainsDecisionAndBlocksContent(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	p := cleanProfessionalEvidenceProcessor(t, s)
	_, err := p.Process(t.Context(), s.input)
	require.NoError(t, err)
	path := s.path + "/revocations/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	created := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	var receipt map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &receipt))
	require.Equal(t, s.input.FileID, receipt["file_id"])
	require.Equal(t, "revoked", receipt["status"])
	require.NotContains(t, receipt, "created_by")
	require.NotContains(t, receipt, "tenant_id")
	replay := testutil.Do(s.router, "PUT", path, "steward", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	changed := strings.Replace(body, "review_reference", "unknown_reference", 1)
	require.Equal(t, 400, testutil.Do(s.router, "PUT", path, "steward", changed).Code)
	changed = fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", changed).Code)
	blocked := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
	require.Equal(t, 409, blocked.Code)
	require.NotEqual(t, s.content, blocked.Body.Bytes())
	require.Equal(t, 1, s.cloud.CopyCount())
	req := httptest.NewRequest("GET", s.path+"/content", nil)
	req.Header.Set("Authorization", "Bearer steward")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantB)
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, req)
	require.Equal(t, 409, response.Code)
}

func TestProfessionalEvidenceContentRequiresReleasedStateAndSeparateEvidenceAuthority(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "steward", "").Code)
	path := s.path + "/revocations/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "steward", body).Code)
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, 200, testutil.Do(s.router, "GET", s.path+"/content", "reader", "").Code)
	require.Equal(t, 403, testutil.Do(s.router, "GET", s.path+"/content", "operator", "").Code)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", path, "reader", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "foreign", body).Code)
	for _, bad := range []string{`null`, `[]`, `{}`, `{"review_reference":"not-a-reference"}`, strings.TrimSuffix(body, "}") + `,"tenant_id":"` + testutil.TenantB + `"}`, strings.TrimSuffix(body, "}") + `,"status":"revoked"}`} {
		got := testutil.Do(s.router, "PUT", path, "steward", bad)
		require.Equal(t, 400, got.Code, got.Body.String())
	}
}
func TestProfessionalEvidenceContentRechecksRevocationAfterActualStorageRead(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	called := atomic.Bool{}
	s.cloud.AfterReleasedRead(func() {
		called.Store(true)
		path := s.path + "/revocations/" + uuid.Must(uuid.NewV7()).String()
		body := fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
		res := testutil.Do(s.router, "PUT", path, "steward", body)
		require.Equal(t, 201, res.Code, res.Body.String())
	})
	blocked := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
	require.True(t, called.Load())
	require.Equal(t, 409, blocked.Code, blocked.Body.String())
	require.NotEqual(t, s.content, blocked.Body.Bytes())
	require.Equal(t, "application/json; charset=utf-8", blocked.Header().Get("Content-Type"))
}
func TestProfessionalEvidenceContentRechecksMembershipAfterActualStorageRead(t *testing.T) {
	for _, outage := range []bool{false, true} {
		t.Run(fmt.Sprint(outage), func(t *testing.T) {
			membership := &evidenceMembership{}
			s := newProfessionalEvidenceScenarioWithAccess(t, membership)
			_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
			require.NoError(t, err)
			s.cloud.AfterReleasedRead(func() {
				if outage {
					membership.unavailable.Store(true)
				} else {
					membership.denied.Store(true)
				}
			})
			got := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
			expected := 403
			if outage {
				expected = 503
			}
			require.Equal(t, expected, got.Code, got.Body.String())
			require.NotEqual(t, s.content, got.Body.Bytes())
			membership.unavailable.Store(false)
			membership.denied.Store(false)
			recovered := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
			require.Equal(t, 200, recovered.Code)
			require.Equal(t, s.content, recovered.Body.Bytes())
		})
	}
}
func TestProfessionalEvidenceContentBlocksReadAuditAndPostReadAuditFailures(t *testing.T) {
	for _, afterRead := range []bool{false, true} {
		t.Run(fmt.Sprint(afterRead), func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
			require.NoError(t, err)
			called := atomic.Bool{}
			blockAudit := func() {
				require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_content_read_audit_outage", SQL: `CREATE FUNCTION reject_content_read_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_registration_evidence' THEN RAISE EXCEPTION 'synthetic read audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_content_read_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_content_read_audit();`}}))
			}
			s.cloud.AfterReleasedRead(func() {
				called.Store(true)
				if afterRead {
					blockAudit()
				}
			})
			if !afterRead {
				blockAudit()
			}
			got := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
			require.Equal(t, 500, got.Code, got.Body.String())
			require.Equal(t, afterRead, called.Load())
			require.NotEqual(t, s.content, got.Body.Bytes())
			require.NotContains(t, got.Body.String(), "synthetic")
		})
	}
}
func TestProfessionalEvidenceContentBlocksUnavailableAndChangedReleasedObjects(t *testing.T) {
	for _, corruption := range []bool{false, true} {
		t.Run(fmt.Sprint(corruption), func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
			require.NoError(t, err)
			if corruption {
				s.cloud.CorruptReleased([]byte("untrusted replacement"))
			} else {
				s.cloud.DenyReads(true)
			}
			got := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
			require.Equal(t, 503, got.Code, got.Body.String())
			require.NotEqual(t, s.content, got.Body.Bytes())
			require.NotContains(t, got.Body.String(), "untrusted replacement")
		})
	}
}
func TestProfessionalEvidenceConcurrentRevocationRetainsOneDecisionAndEvent(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	p := cleanProfessionalEvidenceProcessor(t, s)
	_, err := p.Process(t.Context(), s.input)
	require.NoError(t, err)
	id := uuid.Must(uuid.NewV7()).String()
	path := s.path + "/revocations/" + id
	body := fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
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
	require.Equal(t, 409, testutil.Do(s.router, "PUT", s.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", body).Code)
	state, err := p.Process(t.Context(), s.input)
	require.NoError(t, err)
	require.Equal(t, "revoked", state)
	require.Equal(t, 1, s.cloud.CopyCount())
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=? AND payload->'data'->>'status'='revoked'", "helivanta.in.practitioner.evidence_changed.v1").Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
	for _, sql := range []string{"UPDATE practitioner_evidence_revocations SET review_reference=? WHERE tenant_id=? AND id=?", "DELETE FROM practitioner_evidence_revocations WHERE review_reference<>? AND tenant_id=? AND id=?"} {
		err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; err != nil {
				return err
			}
			return tx.Exec(sql, uuid.Must(uuid.NewV7()), s.input.TenantID, id).Error
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "receipt is immutable")
	}
}

func TestProfessionalEvidenceCannotBeRevokedWithoutRetainedDecision(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	err = s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.actor_subject','user-steward',true),set_config('app.practitioner_evidence_access','true',true),set_config('app.practitioner_evidence_manage','true',true)").Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE practitioner_registration_evidence SET status='revoked' WHERE tenant_id=? AND id=?", s.input.TenantID, s.input.FileID).Error
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires retained receipt")
	got := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
	require.Equal(t, 200, got.Code)
	require.Equal(t, s.content, got.Body.Bytes())
}
func TestProfessionalEvidenceRevocationRollsBackWithAuditOrOutboxFailureAndReplaysSafely(t *testing.T) {
	for _, dependency := range []string{"audit", "outbox"} {
		t.Run(dependency, func(t *testing.T) {
			s := newProfessionalEvidenceScenario(t)
			_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
			require.NoError(t, err)
			path := s.path + "/revocations/" + uuid.Must(uuid.NewV7()).String()
			body := fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
			table, condition := "audit_events", "NEW.action='insert' AND NEW.resource_type='practitioner_evidence_revocations'"
			if dependency == "outbox" {
				table = "outbox_events"
				condition = "NEW.subject='helivanta.in.practitioner.evidence_changed.v1' AND NEW.payload->'data'->>'status'='revoked'"
			}
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_revocation_commit_failure", SQL: "CREATE FUNCTION reject_revocation_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF " + condition + " THEN RAISE EXCEPTION 'synthetic revocation commit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_revocation_commit BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION reject_revocation_commit();"}}))
			failed := testutil.Do(s.router, "PUT", path, "steward", body)
			require.Equal(t, 500, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic")
			got := testutil.Do(s.router, "GET", s.path+"/content", "steward", "")
			require.Equal(t, 200, got.Code)
			require.Equal(t, s.content, got.Body.Bytes())
			require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.practitioner_evidence_access','true',true)").Error; err != nil {
					return err
				}
				var n int64
				err := tx.Table("practitioner_evidence_revocations").Count(&n).Error
				require.Zero(t, n)
				return err
			}))
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_revocation_commit_recovery", SQL: "DROP TRIGGER reject_revocation_commit ON " + table + ";"}}))
			created := testutil.Do(s.router, "PUT", path, "steward", body)
			require.Equal(t, 201, created.Code, created.Body.String())
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_revocation_replay_audit_failure", SQL: `CREATE FUNCTION reject_revocation_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_evidence_revocations' THEN RAISE EXCEPTION 'synthetic receipt audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_revocation_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_revocation_read();`}}))
			replay := testutil.Do(s.router, "PUT", path, "steward", body)
			require.Equal(t, 500, replay.Code, replay.Body.String())
			require.NotContains(t, replay.Body.String(), "review_reference")
			require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "steward", "").Code)
		})
	}
}

func TestProfessionalEvidenceUploadWindowDoesNotEraseRetainedContentOrPreventRevocation(t *testing.T) {
	s := newProfessionalEvidenceScenario(t)
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_retained_content_after_upload_window", SQL: `CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT clock_timestamp()+interval '2 hours' $$;`}}))
	got := testutil.Do(s.router, "GET", s.path+"/content", "reader", "")
	require.Equal(t, 200, got.Code, got.Body.String())
	require.Equal(t, s.content, got.Body.Bytes())
	body := fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	revoked := testutil.Do(s.router, "PUT", s.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", body)
	require.Equal(t, 201, revoked.Code, revoked.Body.String())
	require.Equal(t, 409, testutil.Do(s.router, "GET", s.path+"/content", "reader", "").Code)
}
