package mediconnect_test

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/mediconnect" //nolint:depguard // module under test
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
)

func TestMediConnectReviewSubmissionIsDurableAndOwned(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA, "peer": testutil.TenantA, "other": testutil.TenantB}, Modules: []platform.Module{mediconnect.New()}})
	const root = "/v1/mediconnect/account"
	require.Equal(t, 201, testutil.Do(r, "POST", root, "patient", "{}").Code)
	id := uuid.Must(uuid.NewV7()).String()
	path := root + "/link-requests/" + id
	claim := fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, uuid.NewString(), uuid.NewString())
	require.Equal(t, 202, testutil.Do(r, "PUT", path, "patient", claim).Code)
	submitted := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
	require.Equal(t, 202, submitted.Code, submitted.Body.String())
	var row map[string]any
	require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &row))
	require.Equal(t, id, row["request_id"])
	require.Equal(t, "submitted", row["status"])
	require.Equal(t, "no-store", submitted.Header().Get("Cache-Control"))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		err := tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='mediconnect_review_submissions' AND resource_id=? AND action='insert'`, id).Scan(&count).Error
		require.EqualValues(t, 1, count)
		return err
	}))
	replay := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, submitted.Body.String(), replay.Body.String())
	require.JSONEq(t, submitted.Body.String(), testutil.Do(r, "GET", path+"/review", "patient", "").Body.String())
	for _, token := range []string{"peer", "other"} {
		require.Equal(t, 404, testutil.Do(r, "GET", path+"/review", token, "").Code)
	}
}

func TestReviewProjectionPreservesWithdrawalAndRejectsConflictingReceipts(t *testing.T) {
	for _, withdrawFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(withdrawFirst), func(t *testing.T) {
			r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
			require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
			id := uuid.Must(uuid.NewV7()).String()
			hospital := uuid.NewString()
			patient := uuid.NewString()
			path := "/v1/mediconnect/account/link-requests/" + id
			require.Equal(t, 202, testutil.Do(r, "PUT", path, "patient", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, patient)).Code)
			for _, input := range []string{`null`, `{}`, `{"expected_version":2}`, `{"expected_version":1,"extra":true}`} {
				response := testutil.Do(r, "POST", path+"/review", "patient", input)
				if input == `{"expected_version":2}` {
					require.Equal(t, 409, response.Code)
				} else {
					require.Equal(t, 400, response.Code)
				}
			}
			sub := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
			require.Equal(t, 202, sub.Code)
			var row map[string]any
			require.NoError(t, json.Unmarshal(sub.Body.Bytes(), &row))
			if withdrawFirst {
				require.Equal(t, 200, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
			}
			consumer := mediconnect.New().Consumers(platform.Deps{})[0]
			result := patientmastercontract.RegistrationReviewedData{RequestID: id, AccountID: row["account_id"].(string), HospitalID: hospital, PatientID: patient, DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "approved", ReviewedAt: "2026-10-25T00:00:00Z"}
			deliver := func(value patientmastercontract.RegistrationReviewedData) error {
				data, err := json.Marshal(value)
				require.NoError(t, err)
				return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
						return err
					}
					return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: data})
				})
			}
			for _, field := range []string{"request", "time", "outcome", "missing"} {
				bad := result
				switch field {
				case "request":
					bad.RequestID = "invalid"
				case "time":
					bad.ReviewedAt = "bad"
				case "outcome":
					bad.Outcome = "unknown"
				case "missing":
					bad.AccountID = uuid.Must(uuid.NewV7()).String()
				}
				require.Error(t, deliver(bad))
			}
			require.NoError(t, deliver(result))
			require.NoError(t, deliver(result))
			expected := "identity_verified"
			if withdrawFirst {
				expected = "withdrawn"
			}
			require.Contains(t, testutil.Do(r, "GET", path+"/review", "patient", "").Body.String(), `"status":"`+expected+`"`)
			result.Outcome = "rejected"
			require.Error(t, deliver(result))
			require.Equal(t, 200, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
			require.Equal(t, 409, testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`).Code)
			require.Contains(t, testutil.Do(r, "GET", path+"/review", "patient", "").Body.String(), `"status":"withdrawn"`)
		})
	}
}

func TestPreviousWriterWithdrawalCannotLeaveVerifiedReview(t *testing.T) {
	for _, mode := range []string{"read", "read_outbox_failure", "withdraw_retry", "late_result", "replayed_result"} {
		t.Run(mode, func(t *testing.T) {
			r, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"patient": testutil.TenantA}, Modules: []platform.Module{mediconnect.New()}})
			require.Equal(t, 201, testutil.Do(r, "POST", "/v1/mediconnect/account", "patient", "{}").Code)
			id := uuid.Must(uuid.NewV7()).String()
			hospital := uuid.NewString()
			patient := uuid.NewString()
			path := "/v1/mediconnect/account/link-requests/" + id
			require.Equal(t, 202, testutil.Do(r, "PUT", path, "patient", fmt.Sprintf(`{"hospital_id":%q,"patient_id":%q,"relationship":"self"}`, hospital, patient)).Code)
			sub := testutil.Do(r, "POST", path+"/review", "patient", `{"expected_version":1}`)
			require.Equal(t, 202, sub.Code)
			var row map[string]any
			require.NoError(t, json.Unmarshal(sub.Body.Bytes(), &row))
			consumer := mediconnect.New().Consumers(platform.Deps{Bus: bus})[0]
			result := patientmastercontract.RegistrationReviewedData{RequestID: id, AccountID: row["account_id"].(string), HospitalID: hospital, PatientID: patient, DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "approved", ReviewedAt: "2026-10-25T00:00:00Z"}
			deliver := func() error {
				data, err := json.Marshal(result)
				require.NoError(t, err)
				return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
						return err
					}
					return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: data})
				})
			}
			if mode == "replayed_result" {
				require.NoError(t, deliver())
			}
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.actor_subject','user-patient',true)`).Error; err != nil {
					return err
				}
				return tx.Exec(`UPDATE mediconnect_link_requests SET status='withdrawn',version=2,withdrawn_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND id=?`, testutil.TenantA, id).Error
			}))
			if mode == "late_result" || mode == "replayed_result" {
				require.NoError(t, deliver())
			}
			if mode == "withdraw_retry" {
				require.Equal(t, 200, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
			}
			if mode != "read" && mode != "read_outbox_failure" {
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
					var count int64
					if err := tx.Table("outbox_events").Where("tenant_id=? AND subject=?", testutil.TenantA, "helivanta.in.mediconnect.registration_review_withdrawn.v1").Count(&count).Error; err != nil {
						return err
					}
					require.EqualValues(t, 1, count, "reconciliation must precede the later GET")
					return nil
				}))
			}
			if mode == "read_outbox_failure" {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_recovery_outbox_failure", SQL: `CREATE FUNCTION reject_recovery_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.subject='helivanta.in.mediconnect.registration_review_withdrawn.v1' THEN RAISE EXCEPTION 'synthetic recovery failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_recovery_event BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION reject_recovery_event();`}}))
				failed := testutil.Do(r, "GET", path+"/review", "patient", "")
				require.Equal(t, 500, failed.Code, failed.Body.String())
				require.NotContains(t, failed.Body.String(), "synthetic")
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_recovery_outbox_restore", SQL: `DROP TRIGGER reject_recovery_event ON outbox_events; DROP FUNCTION reject_recovery_event();`}}))
			}
			current := testutil.Do(r, "GET", path+"/review", "patient", "")
			require.Equal(t, 200, current.Code, current.Body.String())
			require.Contains(t, current.Body.String(), `"status":"withdrawn"`)
			require.JSONEq(t, current.Body.String(), testutil.Do(r, "GET", path+"/review", "patient", "").Body.String())
			require.Equal(t, 200, testutil.Do(r, "POST", path+"/withdraw", "patient", `{"expected_version":1}`).Code)
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var count int64
				if err := tx.Table("outbox_events").Where("tenant_id=? AND subject=?", testutil.TenantA, "helivanta.in.mediconnect.registration_review_withdrawn.v1").Count(&count).Error; err != nil {
					return err
				}
				require.EqualValues(t, 1, count, "exactly one recovered cancellation")
				return nil
			}))
			require.Error(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.mediconnect_review_request',?,true)`, "system:"+consumer.Name, id).Error; err != nil {
					return err
				}
				return tx.Exec(`UPDATE mediconnect_link_requests SET updated_at=clock_timestamp() WHERE tenant_id=? AND id=?`, testutil.TenantA, id).Error
			}), "consumer source-read capability cannot write the original request")
		})
	}
}
