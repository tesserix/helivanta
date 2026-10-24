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
