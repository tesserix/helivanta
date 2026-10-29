package pharmacy_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
)

func TestPharmacyKeepsRegisteredPatientIds(t *testing.T) {
	_, db := catalogueHarness(t)
	var consumer events.Consumer
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		if c.Name == "pharmacy-patient-registered" {
			consumer = c
		}
	}
	require.Equal(t, patientmastercontract.SubjectPatientRegistered, consumer.Subject)
	deliver := func(tenant string, raw any) error {
		data, err := json.Marshal(raw)
		require.NoError(t, err)
		return db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return consumer.Handle(t.Context(), tx, events.Event{Type: patientmastercontract.TypePatientRegistered, Version: 1, TenantID: tenant, Data: data})
		})
	}
	held := func(tenant, patient string) []string {
		var hospitals []string
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT hospital_id::text FROM pharmacy_registered_patients WHERE patient_id=?`, patient).Scan(&hospitals).Error
		}))
		return hospitals
	}
	patient, hospital := uuid.NewString(), uuid.NewString()
	reg := patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: hospital}
	require.NoError(t, deliver(testutil.TenantA, reg))
	require.NoError(t, deliver(testutil.TenantA, reg), "a replay is idempotent")
	require.Equal(t, []string{hospital}, held(testutil.TenantA, patient))
	require.Error(t, deliver(testutil.TenantA, patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: uuid.NewString()}), "the same id at another hospital is refused")
	require.Equal(t, []string{hospital}, held(testutil.TenantA, patient))
	require.Empty(t, held(testutil.TenantB, patient), "another tenant sees nothing")
	require.Error(t, deliver(testutil.TenantA, map[string]string{"patient_id": "not-a-uuid", "hospital_id": hospital}))
	require.Error(t, deliver(testutil.TenantA, patientmastercontract.PatientRegisteredData{PatientID: uuid.NewString(), HospitalID: uuid.Nil.String()}))
	require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_registered_patients SET hospital_id=? WHERE patient_id=?`, uuid.NewString(), patient).Error
	}), "a registration's hospital never changes")
}

func TestRegisteredPatientProjectionRejectsInvalidOwnerEnvelopes(t *testing.T) {
	_, db := catalogueHarness(t)
	var consumer events.Consumer
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		if c.Name == "pharmacy-patient-registered" {
			consumer = c
		}
	}
	patient, hospital := uuid.NewString(), uuid.NewString()
	data, err := json.Marshal(patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: hospital})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, kind, destination string
		version                 int
		data                    json.RawMessage
	}{
		{"routing subject is not type", consumer.Subject, "", 1, data},
		{"unknown type", "UnknownRegistration", "", 1, data},
		{"unsupported version", patientmastercontract.TypePatientRegistered, "", 2, data},
		{"directed event", patientmastercontract.TypePatientRegistered, testutil.TenantB, 1, data},
		{"malformed payload", patientmastercontract.TypePatientRegistered, "", 1, json.RawMessage(`{"patient_id":`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				return consumer.Handle(t.Context(), tx, events.Event{Type: tc.kind, Version: tc.version, TenantID: testutil.TenantA, DestinationTenantID: tc.destination, Data: tc.data})
			}))
		})
	}
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_registered_patients").Where("patient_id=?", patient).Count(&count).Error
	}))
	require.Zero(t, count)
}
