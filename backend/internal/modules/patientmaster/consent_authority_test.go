package patientmaster_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // external test package importing its owner
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func consentAuthorityFixture(t *testing.T) (*gin.Engine, *tenantdb.DB, string) {
	t.Helper()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"reviewer": testutil.TenantA, "owner": testutil.TenantA, "peer": testutil.TenantA, "foreign": testutil.TenantB, "ungranted": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"reviewer":  {patientmaster.PermPatientRead, patientmaster.PermPatientRegister, authz.Permission("patient.consent-authority.review")},
			"ungranted": {authz.Permission("patient.consent-authority.review")},
		},
		Modules: []platform.Module{patientmaster.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		diagnostic := func(result *gorm.DB) {
			if result.Error == nil {
				return
			}
			var pg *pgconn.PgError
			if errors.As(result.Error, &pg) {
				t.Logf("authority database rejection: code=%s constraint=%s", pg.Code, pg.ConstraintName)
			} else {
				t.Logf("authority database error type: %T", result.Error)
			}
		}
		if err := tx.Callback().Raw().After("gorm:raw").Register("test_authority_diagnostics_raw", diagnostic); err != nil {
			return err
		}
		if err := tx.Callback().Query().After("gorm:query").Register("test_authority_diagnostics_query", diagnostic); err != nil {
			return err
		}
		return tx.Callback().Row().After("gorm:row").Register("test_authority_diagnostics_row", diagnostic)
	}))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-reviewer','registrar')`, testutil.TenantA, testHospital).Error
	}))
	patient := scopedDo(r, "POST", "/v1/patients", "reviewer", registerBody("SyntheticConsentAuthority", "Person", "1990-01-01", "", false, ""))
	require.Equal(t, 201, patient.Code, patient.Body.String())
	claim := mediconnectcontract.RegistrationReviewData{RequestID: uuid.Must(uuid.NewV7()).String(), AccountID: uuid.Must(uuid.NewV7()).String(), RequesterSubject: "user-owner", HospitalID: testHospital, PatientID: registeredID(t, patient), Relationship: "self"}
	encoded, err := json.Marshal(claim)
	require.NoError(t, err)
	consumer := patientmaster.New().Consumers(platform.Deps{})[0]
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
			return err
		}
		return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
	}))
	read := scopedDo(r, "GET", "/v1/patients/link-reviews/"+claim.RequestID, "reviewer", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	var detail map[string]any
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &detail))
	decision := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"account_id":%q,"outcome":"approved","reason":"identity_confirmed","evidence_reference":%q,"expected_registration_updated_at":%q}`, claim.AccountID, uuid.Must(uuid.NewV7()).String(), detail["registration_updated_at"])
	approved := scopedDo(r, "PUT", "/v1/patients/link-reviews/"+claim.RequestID+"/decision/"+decision, "reviewer", body)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	proposal := fmt.Sprintf(`{"review_request_id":%q,"expected_decision_id":%q,"capacity_confirmed":true,"policy_reference":%q,"evidence_reference":%q,"valid_until":%q}`, claim.RequestID, decision, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano))
	return r, db, proposal
}

func TestConsentAuthorityRequiresSeparateReviewedProposal(t *testing.T) {
	r, _, body := consentAuthorityFixture(t)
	path := "/v1/patients/consent-authorities/" + uuid.Must(uuid.NewV7()).String()
	created := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"status":"proposed"`)
	require.Contains(t, created.Body.String(), `"version":1`)
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	require.NotContains(t, created.Body.String(), "evidence_reference")
	require.NotContains(t, created.Body.String(), "user-owner")
	replay := scopedDo(r, "PUT", path, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
}

func TestConsentAuthorityRequiresSubjectAcceptanceAndRetainsRevocation(t *testing.T) {
	r, _, body := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	staff := "/v1/patients/consent-authorities/" + id
	mine := "/v1/patients/my/consent-authorities/" + id
	require.Equal(t, 201, scopedDo(r, "PUT", staff, "reviewer", body).Code)
	read := scopedDo(r, "GET", mine, "owner", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), `"effective_status":"proposed"`)
	accept := `{"expected_version":1,"accepted":true}`
	require.Equal(t, 404, scopedDo(r, "POST", mine+"/accept", "reviewer", accept).Code)
	accepted := scopedDo(r, "POST", mine+"/accept", "owner", accept)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), `"status":"accepted"`)
	require.Contains(t, accepted.Body.String(), `"version":2`)
	require.JSONEq(t, accepted.Body.String(), scopedDo(r, "POST", mine+"/accept", "owner", accept).Body.String())
	revoked := scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":2}`)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"status":"revoked"`)
	require.Contains(t, revoked.Body.String(), `"version":3`)
	require.JSONEq(t, revoked.Body.String(), scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":2}`).Body.String())
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/accept", "owner", accept).Code)
	replay := scopedDo(r, "PUT", staff, "reviewer", body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), `"status":"revoked"`)
}

func TestConsentAuthorityEnforcesSubjectHospitalAndReviewerScope(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	staff := "/v1/patients/consent-authorities/" + id
	mine := "/v1/patients/my/consent-authorities/" + id
	for _, token := range []string{"owner", "peer", "foreign"} {
		require.Equal(t, 403, scopedDo(r, "PUT", staff, token, body).Code)
	}
	require.Equal(t, 404, scopedDo(r, "PUT", staff, "ungranted", body).Code)
	created := scopedDo(r, "PUT", staff, "reviewer", body)
	require.Equal(t, 201, created.Code, created.Body.String())
	for _, token := range []string{"peer", "foreign", "reviewer", "ungranted"} {
		require.Equal(t, 404, scopedDo(r, "GET", mine, token, "").Code, token)
		require.Equal(t, 404, scopedDo(r, "POST", mine+"/revoke", token, `{"expected_version":1}`).Code, token)
	}
	require.Equal(t, 401, scopedDo(r, "GET", mine, "unknown", "").Code)
	wrong := do(r, "GET", mine+"?hospital_id=22222222-2222-4222-8222-222222222222", "owner", "")
	require.Equal(t, 404, wrong.Code)
	require.Equal(t, 404, do(r, "GET", mine, "owner", "").Code)
	require.Equal(t, 403, scopedDo(r, "GET", staff, "owner", "").Code)
	require.Equal(t, 200, scopedDo(r, "GET", staff, "reviewer", "").Code)
	var altered map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &altered))
	altered["evidence_reference"] = uuid.Must(uuid.NewV7()).String()
	encoded, err := json.Marshal(altered)
	require.NoError(t, err)
	require.Equal(t, 409, scopedDo(r, "PUT", staff, "reviewer", string(encoded)).Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM patient_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject='user-reviewer'`, testutil.TenantA, testHospital).Error
	}))
	require.Equal(t, 404, scopedDo(r, "GET", staff, "reviewer", "").Code)
	require.Equal(t, 404, scopedDo(r, "POST", staff+"/revoke", "reviewer", `{"expected_version":1}`).Code)
	require.Equal(t, 200, scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":1}`).Code)
}

func TestConsentAuthorityListsOnlyOwnedReceiptsWithCursor(t *testing.T) {
	r, _, body := consentAuthorityFixture(t)
	for range 3 {
		w := scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", body)
		require.Equal(t, 201, w.Code, w.Body.String())
	}
	path := "/v1/patients/my/consent-authorities?limit=2"
	first := scopedDo(r, "GET", path, "owner", "")
	require.Equal(t, 200, first.Code, first.Body.String())
	var page struct {
		Data []struct{ ID string }
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		}
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	second := scopedDo(r, "GET", path+"&cursor="+url.QueryEscape(*page.Page.NextCursor), "owner", "")
	require.Equal(t, 200, second.Code, second.Body.String())
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.False(t, page.Page.HasMore)
	for _, token := range []string{"peer", "foreign", "reviewer"} {
		w := scopedDo(r, "GET", path, token, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, w.Body.String())
	}
}

func TestConsentAuthorityRefusesInvalidAndStaleProposals(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	var original map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &original))
	for _, change := range []struct {
		key   string
		value any
		code  int
	}{
		{"capacity_confirmed", nil, 400}, {"capacity_confirmed", false, 400}, {"policy_reference", "", 400},
		{"evidence_reference", uuid.NewString(), 400}, {"expected_decision_id", uuid.Must(uuid.NewV7()).String(), 409},
		{"valid_until", "2020-01-01T00:00:00Z", 409}, {"valid_until", "2099-01-01T00:00:00.000000001Z", 400},
		{"tenant_id", testutil.TenantB, 400},
	} {
		t.Run(change.key+fmt.Sprint(change.value), func(t *testing.T) {
			in := maps.Clone(original)
			in[change.key] = change.value
			encoded, err := json.Marshal(in)
			require.NoError(t, err)
			w := scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", string(encoded))
			require.Equal(t, change.code, w.Code, w.Body.String())
		})
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE patients SET updated_at=updated_at+interval '1 second' WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, testHospital).Error
	}))
	stale := scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", body)
	require.Equal(t, 409, stale.Code, stale.Body.String())
}

func TestConsentAuthorityReadAuditFailureRollsBackAcceptance(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+id, "reviewer", body).Code)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_consent_authority_audit_failure", SQL: `CREATE FUNCTION reject_authority_reads() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='patient_consent_authorities' AND NEW.action='read' THEN RAISE EXCEPTION 'synthetic consent authority audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_authority_reads BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_authority_reads();`}}))
	mine := "/v1/patients/my/consent-authorities/" + id
	for _, request := range []struct{ method, path, body string }{{"GET", mine, ""}, {"POST", mine + "/accept", `{"expected_version":1,"accepted":true}`}} {
		w := scopedDo(r, request.method, request.path, "owner", request.body)
		require.Equal(t, 500, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "policy_reference")
		require.NotContains(t, w.Body.String(), "patient_id")
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		var status string
		if err := tx.Raw(`SELECT status FROM patient_consent_authorities WHERE tenant_id=? AND id=?`, testutil.TenantA, id).Scan(&status).Error; err != nil {
			return err
		}
		require.Equal(t, "proposed", status)
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM outbox_events WHERE tenant_id=? AND subject='helivanta.in.patientmaster.consent_authority_changed.v1'`, testutil.TenantA).Scan(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestConsentAuthorityExpiryDoesNotDependOnATimer(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	original := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+original, "reviewer", body).Code)
	expired := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO patient_consent_authorities(id,tenant_id,hospital_id,patient_id,review_request_id,decision_id,subject,reviewer_subject,policy_reference,evidence_reference,registration_updated_at,fingerprint,valid_until,created_at,updated_at)
   SELECT ?,tenant_id,hospital_id,patient_id,review_request_id,decision_id,subject,reviewer_subject,policy_reference,evidence_reference,registration_updated_at,fingerprint,clock_timestamp()-interval '1 hour',clock_timestamp()-interval '2 hours',clock_timestamp()-interval '2 hours' FROM patient_consent_authorities WHERE tenant_id=? AND id=?`, expired, testutil.TenantA, original).Error
	}))
	mine := "/v1/patients/my/consent-authorities/" + expired
	read := scopedDo(r, "GET", mine, "owner", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), `"effective_status":"expired"`)
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`).Code)
	require.Equal(t, 200, scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":1}`).Code)
}

func TestConsentAuthorityGlobalAPIForwardingAndConcurrentReplay(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	forwarded := consentAuthorityGateway(t, r)
	id := uuid.Must(uuid.NewV7()).String()
	staff := "/v1/patients/consent-authorities/" + id
	mine := "/v1/patients/my/consent-authorities/" + id
	responses := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { responses <- scopedDo(forwarded, "PUT", staff, "reviewer", body) })
	}
	wg.Wait()
	close(responses)
	created := 0
	for w := range responses {
		require.Contains(t, []int{200, 201}, w.Code, w.Body.String())
		if w.Code == 201 {
			created++
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 404, scopedDo(forwarded, "GET", mine, "peer", "").Code)
	responses = make(chan *httptest.ResponseRecorder, 8)
	for range 8 {
		wg.Go(func() {
			responses <- scopedDo(forwarded, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
		})
	}
	wg.Wait()
	close(responses)
	for w := range responses {
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"version":2`)
		require.Equal(t, "1.1 helivanta-global-api", w.Header().Get("Via"))
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM outbox_events WHERE tenant_id=? AND subject='helivanta.in.patientmaster.consent_authority_changed.v1'`, testutil.TenantA).Scan(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestConsentAuthoritySubjectCannotRewriteEvidenceOrSourceReview(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+id, "reviewer", body).Code)
	for _, query := range []string{
		`UPDATE patient_consent_authorities SET subject='user-peer',version=version+1 WHERE id=?`,
		`UPDATE patient_consent_authorities SET policy_reference='019a1b52-0000-7000-8000-000000000099',version=version+1 WHERE id=?`,
		`DELETE FROM patient_consent_authorities WHERE id=?`,
		`UPDATE patient_link_reviews SET status='withdrawn' WHERE request_id=(SELECT review_request_id FROM patient_consent_authorities WHERE id=?)`,
	} {
		err := db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
				return err
			}
			return tx.Exec(query, id).Error
		})
		require.Error(t, err, query)
	}
	read := scopedDo(r, "GET", "/v1/patients/my/consent-authorities/"+id, "owner", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), `"status":"proposed"`)
}

type previousPatientmaster struct{ *patientmaster.Module }

func (m previousPatientmaster) Migrations() []tenantdb.Migration {
	migrations := m.Module.Migrations()
	return migrations[:len(migrations)-1]
}
func TestConsentAuthorityMigrationPreservesPriorRegistrationAndRepeats(t *testing.T) {
	module := patientmaster.New()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"reviewer": {patientmaster.PermPatientRegister, patientmaster.PermPatientRead}}, Modules: []platform.Module{previousPatientmaster{module}}})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-reviewer','registrar')`, testutil.TenantA, testHospital).Error
	}))
	created := scopedDo(r, "POST", "/v1/patients", "reviewer", registerBody("SyntheticUpgradeAuthority", "Person", "1990-01-01", "", false, ""))
	require.Equal(t, 201, created.Code, created.Body.String())
	id := registeredID(t, created)
	require.NoError(t, db.Migrate(ctx, module.Migrations()))
	require.NoError(t, db.Migrate(ctx, module.Migrations()))
	require.NoError(t, db.Migrate(ctx, previousPatientmaster{module}.Migrations()))
	read := scopedDo(r, "GET", "/v1/patients/"+id, "reviewer", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), "SyntheticUpgradeAuthority")
}

func TestConsentAuthorityCannotAcceptWithdrawnIdentityAndReviewerCanRevoke(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	staff := "/v1/patients/consent-authorities/" + id
	mine := "/v1/patients/my/consent-authorities/" + id
	require.Equal(t, 201, scopedDo(r, "PUT", staff, "reviewer", body).Code)
	var input struct {
		ReviewRequestID string `json:"review_request_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &input))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE patient_link_reviews SET status='withdrawn',updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=?`, testutil.TenantA, input.ReviewRequestID).Error
	}))
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`).Code)
	revoked := scopedDo(r, "POST", staff+"/revoke", "reviewer", `{"expected_version":1}`)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"status":"revoked"`)
	require.JSONEq(t, revoked.Body.String(), scopedDo(r, "POST", staff+"/revoke", "reviewer", `{"expected_version":1}`).Body.String())
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`).Code)
}

func TestConsentAuthorityOutboxFailureLeavesNoProposal(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_consent_authority_outbox_failure", SQL: `CREATE FUNCTION reject_authority_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.patientmaster.consent_authority_changed.v1' THEN RAISE EXCEPTION 'synthetic authority outbox outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_authority_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_authority_outbox();`}}))
	id := uuid.Must(uuid.NewV7()).String()
	failed := scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+id, "reviewer", body)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "patient_id")
	require.Equal(t, 404, scopedDo(r, "GET", "/v1/patients/my/consent-authorities/"+id, "owner", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM audit_events WHERE tenant_id=? AND resource_type='patient_consent_authorities'`, testutil.TenantA).Scan(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}

func consentAuthorityGateway(t *testing.T, owner http.Handler) *gin.Engine {
	t.Helper()
	upstream := httptest.NewServer(owner)
	t.Cleanup(upstream.Close)
	cfg, err := globalapi.FromEnv(func(key string) string {
		switch key {
		case "HELIVANTA_ENV":
			return "dev"
		case "GLOBAL_API_UPSTREAMS":
			return fmt.Sprintf(`{"platform":%q}`, upstream.URL)
		default:
			return ""
		}
	})
	require.NoError(t, err)
	gateway, err := globalapi.New(cfg)
	require.NoError(t, err)
	forwarded := gin.New()
	forwarded.NoRoute(func(c *gin.Context) {
		c.Request.Header.Set("X-Tenant-ID", testutil.TenantB)
		c.Request.Header.Set("X-User-ID", "user-owner")
		gateway.ServeHTTP(c.Writer, c.Request)
	})
	return forwarded
}

func TestConsentAuthorityLostResponseReconcilesSameAcceptance(t *testing.T) {
	r, db, body := consentAuthorityFixture(t)
	var dropped atomic.Bool
	owner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/accept") {
			r.ServeHTTP(w, req)
			return
		}
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		if response.Code == 200 && dropped.CompareAndSwap(false, true) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("simulate lost response: %v", err)
				return
			}
			if err := conn.Close(); err != nil {
				t.Errorf("close lost-response connection: %v", err)
			}
			return
		}
		for key, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.Code)
		if _, err := io.Copy(w, response.Body); err != nil {
			t.Errorf("forward owner response: %v", err)
		}
	})
	forwarded := consentAuthorityGateway(t, owner)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, scopedDo(forwarded, "PUT", "/v1/patients/consent-authorities/"+id, "reviewer", body).Code)
	mine := "/v1/patients/my/consent-authorities/" + id
	acceptance := `{"expected_version":1,"accepted":true}`
	lost := scopedDo(forwarded, "POST", mine+"/accept", "owner", acceptance)
	require.Equal(t, 502, lost.Code, lost.Body.String())
	require.True(t, dropped.Load())
	committed := scopedDo(forwarded, "GET", mine, "owner", "")
	require.Equal(t, 200, committed.Code, committed.Body.String())
	require.Contains(t, committed.Body.String(), `"status":"accepted"`)
	require.Contains(t, committed.Body.String(), `"version":2`)
	recovered := scopedDo(forwarded, "POST", mine+"/accept", "owner", acceptance)
	require.Equal(t, 200, recovered.Code, recovered.Body.String())
	require.Contains(t, recovered.Body.String(), `"version":2`)
	require.Contains(t, recovered.Body.String(), `"status":"accepted"`)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM outbox_events WHERE tenant_id=? AND subject='helivanta.in.patientmaster.consent_authority_changed.v1'`, testutil.TenantA).Scan(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestConsentAuthorityCancelledDependencyReturnsRetryableUnavailable(t *testing.T) {
	r, _, body := consentAuthorityFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 201, scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+id, "reviewer", body).Code)
	mine := "/v1/patients/my/consent-authorities/" + id
	for _, request := range []struct{ method, path, body string }{{"GET", mine, ""}, {"POST", mine + "/accept", `{"expected_version":1,"accepted":true}`}} {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		req := httptest.NewRequest(request.method, request.path+"?hospital_id="+testHospital, strings.NewReader(request.body)).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer owner")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		require.Equal(t, 503, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"error":"consent_authority_unavailable"`)
		require.NotContains(t, response.Body.String(), "patient_id")
	}
	current := scopedDo(r, "GET", mine, "owner", "")
	require.Equal(t, 200, current.Code, current.Body.String())
	require.Contains(t, current.Body.String(), `"status":"proposed"`)
}
