package pharmacy

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/pkg/events"
)

// OPD encounters (#987 slice 2): pharmacy's copy of MediCore's encounters,
// so a counter sale can cite the visit it serves. Fed by the two published
// events in either order; see opd_encounters.sql.

var encounterStatuses = map[string]bool{"arrived": true, "in_consultation": true, "closed": true, "left_without_being_seen": true, "cancelled": true}

var errInvalidEncounterEvent = errors.New("invalid OPD encounter event")

type heldOPDEncounter struct {
	HospitalID uuid.UUID  `gorm:"column:hospital_id"`
	PatientID  *uuid.UUID `gorm:"column:patient_id"`
	Status     string     `gorm:"column:status"`
	Version    int        `gorm:"column:version"`
}

func heldEncounter(tx *gorm.DB, tenant string, encounter uuid.UUID) (heldOPDEncounter, error) {
	var h heldOPDEncounter
	err := tx.Raw(`SELECT hospital_id,patient_id,status,version FROM pharmacy_opd_encounters WHERE tenant_id=? AND encounter_id=?`, tenant, encounter).Scan(&h).Error
	return h, err
}

func opdEncounterConsumers() []events.Consumer {
	return []events.Consumer{{Name: "pharmacy-encounter-opened", Subject: medicorecontract.SubjectEncounterOpened, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d medicorecontract.EncounterOpenedData
		if evt.Type != "EncounterOpened" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errInvalidEncounterEvent
		}
		ids, ok := parseIDs(d.EncounterID, d.HospitalID, d.PatientID)
		if !ok {
			return errInvalidEncounterEvent
		}
		// An encounter opens arrived at version 1 (medicore/encounters.go); a
		// status that arrived first is newer and is kept.
		if err := tx.Exec(`INSERT INTO pharmacy_opd_encounters (tenant_id,encounter_id,hospital_id,patient_id,status,version) VALUES (?,?,?,?,'arrived',1)
 ON CONFLICT (tenant_id,encounter_id) DO UPDATE SET patient_id=EXCLUDED.patient_id WHERE pharmacy_opd_encounters.patient_id IS NULL
  AND pharmacy_opd_encounters.hospital_id=EXCLUDED.hospital_id`, evt.TenantID, ids[0], ids[1], ids[2]).Error; err != nil {
			return err
		}
		h, err := heldEncounter(tx, evt.TenantID, ids[0])
		if err != nil {
			return err
		}
		// Fail closed: a replay naming another patient or hospital is never
		// taken as this encounter; the bus retries and it is investigated.
		if h.HospitalID != ids[1] || h.PatientID == nil || *h.PatientID != ids[2] {
			return errors.New("a replayed OPD encounter differs from the one recorded")
		}
		return nil
	}}, {Name: "pharmacy-encounter-status-changed", Subject: medicorecontract.SubjectEncounterStatusChanged, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d medicorecontract.EncounterStatusChangedData
		if evt.Type != "EncounterStatusChanged" || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errInvalidEncounterEvent
		}
		ids, ok := parseIDs(d.EncounterID, d.HospitalID)
		if !ok || !encounterStatuses[d.ToStatus] || d.Version < 2 {
			return errInvalidEncounterEvent
		}
		if err := tx.Exec(`INSERT INTO pharmacy_opd_encounters (tenant_id,encounter_id,hospital_id,status,version) VALUES (?,?,?,?,?)
 ON CONFLICT (tenant_id,encounter_id) DO UPDATE SET status=EXCLUDED.status,version=EXCLUDED.version
 WHERE pharmacy_opd_encounters.version < EXCLUDED.version AND pharmacy_opd_encounters.hospital_id=EXCLUDED.hospital_id`,
			evt.TenantID, ids[0], ids[1], d.ToStatus, d.Version).Error; err != nil {
			return err
		}
		h, err := heldEncounter(tx, evt.TenantID, ids[0])
		if err != nil {
			return err
		}
		// A newer version may already be held; the same version must say the same.
		if h.HospitalID != ids[1] || (h.Version == d.Version && h.Status != d.ToStatus) {
			return errors.New("a replayed OPD encounter status differs from the one recorded")
		}
		return nil
	}}}
}
