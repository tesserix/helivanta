package medicore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// An OPD encounter is a medicore_visits row with a hospital (#80 E1).
type encounter struct {
	ID           uuid.UUID  `gorm:"column:id" json:"id"`
	TenantID     uuid.UUID  `gorm:"column:tenant_id" json:"-"`
	HospitalID   uuid.UUID  `gorm:"column:hospital_id" json:"hospital_id"`
	PatientID    uuid.UUID  `gorm:"column:patient_id" json:"patient_id"`
	Department   string     `gorm:"column:department" json:"-"`
	ConsultMode  string     `gorm:"column:consult_mode" json:"consult_mode"`
	VisitType    string     `gorm:"column:visit_type" json:"visit_type"`
	Status       string     `gorm:"column:status" json:"status"`
	Version      int        `gorm:"column:version" json:"version"`
	CreatedBy    string     `gorm:"column:created_by" json:"created_by"`
	IntentSHA256 string     `gorm:"column:intent_sha256" json:"-"`
	CreatedAt    time.Time  `gorm:"column:created_at" json:"created_at"`
	ClosedAt     *time.Time `gorm:"column:closed_at" json:"closed_at"`
}

func (encounter) TableName() string { return "medicore_visits" }

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (e encounter) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

var (
	errNoHospital      = errors.New("hospital not found")
	errNoEncounter     = errors.New("encounter not found")
	errNoPatient       = errors.New("patient not found at this hospital")
	errIntentConflict  = errors.New("this encounter id was already used for a different encounter")
	errStaleEncounter  = errors.New("the encounter changed since it was read")
	errIllegalMove     = errors.New("that status change is not allowed from the encounter's current status")
	errReasonRequired  = errors.New("this status change needs a reason")
	terminalStatuses   = map[string]bool{"closed": true, "left_without_being_seen": true, "cancelled": true}
	encounterStatuses  = map[string]bool{"arrived": true, "in_consultation": true, "closed": true, "left_without_being_seen": true, "cancelled": true}
	legalTransitions   = map[string]map[string]bool{"arrived": {"in_consultation": true, "left_without_being_seen": true, "cancelled": true}, "in_consultation": {"closed": true}}
	transitionsWithWhy = map[string]bool{"left_without_being_seen": true, "cancelled": true}
)

type encounterHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// inHospital runs fn in one tenant transaction after checking the caller's
// MediCore grant at that hospital in the same transaction, so a revoked
// grant takes effect on the next request (#80 E3). No grant, or a hospital
// that is not a UUID, is the same 404 as a hospital that does not exist.
func (h *encounterHandlers) inHospital(c *gin.Context, fn func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error) error {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return errAborted
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		return errNoHospital
	}
	return h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var granted int64
		if err := tx.Table("medicore_hospital_access").Where("tenant_id = ? AND hospital_id = ? AND subject = ?", p.TenantID, hospital, p.Subject).Count(&granted).Error; err != nil {
			return err
		}
		if granted == 0 {
			return errNoHospital
		}
		return fn(tx, p, hospital)
	})
}

var errAborted = errors.New("request already answered")

func encounterFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errAborted):
	case errors.Is(err, errNoHospital):
		respond.NotFound(c, "hospital")
	case errors.Is(err, errNoEncounter):
		respond.NotFound(c, "encounter")
	case errors.Is(err, errNoPatient):
		respond.NotFound(c, "patient")
	case errors.Is(err, errIntentConflict), errors.Is(err, errStaleEncounter), errors.Is(err, errIllegalMove):
		respond.Conflict(c, err.Error())
	case errors.Is(err, errReasonRequired):
		respond.BadRequest(c, err)
	default:
		respond.InternalErr(c, err, "could not complete the encounter request")
	}
}

type openEncounterRequest struct {
	PatientID   string `json:"patient_id" binding:"required,uuid"`
	ConsultMode string `json:"consult_mode" binding:"required,oneof=in_person teleconsultation"`
	VisitType   string `json:"visit_type" binding:"required,oneof=first follow_up"`
}

// open records an encounter under a client-chosen UUIDv7, idempotently: the
// same intent replays the stored encounter, a different one is 409 (#80 E4).
func (h *encounterHandlers) open(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "encounter")
		return
	}
	var req openEncounterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	patient := uuid.MustParse(req.PatientID)
	var row encounter
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		sum := sha256.Sum256([]byte(strings.Join([]string{hospital.String(), patient.String(), req.ConsultMode, req.VisitType}, "|")))
		digest := hex.EncodeToString(sum[:])
		err := tx.Where("tenant_id = ? AND id = ?", p.TenantID, id).Take(&row).Error
		if err == nil {
			if row.HospitalID != hospital || row.IntentSHA256 != digest || row.CreatedBy != p.Subject {
				return errIntentConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// The patient must be registered at this hospital, as patientmaster
		// announced (#80 E2). The foreign key enforces the same; this check
		// turns it into a 404 instead of a constraint error.
		var known int64
		if err := tx.Table("medicore_patients").Where("tenant_id = ? AND patient_id = ? AND hospital_id = ?", p.TenantID, patient, hospital).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoPatient
		}
		row = encounter{
			ID: id, TenantID: uuid.MustParse(p.TenantID), HospitalID: hospital, PatientID: patient, Department: "OPD",
			ConsultMode: req.ConsultMode, VisitType: req.VisitType, Status: "arrived", Version: 1,
			CreatedBy: p.Subject, IntentSHA256: digest,
		}
		if err := tx.Omit("created_at", "closed_at").Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND id = ?", p.TenantID, id).Take(&row).Error; err != nil {
			return err
		}
		created = true
		return publish(tx, p.TenantID, medicorecontract.SubjectEncounterOpened, "EncounterOpened", medicorecontract.EncounterOpenedData{
			EncounterID: row.ID.String(), HospitalID: hospital.String(), PatientID: patient.String(),
		})
	})
	if err != nil {
		encounterFailure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

// list returns one page of a hospital's encounters, newest first; status
// narrows it. Legacy visits (no hospital) never appear here.
func (h *encounterHandlers) list(c *gin.Context, params pagination.Params) ([]encounter, error) {
	status := c.Query("status")
	if status != "" && !encounterStatuses[status] {
		respond.BadRequest(c, fmt.Errorf("status must be one of arrived, in_consultation, closed, left_without_being_seen, cancelled"))
		return nil, nil
	}
	var rows []encounter
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		q := tenantdb.ApplyKeyset(tx, params).Where("tenant_id = ? AND hospital_id = ?", p.TenantID, hospital)
		if status != "" {
			q = q.Where("status = ?", status)
		}
		return q.Find(&rows).Error
	})
	if err != nil {
		encounterFailure(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *encounterHandlers) get(c *gin.Context) {
	var row encounter
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return findEncounter(tx, p, hospital, c.Param("id"), false, &row)
	})
	if err != nil {
		encounterFailure(c, err)
		return
	}
	respond.OK(c, row)
}

func findEncounter(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, raw string, lock bool, row *encounter) error {
	id, err := uuid.Parse(raw)
	if err != nil {
		return errNoEncounter
	}
	q := tx.Where("tenant_id = ? AND hospital_id = ? AND id = ?", p.TenantID, hospital, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errNoEncounter
		}
		return err
	}
	return nil
}

type transitionRequest struct {
	To              string `json:"to" binding:"required"`
	ExpectedVersion int    `json:"expected_version" binding:"required,min=1"`
	Reason          string `json:"reason" binding:"max=500"`
}

// transition moves an encounter along its fixed lifecycle (#80 E5). The
// database guard re-checks the same table, so no other path can move it.
func (h *encounterHandlers) transition(c *gin.Context) {
	var req transitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	var row encounter
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := findEncounter(tx, p, hospital, c.Param("id"), true, &row); err != nil {
			return err
		}
		if row.Version != req.ExpectedVersion {
			return errStaleEncounter
		}
		if !legalTransitions[row.Status][req.To] {
			return errIllegalMove
		}
		if transitionsWithWhy[req.To] && reason == "" {
			return errReasonRequired
		}
		from, next := row.Status, row.Version+1
		var why *string
		if reason != "" {
			why = &reason
		}
		if err := tx.Exec(`INSERT INTO medicore_encounter_transitions (tenant_id, encounter_id, version, from_status, to_status, reason, actor_subject)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, p.TenantID, row.ID, next, from, req.To, why, p.Subject).Error; err != nil {
			return err
		}
		closedAt := gorm.Expr("NULL")
		if terminalStatuses[req.To] {
			closedAt = gorm.Expr("now()")
		}
		if err := tx.Table("medicore_visits").Where("tenant_id = ? AND id = ? AND version = ?", p.TenantID, row.ID, row.Version).
			Updates(map[string]any{"status": req.To, "version": next, "closed_at": closedAt}).Error; err != nil {
			return err
		}
		if err := findEncounter(tx, p, hospital, row.ID.String(), false, &row); err != nil {
			return err
		}
		return publish(tx, p.TenantID, medicorecontract.SubjectEncounterStatusChanged, "EncounterStatusChanged", medicorecontract.EncounterStatusChangedData{
			EncounterID: row.ID.String(), HospitalID: hospital.String(), FromStatus: from, ToStatus: row.Status, Version: row.Version,
		})
	})
	if err != nil {
		encounterFailure(c, err)
		return
	}
	respond.OK(c, row)
}

func publish(tx *gorm.DB, tenant, subject, typ string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", subject, err)
	}
	return events.Enqueue(tx, subject, events.Event{Type: typ, Version: 1, TenantID: tenant, Data: data})
}

// patientRegistered records which hospital a patient is registered at, so an
// encounter can prove its patient belongs there (#80 E2). Redelivery is a
// no-op. A malformed event is an error, not a skip: the bus retries it and
// it is never silently dropped.
func patientRegistered(_ context.Context, tx *gorm.DB, evt events.Event) error {
	var d patientmastercontract.PatientRegisteredData
	if evt.Version != 1 || json.Unmarshal(evt.Data, &d) != nil {
		return errors.New("invalid patient registration event")
	}
	patient, err := uuid.Parse(d.PatientID)
	if err != nil || patient == uuid.Nil {
		return errors.New("invalid patient reference")
	}
	hospital, err := uuid.Parse(d.HospitalID)
	if err != nil || hospital == uuid.Nil {
		return errors.New("invalid hospital reference")
	}
	return tx.Exec(`INSERT INTO medicore_patients (tenant_id, patient_id, hospital_id) VALUES (?, ?, ?)
		ON CONFLICT (tenant_id, patient_id) DO NOTHING`, evt.TenantID, patient, hospital).Error
}
