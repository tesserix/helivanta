package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/globalapi"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect"
	"github.com/tesserix/helivanta/internal/modules/patientmaster"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestMediConnectReviewReachesOnlyHospitalRegistrar(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA, "registrar": testutil.TenantA, "reader": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"registrar": {patientmaster.PermPatientRegister}, "reader": {patientmaster.PermPatientRegister}, "other": {patientmaster.PermPatientRegister}}, Modules: []platform.Module{patientmaster.New(), mediconnect.New()}})
	hospital := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-registrar','registrar'),(?,?,'user-reader','reader')`, testutil.TenantA, hospital, testutil.TenantA, hospital).Error
	}))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + id
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "patient", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, uuid.NewString())).Code)
	require.Equal(t, 202, testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`).Code)
	list := "/v1/patients/link-reviews?hospital_id=" + hospital
	first := testutil.Do(r, "GET", list, "registrar", "")
	require.Equal(t, 200, first.Code, first.Body.String())
	require.Eventually(t, func() bool { return strings.Contains(testutil.Do(r, "GET", list, "registrar", "").Body.String(), id) }, 10*time.Second, 20*time.Millisecond)
	detail := "/v1/patients/link-reviews/" + id + "?hospital_id=" + hospital
	require.Equal(t, 200, testutil.Do(r, "GET", detail, "registrar", "").Code)
	for _, token := range []string{"reader", "other"} {
		require.Equal(t, 404, testutil.Do(r, "GET", detail, token, "").Code)
	}
}

func TestMediConnectRegistrarDecisionRoundTrip(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA, "registrar": testutil.TenantA}, Perms: map[string][]authz.Permission{"registrar": {patientmaster.PermPatientRegister}}, Modules: []platform.Module{patientmaster.New(), mediconnect.New()}})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"mediconnect": upstream.URL, "platform": upstream.URL}})
	require.NoError(t, err)
	forwarded := gin.New()
	forwarded.NoRoute(func(c *gin.Context) {
		c.Request.Header.Set("X-Tenant-ID", testutil.TenantB)
		c.Request.Header.Set("X-User-ID", "forged")
		gateway.ServeHTTP(c.Writer, c.Request)
	})
	r = forwarded
	hospital := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-registrar','registrar')`, testutil.TenantA, hospital).Error
	}))
	patient := testutil.Do(r, "POST", "/v1/patients?hospital_id="+hospital, "registrar", fmt.Sprintf(`{"request_id":%q,"given_name":"SyntheticReview","family_name":"Example","dob":"1990-01-01","notice_version":"test-v1","consented_by":"patient"}`, uuid.NewString()))
	require.Equal(t, 201, patient.Code, patient.Body.String())
	var registration map[string]any
	require.NoError(t, json.Unmarshal(patient.Body.Bytes(), &registration))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + id
	request := testutil.Do(r, "PUT", path, "patient", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, registration["patient"].(map[string]any)["id"]))
	require.Equal(t, 202, request.Code, request.Body.String())
	submitted := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
	require.Equal(t, 202, submitted.Code, submitted.Body.String())
	var submission map[string]any
	require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &submission))
	detail := "/v1/patients/link-reviews/" + id
	var review map[string]any
	require.Eventually(t, func() bool {
		w := testutil.Do(r, "GET", detail+"?hospital_id="+hospital, "registrar", "")
		if w.Code != 200 {
			return false
		}
		return json.Unmarshal(w.Body.Bytes(), &review) == nil
	}, 10*time.Second, 20*time.Millisecond)
	decision := detail + "/decision/" + uuid.Must(uuid.NewV7()).String() + "?hospital_id=" + hospital
	body := fmt.Sprintf(`{"account_id":%q,"outcome":"approved","reason":"identity_confirmed","evidence_reference":%q,"expected_registration_updated_at":%q}`, submission["account_id"], uuid.Must(uuid.NewV7()).String(), review["registration_updated_at"])
	var stale map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &stale))
	stale["expected_registration_updated_at"] = "2020-01-01T00:00:00Z"
	staleBody, err := json.Marshal(stale)
	require.NoError(t, err)
	require.Equal(t, 409, testutil.Do(r, "PUT", decision, "registrar", string(staleBody)).Code)
	approved := testutil.Do(r, "PUT", decision, "registrar", body)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	require.JSONEq(t, approved.Body.String(), testutil.Do(r, "PUT", decision, "registrar", body).Body.String())
	require.Eventually(t, func() bool {
		return strings.Contains(testutil.Do(r, "GET", path+"/review", "patient", "").Body.String(), `"status":"identity_verified"`)
	}, 10*time.Second, 20*time.Millisecond)
	withdrawn := testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`)
	require.Equal(t, 200, withdrawn.Code, withdrawn.Body.String())
	require.Contains(t, testutil.Do(r, "GET", path+"/review", "patient", "").Body.String(), `"status":"withdrawn"`)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.patient_subject','user-registrar',true),set_config('app.patient_hospital',?,true)`, hospital).Error; err != nil {
			return err
		}
		var consentCount, grantCount int64
		if err := tx.Table("patient_consents").Count(&consentCount).Error; err != nil {
			return err
		}
		if err := tx.Table("patient_hospital_access").Count(&grantCount).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, consentCount, "only the original registration receipt")
		require.EqualValues(t, 1, grantCount, "only the pre-existing registrar grant")
		return nil
	}))

	require.Eventually(t, func() bool {
		return strings.Contains(testutil.Do(r, "GET", detail+"?hospital_id="+hospital, "registrar", "").Body.String(), `"status":"withdrawn"`)
	}, 10*time.Second, 20*time.Millisecond)
}

func TestMediConnectReviewDecisionRejectsUnsafeCommands(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA, "registrar": testutil.TenantA, "reader": testutil.TenantA, "admin": testutil.TenantA, "other": testutil.TenantB}, Perms: map[string][]authz.Permission{"patient": {patientmaster.PermPatientRegister}, "registrar": {patientmaster.PermPatientRegister}, "reader": {patientmaster.PermPatientRegister}, "admin": {patientmaster.PermPatientRegister}, "other": {patientmaster.PermPatientRegister}}, Modules: []platform.Module{patientmaster.New(), mediconnect.New()}})
	hospital := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-registrar','registrar'),(?,?,'user-patient','registrar'),(?,?,'user-reader','reader')`, testutil.TenantA, hospital, testutil.TenantA, hospital, testutil.TenantA, hospital).Error
	}))
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/mediconnect/account/link-requests/" + id
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "patient", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"child"}`, hospital, uuid.NewString())).Code)
	sub := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
	require.Equal(t, 202, sub.Code, sub.Body.String())
	var submission map[string]any
	require.NoError(t, json.Unmarshal(sub.Body.Bytes(), &submission))
	detail := "/v1/patients/link-reviews/" + id
	require.Eventually(t, func() bool {
		return testutil.Do(r, "GET", detail+"?hospital_id="+hospital, "registrar", "").Code == 200
	}, 10*time.Second, 20*time.Millisecond)
	pathDecision := detail + "/decision/" + uuid.Must(uuid.NewV7()).String() + "?hospital_id=" + hospital
	body := fmt.Sprintf(`{"account_id":%q,"outcome":"rejected","reason":"registration_missing","evidence_reference":%q}`, submission["account_id"], uuid.Must(uuid.NewV7()).String())
	for _, token := range []string{"reader", "admin", "other"} {
		w := testutil.Do(r, "PUT", pathDecision, token, body)
		require.Equal(t, 404, w.Code, w.Body.String())
	}
	require.Equal(t, 409, testutil.Do(r, "PUT", pathDecision, "patient", body).Code)
	for _, bad := range []string{`null`, `{}`, strings.Replace(body, `"rejected"`, `"approved"`, 1), strings.Replace(body, `"registration_missing"`, `"guess"`, 1), strings.TrimSuffix(body, "}") + `,"tenant_id":"forged"}`, body + ` {}`} {
		w := testutil.Do(r, "PUT", pathDecision, "registrar", bad)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	missingApproval := strings.Replace(strings.Replace(body, `"rejected"`, `"approved"`, 1), `"registration_missing"`, `"identity_confirmed"`, 1)
	missingApproval = strings.TrimSuffix(missingApproval, "}") + `,"expected_registration_updated_at":"2026-01-01T00:00:00Z"}`
	require.Equal(t, 409, testutil.Do(r, "PUT", pathDecision, "registrar", missingApproval).Code)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_review_outbox_failure", SQL: `CREATE FUNCTION reject_review_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.patientmaster.registration_reviewed.v1' THEN RAISE EXCEPTION 'synthetic event outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_review_result BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_review_result();`}}))
	failed := testutil.Do(r, "PUT", pathDecision, "registrar", body)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NotContains(t, failed.Body.String(), "synthetic")
	require.Contains(t, testutil.Do(r, "GET", detail+"?hospital_id="+hospital, "registrar", "").Body.String(), `"status":"pending"`)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_restore_review_outbox", SQL: `DROP TRIGGER reject_review_result ON outbox_events; DROP FUNCTION reject_review_result();`}}))
	rejected := testutil.Do(r, "PUT", pathDecision, "registrar", body)
	require.Equal(t, 200, rejected.Code, rejected.Body.String())
	require.Eventually(t, func() bool {
		return strings.Contains(testutil.Do(r, "GET", path+"/review", "patient", "").Body.String(), `"status":"rejected"`)
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, 409, testutil.Do(r, "PUT", pathDecision, "registrar", strings.Replace(body, "registration_missing", "identity_mismatch", 1)).Code)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM patient_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject='user-registrar'`, testutil.TenantA, hospital).Error
	}))
	require.Equal(t, 404, testutil.Do(r, "PUT", pathDecision, "registrar", body).Code)
	require.Equal(t, 404, testutil.Do(r, "GET", detail+"?hospital_id="+hospital, "registrar", "").Code)
}

func TestMediConnectReviewDeliveryOrderingAndConflicts(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA, "registrar": testutil.TenantA}, Perms: map[string][]authz.Permission{"registrar": {patientmaster.PermPatientRegister}}, Modules: []platform.Module{patientmaster.New(), mediconnect.New()}})
	hospital := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access(tenant_id,hospital_id,subject,level) VALUES (?,?,'user-registrar','registrar')`, testutil.TenantA, hospital).Error
	}))
	deliver := func(consumer events.Consumer, tenant string, data any) error {
		encoded, err := json.Marshal(data)
		require.NoError(t, err)
		return db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
				return err
			}
			return consumer.Handle(ctx, tx, events.Event{ID: uuid.NewString(), Type: consumer.Subject, Version: 1, TenantID: tenant, Data: encoded})
		})
	}
	consumers := patientmaster.New().Consumers(platform.Deps{})
	request := mediconnectcontract.RegistrationReviewData{RequestID: uuid.Must(uuid.NewV7()).String(), AccountID: uuid.Must(uuid.NewV7()).String(), RequesterSubject: "user-patient", HospitalID: hospital, PatientID: uuid.NewString(), Relationship: "child"}
	require.NoError(t, deliver(consumers[1], testutil.TenantA, request))
	require.NoError(t, deliver(consumers[0], testutil.TenantA, request))
	require.NoError(t, deliver(consumers[0], testutil.TenantA, request))
	detail := "/v1/patients/link-reviews/" + request.RequestID + "?hospital_id=" + hospital
	require.Contains(t, testutil.Do(r, "GET", detail, "registrar", "").Body.String(), `"status":"withdrawn"`)
	changed := request
	changed.AccountID = uuid.Must(uuid.NewV7()).String()
	require.ErrorContains(t, deliver(consumers[0], testutil.TenantA, changed), "mismatch")
	require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
	path := "/v1/mediconnect/account/link-requests/" + uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "patient", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, request.PatientID)).Code)
	sub := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
	require.Equal(t, 202, sub.Code)
	var row map[string]any
	require.NoError(t, json.Unmarshal(sub.Body.Bytes(), &row))
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
	result := patientmastercontract.RegistrationReviewedData{RequestID: row["request_id"].(string), AccountID: row["account_id"].(string), HospitalID: hospital, PatientID: request.PatientID, DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "approved", ReviewedAt: "2026-10-25T00:00:00Z"}
	consumer := mediconnect.New().Consumers(platform.Deps{})[0]
	require.NoError(t, deliver(consumer, testutil.TenantA, result))
	require.NoError(t, deliver(consumer, testutil.TenantA, result))
	current := testutil.Do(r, "GET", path+"/review", "patient", "")
	require.Contains(t, current.Body.String(), `"status":"withdrawn"`)
	require.Contains(t, current.Body.String(), result.DecisionID)
	result.Outcome = "rejected"
	require.ErrorContains(t, deliver(consumer, testutil.TenantA, result), "conflicts")
	result.Outcome = "approved"
	require.Error(t, deliver(consumer, testutil.TenantB, result))
	require.JSONEq(t, current.Body.String(), testutil.Do(r, "GET", path+"/review", "patient", "").Body.String())
}
