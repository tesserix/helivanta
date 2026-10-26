package practitioner_test

import (
	"fmt"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func reviewScenario(t *testing.T) (professionalEvidenceScenario, string, string, string) {
	t.Helper()
	s := newProfessionalEvidenceScenario(t)
	_, err := cleanProfessionalEvidenceProcessor(t, s).Process(t.Context(), s.input)
	require.NoError(t, err)
	appointment := uuid.Must(uuid.NewV7()).String()
	nominee := fmt.Sprintf(`{"subject":"user-reviewer","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/steward-appointments/"+appointment, "steward", nominee).Code)
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/my/steward-appointments/"+appointment+"/accept", "reviewer", `{"expected_version":1}`).Code)
	path := fmt.Sprintf("/v1/practitioners/%s/registrations/%s/revisions/%s/reviews/%s", s.input.PractitionerID, s.input.RegistrationID, s.input.RevisionID, uuid.Must(uuid.NewV7()))
	body := fmt.Sprintf(`{"appointment_id":"%s","appointment_version":2,"review_reference":"%s","source_reference":"%s","source_checked_at":"2026-01-01T00:00:00Z","finding":"active","evidence_ids":["%s"]}`, appointment, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), s.input.FileID)
	return s, appointment, path, body
}
func TestRegistrationReviewRetainsExactRevisionEvidenceAndIndependentReviewer(t *testing.T) {
	s, _, path, body := reviewScenario(t)
	created := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"finding":"active"`)
	require.Contains(t, created.Body.String(), `"reviewer":"user-reviewer"`)
	require.NotContains(t, created.Body.String(), "released_generation")
	replay := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	read := testutil.Do(s.router, "GET", path, "reviewer", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())
	owner := testutil.Do(s.router, "GET", "/v1/practitioners/"+s.input.PractitionerID, "steward", "")
	require.Contains(t, owner.Body.String(), `"verification_state":"unverified"`)
}

func TestRegistrationReviewRejectsWrongScopeIntentAndAuthority(t *testing.T) {
	s, appointment, path, body := reviewScenario(t)
	require.Equal(t, 403, testutil.Do(s.router, "PUT", path, "operator", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", path, "foreign", body).Code)
	require.Equal(t, 404, testutil.Do(s.router, "PUT", strings.Replace(path, s.input.RevisionID, uuid.Must(uuid.NewV7()).String(), 1), "reviewer", body).Code)
	for _, bad := range []string{`null`, `[]`, `{}`, strings.Replace(body, `"appointment_version":2`, `"appointment_version":1`, 1), strings.Replace(body, `"finding":"active"`, `"finding":"verified"`, 1), strings.Replace(body, s.input.FileID, "not-a-file", 1), strings.Replace(body, `["`+s.input.FileID+`"]`, `[]`, 1), strings.Replace(body, `["`+s.input.FileID+`"]`, `["`+s.input.FileID+`","`+s.input.FileID+`"]`, 1), strings.TrimSuffix(body, "}") + `,"verified":true}`} {
		require.Equal(t, 400, testutil.Do(s.router, "PUT", path, "reviewer", bad).Code, bad)
	}
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "reviewer", strings.Replace(body, "2026-01-01", "2099-01-01", 1)).Code)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "reviewer", body).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "reviewer", strings.Replace(body, `"finding":"active"`, `"finding":"unavailable"`, 1)).Code)
	require.Equal(t, 404, testutil.Do(s.router, "GET", path, "foreign", "").Code)
	revoke := fmt.Sprintf(`{"expected_version":2,"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/steward-appointments/"+appointment+"/revoke", "steward", revoke).Code)
	nextPath := path[:len(path)-36] + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, testutil.Do(s.router, "PUT", nextPath, "reviewer", body).Code)
	require.Equal(t, 200, testutil.Do(s.router, "PUT", path, "reviewer", body).Code, "retained receipt replay is audited read, not reactivation")
}

func TestRegistrationReviewPreventsSubmitterReviewEvenWithAcceptedAppointment(t *testing.T) {
	s, oldAppointment, path, body := reviewScenario(t)
	appointment := uuid.Must(uuid.NewV7()).String()
	nominee := fmt.Sprintf(`{"subject":"user-steward","review_reference":"%s","valid_until":"2098-01-01T00:00:00Z"}`, uuid.Must(uuid.NewV7()))
	require.Equal(t, 201, testutil.Do(s.router, "PUT", "/v1/practitioners/steward-appointments/"+appointment, "governor", nominee).Code)
	require.Equal(t, 200, testutil.Do(s.router, "POST", "/v1/practitioners/my/steward-appointments/"+appointment+"/accept", "steward", `{"expected_version":1}`).Code)
	denied := testutil.Do(s.router, "PUT", path, "steward", strings.Replace(body, oldAppointment, appointment, 1))
	require.Equal(t, 409, denied.Code, denied.Body.String())
}

func TestRegistrationReviewRetainsReceiptAfterRenewalAndEvidenceRevocation(t *testing.T) {
	s, _, path, body := reviewScenario(t)
	saved := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 201, saved.Code, saved.Body.String())
	revision := uuid.Must(uuid.NewV7()).String()
	renewal := fmt.Sprintf("/v1/practitioners/%s/registrations/%s/revisions/%s", s.input.PractitionerID, s.input.RegistrationID, revision)
	require.Equal(t, 201, testutil.Do(s.router, "PUT", renewal, "steward", `{"expected_version":1}`).Code)
	require.Equal(t, 409, testutil.Do(s.router, "PUT", path[:len(path)-36]+uuid.Must(uuid.NewV7()).String(), "reviewer", body).Code)
	revoked := testutil.Do(s.router, "PUT", s.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7())))
	require.Equal(t, 201, revoked.Code, revoked.Body.String())
	replay := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, saved.Body.String(), replay.Body.String())
	list := testutil.Do(s.router, "GET", path[:len(path)-37]+"?limit=1", "reviewer", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"finding":"active"`)
	require.NotContains(t, list.Body.String(), "released_generation")
}

func TestRegistrationReviewConcurrentReplayRetainsOneAuditDecisionAndEvent(t *testing.T) {
	s, _, path, body := reviewScenario(t)
	codes := make(chan int, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { codes <- testutil.Do(s.router, "PUT", path, "reviewer", body).Code })
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
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.practitioner.registration_review_recorded.v1").Count(&n).Error
		require.EqualValues(t, 1, n)
		return err
	}))
}

func TestRegistrationReviewRollsBackOnAuditOrOutboxFailureAndProtectsHistory(t *testing.T) {
	for _, dependency := range []string{"audit", "outbox"} {
		t.Run(dependency, func(t *testing.T) {
			s, _, path, body := reviewScenario(t)
			table, condition := "audit_events", "NEW.resource_type='practitioner_credential_reviews'"
			if dependency == "outbox" {
				table = "outbox_events"
				condition = "NEW.subject='helivanta.in.practitioner.registration_review_recorded.v1'"
			}
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_review_failure", SQL: "CREATE FUNCTION reject_review() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF " + condition + " THEN RAISE EXCEPTION 'synthetic review outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_review BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION reject_review();"}}))
			failed := testutil.Do(s.router, "PUT", path, "reviewer", body)
			require.Equal(t, 500, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic")
			require.Equal(t, 404, testutil.Do(s.router, "GET", path, "reviewer", "").Code)
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_review_recovery", SQL: "DROP TRIGGER reject_review ON " + table + ";"}}))
			require.Equal(t, 201, testutil.Do(s.router, "PUT", path, "reviewer", body).Code)
			for _, query := range []string{"UPDATE practitioner_credential_reviews SET finding='unavailable' WHERE tenant_id=? AND id=?", "DELETE FROM practitioner_credential_reviews WHERE tenant_id=? AND id=?"} {
				err := s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
					if err := tx.Exec("SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.practitioner_access','true',true),set_config('app.practitioner_review','true',true)").Error; err != nil {
						return err
					}
					return tx.Exec(query, s.input.TenantID, path[len(path)-36:]).Error
				})
				require.ErrorContains(t, err, "review is immutable")
			}
			require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_review_read_failure", SQL: "CREATE FUNCTION reject_review_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='read' AND NEW.resource_type='practitioner_credential_reviews' THEN RAISE EXCEPTION 'synthetic read outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_review_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_review_read();"}}))
			read := testutil.Do(s.router, "GET", path, "reviewer", "")
			require.Equal(t, 500, read.Code)
			require.NotContains(t, read.Body.String(), "source_reference")
			require.Equal(t, 500, testutil.Do(s.router, "PUT", path, "reviewer", body).Code)
		})
	}
}

func TestRegistrationReviewRejectsElapsedAuthorityAndUnavailableEvidence(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			s, _, path, body := reviewScenario(t)
			if expired {
				require.NoError(t, s.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_review_expiry", SQL: `CREATE OR REPLACE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT '2099-01-01T00:00:00Z'::timestamptz $$;`}}))
			} else {
				require.Equal(t, 201, testutil.Do(s.router, "PUT", s.path+"/revocations/"+uuid.Must(uuid.NewV7()).String(), "steward", fmt.Sprintf(`{"review_reference":"%s"}`, uuid.Must(uuid.NewV7()))).Code)
			}
			require.Equal(t, 409, testutil.Do(s.router, "PUT", path, "reviewer", body).Code)
		})
	}
}

func TestRegistrationReviewPreservesUnavailableAndContradictorySourceFindings(t *testing.T) {
	s, _, path, body := reviewScenario(t)
	first := testutil.Do(s.router, "PUT", path, "reviewer", body)
	require.Equal(t, 201, first.Code, first.Body.String())
	unavailable := strings.Replace(body, `"finding":"active"`, `"finding":"unavailable"`, 1)
	secondPath := path[:len(path)-36] + uuid.Must(uuid.NewV7()).String()
	second := testutil.Do(s.router, "PUT", secondPath, "reviewer", unavailable)
	require.Equal(t, 201, second.Code, second.Body.String())
	list := testutil.Do(s.router, "GET", path[:len(path)-37], "reviewer", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"finding":"active"`)
	require.Contains(t, list.Body.String(), `"finding":"unavailable"`)
	require.NoError(t, s.db.WithTenant(t.Context(), s.input.TenantID, func(tx *gorm.DB) error {
		var rows []struct{ Payload string }
		if err := tx.Table("outbox_events").Select("payload::text AS payload").Where("subject=?", "helivanta.in.practitioner.registration_review_recorded.v1").Find(&rows).Error; err != nil {
			return err
		}
		require.Len(t, rows, 2)
		for _, row := range rows {
			require.NotContains(t, row.Payload, "source_reference")
			require.NotContains(t, row.Payload, "finding")
			require.NotContains(t, row.Payload, "sha256")
		}
		return nil
	}))
}
