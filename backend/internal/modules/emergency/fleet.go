package emergency

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VehicleInput is the versioned, reviewed vehicle configuration.
type VehicleInput struct {
	Version          int64     `json:"version" binding:"min=0"`
	Code             string    `json:"code" binding:"required,max=40"`
	VehicleClass     string    `json:"vehicle_class" binding:"required,oneof=patient_transport bls als"`
	CertifiedUntil   time.Time `json:"certified_until" binding:"required"`
	PolicyReference  string    `json:"policy_reference" binding:"required,max=120"`
	OxygenMinBar     int       `json:"oxygen_min_bar" binding:"min=1,max=300"`
	ReadinessMinutes int       `json:"readiness_minutes" binding:"min=1,max=1440"`
	Retired          bool      `json:"retired"`
}
type vehicle struct {
	ID     uuid.UUID `json:"id"`
	BaseID uuid.UUID `json:"base_id"`
	VehicleInput
	Dispatchable        bool            `json:"dispatchable"`
	EffectiveCapability *string         `json:"effective_capability"`
	Blockers            json.RawMessage `json:"blockers" gorm:"type:jsonb"`
	OpenConstraints     json.RawMessage `json:"open_constraints" gorm:"type:jsonb"`
	ComputedAt          time.Time       `json:"computed_at"`
	DutyID              *uuid.UUID      `json:"duty_id"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

func (vehicle) TableName() string                 { return "emergency_vehicles" }
func (v vehicle) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }

const vehicleColumns = "id,base_id,version,code,vehicle_class,certified_until,policy_reference,oxygen_min_bar,readiness_minutes,retired,created_at,updated_at"

func resourceID(c *gin.Context, param string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "resource")
		return uuid.Nil, false
	}
	return id, true
}
func (h *handlers) putVehicle(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "vehicle")
	if !ok {
		return
	}
	var in VehicleInput
	if !decode(c, &in) {
		return
	}
	if strings.TrimSpace(in.Code) != in.Code || !policyShape.MatchString(in.PolicyReference) {
		respond.Error(c, 400, "invalid_request", "invalid fleet fields")
		return
	}
	var out vehicle
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO emergency_vehicles(tenant_id,base_id,id,code,vehicle_class,certified_until,policy_reference,oxygen_min_bar,readiness_minutes,retired)
 SELECT ?,?,?,?,?,?,?,?,?,? WHERE ?=0
 ON CONFLICT(tenant_id,base_id,id) DO NOTHING`, p.TenantID, b, id, in.Code, in.VehicleClass, in.CertifiedUntil, in.PolicyReference, in.OxygenMinBar, in.ReadinessMinutes, in.Retired, in.Version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 && in.Version > 0 {
			result = tx.Exec(`UPDATE emergency_vehicles SET code=?,vehicle_class=?,certified_until=?,policy_reference=?,oxygen_min_bar=?,readiness_minutes=?,retired=?,version=version+1,updated_at=now()
 WHERE tenant_id=? AND base_id=? AND id=? AND version=?`, in.Code, in.VehicleClass, in.CertifiedUntil, in.PolicyReference, in.OxygenMinBar, in.ReadinessMinutes, in.Retired, p.TenantID, b, id, in.Version)
			if result.Error != nil {
				return result.Error
			}
		}
		if result.RowsAffected > 0 {
			if err := fleetChanged(tx, p, b, id, "vehicle_configured"); err != nil {
				return err
			}
		}
		if err := tx.Select(vehicleColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		if out.Version != in.Version+1 || out.Code != in.Code || out.VehicleClass != in.VehicleClass || !out.CertifiedUntil.Equal(in.CertifiedUntil) || out.PolicyReference != in.PolicyReference || out.OxygenMinBar != in.OxygenMinBar || out.ReadinessMinutes != in.ReadinessMinutes || out.Retired != in.Retired {
			return errCaseConflict
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) listVehicles(c *gin.Context, params pagination.Params) ([]vehicle, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	var rows []vehicle
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx.Table("emergency_fleet_status"), params).Select(vehicleColumns+`, dispatchable,effective_capability,computed_at,blockers,duty_id,open_constraints`).Where("tenant_id=? AND base_id=?", p.TenantID, b).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

// CrewInput records a supervisor's verification, not a workforce-issued credential.
type CrewInput struct {
	Version           int64     `json:"version" binding:"min=0"`
	Subject           string    `json:"subject" binding:"required,max=200"`
	Qualification     string    `json:"qualification" binding:"required,oneof=driver emt paramedic"`
	CertifiedUntil    time.Time `json:"certified_until" binding:"required"`
	EvidenceReference string    `json:"evidence_reference" binding:"required,max=120"`
	Suspended         bool      `json:"suspended"`
}
type crew struct {
	ID uuid.UUID `json:"id"`
	CrewInput
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (crew) TableName() string                 { return "emergency_crew" }
func (v crew) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }

const crewColumns = "id,subject,qualification,certified_until,evidence_reference,suspended,version,created_at,updated_at"

func (h *handlers) putCrew(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "crew")
	if !ok {
		return
	}
	var in CrewInput
	if !decode(c, &in) {
		return
	}
	if !cleanText(in.Subject) || !policyShape.MatchString(in.EvidenceReference) {
		respond.Error(c, 400, "invalid_request", "invalid credential fields")
		return
	}
	var out crew
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO emergency_crew(tenant_id,base_id,id,subject,qualification,certified_until,evidence_reference,suspended) SELECT ?,?,?,?,?,?,?,? WHERE ?=0 ON CONFLICT(tenant_id,base_id,id) DO NOTHING`, p.TenantID, b, id, in.Subject, in.Qualification, in.CertifiedUntil, in.EvidenceReference, in.Suspended, in.Version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 && in.Version > 0 {
			result = tx.Exec(`UPDATE emergency_crew SET qualification=?,certified_until=?,evidence_reference=?,suspended=?,version=version+1,updated_at=now() WHERE tenant_id=? AND base_id=? AND id=? AND subject=? AND version=?`, in.Qualification, in.CertifiedUntil, in.EvidenceReference, in.Suspended, p.TenantID, b, id, in.Subject, in.Version)
			if result.Error != nil {
				return result.Error
			}
		}
		if result.RowsAffected > 0 {
			if err := fleetChanged(tx, p, b, id, "credential_verified"); err != nil {
				return err
			}
		}
		if err := tx.Select(crewColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		if out.Version != in.Version+1 || out.Subject != in.Subject || out.Qualification != in.Qualification || !out.CertifiedUntil.Equal(in.CertifiedUntil) || out.EvidenceReference != in.EvidenceReference || out.Suspended != in.Suspended {
			return errCaseConflict
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) listCrew(c *gin.Context, params pagination.Params) ([]crew, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	var rows []crew
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(crewColumns).Where("tenant_id=? AND base_id=?", p.TenantID, b).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

// DutyInput binds two separately verified people to one vehicle for a bounded shift.
type DutyInput struct {
	VehicleID        uuid.UUID `json:"vehicle_id" binding:"required"`
	DriverSubject    string    `json:"driver_subject" binding:"required,max=200"`
	ClinicianSubject string    `json:"clinician_subject" binding:"required,max=200"`
	EndsAt           time.Time `json:"ends_at" binding:"required"`
}
type duty struct {
	ID uuid.UUID `json:"id"`
	DutyInput
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (duty) TableName() string                 { return "emergency_duties" }
func (d duty) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

const dutyColumns = "id,vehicle_id,driver_subject,clinician_subject,ends_at,ended_at,created_at,updated_at"

func (h *handlers) putDuty(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "duty")
	if !ok {
		return
	}
	var in DutyInput
	if !decode(c, &in) {
		return
	}
	if in.VehicleID.Version() != 7 || !cleanText(in.DriverSubject) || !cleanText(in.ClinicianSubject) || in.DriverSubject == in.ClinicianSubject {
		respond.Error(c, 400, "invalid_request", "invalid duty fields")
		return
	}
	var out duty
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		var v vehicle
		if err := tx.Select(vehicleColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, in.VehicleID).First(&v).Error; err != nil {
			return err
		}
		var existing duty
		err := tx.Select(dutyColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&existing).Error
		if err == nil {
			out = existing
			if out.VehicleID != in.VehicleID || out.DriverSubject != in.DriverSubject || out.ClinicianSubject != in.ClinicianSubject || !out.EndsAt.Equal(in.EndsAt) {
				return errCaseConflict
			}
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}

		result := tx.Exec(`INSERT INTO emergency_duties(tenant_id,base_id,id,vehicle_id,driver_subject,clinician_subject,ends_at)
 SELECT ?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM emergency_crew WHERE tenant_id=? AND base_id=? AND subject=? AND qualification='driver' AND NOT suspended AND certified_until>now())
 AND EXISTS(SELECT 1 FROM emergency_crew WHERE tenant_id=? AND base_id=? AND subject=? AND qualification IN('emt','paramedic') AND NOT suspended AND certified_until>now())`, p.TenantID, b, id, in.VehicleID, in.DriverSubject, in.ClinicianSubject, in.EndsAt, p.TenantID, b, in.DriverSubject, p.TenantID, b, in.ClinicianSubject)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errCaseConflict
		}
		if err := tx.Exec(`INSERT INTO emergency_duty_members(tenant_id,base_id,duty_id,subject) VALUES(?,?,?,?),(?,?,?,?)`, p.TenantID, b, id, in.DriverSubject, p.TenantID, b, id, in.ClinicianSubject).Error; err != nil {
			return err
		}
		if err := fleetChanged(tx, p, b, id, "duty_started"); err != nil {
			return err
		}
		return tx.Select(dutyColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) endDuty(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "duty")
	if !ok {
		return
	}
	var in struct{}
	if !decode(c, &in) {
		return
	}
	var out duty
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE emergency_duties SET ended_at=now(),updated_at=now() WHERE tenant_id=? AND base_id=? AND id=? AND ended_at IS NULL`, p.TenantID, b, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			if err := tx.Exec(`UPDATE emergency_duty_members SET ended_at=d.ended_at,updated_at=now() FROM emergency_duties d WHERE emergency_duty_members.tenant_id=d.tenant_id AND emergency_duty_members.base_id=d.base_id AND emergency_duty_members.duty_id=d.id AND d.tenant_id=? AND d.base_id=? AND d.id=? AND emergency_duty_members.ended_at IS NULL`, p.TenantID, b, id).Error; err != nil {
				return err
			}
			if err := fleetChanged(tx, p, b, id, "duty_ended"); err != nil {
				return err
			}
		}
		return tx.Select(dutyColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) listDuties(c *gin.Context, params pagination.Params) ([]duty, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	var rows []duty
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(dutyColumns).Where("tenant_id=? AND base_id=?", p.TenantID, b).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}

// ReadinessInput is the server-attributed observation under a reviewed vehicle policy.
type ReadinessInput struct {
	VehicleID           uuid.UUID `json:"vehicle_id" binding:"required"`
	VehicleVersion      int64     `json:"vehicle_version" binding:"min=1"`
	DutyID              uuid.UUID `json:"duty_id" binding:"required"`
	OxygenBar           *int      `json:"oxygen_bar" binding:"required,min=0,max=300"`
	DefibrillatorPassed *bool     `json:"defibrillator_passed" binding:"required"`
	EquipmentComplete   *bool     `json:"equipment_complete" binding:"required"`
	Roadworthy          *bool     `json:"roadworthy" binding:"required"`
}
type readiness struct {
	ID uuid.UUID `json:"id"`
	ReadinessInput
	ActorSubject string    `json:"actor_subject"`
	ValidUntil   time.Time `json:"valid_until"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (readiness) TableName() string { return "emergency_readiness" }

const readinessColumns = "id,vehicle_id,vehicle_version,duty_id,oxygen_bar,defibrillator_passed,equipment_complete,roadworthy,actor_subject,valid_until,created_at,updated_at"

func (h *handlers) putReadiness(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "check")
	if !ok {
		return
	}
	var in ReadinessInput
	if !decode(c, &in) {
		return
	}
	if in.VehicleID.Version() != 7 || in.DutyID.Version() != 7 {
		respond.Error(c, 400, "invalid_request", "invalid readiness fields")
		return
	}
	var out readiness
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "intake"); err != nil {
			return err
		}
		var v vehicle
		if err := tx.Select(vehicleColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, in.VehicleID).First(&v).Error; err != nil {
			return err
		}
		var existing readiness
		err := tx.Select(readinessColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&existing).Error
		if err == nil {
			out = existing
			if out.VehicleID != in.VehicleID || out.VehicleVersion != in.VehicleVersion || out.DutyID != in.DutyID || *out.OxygenBar != *in.OxygenBar || *out.DefibrillatorPassed != *in.DefibrillatorPassed || *out.EquipmentComplete != *in.EquipmentComplete || *out.Roadworthy != *in.Roadworthy || out.ActorSubject != p.Subject {
				return errCaseConflict
			}
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}

		result := tx.Exec(`INSERT INTO emergency_readiness(tenant_id,base_id,id,vehicle_id,vehicle_version,duty_id,oxygen_bar,defibrillator_passed,equipment_complete,roadworthy,actor_subject,valid_until)
 SELECT ?,?,?,?,?,?,?,?,?,?,?,LEAST(now()+v.readiness_minutes*interval '1 minute',d.ends_at,v.certified_until,driver.certified_until,medic.certified_until)
 FROM emergency_vehicles v JOIN emergency_duties d ON d.tenant_id=v.tenant_id AND d.base_id=v.base_id AND d.vehicle_id=v.id
 JOIN emergency_crew driver ON driver.tenant_id=d.tenant_id AND driver.base_id=d.base_id AND driver.subject=d.driver_subject
 JOIN emergency_crew medic ON medic.tenant_id=d.tenant_id AND medic.base_id=d.base_id AND medic.subject=d.clinician_subject
 WHERE v.tenant_id=? AND v.base_id=? AND v.id=? AND v.version=? AND NOT v.retired AND v.certified_until>now()
 AND d.id=? AND d.ended_at IS NULL AND d.ends_at>now() AND driver.certified_until>now() AND NOT driver.suspended AND driver.qualification='driver'
 AND medic.certified_until>now() AND NOT medic.suspended AND medic.qualification IN('emt','paramedic')`, p.TenantID, b, id, in.VehicleID, in.VehicleVersion, in.DutyID, *in.OxygenBar, *in.DefibrillatorPassed, *in.EquipmentComplete, *in.Roadworthy, p.Subject, p.TenantID, b, in.VehicleID, in.VehicleVersion, in.DutyID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errCaseConflict
		}
		if err := fleetChanged(tx, p, b, id, "readiness_recorded"); err != nil {
			return err
		}
		return tx.Select(readinessColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
