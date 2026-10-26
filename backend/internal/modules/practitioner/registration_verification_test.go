package practitioner_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

type registrationAttestationScenario struct {
	professionalEvidenceScenario
	path, body, base, policy, review, appointment string
}

func newRegistrationAttestationScenario(t *testing.T) registrationAttestationScenario {
	t.Helper()
	s := newProfessionalEvidenceScenario(t)
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	appointment := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/steward-appointments/"+appointment, "steward", fmt.Sprintf(`{"subject":"user-reviewer","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))).Code)
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/my/steward-appointments/"+appointment+"/accept", "reviewer", `{"expected_version":1}`).Code)
	policy := uuid.Must(uuid.NewV7()).String()
	config := fmt.Sprintf(`{"source_system":"synthetic.council","normalization":"ascii_upper","expiry_time_zone":"Asia/Kolkata","max_source_age_seconds":86400,"date_rule":"allow_undated_with_attestation","expiry_boundary":"end_of_day","valid_until":"2097-01-01T00:00:00Z","review_reference":"%s","appointment_id":"%s","appointment_version":2}`, uuid.Must(uuid.NewV7()), appointment)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/registration-policies/"+policy, "reviewer", config).Code)
	review := uuid.Must(uuid.NewV7()).String()
	base := strings.Split(s.path, "/evidence/")[0]
	observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	source := fmt.Sprintf(`{"appointment_id":"%s","appointment_version":2,"review_reference":"%s","source_reference":"%s","source_checked_at":"%s","finding":"active","evidence_ids":["%s"]}`, appointment, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), observed, s.input.FileID)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", base+"/reviews/"+review, "reviewer", source).Code)
	body := fmt.Sprintf(`{"expected_version":0,"policy_id":"%s","policy_version":1,"review_id":"%s","identity_review_reference":"%s","undated_attestation_reference":"%s"}`, policy, review, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))
	path := base + "/verifications/" + uuid.Must(uuid.NewV7()).String()
	return registrationAttestationScenario{s, path, body, base, policy, review, appointment}
}
func TestRegistrationAttestationRetainsCanonicalIdentityWithoutClinicalGrant(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	path, body := s.path, s.body
	verified := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 201, verified.Code, verified.Body.String())
	require.Contains(t, verified.Body.String(), `"method":"independent_source_review"`)
	require.Contains(t, verified.Body.String(), `"clinical_authority":"not_evaluated"`)
	replay := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, verified.Body.String(), replay.Body.String())
}

func TestRegistrationAttestationWithdrawalPreservesHistoryAndCanonicalOwnership(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	saved := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	root := strings.Split(s.base, "/revisions/")[0]
	current := testutil.Do(s.router, "GET", root+"/verification", "operator", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"state":"verified"`)
	revokePath := root + "/verification-revocations/" + uuid.Must(uuid.NewV7()).String()
	revokeBody := fmt.Sprintf(`{"expected_version":1,"target_verification_id":"%s","reason_reference":"%s"}`, s.path[len(s.path)-36:], uuid.Must(uuid.NewV7()))
	withdrawn := testutil.Do(s.router, "PUT", revokePath, "reviewer", revokeBody)
	require.Equal(t, 201, withdrawn.Code, withdrawn.Body.String())
	require.Equal(t, 200, testutil.Do(s.router, "PUT", revokePath, "reviewer", revokeBody).Code)
	current = testutil.Do(s.router, "GET", root+"/verification", "operator", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"state":"revoked"`)
	retained := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 200, retained.Code)
	require.JSONEq(t, saved.Body.String(), retained.Body.String())
	read := testutil.Do(s.router, "GET", revokePath, "operator", "")
	require.Equal(t, 200, read.Code)
	require.JSONEq(t, withdrawn.Body.String(), read.Body.String())
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var n int64
		err := tx.Table("practitioner_registration_identifiers").Where("registration_id=?", s.input.RegistrationID).Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
}

func TestRegistrationAttestationConcurrentReplayAndStaleIntent(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	codes := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code })
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
	stale := testutil.Do(s.router, "PUT", s.path[:len(s.path)-36]+uuid.Must(uuid.NewV7()).String(), "reviewer", s.body)
	require.Equal(t, 409, stale.Code)
	require.Contains(t, stale.Body.String(), "verification_version_conflict")
	changed := testutil.Do(s.router, "PUT", s.path, "reviewer", strings.Replace(s.body, `"policy_version":1`, `"policy_version":2`, 1))
	require.Equal(t, 409, changed.Code)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var n int64
		err := tx.Table("practitioner_registration_verifications").Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
}

func TestRegistrationAttestationRejectsSelfWrongScopeAndChangedSource(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", s.path, "operator", s.body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", s.path, "foreign", s.body).Code)
	self := testutil.Do(s.router, "PUT", s.path, "steward", s.body)
	require.Equal(t, 409, self.Code)
	require.Contains(t, self.Body.String(), "independent_verifier_required")
	require.Equal(t, 400, testutil.Do(s.router, "PUT", s.path, "reviewer", strings.TrimSuffix(s.body, "}")+`,"clinical_authority":"granted"}`).Code)
	revoked := testutil.Do(s.router, "PUT", s.professionalEvidenceScenario.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7())))
	require.Equal(t, 201, revoked.Code, revoked.Body.String())
	blocked := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 409, blocked.Code)
	require.Contains(t, blocked.Body.String(), "reviewed_evidence_not_current")
}

func TestRegistrationAttestationCurrentProjectionInvalidatesOnEvidenceOrPolicyChange(t *testing.T) {
	for _, change := range []string{"evidence", "policy", "revision", "observation"} {
		t.Run(change, func(t *testing.T) {
			s := newRegistrationAttestationScenario(t)
			require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
			root := strings.Split(s.base, "/revisions/")[0]
			switch change {
			case "evidence":
				require.Equal(t, 201, testutil.Do(s.router, "PUT", s.professionalEvidenceScenario.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))).Code)
			case "policy":
				edition := fmt.Sprintf(`{"expected_version":1,"max_source_age_seconds":3600,"date_rule":"allow_undated_with_attestation","expiry_boundary":"end_of_day","valid_until":"2097-01-01T00:00:00Z","review_reference":"%s","appointment_id":"%s","appointment_version":2}`, uuid.Must(uuid.NewV7()), s.appointment)
				require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/registration-policies/"+s.policy+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", edition).Code)
			case "revision":
				require.Equal(t, 201, testutil.Do(s.router, "PUT", root+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "steward", `{"expected_version":1}`).Code)
			case "observation":
				finding := fmt.Sprintf(`{"appointment_id":"%s","appointment_version":2,"review_reference":"%s","source_reference":"%s","source_checked_at":"%s","finding":"unavailable","evidence_ids":["%s"]}`, s.appointment, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano), s.input.FileID)
				require.Equal(t, 201, testutil.Do(s.router, "PUT", s.base+"/reviews/"+uuid.Must(uuid.NewV7()).String(), "reviewer", finding).Code)
			}
			projected := testutil.Do(s.router, "GET", root+"/verification", "operator", "")
			require.Equal(t, 200, projected.Code, projected.Body.String())
			require.Contains(t, projected.Body.String(), `"state":"stale"`)
			require.NotContains(t, projected.Body.String(), `"state":"verified"`)
		})
	}
}

func TestRegistrationAttestationConflictingCanonicalIdentityDoesNotMergePeople(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
	person, registration, file := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	personPath := "/v1/practitioners/" + person
	require.Equal(t, 201, testutil.Do(s.router, "PUT", personPath, "steward", `{"given_name":"Other","professional_type":"physician"}`).Code)
	regPath := personPath + "/registrations/" + registration
	require.Equal(t, 201, testutil.Do(s.router, "PUT", regPath, "steward", `{"council_system":"synthetic.council","registration_number":"qa-1"}`).Code)
	base := regPath + "/revisions/" + registration
	evidence := base + "/evidence/" + file
	require.Equal(t, 201, testutil.Do(s.router, "PUT", evidence, "steward", fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(s.content), sha256.Sum256(s.content))).Code)
	request := httptest.NewRequest("PUT", evidence+"/content", bytes.NewReader(s.content))
	request.Header.Set("Authorization", "Bearer steward")
	request.Header.Set("Content-Type", "image/png")
	uploaded := httptest.NewRecorder()
	s.router.ServeHTTP(uploaded, request)
	require.Equal(t, 202, uploaded.Code, uploaded.Body.String())
	input := practitionercontract.EvidenceScanInput{TenantID: testutil.TenantA, PractitionerID: person, RegistrationID: registration, RevisionID: registration, FileID: file, ScanAttempt: 1}
	status, err := cleanProfessionalEvidenceProcessor(t, s.professionalEvidenceScenario).Process(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	review := uuid.Must(uuid.NewV7()).String()
	source := fmt.Sprintf(`{"appointment_id":"%s","appointment_version":2,"review_reference":"%s","source_reference":"%s","source_checked_at":"%s","finding":"active","evidence_ids":["%s"]}`, s.appointment, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano), file)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", base+"/reviews/"+review, "reviewer", source).Code)
	body := strings.Replace(s.body, s.review, review, 1)
	conflict := testutil.Do(s.router, "PUT", base+"/verifications/"+uuid.Must(uuid.NewV7()).String(), "reviewer", body)
	require.Equal(t, 409, conflict.Code, conflict.Body.String())
	require.Contains(t, conflict.Body.String(), "registration_identifier_conflict")
	require.NotContains(t, conflict.Body.String(), s.input.PractitionerID)
	require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
			return err
		}
		var bindings []struct{ PractitionerID string }
		if err := tx.Table("practitioner_registration_identifiers").Select("practitioner_id").Find(&bindings).Error; err != nil {
			return err
		}
		require.Len(t, bindings, 1)
		require.Equal(t, s.input.PractitionerID, bindings[0].PractitionerID)
		var n int64
		err := tx.Table("practitioner_registration_verifications").Where("registration_id=?", registration).Count(&n).Error
		require.Zero(t, n)
		return err
	}))
}

func TestRegistrationAttestationAuditOutboxFailureAndHistoryGuards(t *testing.T) {
	for _, dependency := range []string{"audit", "outbox"} {
		t.Run(dependency, func(t *testing.T) {
			s := newRegistrationAttestationScenario(t)
			table, condition := "audit_events", "NEW.action='insert' AND NEW.resource_type IN ('practitioner_registration_verifications','practitioner_registration_identifiers')"
			if dependency == "outbox" {
				table = "outbox_events"
				condition = "NEW.subject='helivanta.in.practitioner.registration_verification_changed.v1'"
			}
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_attestation_outage", SQL: "CREATE FUNCTION reject_attestation_dependency() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF " + condition + " THEN RAISE EXCEPTION 'synthetic attestation dependency outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_attestation_dependency BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION reject_attestation_dependency();"}}))
			failed := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
			require.Equal(t, 500, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic")
			require.NoError(t, s.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
					return err
				}
				for _, table := range []string{"practitioner_registration_verifications", "practitioner_registration_identifiers"} {
					var n int64
					if err := tx.Table(table).Count(&n).Error; err != nil {
						return err
					}
					require.Zero(t, n)
				}
				var reads int64
				err := tx.Table("audit_events").Where("action='read' AND resource_type='practitioner_registrations' AND resource_id=?", s.input.RegistrationID).Count(&reads).Error
				require.Positive(t, reads)
				return err
			}))
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_attestation_recovery", SQL: "DROP TRIGGER reject_attestation_dependency ON " + table + ";"}}))
			require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
			for _, sql := range []string{"UPDATE practitioner_registration_verifications SET valid_until=clock_timestamp() WHERE tenant_id=?", "DELETE FROM practitioner_registration_verifications WHERE tenant_id=?", "DELETE FROM practitioner_registration_identifiers WHERE tenant_id=?"} {
				err := s.db.WithTenant(tenantdb.WithActor(t.Context(), "user-reviewer"), testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true),set_config('app.practitioner_verify','true',true)").Error; err != nil {
						return err
					}
					return tx.Exec(sql, testutil.TenantA).Error
				})
				require.Error(t, err)
			}
		})
	}
}

func TestRegistrationAttestationRequiresUndatedEvidenceAndCurrentFreshness(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired_%t", expired), func(t *testing.T) {
			s := newRegistrationAttestationScenario(t)
			var body map[string]any
			require.NoError(t, json.Unmarshal([]byte(s.body), &body))
			if expired {
				require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_attestation_expired_source", SQL: `CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql STABLE AS $$ SELECT '2030-01-01T00:00:00Z'::timestamptz $$;`}}))
			} else {
				delete(body, "undated_attestation_reference")
			}
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			rejected := testutil.Do(s.router, "PUT", s.path, "reviewer", string(encoded))
			require.Equal(t, 409, rejected.Code, rejected.Body.String())
			root := strings.Split(s.base, "/revisions/")[0]
			current := testutil.Do(s.router, "GET", root+"/verification", "operator", "")
			require.Equal(t, 200, current.Code, current.Body.String())
			require.Contains(t, current.Body.String(), `"state":"unverified"`)
		})
	}
}

func TestRegistrationAttestationLeaseExpiresAtExactBoundary(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	saved := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	var receipt struct {
		ValidUntil time.Time `json:"valid_until"`
	}
	require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &receipt))
	require.False(t, receipt.ValidUntil.IsZero())
	sql := fmt.Sprintf(`CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql STABLE AS $$ SELECT '%s'::timestamptz $$;`, receipt.ValidUntil.UTC().Format(time.RFC3339Nano))
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_attestation_lease_boundary", SQL: sql}}))
	root := strings.Split(s.base, "/revisions/")[0]
	current := testutil.Do(s.router, "GET", root+"/verification", "operator", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"state":"stale"`)
}

func TestRegistrationAttestationReadAuditOutageBlocksReceiptsProjectionHistoryAndReplay(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	saved := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	root := strings.Split(s.base, "/revisions/")[0]
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_verification_read_audit_outage", SQL: `CREATE FUNCTION reject_verification_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type IN ('practitioner_registrations','practitioner_registration_verifications') THEN RAISE EXCEPTION 'synthetic sensitive audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_verification_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_verification_read();`}}))
	for _, path := range []string{s.path, root + "/verification", root + "/verifications"} {
		denied := testutil.Do(s.router, "GET", path, "operator", "")
		require.Equal(t, 500, denied.Code, denied.Body.String())
		require.NotContains(t, denied.Body.String(), "canonical_value")
		require.NotContains(t, denied.Body.String(), "synthetic")
	}
	replay := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 500, replay.Code, replay.Body.String())
	require.NotContains(t, replay.Body.String(), "canonical_value")
	require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_verification_read_audit_recovery", SQL: `DROP TRIGGER reject_verification_read ON audit_events;`}}))
	recovered := testutil.Do(s.router, "PUT", s.path, "reviewer", s.body)
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.JSONEq(t, saved.Body.String(), recovered.Body.String())
}

func TestRegistrationAttestationReadScopeAndForcedRLS(t *testing.T) {
	s := newRegistrationAttestationScenario(t)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path, "reviewer", s.body).Code)
	root := strings.Split(s.base, "/revisions/")[0]
	for _, path := range []string{s.path, root + "/verification", root + "/verifications"} {
		denied := testutil.Do(s.router, "GET", path, "foreign", "")
		require.Equal(t, 404, denied.Code, denied.Body.String())
		require.NotContains(t, denied.Body.String(), "canonical_value")
	}
	for _, tenant := range []string{testutil.TenantA, testutil.TenantB} {
		require.NoError(t, s.db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if tenant == testutil.TenantB {
				if err := tx.Exec("SELECT set_config('app.practitioner_access','true',true)").Error; err != nil {
					return err
				}
			}
			for _, table := range []string{"practitioner_registration_verifications", "practitioner_registration_identifiers"} {
				var n int64
				if err := tx.Table(table).Count(&n).Error; err != nil {
					return err
				}
				require.Zero(t, n)
			}
			return nil
		}))
	}
}

func TestRegistrationAttestationUsesConfiguredIssuerCalendarBoundary(t *testing.T) {
	cases := []struct {
		name, boundary, issued, expires, at string
		code                                int
	}{
		{"start_of_day_expired", "start_of_day", "2029-01-01", "2030-01-03", "2030-01-02T18:30:00Z", 409},
		{"end_of_day_still_current", "end_of_day", "2029-01-01", "2030-01-03", "2030-01-02T18:30:00Z", 201},
		{"end_of_day_exact_expiry", "end_of_day", "2029-01-01", "2030-01-02", "2030-01-02T18:30:00Z", 409},
		{"end_of_day_before_expiry", "end_of_day", "2029-01-01", "2030-01-02", "2030-01-02T18:29:59Z", 201},
		{"future_issue_date", "end_of_day", "2030-01-04", "2031-01-01", "2030-01-02T18:30:00Z", 409},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRegistrationAttestationScenario(t)
			root := strings.Split(s.base, "/revisions/")[0]
			revision, file := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
			base := root + "/revisions/" + revision
			revised := testutil.Do(s.router, "PUT", base, "steward", fmt.Sprintf(`{"expected_version":1,"issued_on":"%s","expires_on":"%s"}`, tc.issued, tc.expires))
			require.Equal(t, 201, revised.Code, revised.Body.String())
			evidence := base + "/evidence/" + file
			require.Equal(t, 201, testutil.Do(s.router, "PUT", evidence, "steward", fmt.Sprintf(`{"content_type":"image/png","size":%d,"sha256":"%x"}`, len(s.content), sha256.Sum256(s.content))).Code)
			request := httptest.NewRequest("PUT", evidence+"/content", bytes.NewReader(s.content))
			request.Header.Set("Authorization", "Bearer steward")
			request.Header.Set("Content-Type", "image/png")
			uploaded := httptest.NewRecorder()
			s.router.ServeHTTP(uploaded, request)
			require.Equal(t, 202, uploaded.Code, uploaded.Body.String())
			input := s.input
			input.RevisionID = revision
			input.FileID = file
			status, err := cleanProfessionalEvidenceProcessor(t, s.professionalEvidenceScenario).Process(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, "available", status)
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_issuer_calendar_clock", SQL: fmt.Sprintf(`CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql STABLE AS $$ SELECT '%s'::timestamptz $$;`, tc.at)}}))
			edition := fmt.Sprintf(`{"expected_version":1,"max_source_age_seconds":86400,"date_rule":"date_required","expiry_boundary":"%s","valid_until":"2097-01-01T00:00:00Z","review_reference":"%s","appointment_id":"%s","appointment_version":2}`, tc.boundary, uuid.Must(uuid.NewV7()), s.appointment)
			policy := testutil.Do(s.router, "PUT", "/v1/practitioners/registration-policies/"+s.policy+"/revisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", edition)
			require.Equal(t, 201, policy.Code, policy.Body.String())
			review := uuid.Must(uuid.NewV7()).String()
			source := fmt.Sprintf(`{"appointment_id":"%s","appointment_version":2,"review_reference":"%s","source_reference":"%s","source_checked_at":"%s","finding":"active","evidence_ids":["%s"]}`, s.appointment, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), tc.at, file)
			reviewed := testutil.Do(s.router, "PUT", base+"/reviews/"+review, "reviewer", source)
			require.Equal(t, 201, reviewed.Code, reviewed.Body.String())
			body := strings.Replace(strings.Replace(s.body, s.review, review, 1), `"policy_version":1`, `"policy_version":2`, 1)
			result := testutil.Do(s.router, "PUT", base+"/verifications/"+uuid.Must(uuid.NewV7()).String(), "reviewer", body)
			require.Equal(t, tc.code, result.Code, result.Body.String())
			if tc.code == 201 {
				var receipt struct {
					ValidUntil time.Time `json:"valid_until"`
				}
				require.NoError(t, json.Unmarshal(result.Body.Bytes(), &receipt))
				location, err := time.LoadLocation("Asia/Kolkata")
				require.NoError(t, err)
				expiry, err := time.ParseInLocation("2006-01-02", tc.expires, location)
				require.NoError(t, err)
				expected := expiry.AddDate(0, 0, 1).UTC()
				checked, err := time.Parse(time.RFC3339, tc.at)
				require.NoError(t, err)
				if sourceCeiling := checked.Add(24 * time.Hour); sourceCeiling.Before(expected) {
					expected = sourceCeiling
				}
				require.True(t, receipt.ValidUntil.Equal(expected), "got %s, expected %s", receipt.ValidUntil, expected)
			}
		})
	}
}
