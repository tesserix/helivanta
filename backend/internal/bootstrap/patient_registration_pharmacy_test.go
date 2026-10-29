package bootstrap_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/bootstrap"
	patientcontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

func TestPharmacyAcceptsPatientmastersPersistedRegistrationEnvelope(t *testing.T) {
	var modules []platform.Module
	var consumer events.Consumer
	for _, m := range bootstrap.Modules(nil, nil) {
		switch m.Name() {
		case "workforce", "patientmaster", "pharmacy":
			modules = append(modules, m)
		}
		if m.Name() == "pharmacy" {
			for _, c := range m.Consumers(platform.Deps{}) {
				if c.Name == "pharmacy-patient-registered" {
					consumer = c
				}
			}
		}
	}
	require.NotNil(t, consumer.Handle)
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"registrar": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"registrar": {"workforce.hospital.manage", "patient.access.manage", "patient.register"}}, Modules: modules,
	})
	hospital := uuid.NewString()
	w := testutil.Do(r, http.MethodPut, "/v1/workforce/hospitals/"+hospital, "registrar", `{"name":"Synthetic registration integration hospital"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(r, http.MethodPut, "/v1/patients/hospitals/"+hospital+"/access/user-registrar", "registrar", `{"level":"registrar"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	body := fmt.Sprintf(`{"request_id":%q,"given_name":"SYNTHETIC-REGISTRATION-OWNER","family_name":"CONTRACT-TEST","dob":"1990-01-01","sex":"other","notice_version":"TEST-INTEGRATION","consented_by":"patient"}`, uuid.NewString())
	w = testutil.Do(r, http.MethodPost, "/v1/patients?hospital_id="+hospital, "registrar", body)
	require.Equal(t, 201, w.Code, w.Body.String())
	var response struct {
		Patient struct {
			ID string `json:"id"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotEmpty(t, response.Patient.ID)
	var stored struct{ Payload []byte }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload FROM outbox_events WHERE subject=? AND payload->'data'->>'patient_id'=?`, patientcontract.SubjectPatientRegistered, response.Patient.ID).Take(&stored).Error
	}))
	require.NotContains(t, string(stored.Payload), "SYNTHETIC-REGISTRATION-OWNER")
	require.NotContains(t, string(stored.Payload), "1990-01-01")
	var evt events.Event
	require.NoError(t, json.Unmarshal(stored.Payload, &evt))
	require.Equal(t, "PatientRegistered", evt.Type)
	require.Equal(t, testutil.TenantA, evt.TenantID)
	require.Equal(t, patientcontract.SubjectPatientRegistered, consumer.Subject)
	for range 2 {
		require.NoError(t, db.WithTenant(ctx, evt.TenantID, func(tx *gorm.DB) error { return consumer.Handle(ctx, tx, evt) }))
	}
	var rows []struct{ HospitalID string }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT hospital_id FROM pharmacy_registered_patients WHERE patient_id=?`, response.Patient.ID).Scan(&rows).Error
	}))
	require.Len(t, rows, 1)
	require.Equal(t, hospital, rows[0].HospitalID)
	var foreignRows []struct{ HospitalID string }
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		result := tx.Raw(`SELECT hospital_id FROM pharmacy_registered_patients WHERE patient_id=?`, response.Patient.ID).Scan(&foreignRows)
		require.Zero(t, result.RowsAffected)
		return result.Error
	}))
	require.Empty(t, foreignRows)
}
