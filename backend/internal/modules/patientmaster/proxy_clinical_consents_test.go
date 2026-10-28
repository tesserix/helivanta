package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestProxyClinicalConsentRequiresSignedGuardianDeclaration(t *testing.T) {
	for _, capacity := range []string{"parental", "appointed_guardian"} {
		t.Run(capacity, func(t *testing.T) {
			f := newClinicalFixtureForCapacity(t, capacity)
			id := uuid.Must(uuid.NewV7()).String()
			created := scopedDo(f.router, "PUT", "/v1/patients/my/proxy-clinical-consents/"+id, "owner", f.body)
			require.Equal(t, 201, created.Code, created.Body.String())
			require.Contains(t, created.Body.String(), fmt.Sprintf(`"authority_kind":%q`, capacity))
			require.Contains(t, created.Body.String(), `"hmac":"vault:v1:`)
			path := "/v1/patients/my/clinical-consents/" + id
			read := scopedDo(f.router, "GET", path, "owner", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.JSONEq(t, created.Body.String(), read.Body.String())
			decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
			require.Equal(t, 200, decision.Code, decision.Body.String())
			require.Contains(t, decision.Body.String(), `"consent_gate":"allow"`)
			require.Contains(t, decision.Body.String(), `"record_access_authorized":false`)
			var receipt map[string]any
			require.NoError(t, json.Unmarshal(created.Body.Bytes(), &receipt))
			for _, actor := range []string{"owner", "recipient"} {
				direct := scopedDo(f.router, "GET", "/v1/patients/"+receipt["patient_id"].(string), actor, "")
				require.Equal(t, 404, direct.Code, direct.Body.String())
			}
		})
	}
}

func TestProxyClinicalCatalogUsesReviewedCapacity(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "parental")
	var in map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &in))
	root := "/v1/patients/my/proxy-authorities/" + in["authority_id"].(string)
	notices := scopedDo(f.router, "GET", root+"/sharing-notices", "owner", "")
	require.Equal(t, 200, notices.Code, notices.Body.String())
	require.Contains(t, notices.Body.String(), `"authority_kind":"parental"`)
	recipients := scopedDo(f.router, "GET", root+"/sharing-recipients", "owner", "")
	require.Equal(t, 200, recipients.Code, recipients.Body.String())
	require.Contains(t, recipients.Body.String(), `"display_name":"Synthetic Facility"`)
}

func TestProxyClinicalWithdrawalSurvivesRevokedAuthorityAndDependencies(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "parental")
	id := uuid.Must(uuid.NewV7()).String()
	capture := "/v1/patients/my/proxy-clinical-consents/" + id
	made := scopedDo(f.router, "PUT", capture, "owner", f.body)
	require.Equal(t, 201, made.Code, made.Body.String())
	var in map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &in))
	revoked := scopedDo(f.router, "POST", "/v1/patients/my/proxy-authorities/"+in["authority_id"].(string)+"/revoke", "owner", `{"expected_version":2}`)
	require.Equal(t, 200, revoked.Code, revoked.Body.String())
	f.signerUnavailable.Store(true)
	f.claimUnavailable.Store(true)
	withdrawn := scopedDo(f.router, "POST", "/v1/patients/my/clinical-consents/"+id+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, withdrawn.Body.String(), `"status":"withdrawn"`)
	f.signerUnavailable.Store(false)
	replay := scopedDo(f.router, "PUT", capture, "owner", f.body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), `"status":"withdrawn"`)
	decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
	require.Equal(t, 200, decision.Code, decision.Body.String())
	require.Contains(t, decision.Body.String(), `"consent_gate":"deny"`)
}

func TestProxyClinicalConsentRequiresExactNoticeCapacityAndActor(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "parental")
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/proxy-clinical-consents/" + id
	for _, actor := range []string{"peer", "foreign"} {
		refused := scopedDo(f.router, "PUT", path, actor, f.body)
		require.Equal(t, 404, refused.Code, refused.Body.String())
	}
	wrongRoute := scopedDo(f.router, "PUT", "/v1/patients/my/clinical-consents/"+id, "owner", f.body)
	require.Equal(t, 404, wrongRoute.Code, wrongRoute.Body.String())
	noticeID := uuid.Must(uuid.NewV7()).String()
	noticePath := "/v1/patients/sharing-notices/" + noticeID
	require.Equal(t, 201, scopedDo(f.router, "PUT", noticePath, "reviewer", strings.Replace(syntheticSharingNotice(), "synthetic-1", "synthetic-self-only", 1)).Code)
	approved := scopedDo(f.router, "POST", noticePath+"/approve", "approver", fmt.Sprintf(`{"expected_version":1,"review_reference":%q}`, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, 200, approved.Code, approved.Body.String())
	var notice, in map[string]any
	require.NoError(t, json.Unmarshal(approved.Body.Bytes(), &notice))
	require.NoError(t, json.Unmarshal([]byte(f.body), &in))
	in["notice_id"], in["notice_hash"] = noticeID, notice["content_hash"]
	mismatch := scopedDo(f.router, "PUT", path, "owner", proxyJSON(t, in))
	require.Equal(t, 409, mismatch.Code, mismatch.Body.String())
	catalog := scopedDo(f.router, "GET", "/v1/patients/my/proxy-authorities/"+in["authority_id"].(string)+"/sharing-notices", "owner", "")
	require.Equal(t, 200, catalog.Code, catalog.Body.String())
	require.NotContains(t, catalog.Body.String(), noticeID)
	require.Equal(t, 404, scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents/"+id, "owner", "").Code)
	for _, bad := range []string{strings.TrimSuffix(f.body, "}") + `,"authority_kind":"self"}`, strings.Replace(f.body, `"affirmed":true`, `"affirmed":false`, 1)} {
		rejected := scopedDo(f.router, "PUT", path, "owner", bad)
		require.Equal(t, 400, rejected.Code, rejected.Body.String())
	}
}

func TestProxyClinicalConsentRechecksLiveOwnerAndLocalSources(t *testing.T) {
	for _, change := range []string{"claim", "authority", "notice", "recipient"} {
		t.Run(change, func(t *testing.T) {
			f := newClinicalFixtureForCapacity(t, "parental")
			id := uuid.Must(uuid.NewV7()).String()
			capture := "/v1/patients/my/proxy-clinical-consents/" + id
			made := scopedDo(f.router, "PUT", capture, "owner", f.body)
			require.Equal(t, 201, made.Code, made.Body.String())
			var in map[string]any
			require.NoError(t, json.Unmarshal([]byte(f.body), &in))
			path, actor, body := "", "owner", `{"expected_version":2}`
			switch change {
			case "claim":
				path = "/v1/mediconnect/account/link-requests/" + f.requestID + "/withdraw"
				body = `{"expected_version":1}`
			case "authority":
				path = "/v1/patients/my/proxy-authorities/" + in["authority_id"].(string) + "/revoke"
			case "notice":
				path = "/v1/patients/sharing-notices/" + in["notice_id"].(string) + "/retire"
				actor = "approver"
			case "recipient":
				path = "/v1/patients/sharing-recipients/" + in["recipient_id"].(string) + "/revoke"
				actor = "approver"
				body = fmt.Sprintf(`{"expected_version":2,"reason_reference":%q}`, uuid.Must(uuid.NewV7()).String())
			}
			changed := scopedDo(f.router, "POST", path, actor, body)
			require.Equal(t, 200, changed.Code, changed.Body.String())
			decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
			require.Equal(t, 200, decision.Code, decision.Body.String())
			require.Contains(t, decision.Body.String(), `"consent_gate":"deny"`)
			require.NotContains(t, decision.Body.String(), `"consent_gate":"allow"`)
			retained := scopedDo(f.router, "GET", "/v1/patients/my/clinical-consents/"+id, "owner", "")
			require.Equal(t, 200, retained.Code, retained.Body.String())
		})
	}
}

func TestProxyClinicalConsentDependenciesFailClosedAndRecoverSameCommand(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "appointed_guardian")
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/patients/my/proxy-clinical-consents/" + id
	f.claimUnavailable.Store(true)
	require.Equal(t, 503, scopedDo(f.router, "PUT", path, "owner", f.body).Code)
	f.claimUnavailable.Store(false)
	f.signerUnavailable.Store(true)
	require.Equal(t, 503, scopedDo(f.router, "PUT", path, "owner", f.body).Code)
	f.signerUnavailable.Store(false)
	made := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 201, made.Code, made.Body.String())
	f.claimUnavailable.Store(true)
	decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
	require.Equal(t, 503, decision.Code, decision.Body.String())
	require.NotContains(t, decision.Body.String(), `"consent_gate":"allow"`)
	f.claimUnavailable.Store(false)
	replay := scopedDo(f.router, "PUT", path, "owner", f.body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, made.Body.String(), replay.Body.String())
}

func TestProxyClinicalConsentIntegrityIncidentSurvivesWithdrawal(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "parental")
	id := uuid.Must(uuid.NewV7()).String()
	f.corruptSigner.Store(true)
	made := scopedDo(f.router, "PUT", "/v1/patients/my/proxy-clinical-consents/"+id, "owner", f.body)
	require.Equal(t, 201, made.Code, made.Body.String())
	f.corruptSigner.Store(false)
	path := "/v1/patients/my/clinical-consents/" + id
	bad := scopedDo(f.router, "GET", path, "owner", "")
	require.Equal(t, 409, bad.Code, bad.Body.String())
	require.Contains(t, bad.Body.String(), "clinical_consent_integrity_failed")
	withdrawn := scopedDo(f.router, "POST", path+"/withdraw", "owner", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, withdrawn.Body.String(), `"status":"quarantined"`)
	require.Contains(t, withdrawn.Body.String(), `"version":3`)
}

func TestProxyClinicalConsentConcurrentRetriesPublishOneTransition(t *testing.T) {
	f := newClinicalFixtureForCapacity(t, "parental")
	id := uuid.Must(uuid.NewV7()).String()
	capture := "/v1/patients/my/proxy-clinical-consents/" + id
	run := func(method, path, body string, expected ...int) {
		start := make(chan struct{})
		replies := make(chan *httptest.ResponseRecorder, 4)
		for range 4 {
			go func() { <-start; replies <- scopedDo(f.router, method, path, "owner", body) }()
		}
		close(start)
		for range 4 {
			reply := <-replies
			require.Contains(t, expected, reply.Code, reply.Body.String())
		}
	}
	run("PUT", capture, f.body, 200, 201)
	run("POST", "/v1/patients/my/clinical-consents/"+id+"/withdraw", `{"expected_version":1}`, 200)
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("outbox_events").Where("tenant_id=? AND subject=? AND payload->'data'->>'consent_id'=?", testutil.TenantA, "helivanta.in.patientmaster.clinical_consent_changed.v1", id).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count)
		return nil
	}))
}

func TestProxyClinicalConsentAuditOrOutboxFailureDoesNotCommit(t *testing.T) {
	for _, target := range []string{"audit_events", "outbox_events"} {
		t.Run(target, func(t *testing.T) {
			f := newClinicalFixtureForCapacity(t, "parental")
			require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_proxy_consent_write_outage", SQL: fmt.Sprintf(`CREATE FUNCTION reject_proxy_consent_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic write outage'; END $$; CREATE TRIGGER reject_proxy_consent_write BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION reject_proxy_consent_write();`, target)}}))
			id := uuid.Must(uuid.NewV7()).String()
			failed := scopedDo(f.router, "PUT", "/v1/patients/my/proxy-clinical-consents/"+id, "owner", f.body)
			require.Equal(t, 503, failed.Code, failed.Body.String())
			require.NotContains(t, failed.Body.String(), "synthetic write outage")
			require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.actor_subject','user-owner',true),set_config('app.patient_consent_subject','user-owner',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
					return err
				}
				var count int64
				if err := tx.Table("patient_clinical_consents").Where("tenant_id=? AND id=?", testutil.TenantA, id).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count)
				return nil
			}))
		})
	}
}

func TestProxyClinicalConsentRechecksParentalDOBWithoutVersionChange(t *testing.T) {
	for _, change := range []string{"dob", "estimated"} {
		t.Run(change, func(t *testing.T) {
			f := newClinicalFixtureForCapacity(t, "parental")
			id := uuid.Must(uuid.NewV7()).String()
			made := scopedDo(f.router, "PUT", "/v1/patients/my/proxy-clinical-consents/"+id, "owner", f.body)
			require.Equal(t, 201, made.Code, made.Body.String())
			var receipt map[string]any
			require.NoError(t, json.Unmarshal(made.Body.Bytes(), &receipt))
			require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.actor_subject','user-reviewer',true),set_config('app.patient_subject','user-reviewer',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
					return err
				}
				if change == "dob" {
					result := tx.Exec(`UPDATE patients SET dob='1990-01-01' WHERE tenant_id=? AND id=?`, testutil.TenantA, receipt["patient_id"])
					require.EqualValues(t, 1, result.RowsAffected)
					return result.Error
				}
				result := tx.Exec(`UPDATE patients SET dob_estimated=true WHERE tenant_id=? AND id=?`, testutil.TenantA, receipt["patient_id"])
				require.EqualValues(t, 1, result.RowsAffected)
				return result.Error
			}))
			decision := scopedDo(f.router, "POST", "/v1/patients/clinical-consents/"+id+"/decide", "recipient", `{"purpose":"care_coordination","resource_type":"diagnostic_report"}`)
			require.Equal(t, 200, decision.Code, decision.Body.String())
			require.Contains(t, decision.Body.String(), `"consent_gate":"deny"`)
			fresh := scopedDo(f.router, "PUT", "/v1/patients/my/proxy-clinical-consents/"+uuid.Must(uuid.NewV7()).String(), "owner", f.body)
			require.Equal(t, 409, fresh.Code, fresh.Body.String())
		})
	}
}

func TestProxyClinicalCaptureRequiresExplicitRolloutActivation(t *testing.T) {
	f := newClinicalFixtureWithCapture(t, "parental", false)
	capture := scopedDo(f.router, "PUT", "/v1/patients/my/proxy-clinical-consents/"+uuid.Must(uuid.NewV7()).String(), "owner", f.body)
	require.Equal(t, 503, capture.Code, capture.Body.String())
	require.Contains(t, capture.Body.String(), "proxy_clinical_capture_disabled")
	var in map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.body), &in))
	registry := scopedDo(f.router, "GET", "/v1/patients/my/proxy-authorities/"+in["authority_id"].(string), "owner", "")
	require.Equal(t, 200, registry.Code, registry.Body.String())
}
