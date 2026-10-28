package patientmaster

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

func (h *patientHandlers) putAccess(c *gin.Context)    { h.changeAccess(c, false) }
func (h *patientHandlers) deleteAccess(c *gin.Context) { h.changeAccess(c, true) }

func (h *patientHandlers) changeAccess(c *gin.Context, revoke bool) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "hospital")
		return
	}
	subject := c.Param("subject")
	if strings.TrimSpace(subject) == "" || len(subject) > 200 {
		respond.NotFound(c, "subject")
		return
	}
	var input struct {
		Level string `json:"level" binding:"required,oneof=reader registrar"`
	}
	if !revoke && !decodePatientInput(c, &input) {
		return
	}
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		action := "grant"
		if revoke {
			action = "revoke"
			if err := tx.Exec(`DELETE FROM patient_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ?`, p.TenantID, hospital, subject).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Exec(`INSERT INTO patient_hospital_access (tenant_id, hospital_id, subject, level) VALUES (?, ?, ?, ?)
    ON CONFLICT (tenant_id, hospital_id, subject) DO UPDATE SET level = EXCLUDED.level, updated_at = now()
    WHERE patient_hospital_access.level IS DISTINCT FROM EXCLUDED.level`, p.TenantID, hospital, subject, input.Level).Error; err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO patient_access_audit (tenant_id, hospital_id, actor_subject, action, target_subject, access_level) VALUES (?, ?, ?, ?, ?, ?)`, p.TenantID, hospital, p.Subject, action, subject, input.Level).Error
	})
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospital, "subject": subject, "level": input.Level})
}

func (h *patientHandlers) getAccess(c *gin.Context) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "hospital")
		return
	}
	var grant struct {
		Level string `json:"level"`
	}
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, false, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT level FROM patient_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ?`, p.TenantID, hospital, p.Subject).Scan(&grant).Error
	})
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, grant)
}
