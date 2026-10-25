package patientmaster_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/mediconnectclaims"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // owner under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
	"gorm.io/gorm"
	authentication "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type clinicalFixture struct {
	router                              *gin.Engine
	crypto                              *transit.Client
	db                                  *tenantdb.DB
	body, requestID                     string
	corruptSigner                       *atomic.Bool
	signerUnavailable, claimUnavailable *atomic.Bool
}

func newClinicalFixture(t *testing.T) clinicalFixture {
	t.Helper()
	addr, root := testinfra.StartOpenBao(t)
	policy, err := json.Marshal(map[string]string{"policy": testinfra.ClinicalConsentTransitPolicy})
	require.NoError(t, err)
	require.NoError(t, testinfra.OpenBaoWrite(addr, root, http.MethodPut, "/v1/sys/policies/acl/clinical-consent-fixture", string(policy)))
	user, password := testinfra.OpenBaoUser(t, time.Hour, "clinical-consent-fixture")
	var corruptSigner, signerUnavailable, claimUnavailable atomic.Bool
	baoURL, err := url.Parse(addr)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(baoURL)
	proxy.ModifyResponse = func(response *http.Response) error {
		if corruptSigner.Load() && strings.Contains(response.Request.URL.Path, "/hmac/") && response.StatusCode == 200 {
			if err := response.Body.Close(); err != nil {
				return err
			}
			payload := `{"data":{"hmac":"vault:v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}}`
			response.Body = io.NopCloser(strings.NewReader(payload))
			response.ContentLength = int64(len(payload))
			response.Header.Del("Content-Length")
		}
		return nil
	}
	signerProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if signerUnavailable.Load() {
			w.WriteHeader(503)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(signerProxy.Close)
	crypto, err := transit.New(signerProxy.URL, "transit", transit.UserpassLogin(user, password), nil)
	require.NoError(t, err)
	kube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var review authentication.TokenReview
		if json.NewDecoder(r.Body).Decode(&review) != nil {
			w.WriteHeader(400)
			return
		}
		allowed := review.Spec.Token == "projected-clinical-owner" && len(review.Spec.Audiences) == 1 && review.Spec.Audiences[0] == mediconnectcontract.ClaimWorkloadAudience
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authentication.TokenReview{TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "TokenReview"}, Status: authentication.TokenReviewStatus{Authenticated: allowed, Audiences: []string{mediconnectcontract.ClaimWorkloadAudience}, User: authentication.UserInfo{Username: "system:serviceaccount:helivanta:patientmaster-api", UID: "synthetic-source-account"}}})
	}))
	t.Cleanup(kube.Close)
	workload, err := workloadidentity.New(&rest.Config{Host: kube.URL}, mediconnectcontract.ClaimWorkloadAudience, "system:serviceaccount:helivanta:patientmaster-api")
	require.NoError(t, err)
	var owner atomic.Pointer[gin.Engine]
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claimUnavailable.Load() {
			w.WriteHeader(503)
			return
		}
		if handler := owner.Load(); handler != nil {
			handler.ServeHTTP(w, r)
		} else {
			w.WriteHeader(503)
		}
	}))
	t.Cleanup(source.Close)
	token := filepath.Join(t.TempDir(), "projected-token")
	require.NoError(t, os.WriteFile(token, []byte("projected-clinical-owner"), 0600))
	claims, err := mediconnectclaims.New(source.URL, token, "", source.Client())
	require.NoError(t, err)
	reviewer := []authz.Permission{patientmaster.PermPatientRead, patientmaster.PermPatientRegister, patientmaster.PermConsentAuthorityReview, patientmaster.PermSharingNoticeReview, patientmaster.PermSharingRecipientReview}
	var claimModule platform.Module
	for _, candidate := range bootstrap.Modules(nil, nil) {
		if candidate.Name() == "mediconnect" {
			claimModule = candidate
		}
	}
	require.NotNil(t, claimModule)
	r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA, "approver": testutil.TenantA, "recipient": testutil.TenantA, "peer": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"reviewer": reviewer, "approver": {patientmaster.PermSharingNoticeReview, patientmaster.PermSharingRecipientReview}}, Modules: []platform.Module{patientmaster.New(), claimModule}, Transit: crypto, CurrentClaims: claims})
	internal := gin.New()
	internal.POST(mediconnectcontract.CurrentClaimPath, testutil.MediConnectClaimHandler(db, workload))
	owner.Store(internal)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-reviewer','registrar'),(?,?,'user-approver','registrar')`, testutil.TenantA, testHospital, testutil.TenantA, testHospital).Error
	}))
	patient := scopedDo(r, "POST", "/v1/patients", "reviewer", registerBody("SyntheticClinical", "Subject", "1990-01-01", "", false, ""))
	require.Equal(t, 201, patient.Code, patient.Body.String())
	patientID := registeredID(t, patient)
	account := testutil.Do(r, "POST", "/v1/mediconnect/account", "owner", "{}")
	require.Equal(t, 201, account.Code, account.Body.String())
	requestID := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + requestID
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "owner", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, testHospital, patientID)).Code)
	require.Equal(t, 202, testutil.Do(r, "POST", path+"/review", "owner", `{"expected_version":1}`).Code)
	consumers := append(patientmaster.New().Consumers(platform.Deps{}), claimModule.Consumers(platform.Deps{})...)
	background, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	require.NoError(t, bus.StartConsumers(background, db, consumers))
	go bus.RunDispatcher(background, db)
	reviewPath := "/v1/patients/link-reviews/" + requestID
	require.Eventually(t, func() bool { return scopedDo(r, "GET", reviewPath, "reviewer", "").Code == 200 }, 10*time.Second, 25*time.Millisecond)
	var review map[string]any
	require.NoError(t, json.Unmarshal(scopedDo(r, "GET", reviewPath, "reviewer", "").Body.Bytes(), &review))
	decisionID := uuid.Must(uuid.NewV7()).String()
	decided := scopedDo(r, "PUT", reviewPath+"/decision/"+decisionID, "reviewer", fmt.Sprintf(`{"account_id":%q,"outcome":"approved","reason":"identity_confirmed","evidence_reference":%q,"expected_registration_updated_at":%q}`, review["account_id"], uuid.Must(uuid.NewV7()).String(), review["registration_updated_at"]))
	require.Equal(t, 200, decided.Code, decided.Body.String())
	authorityID := uuid.Must(uuid.NewV7()).String()
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	authority := fmt.Sprintf(`{"review_request_id":%q,"expected_decision_id":%q,"capacity_confirmed":true,"policy_reference":%q,"evidence_reference":%q,"valid_until":%q}`, requestID, decisionID, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), until.Format(time.RFC3339Nano))
	proposedAuthority := scopedDo(r, "PUT", "/v1/patients/consent-authorities/"+authorityID, "reviewer", authority)
	require.Equal(t, 201, proposedAuthority.Code, proposedAuthority.Body.String())
	accepted := scopedDo(r, "POST", "/v1/patients/my/consent-authorities/"+authorityID+"/accept", "owner", `{"expected_version":1,"accepted":true}`)
	require.Equal(t, 200, accepted.Code, accepted.Body.String())
	noticeID := uuid.Must(uuid.NewV7()).String()
	noticePath := "/v1/patients/sharing-notices/" + noticeID
	notice := fmt.Sprintf(`{"purpose":"care_coordination","locale":"en-IN","version_label":"synthetic-1","title":"Synthetic sharing notice","text":"Synthetic test wording; not approved real policy.","withdrawal_consequence":"Synthetic test consequence.","policy_reference":%q}`, uuid.Must(uuid.NewV7()).String())
	require.Equal(t, 201, scopedDo(r, "PUT", noticePath, "reviewer", notice).Code)
	approved := scopedDo(r, "POST", noticePath+"/approve", "approver", fmt.Sprintf(`{"expected_version":1,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, 200, approved.Code, approved.Body.String())
	var approvedNotice map[string]any
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &approvedNotice))
	recipientID := uuid.Must(uuid.NewV7()).String()
	recipientPath := "/v1/patients/sharing-recipients/" + recipientID
	recipient := fmt.Sprintf(`{"organization_reference":"synthetic-facility-ref","display_name":"Synthetic Facility","grantee_subject":"user-recipient","policy_reference":%q,"identity_evidence_reference":%q,"valid_until":%q}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), until.Format(time.RFC3339Nano))
	require.Equal(t, 201, scopedDo(r, "PUT", recipientPath, "reviewer", recipient).Code)
	approvedRecipient := scopedDo(r, "POST", recipientPath+"/approve", "approver", fmt.Sprintf(`{"expected_version":1,"identity_confirmed":true,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, 200, approvedRecipient.Code, approvedRecipient.Body.String())
	grant := fmt.Sprintf(`{"authority_id":%q,"expected_authority_version":2,"notice_id":%q,"expected_notice_version":2,"notice_hash":%q,"recipient_id":%q,"expected_recipient_version":2,"purpose":"care_coordination","resource_types":["diagnostic_report"],"valid_until":%q,"affirmed":true}`, authorityID, noticeID, approvedNotice["content_hash"], recipientID, time.Now().UTC().Add(10*time.Minute).Truncate(time.Microsecond).Format(time.RFC3339Nano))
	return clinicalFixture{router: consentAuthorityGateway(t, r), crypto: crypto, db: db, body: grant, requestID: requestID, corruptSigner: &corruptSigner, signerUnavailable: &signerUnavailable, claimUnavailable: &claimUnavailable}
}

func TestClinicalConsentGrantIsExplicitSignedAndReconcilesWithoutReactivation(t *testing.T) {
	f := newClinicalFixture(t)
	path := "/v1/patients/my/clinical-consents/" + uuid.Must(uuid.NewV7()).String()
	created := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"status":"granted"`)
	require.Contains(t, created.Body.String(), `"hmac":"vault:v1:`)
	require.NotContains(t, created.Body.String(), "user-recipient")
	require.NotContains(t, created.Body.String(), "user-owner")
	replay := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	read := scopedDo(f.router, "GET", path, "owner", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.JSONEq(t, created.Body.String(), read.Body.String())
	require.Equal(t, 404, scopedDo(f.router, "GET", path, "peer", "").Code)
	require.Equal(t, 404, scopedDo(f.router, "POST", path+"/withdraw", "recipient", `{"expected_version":1}`).Code)
	decisionPath := "/v1/patients/clinical-consents/" + strings.TrimPrefix(path, "/v1/patients/my/clinical-consents/") + "/decide"
	decisionBody := `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`
	allowed := scopedDo(f.router, "POST", decisionPath, "recipient", decisionBody)
	require.Equal(t, 200, allowed.Code, allowed.Body.String())
	require.Contains(t, allowed.Body.String(), `"consent_gate":"allow"`)
	require.Contains(t, allowed.Body.String(), `"record_access_authorized":false`)
	wrongKind := scopedDo(f.router, "POST", decisionPath, "recipient", `{"purpose":"care_coordination","resource_type":"prescription"}`)
	require.Equal(t, 200, wrongKind.Code, wrongKind.Body.String())
	require.Contains(t, wrongKind.Body.String(), `"consent_gate":"deny"`)
	require.Equal(t, 404, scopedDo(f.router, "POST", decisionPath, "peer", decisionBody).Code)
	withdrawn := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, withdrawn.Body.String(), `"withdrawal_recorded":true`)
	require.Contains(t, withdrawn.Body.String(), `"status":"withdrawn"`)
	denied := scopedDo(f.router, "POST", decisionPath, "recipient", decisionBody)
	require.Equal(t, 200, denied.Code, denied.Body.String())
	require.Contains(t, denied.Body.String(), `"consent_gate":"deny"`)
	require.NotContains(t, denied.Body.String(), `"patient_id"`)
	replay = scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, withdrawn.Body.String(), replay.Body.String())
	replay = scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), `"status":"withdrawn"`)
	require.Contains(t, replay.Body.String(), `"version":2`)

}

func TestClinicalConsentDecisionDeniesAfterOriginalClaimWithdrawal(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + id
	created := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	decisionPath := "/v1/patients/clinical-consents/" + id + "/decide"
	body := `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`
	withdrawn := testutil.Do(f.router, "POST", "/v1/mediconnect/account/link-requests/"+f.requestID+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	decision := scopedDo(f.router, "POST", decisionPath, "recipient", body)
	require.Equal(t, 200, decision.Code, decision.Body.String())
	require.Contains(t, decision.Body.String(), `"consent_gate":"deny"`)
	require.NotContains(t, decision.Body.String(), `"patient_id"`)
	require.Equal(t, 404, scopedDo(f.router, "POST", decisionPath, "foreign", body).Code)
	// Consent withdrawal remains possible after the independent identity claim ends.
	consentWithdrawal := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, consentWithdrawal.Code, consentWithdrawal.Body.String())
	require.Contains(t, consentWithdrawal.Body.String(), `"withdrawal_recorded":true`)
}

func TestClinicalConsentTamperIsQuarantinedAndWithdrawalPreservesIncident(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + id
	f.corruptSigner.Store(true)
	created := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())

	f.corruptSigner.Store(false)
	read := scopedDo(f.router, "GET", path, "owner", "")
	require.Equal(t, 409, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), "clinical_consent_integrity_failed")
	withdrawn := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, withdrawn.Body.String(), `"status":"quarantined"`)
	require.Contains(t, withdrawn.Body.String(), `"version":3`)
	require.Contains(t, withdrawn.Body.String(), `"withdrawal_recorded":true`)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table("patient_clinical_integrity_incidents").Where("tenant_id=? AND consent_id=?", testutil.TenantA, id).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestClinicalConsentDecisionRechecksLocalOwnerState(t *testing.T) {
	for _, change := range []string{"authority", "notice", "recipient"} {
		t.Run(change, func(t *testing.T) {
			f := newClinicalFixture(t)
			id := uuid.Must(uuid.NewV7()).String()
			created := scopedDo(f.router, "PUT", "/v1/patients/my/clinical-consents/"+id, "owner", f.body)
			require.Equal(t, 201, created.Code, created.Body.String())
			var input map[string]any
			require.NoError(t, json.Unmarshal([]byte(f.body), &input))
			var path, actor, body string
			switch change {
			case "authority":
				path = "/v1/patients/my/consent-authorities/" + input["authority_id"].(string) + "/revoke"
				actor = "owner"
				body = `{"expected_version":2}`
			case "notice":
				path = "/v1/patients/sharing-notices/" + input["notice_id"].(string) + "/retire"
				actor = "approver"
				body = `{"expected_version":2}`
			case "recipient":
				path = "/v1/patients/sharing-recipients/" + input["recipient_id"].(string) + "/revoke"
				actor = "approver"
				body = fmt.Sprintf(`{"expected_version":2,"reason_reference":%q}`, uuid.Must(uuid.NewV7()).String())
			}
			changed := scopedDo(f.router, "POST", path, actor, body)
			require.Equal(t, 200, changed.Code, changed.Body.String())
			checked := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
			require.Equal(t, 200, checked.Code, checked.Body.String())
			require.Contains(t, checked.Body.String(), `"consent_gate":"deny"`)
		})
	}
}

func TestClinicalConsentWithdrawalSurvivesDependencyOutage(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + id
	created := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	decisionPath := "/v1/patients/clinical-consents/" + id + "/decide"
	body := `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`
	for _, dependency := range []*atomic.Bool{f.signerUnavailable, f.claimUnavailable} {
		dependency.Store(true)
		unavailable := scopedDo(f.router, "POST", decisionPath, "recipient", body)
		require.Equal(t, 503, unavailable.Code, unavailable.Body.String())
		require.NotContains(t, unavailable.Body.String(), `"consent_gate":"allow"`)
		dependency.Store(false)
	}
	f.signerUnavailable.Store(true)
	f.claimUnavailable.Store(true)
	withdrawal := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawal.Code, withdrawal.Body.String())
	require.Contains(t, withdrawal.Body.String(), `"withdrawal_recorded":true`)
	denied := scopedDo(f.router, "POST", decisionPath, "recipient", body)
	require.Equal(t, 200, denied.Code, denied.Body.String())
	require.Contains(t, denied.Body.String(), `"consent_gate":"deny"`)
}

func TestClinicalConsentCaptureCatalogsAreScopedAndMinimal(t *testing.T) {
	f := newClinicalFixture(t)
	var in map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &in))
	base := "/v1/patients/my/consent-authorities/" + in["authority_id"].(string)
	for _, catalog := range []string{"sharing-notices", "sharing-recipients"} {
		response := scopedDo(f.router, "GET", base+"/"+catalog+"?limit=1", "owner", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"data":[{`)
		require.NotContains(t, response.Body.String(), "user-recipient")
		require.NotContains(t, response.Body.String(), "identity_evidence_reference")
		require.Equal(t, 404, scopedDo(f.router, "GET", base+"/"+catalog, "peer", "").Code)
		require.Equal(t, 404, scopedDo(f.router, "GET", base+"/"+catalog, "foreign", "").Code)
	}
}

func TestClinicalConsentOutboxFailureCannotCommitGrantOrWithdrawal(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + id
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_clinical_outbox_failure", SQL: `CREATE FUNCTION reject_clinical_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.patientmaster.clinical_consent_changed.v1' THEN RAISE EXCEPTION 'synthetic outbox outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_clinical_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_clinical_outbox();`}}))
	failed := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 503, failed.Code, failed.Body.String())
	require.Equal(t, 404, scopedDo(f.router, "GET", path, "owner", "").Code)
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_clinical_outbox_recovery", SQL: `DROP TRIGGER reject_clinical_outbox ON outbox_events;`}}))
	recovered := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, recovered.Code, recovered.Body.String())
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_clinical_withdraw_failure", SQL: `CREATE TRIGGER reject_clinical_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_clinical_outbox();`}}))
	failed = scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 503, failed.Code, failed.Body.String())
	retained := scopedDo(f.router, "GET", path, "owner", "")
	require.Equal(t, 200, retained.Code, retained.Body.String())
	require.Contains(t, retained.Body.String(), `"status":"granted"`)
	require.Contains(t, retained.Body.String(), `"version":1`)
}

func TestClinicalConsentDecisionCannotAllowWithoutDurableAudit(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	granted := scopedDo(f.router, "PUT", "/v1/patients/my/clinical-consents/"+id, "owner", f.body)
	require.Equal(t, 201, granted.Code, granted.Body.String())
	require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_clinical_decision_failure", SQL: `CREATE FUNCTION reject_clinical_decision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit outage'; END $$; CREATE TRIGGER reject_clinical_decision BEFORE INSERT ON patient_clinical_consent_decisions FOR EACH ROW EXECUTE FUNCTION reject_clinical_decision();`}}))
	decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
	require.Equal(t, 503, decision.Code, decision.Body.String())
	require.NotContains(t, decision.Body.String(), `"consent_gate":"allow"`)
}

func TestClinicalConsentOwnerCanDiscoverRetainedDeclarations(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	created := scopedDo(f.router, "PUT", "/v1/patients/my/clinical-consents/"+id, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	listed := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents?limit=1", "owner", "")
	require.Equal(t, 200, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), id)
	require.NotContains(t, listed.Body.String(), "user-owner")
	require.NotContains(t, listed.Body.String(), "user-recipient")
	require.NotContains(t, listed.Body.String(), "hmac")
	require.NotContains(t, listed.Body.String(), "patient_id")
	for _, token := range []string{"peer", "foreign"} {
		response := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents", token, "")
		require.Equal(t, 200, response.Code, response.Body.String())
		require.Contains(t, response.Body.String(), `"data":[]`)
	}
}

func TestClinicalConsentConcurrentRetriesCommitOneGrantAndWithdrawal(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + id
	run := func(method, path, body string, expected ...int) {
		start := make(chan struct{})
		responses := make(chan *httptest.ResponseRecorder, 4)
		for range 4 {
			go func() { <-start; responses <- scopedDo(f.router, method, path, "owner", body) }()
		}
		close(start)
		for range 4 {
			response := <-responses
			require.Contains(t, expected, response.Code, response.Body.String())
		}
	}
	run("PUT", path, f.body, 200, 201)
	run("POST", path+"/withdraw", `{"expected_version":1}`, 200)
	read := scopedDo(f.router, "GET", path, "owner", "")
	require.Equal(t, 200, read.Code, read.Body.String())
	require.Contains(t, read.Body.String(), `"status":"withdrawn"`)
	require.Contains(t, read.Body.String(), `"version":2`)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("outbox_events").Where("tenant_id=? AND subject='helivanta.in.patientmaster.clinical_consent_changed.v1' AND payload->'data'->>'consent_id'=?", testutil.TenantA, id).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count, "one grant and one withdrawal event despite duplicate submissions")
		return nil
	}))
}

func TestClinicalConsentExpiredDeclarationDeniesDuringDependencyOutage(t *testing.T) {
	f := newClinicalFixture(t)
	original := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + original
	created := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	withdrawn := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	var stored struct{ Body string }
	scope := func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error
	}
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := scope(tx); err != nil {
			return err
		}
		return tx.Table("patient_clinical_consents").Select("body").Where("tenant_id=? AND id=?", testutil.TenantA, original).Take(&stored).Error
	}))
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(stored.Body), &body))
	id := uuid.Must(uuid.NewV7()).String()
	captured := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	expired := captured.Add(time.Minute)
	body["id"] = id
	body["captured_at"] = captured
	body["valid_until"] = expired
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	signature, err := f.crypto.HMAC(t.Context(), "helivanta-consent-"+testutil.TenantA, payload)
	require.NoError(t, err)
	// Insert a synthetic historical declaration through the owning application role;
	// forced RLS, immutable-history guards and audit triggers remain enabled.
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := scope(tx); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO patient_clinical_consents(tenant_id,hospital_id,id,patient_id,subject,authority_id,review_request_id,notice_id,recipient_id,purpose,fingerprint,body,hmac,valid_until,captured_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, testutil.TenantA, testHospital, id, body["patient_id"], body["subject"], body["authority_id"], body["review_request_id"], body["notice_id"], body["recipient_id"], body["purpose"], body["fingerprint"], string(payload), signature, expired, captured, captured, captured).Error
	}))
	f.signerUnavailable.Store(true)
	f.claimUnavailable.Store(true)
	decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
	require.Equal(t, 200, decision.Code, decision.Body.String())
	require.Contains(t, decision.Body.String(), `"consent_gate":"deny"`)
}

func TestClinicalConsentRejectsStaleOrUnacknowledgedScopeWithoutCreatingGrant(t *testing.T) {
	f := newClinicalFixture(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + id
	for _, invalid := range []string{
		strings.Replace(f.body, `"affirmed":true`, `"affirmed":false`, 1),
		strings.Replace(f.body, `"expected_authority_version":2`, `"expected_authority_version":1`, 1),
		strings.Replace(f.body, `"resource_types":["diagnostic_report"]`, `"resource_types":["diagnostic_report","diagnostic_report"]`, 1),
		strings.Replace(f.body, `"resource_types":["diagnostic_report"]`, `"resource_types":["prescription","diagnostic_report"]`, 1),
		strings.TrimSuffix(f.body, "}") + `,"patient_id":"untrusted"}`,
	} {
		denied := scopedDo(f.router, "PUT", path, "owner", invalid)
		require.Equal(t, 400, denied.Code, denied.Body.String())
	}
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &input))
	input["notice_hash"] = strings.Repeat("0", 64)
	wrongNotice, err := json.Marshal(input)
	require.NoError(t, err)
	mismatch := scopedDo(f.router, "PUT", path, "owner", string(wrongNotice))
	require.Equal(t, 409, mismatch.Code, mismatch.Body.String())
	require.Equal(t, 404, scopedDo(f.router, "GET", path, "owner", "").Code)
	require.Equal(t, 404, scopedDo(f.router, "PUT", path, "foreign", f.body).Code)
	wrongHospital := do(f.router, "PUT", path+"?hospital_id=22222222-2222-4222-8222-222222222222", "owner", f.body)
	require.Equal(t, 404, wrongHospital.Code, wrongHospital.Body.String())
	accepted := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, accepted.Code, accepted.Body.String())
	// A recorded command may not be repurposed, even while the grant remains active.
	changed := scopedDo(f.router, "PUT", path, "owner", strings.Replace(f.body, `"resource_types":["diagnostic_report"]`, `"resource_types":["prescription"]`, 1))
	require.Equal(t, 409, changed.Code, changed.Body.String())
}

func TestClinicalConsentHistoryCursorRetainsWithdrawnDeclarations(t *testing.T) {
	f := newClinicalFixture(t)
	first := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/clinical-consents/" + first
	created := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	withdrawn := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	second := uuid.Must(uuid.NewV7()).String()
	created = scopedDo(f.router, "PUT", "/v1/patients/my/clinical-consents/"+second, "owner", f.body)
	require.Equal(t, 201, created.Code, created.Body.String())
	type historyPage struct {
		Data []struct{ ID, Status string }
		Page struct {
			HasMore    bool    `json:"has_more"`
			NextCursor *string `json:"next_cursor"`
		}
	}
	response := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents?limit=1", "owner", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var page historyPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, second, page.Data[0].ID)
	require.True(t, page.Page.HasMore)
	require.NotNil(t, page.Page.NextCursor)
	next := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents?limit=1&cursor="+*page.Page.NextCursor, "owner", "")
	require.Equal(t, 200, next.Code, next.Body.String())
	var older historyPage
	require.NoError(t, json.Unmarshal(next.Body.Bytes(), &older))
	require.Len(t, older.Data, 1)
	require.Equal(t, first, older.Data[0].ID)
	require.Equal(t, "withdrawn", older.Data[0].Status)
	require.False(t, older.Page.HasMore)
	require.Nil(t, older.Page.NextCursor)
	for _, query := range []string{"?limit=201", "?cursor=invalid"} {
		invalid := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents"+query, "owner", "")
		require.Equal(t, 400, invalid.Code, invalid.Body.String())
	}
	foreign := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents?cursor="+*page.Page.NextCursor, "foreign", "")
	require.Equal(t, 400, foreign.Code, foreign.Body.String())
	require.NotContains(t, foreign.Body.String(), first)
	require.NotContains(t, foreign.Body.String(), second)
}
