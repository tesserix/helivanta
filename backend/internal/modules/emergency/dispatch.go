package emergency

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

var errDispatchDisabled = errors.New("dispatch disabled")

type dispatch struct {
	OwnAcknowledged         *bool      `json:"own_acknowledged,omitempty" gorm:"->"`
	BaseID                  uuid.UUID  `json:"base_id"`
	DriverAcknowledged      bool       `json:"driver_acknowledged"`
	ClinicianAcknowledged   bool       `json:"clinician_acknowledged"`
	AcknowledgementDeadline *time.Time `json:"acknowledgement_deadline"`
	ID                      uuid.UUID  `json:"id"`
	DispatchReservationInput
	RequestedBy string    `json:"requested_by"`
	State       string    `json:"state"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (dispatch) TableName() string { return "emergency_dispatches" }

const dispatchColumns = "id,base_id,driver_acknowledged,clinician_acknowledged,acknowledgement_deadline,case_id,case_version,assessment_id,vehicle_id,duty_id,acknowledgement_seconds,requested_by,state,version,created_at,updated_at"

func (h *handlers) putDispatch(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "dispatch")
	if !ok {
		return
	}
	var in DispatchReservationInput
	if !decode(c, &in) {
		return
	}
	for _, v := range []uuid.UUID{in.CaseID, in.AssessmentID, in.VehicleID, in.DutyID} {
		if v.Version() != 7 {
			respond.Error(c, 400, "invalid_request", "invalid reservation references")
			return
		}
	}
	var out dispatch
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "intake"); err != nil {
			return err
		}
		if !h.dispatchEnabled {
			return errDispatchDisabled
		}
		// Serialize retries of the same command before eligibility can change.
		if err := clinicalBaseLock(tx, p, b, "NO KEY UPDATE"); err != nil {
			return err
		}
		err := tx.Select(dispatchColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
		if err == nil {
			if out.DispatchReservationInput != in || out.RequestedBy != p.Subject {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		result := tx.Exec(`INSERT INTO emergency_dispatches(tenant_id,base_id,id,case_id,case_version,assessment_id,vehicle_id,duty_id,acknowledgement_seconds,requested_by,driver_subject,clinician_subject)
   SELECT ?,?,?,?,?,?,?,?,?,?,d.driver_subject,d.clinician_subject FROM emergency_duties d
   WHERE d.tenant_id=? AND d.base_id=? AND d.id=? AND d.vehicle_id=?`, p.TenantID, b, id, in.CaseID, in.CaseVersion, in.AssessmentID, in.VehicleID, in.DutyID, in.AcknowledgementSeconds, p.Subject, p.TenantID, b, in.DutyID, in.VehicleID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errCaseConflict
		}
		if err := tx.Select(dispatchColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		if err := audit(tx, p, b, id, "dispatch.reserved", nil); err != nil {
			return err
		}
		data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: b.String(), DispatchID: id.String(), Version: 1})
		if err != nil {
			return err
		}
		return transportEvent(tx, p, emergencycontract.SubjectDispatchChanged, data)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	// This immutable receipt describes acceptance, not the current crew lifecycle.
	out.DriverAcknowledged = false
	out.ClinicianAcknowledged = false
	out.AcknowledgementDeadline = nil
	out.State = "workflow_pending"
	out.Version = 1
	out.UpdatedAt = out.CreatedAt
	respond.Accepted(c, out)
}
