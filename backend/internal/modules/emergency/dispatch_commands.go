package emergency

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"time"
)

type dispatchCommand struct {
	ReceivingRequestID *uuid.UUID `json:"receiving_request_id,omitempty"`
	ID                 uuid.UUID  `json:"id"`
	DispatchID         uuid.UUID  `json:"dispatch_id"`
	ExpectedVersion    int64      `json:"expected_version"`
	Operation          string     `json:"operation"`
	EvidenceReference  string     `json:"evidence_reference"`
	ActorSubject       string     `json:"actor_subject"`
	State              string     `json:"state" gorm:"column:result_state"`
	Version            int64      `json:"version" gorm:"column:result_version"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (dispatchCommand) TableName() string { return "emergency_dispatch_commands" }

const commandColumns = "id,receiving_request_id,dispatch_id,expected_version,operation,evidence_reference,actor_subject,result_state,result_version,created_at"

func (h *handlers) putDispatchCommand(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	d, ok := resourceID(c, "dispatch")
	if !ok {
		return
	}
	id, ok := resourceID(c, "command")
	if !ok {
		return
	}
	var in DispatchCommandInput
	if !decode(c, &in) {
		return
	}
	crewOperation := in.Operation == "acknowledge" || in.Operation == "en_route" || in.Operation == "on_scene" || receivingRequestOperations[in.Operation]
	permission := PermDispatchManage
	if crewOperation {
		permission = PermCrewAct
	} else if in.Operation == "reconcile_cancelled" || in.Operation == "clear" {
		permission = PermCaseClose
	}
	permissions, ok := authz.PermissionsFrom(c)
	if !ok || !permissions.Has(permission) {
		respond.Forbidden(c, "missing permission "+string(permission))
		return
	}
	if !policyShape.MatchString(in.EvidenceReference) {
		respond.Error(c, 400, "invalid_request", "approved operational evidence reference required")
		return
	}
	var out dispatchCommand
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		ability := "intake"
		if in.Operation == "reconcile_cancelled" || in.Operation == "clear" {
			ability = "close"
		}
		if !crewOperation {
			if err := requireAccess(tx, p, b, ability); err != nil {
				return err
			}
		}
		if err := clinicalBaseLock(tx, p, b, "NO KEY UPDATE"); err != nil {
			return err
		}
		err := tx.Select(commandColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
		if err == nil {
			if !sameOptionalID(out.ReceivingRequestID, in.ReceivingRequestID) || out.DispatchID != d || out.ExpectedVersion != in.Version || out.Operation != in.Operation || out.EvidenceReference != in.EvidenceReference || out.ActorSubject != p.Subject {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if crewOperation {
			var assignment dispatch
			if err := tx.Select(dispatchColumns).Where("tenant_id=? AND base_id=? AND id=? AND ? IN(driver_subject,clinician_subject)", p.TenantID, b, d, p.Subject).First(&assignment).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO emergency_dispatch_commands(tenant_id,base_id,id,dispatch_id,expected_version,operation,evidence_reference,actor_subject,receiving_request_id) VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, b, id, d, in.Version, in.Operation, in.EvidenceReference, p.Subject, in.ReceivingRequestID).Error; err != nil {
			return err
		}
		if err := tx.Select(commandColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error; err != nil {
			return err
		}
		if err := audit(tx, p, b, d, "dispatch."+in.Operation, nil); err != nil {
			return err
		}
		data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: b.String(), DispatchID: d.String(), Version: out.Version})
		if err != nil {
			return err
		}
		return transportEvent(tx, p, emergencycontract.SubjectDispatchChanged, data)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
