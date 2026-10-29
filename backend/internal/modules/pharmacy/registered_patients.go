package pharmacy

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

// registeredPatientConsumer keeps the ids of the tenant's registered
// patients (#1416), so a sale can prove a patient id names a real
// registration at the hospital it cites.
func registeredPatientConsumer() events.Consumer {
	subject := patientmastercontract.SubjectPatientRegistered
	return events.Consumer{Name: "pharmacy-patient-registered", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d patientmastercontract.PatientRegisteredData
		if evt.Type != patientmastercontract.TypePatientRegistered || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errors.New("invalid patient registration event")
		}
		patient, err1 := uuid.Parse(d.PatientID)
		hospital, err2 := uuid.Parse(d.HospitalID)
		if err1 != nil || err2 != nil || patient == uuid.Nil || hospital == uuid.Nil {
			return errors.New("invalid patient registration event")
		}
		var kept []struct{ HospitalID uuid.UUID }
		if err := tx.Raw(`INSERT INTO pharmacy_registered_patients (tenant_id,patient_id,hospital_id) VALUES (?,?,?)
 ON CONFLICT (tenant_id,patient_id) DO NOTHING RETURNING hospital_id`, evt.TenantID, patient, hospital).Scan(&kept).Error; err != nil {
			return err
		}
		if len(kept) == 1 {
			return nil
		}
		// A replay repeats the pair; the same id at another hospital is not a
		// registration this list may silently absorb.
		var held struct{ HospitalID uuid.UUID }
		if err := tx.Raw(`SELECT hospital_id FROM pharmacy_registered_patients WHERE tenant_id=? AND patient_id=?`, evt.TenantID, patient).Scan(&held).Error; err != nil {
			return err
		}
		if held.HospitalID != hospital {
			return errors.New("patient registration names a different hospital")
		}
		return nil
	}}
}
