package emergency

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

type fleetConstraint struct {
	ID                uuid.UUID  `json:"id"`
	VehicleID         uuid.UUID  `json:"vehicle_id"`
	Code              string     `json:"code"`
	ClearedAt         *time.Time `json:"cleared_at"`
	ClearedBy         *string    `json:"cleared_by"`
	EvidenceReference *string    `json:"evidence_reference"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (fleetConstraint) TableName() string { return "emergency_fleet_constraints" }
func (h *handlers) clearConstraint(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "check")
	if !ok {
		return
	}
	code := c.Param("code")
	var in struct {
		EvidenceReference string `json:"evidence_reference" binding:"required,max=120"`
	}
	if !decode(c, &in) {
		return
	}
	if !policyShape.MatchString(in.EvidenceReference) {
		respond.Error(c, 400, "invalid_request", "repair evidence reference is required")
		return
	}
	var out fleetConstraint
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "close"); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE emergency_fleet_constraints SET cleared_at=now(),cleared_by=?,evidence_reference=?,updated_at=now() WHERE tenant_id=? AND base_id=? AND id=? AND code=? AND cleared_at IS NULL`, p.Subject, in.EvidenceReference, p.TenantID, b, id, code)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			if err := fleetChanged(tx, p, b, id, "constraint_cleared"); err != nil {
				return err
			}
		}
		if err := tx.Select("id,vehicle_id,code,cleared_at,cleared_by,evidence_reference,created_at,updated_at").Where("tenant_id=? AND base_id=? AND id=? AND code=?", p.TenantID, b, id, code).First(&out).Error; err != nil {
			return err
		}
		if out.ClearedBy == nil || *out.ClearedBy != p.Subject || out.EvidenceReference == nil || *out.EvidenceReference != in.EvidenceReference {
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
