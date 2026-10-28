package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // owner under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func proxyAuthorityFixture(t *testing.T, kind string) (*gin.Engine, *tenantdb.DB, map[string]any) {
	t.Helper()
	reviewPerm := authz.Permission("patient.proxy-authority.review")
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"reviewer": testutil.TenantA, "owner": testutil.TenantA, "peer": testutil.TenantA, "foreign": testutil.TenantB, "ungranted": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"reviewer": {patientmaster.PermPatientRead, patientmaster.PermPatientRegister, patientmaster.PermConsentAuthorityReview, reviewPerm}, "owner": {patientmaster.PermPatientRead}, "foreign": {reviewPerm}, "ungranted": {reviewPerm}}, Modules: []platform.Module{patientmaster.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-reviewer','registrar')`, testutil.TenantA, testHospital).Error
	}))
	dob, relationship := "2018-01-01", "child"
	if kind == "appointed_guardian" {
		dob, relationship = "1990-01-01", "ward"
	}
	var registration map[string]any
	require.NoError(t, json.Unmarshal([]byte(registerBody("SyntheticProxy", "Person", dob, "", false, "")), &registration))
	registration["consented_by"] = "guardian"
	registration["guardian_name"] = "Synthetic Guardian"
	registration["guardian_relationship"] = "parent"
	b, err := json.Marshal(registration)
	require.NoError(t, err)
	created := scopedDo(r, "POST", "/v1/patients", "reviewer", string(b))
	require.Equal(t, 201, created.Code, created.Body.String())
	claim := mediconnectcontract.RegistrationReviewData{RequestID: uuid.Must(uuid.NewV7()).String(), AccountID: uuid.Must(uuid.NewV7()).String(), RequesterSubject: "user-owner", HospitalID: testHospital, PatientID: registeredID(t, created), Relationship: relationship}
	encoded, err := json.Marshal(claim)
	require.NoError(t, err)
	consumer := patientmaster.New().Consumers(platform.Deps{})[0]
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
			return err
		}
		return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
	}))
	detail := scopedDo(r, "GET", "/v1/patients/link-reviews/"+claim.RequestID, "reviewer", "")
	require.Equal(t, 200, detail.Code, detail.Body.String())
	var review map[string]any
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &review))
	decision := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"account_id":%q,"outcome":"approved","reason":"identity_confirmed","evidence_reference":%q,"expected_registration_updated_at":%q}`, claim.AccountID, uuid.Must(uuid.NewV7()).String(), review["registration_updated_at"])
	decided := scopedDo(r, "PUT", "/v1/patients/link-reviews/"+claim.RequestID+"/decision/"+decision, "reviewer", body)
	require.Equal(t, 200, decided.Code, decided.Body.String())
	in := map[string]any{"kind": kind, "country_code": "IN", "review_request_id": claim.RequestID, "expected_decision_id": decision, "policy_reference": uuid.Must(uuid.NewV7()).String(), "identity_age_reference": uuid.Must(uuid.NewV7()).String(), "authority_evidence_reference": uuid.Must(uuid.NewV7()).String(), "identity_age_confirmed": true, "authority_confirmed": true, "valid_until": time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano)}
	if kind == "appointed_guardian" {
		in["appointment_reference"] = uuid.Must(uuid.NewV7()).String()
	}
	return r, db, in
}

func proxyJSON(t *testing.T, in map[string]any) string {
	t.Helper()
	b, err := json.Marshal(in)
	require.NoError(t, err)
	return string(b)
}

func TestProxyAuthorityKeepsGuardianReviewSeparateFromSelfAndDataAccess(t *testing.T) {
	r, _, in := proxyAuthorityFixture(t, "parental")
	id := uuid.Must(uuid.NewV7()).String()
	staff := "/v1/patients/proxy-authorities/" + id
	mine := "/v1/patients/my/proxy-authorities/" + id
	created := scopedDo(r, "PUT", staff, "reviewer", proxyJSON(t, in))
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"status":"proposed"`)
	require.Contains(t, created.Body.String(), `"kind":"parental"`)
	require.Contains(t, created.Body.String(), `"record_access_authorized":false`)
	require.Equal(t, "no-store", created.Header().Get("Cache-Control"))
	require.NotContains(t, created.Body.String(), "identity_age_reference")
	require.NotContains(t, created.Body.String(), "user-owner")
	require.JSONEq(t, created.Body.String(), scopedDo(r, "PUT", staff, "reviewer", proxyJSON(t, in)).Body.String())
	require.Equal(t, 404, scopedDo(r, "GET", "/v1/patients/my/consent-authorities/"+id, "owner", "").Code)
	accepted := scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), `"status":"accepted"`)
	require.Contains(t, accepted.Body.String(), `"version":2`)
	require.Contains(t, accepted.Body.String(), `"record_access_authorized":false`)
	require.JSONEq(t, accepted.Body.String(), scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`).Body.String())
	revoked := scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":2}`)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"status":"revoked"`)
	require.Contains(t, revoked.Body.String(), `"version":3`)
	require.JSONEq(t, revoked.Body.String(), scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":2}`).Body.String())
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`).Code)
	require.Contains(t, scopedDo(r, "PUT", staff, "reviewer", proxyJSON(t, in)).Body.String(), `"status":"revoked"`)
}

func proxyStaffTransaction(db *tenantdb.DB, t *testing.T, fn func(*gorm.DB) error) error {
	t.Helper()
	return db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func proxyID(t *testing.T, r *gin.Engine, body map[string]any) (string, string) {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	w := scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+id, "reviewer", proxyJSON(t, body))
	require.Equal(t, 201, w.Code, w.Body.String())
	var d map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &d))
	return id, d["patient_id"].(string)
}
func TestProxyAuthorityAppointmentRequiresExplicitIndependentEvidence(t *testing.T) {
	r, _, in := proxyAuthorityFixture(t, "appointed_guardian")
	id, _ := proxyID(t, r, in)
	mine := "/v1/patients/my/proxy-authorities/" + id
	accepted := scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), `"kind":"appointed_guardian"`)
	require.NotContains(t, accepted.Body.String(), "appointment_reference")
	revoked := scopedDo(r, "POST", "/v1/patients/proxy-authorities/"+id+"/revoke", "reviewer", `{"expected_version":2}`)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"status":"revoked"`)
	require.Contains(t, scopedDo(r, "GET", mine, "owner", "").Body.String(), `"record_access_authorized":false`)
}
func TestProxyAuthorityRefusesUnknownClassesAndIncompleteOrConflictingEvidence(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	for _, change := range []map[string]any{
		{"kind": "adult_delegation"}, {"kind": "self"}, {"kind": "appointed_guardian"}, {"country_code": "US"}, {"country_code": ""},
		{"identity_age_confirmed": false}, {"identity_age_confirmed": nil}, {"authority_confirmed": false}, {"authority_confirmed": nil},
		{"identity_age_reference": uuid.NewString()}, {"authority_evidence_reference": ""}, {"policy_reference": nil}, {"appointment_reference": uuid.Must(uuid.NewV7()).String()},
		{"subject": "user-peer"}, {"valid_until": "2030-01-01T00:00:00.000000001Z"},
	} {
		t.Run(fmt.Sprint(change), func(t *testing.T) {
			body := maps.Clone(in)
			maps.Copy(body, change)
			w := scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", proxyJSON(t, body))
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	for _, bad := range []string{`null`, `{} {}`, strings.Repeat(" ", 9000) + `{}`} {
		require.Equal(t, 400, scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", bad).Code)
	}
	body := maps.Clone(in)
	body["expected_decision_id"] = uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", proxyJSON(t, body)).Code)
	body = maps.Clone(in)
	body["valid_until"] = "2020-01-01T00:00:00Z"
	require.Equal(t, 409, scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", proxyJSON(t, body)).Code)
	var n int64
	require.NoError(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error { return tx.Table("patient_proxy_authorities").Count(&n).Error }))
	require.Zero(t, n)
	id, _ := proxyID(t, r, in)
	changed := maps.Clone(in)
	changed["policy_reference"] = uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 409, scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+id, "reviewer", proxyJSON(t, changed)).Code)
}
func TestProxyAuthorityCannotElevatePatientAccessOrChangeSelfAuthoritySemantics(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	r = consentAuthorityGateway(t, r)
	id, patient := proxyID(t, r, in)
	mine := "/v1/patients/my/proxy-authorities/" + id
	staff := "/v1/patients/proxy-authorities/" + id
	self := map[string]any{"review_request_id": in["review_request_id"], "expected_decision_id": in["expected_decision_id"], "capacity_confirmed": true, "policy_reference": in["policy_reference"], "evidence_reference": in["authority_evidence_reference"], "valid_until": in["valid_until"]}
	require.Equal(t, 409, scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", proxyJSON(t, self)).Code)
	for _, actor := range []string{"peer", "foreign", "reviewer", "ungranted"} {
		require.Equal(t, 404, scopedDo(r, "GET", mine, actor, "").Code, actor)
		require.Equal(t, 404, scopedDo(r, "POST", mine+"/accept", actor, `{"expected_version":1,"accepted":true}`).Code, actor)
		require.Equal(t, 404, scopedDo(r, "POST", mine+"/revoke", actor, `{"expected_version":1}`).Code, actor)
	}
	require.Equal(t, 403, scopedDo(r, "GET", staff, "owner", "").Code)
	require.Equal(t, 404, scopedDo(r, "GET", staff, "ungranted", "").Code)
	require.Equal(t, 404, scopedDo(r, "GET", staff, "foreign", "").Code)
	require.Equal(t, 401, scopedDo(r, "GET", mine, "unknown", "").Code)
	require.Equal(t, 404, do(r, "GET", mine+"?hospital_id=22222222-2222-4222-8222-222222222222", "owner", "").Code)
	for _, body := range []string{`{}`, `{"expected_version":1,"accepted":false}`, `{"expected_version":1,"accepted":null}`, `{"expected_version":0,"accepted":true}`} {
		require.Equal(t, 400, scopedDo(r, "POST", mine+"/accept", "owner", body).Code)
	}
	accepted := scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	require.Equal(t, 404, scopedDo(r, "GET", "/v1/patients/"+patient, "owner", "").Code)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Table("patients").Where("id=?", patient).Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n)
		return nil
	}))
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":1}`).Code)
}
func TestProxyAuthorityRejectsRegistrationChangesBeforeAcceptance(t *testing.T) {
	for _, mutation := range []string{"version", "birthdate", "estimated"} {
		t.Run(mutation, func(t *testing.T) {
			r, db, in := proxyAuthorityFixture(t, "parental")
			id, patient := proxyID(t, r, in)
			require.NoError(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error {
				switch mutation {
				case "version":
					return tx.Exec(`UPDATE patients SET updated_at=clock_timestamp() WHERE id=?`, patient).Error
				case "birthdate":
					return tx.Exec(`UPDATE patients SET dob='1990-01-01' WHERE id=?`, patient).Error
				default:
					return tx.Exec(`UPDATE patients SET dob_estimated=true WHERE id=?`, patient).Error
				}
			}))
			w := scopedDo(r, "POST", "/v1/patients/my/proxy-authorities/"+id+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, scopedDo(r, "GET", "/v1/patients/my/proxy-authorities/"+id, "owner", "").Body.String(), `"status":"proposed"`)
		})
	}
}
func TestProxyAuthorityConcurrentRetriesCreateOneRevisionPerTransition(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/proxy-authorities/" + id
	proposalBody := proxyJSON(t, in)
	for _, stage := range []string{"proposal", "acceptance"} {
		responses := make(chan *httptest.ResponseRecorder, 6)
		var workers sync.WaitGroup
		for range 6 {
			workers.Go(func() {
				if stage == "proposal" {
					responses <- scopedDo(r, "PUT", path, "reviewer", proposalBody)
				} else {
					responses <- scopedDo(r, "POST", "/v1/patients/my/proxy-authorities/"+id+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
				}
			})
		}
		workers.Wait()
		close(responses)
		created := 0
		var first string
		for w := range responses {
			require.Contains(t, []int{200, 201}, w.Code, w.Body.String())
			if w.Code == 201 {
				created++
			}
			if first == "" {
				first = w.Body.String()
			}
			require.JSONEq(t, first, w.Body.String())
		}
		if stage == "proposal" {
			require.Equal(t, 1, created)
		} else {
			require.Zero(t, created)
		}
	}
	require.NoError(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.patientmaster.proxy_authority_changed.v1").Count(&n).Error; err != nil {
			return err
		}
		require.Equal(t, int64(2), n)
		return nil
	}))
}

func TestProxyAuthorityParentalBoundaryAndEstimatedDOBFailClosed(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	after := maps.Clone(in)
	after["valid_until"] = "2035-12-31T18:30:00.000001Z"
	denied := scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", proxyJSON(t, after))
	require.Equal(t, 409, denied.Code, denied.Body.String())
	exact := maps.Clone(in)
	exact["valid_until"] = "2035-12-31T18:30:00Z"
	id, patient := proxyID(t, r, exact)
	require.NotContains(t, scopedDo(r, "GET", "/v1/patients/my/proxy-authorities/"+id, "owner", "").Body.String(), "parental_end_at")
	require.NoError(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE patients SET dob_estimated=true WHERE id=?`, patient).Error
	}))
	w := scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+uuid.Must(uuid.NewV7()).String(), "reviewer", proxyJSON(t, in))
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestProxyAuthorityListUsesOwnerKeysetHistoryAndRevokesUnacceptedOffers(t *testing.T) {
	r, _, in := proxyAuthorityFixture(t, "parental")
	ids := make(map[string]bool)
	for range 3 {
		id, _ := proxyID(t, r, in)
		ids[id] = true
	}
	list := scopedDo(r, "GET", "/v1/patients/my/proxy-authorities?limit=1", "owner", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	var page struct {
		Data []struct {
			ID string `json:"id"`
		}
		Page struct {
			Next *string `json:"next_cursor"`
			More bool    `json:"has_more"`
		}
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.True(t, page.Page.More)
	require.NotNil(t, page.Page.Next)
	first := page.Data[0].ID
	require.True(t, ids[first])
	next := scopedDo(r, "GET", "/v1/patients/my/proxy-authorities?limit=1&cursor="+*page.Page.Next, "owner", "")
	require.Equal(t, 200, next.Code, next.Body.String())
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.NotEqual(t, first, page.Data[0].ID)
	peer := scopedDo(r, "GET", "/v1/patients/my/proxy-authorities", "peer", "")
	require.Equal(t, 200, peer.Code, peer.Body.String())
	require.JSONEq(t, `{"data":[],"page":{"has_more":false,"next_cursor":null}}`, peer.Body.String())
	mine := "/v1/patients/my/proxy-authorities/" + first
	revoked := scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	require.Contains(t, revoked.Body.String(), `"version":2`)
	require.JSONEq(t, revoked.Body.String(), scopedDo(r, "POST", mine+"/revoke", "owner", `{"expected_version":1}`).Body.String())
	require.Equal(t, 409, scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`).Code)
	require.Equal(t, 400, scopedDo(r, "GET", "/v1/patients/my/proxy-authorities?limit=201", "owner", "").Code)
}

func TestProxyAuthorityHistoryCannotBeRewrittenAndSubjectCannotMutateSource(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	id, _ := proxyID(t, r, in)
	for _, query := range []string{
		`UPDATE patient_proxy_authorities SET subject='user-peer',version=version+1 WHERE id=?`,
		`UPDATE patient_proxy_authorities SET policy_reference='0199e000-0000-7000-8000-000000000099',version=version+1 WHERE id=?`,
		`UPDATE patient_proxy_authorities SET valid_until=valid_until+interval '1 hour',version=version+1 WHERE id=?`,
		`DELETE FROM patient_proxy_authorities WHERE id=?`,
	} {
		require.Error(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error { return tx.Exec(query, id).Error }), query)
	}
	require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE patient_link_reviews SET status='withdrawn' WHERE request_id=?`, in["review_request_id"]).Error
	}))
	require.Contains(t, scopedDo(r, "GET", "/v1/patients/my/proxy-authorities/"+id, "owner", "").Body.String(), `"status":"proposed"`)
}

func TestProxyAuthorityAuditFailurePreventsDisclosureAndRollsBackAcceptance(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	id, _ := proxyID(t, r, in)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_proxy_audit_failure", SQL: `CREATE FUNCTION reject_proxy_reads() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='patient_proxy_authorities' AND NEW.action='read' THEN RAISE EXCEPTION 'synthetic proxy audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_proxy_reads BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_proxy_reads();`}}))
	mine := "/v1/patients/my/proxy-authorities/" + id
	read := scopedDo(r, "GET", mine, "owner", "")
	require.Equal(t, 500, read.Code, read.Body.String())
	require.NotContains(t, read.Body.String(), id)
	changed := scopedDo(r, "POST", mine+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
	require.Equal(t, 500, changed.Code, changed.Body.String())
	require.NotContains(t, changed.Body.String(), "accepted_at")
	require.NoError(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error {
		var row struct {
			Status  string
			Version int64
		}
		if err := tx.Table("patient_proxy_authorities").Select("status,version").Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		require.Equal(t, "proposed", row.Status)
		require.EqualValues(t, 1, row.Version)
		var n int64
		if err := tx.Table("outbox_events").Where("subject=?", "helivanta.in.patientmaster.proxy_authority_changed.v1").Count(&n).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, n)
		return nil
	}))
}

func TestProxyAuthorityOutboxFailureRollsBackReviewAndAudit(t *testing.T) {
	r, db, in := proxyAuthorityFixture(t, "parental")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_proxy_outbox_failure", SQL: `CREATE FUNCTION reject_proxy_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.patientmaster.proxy_authority_changed.v1' THEN RAISE EXCEPTION 'synthetic proxy outbox outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_proxy_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_proxy_outbox();`}}))
	id := uuid.Must(uuid.NewV7()).String()
	w := scopedDo(r, "PUT", "/v1/patients/proxy-authorities/"+id, "reviewer", proxyJSON(t, in))
	require.Equal(t, 500, w.Code, w.Body.String())
	require.NoError(t, proxyStaffTransaction(db, t, func(tx *gorm.DB) error {
		for _, table := range []string{"patient_proxy_authorities", "audit_events"} {
			var n int64
			q := tx.Table(table)
			if table == "audit_events" {
				q = q.Where("resource_type=?", "patient_proxy_authorities")
			}
			if err := q.Count(&n).Error; err != nil {
				return err
			}
			require.Zero(t, n, table)
		}
		return nil
	}))
}
