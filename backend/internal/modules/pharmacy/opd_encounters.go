package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"

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

// maxCitableEncounters bounds the counter's list: a patient's recent visits,
// not their history.
const maxCitableEncounters = 20

type citableEncounter struct {
	EncounterID uuid.UUID `gorm:"column:encounter_id" json:"encounter_id"`
	Status      string    `gorm:"column:status" json:"status"`
	ReceivedAt  time.Time `gorm:"column:received_at" json:"received_at"`
}

// citableEncounters lists a registered patient's OPD visits a sale may cite,
// newest first, at a hospital that has verified this outlet's pharmacy. It is
// a read about one patient, so every answer is audited.
func (h *catalogueHandlers) citableEncounters(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	patient, perr := uuid.Parse(c.Query("patient_id"))
	hospital, herr := uuid.Parse(c.Query("hospital_id"))
	if perr != nil || herr != nil || patient == uuid.Nil || hospital == uuid.Nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "patient_id and hospital_id are required")
		return
	}
	found := []citableEncounter{}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		// Fail closed: an unverified hospital or a patient the pharmacy has
		// not received is not found, as the sale itself would refuse it.
		var known int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_registered_patients r
 JOIN pharmacy_outlets o ON o.tenant_id=r.tenant_id AND o.id=?
 JOIN pharmacy_hospital_affiliations a ON a.tenant_id=o.tenant_id AND a.pharmacy_id=o.pharmacy_id AND a.hospital_id=r.hospital_id
 WHERE r.tenant_id=? AND r.patient_id=? AND r.hospital_id=?
  AND (SELECT v.kind FROM pharmacy_affiliation_verifications v WHERE v.tenant_id=a.tenant_id AND v.affiliation_id=a.id ORDER BY v.sequence DESC LIMIT 1)='accepted'`,
			outlet, p.TenantID, patient, hospital).Scan(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Raw(`SELECT encounter_id,status,received_at FROM pharmacy_opd_encounters
 WHERE tenant_id=? AND patient_id=? AND hospital_id=? AND status IN ('arrived','in_consultation','closed')
 ORDER BY received_at DESC, encounter_id DESC LIMIT ?`, p.TenantID, patient, hospital, maxCitableEncounters).Scan(&found).Error; err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "opd_encounters", p.TenantID, p.Subject, outlet, "patient_visits_read",
			"patient_id", patient.String(), "hospital_id", hospital.String())
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, gin.H{"data": found})
}
