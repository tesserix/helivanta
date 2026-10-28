package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
)

func TestRegistrarReviewDecisionAndCancellationContracts(t *testing.T) {
	r, db, ctx := setupHTTP(t)
	patient := scopedDo(r, "POST", "/v1/patients", "tokA", registerBody("SyntheticReview", "Evidence", "1990-01-01", "", false, ""))
	require.Equal(t, 201, patient.Code)
	consumers := patientmaster.New().Consumers(platform.Deps{})
	deliver := func(consumer events.Consumer, data mediconnectcontract.RegistrationReviewData) error {
		encoded, err := json.Marshal(data)
		require.NoError(t, err)
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
				return err
			}
			return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
		})
	}
	for _, outcome := range []string{"approved", "rejected", "withdrawn"} {
		t.Run(outcome, func(t *testing.T) {
			data := mediconnectcontract.RegistrationReviewData{RequestID: uuid.Must(uuid.NewV7()).String(), AccountID: uuid.Must(uuid.NewV7()).String(), RequesterSubject: "independent-patient", HospitalID: testHospital, PatientID: registeredID(t, patient), Relationship: "self"}
			if outcome == "withdrawn" {
				require.NoError(t, deliver(consumers[1], data))
			}
			require.NoError(t, deliver(consumers[0], data))
			require.NoError(t, deliver(consumers[0], data))
			bad := data
			bad.AccountID = uuid.Must(uuid.NewV7()).String()
			require.Error(t, deliver(consumers[0], bad))
			detail := "/v1/patients/link-reviews/" + data.RequestID
			read := scopedDo(r, "GET", detail, "tokA", "")
			require.Equal(t, 200, read.Code, read.Body.String())
			require.Equal(t, 404, scopedDo(r, "GET", detail, "tokB", "").Code)
			var row map[string]any
			require.NoError(t, json.Unmarshal(read.Body.Bytes(), &row))
			decision := detail + "/decision/" + uuid.Must(uuid.NewV7()).String()
			body := fmt.Sprintf(`{"account_id":%q,"outcome":"rejected","reason":"insufficient_evidence","evidence_reference":%q}`, data.AccountID, uuid.Must(uuid.NewV7()).String())
			if outcome == "approved" {
				body = fmt.Sprintf(`{"account_id":%q,"outcome":"approved","reason":"identity_confirmed","evidence_reference":%q,"expected_registration_updated_at":%q}`, data.AccountID, uuid.Must(uuid.NewV7()).String(), row["registration_updated_at"])
			}
			require.Equal(t, 400, scopedDo(r, "PUT", decision, "tokA", `{}`).Code)
			result := scopedDo(r, "PUT", decision, "tokA", body)
			if outcome == "withdrawn" {
				require.Equal(t, 409, result.Code)
				return
			}
			require.Equal(t, 200, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), `"status":"`+outcome+`"`)
			require.JSONEq(t, result.Body.String(), scopedDo(r, "PUT", decision, "tokA", body).Body.String())
			require.Equal(t, 409, scopedDo(r, "PUT", detail+"/decision/"+uuid.Must(uuid.NewV7()).String(), "tokA", body).Code)
			require.NoError(t, deliver(consumers[1], data))
			require.NoError(t, deliver(consumers[0], data))
			require.Contains(t, scopedDo(r, "GET", detail, "tokA", "").Body.String(), `"status":"withdrawn"`)
		})
	}
	list := scopedDo(r, "GET", "/v1/patients/link-reviews?limit=2", "tokA", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"has_more":true`)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_review_read_audit_failure", SQL: `CREATE FUNCTION reject_review_read() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_type='patient_link_reviews' AND NEW.action='read' THEN RAISE EXCEPTION 'synthetic audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_review_read BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION reject_review_read();`}}))
	blocked := scopedDo(r, "GET", "/v1/patients/link-reviews", "tokA", "")
	require.Equal(t, 500, blocked.Code, blocked.Body.String())
	require.NotContains(t, blocked.Body.String(), "account_id")
	require.NotContains(t, blocked.Body.String(), "synthetic")

}
