package medicore

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

// changeAccess grants or revokes one subject's MediCore access at one
// hospital (#80 E3). Hospital ids are references to the tenant's hospital
// directory, as in patientmaster; the grant is MediCore's own.
func (h *encounterHandlers) changeAccess(c *gin.Context, revoke bool) {
	p, _, ok := authn.TenantPrincipal(c)
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
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if revoke {
			return tx.Exec(`DELETE FROM medicore_hospital_access WHERE tenant_id = ? AND hospital_id = ? AND subject = ?`, p.TenantID, hospital, subject).Error
		}
		return tx.Exec(`INSERT INTO medicore_hospital_access (tenant_id, hospital_id, subject, created_by) VALUES (?, ?, ?, ?)
			ON CONFLICT (tenant_id, hospital_id, subject) DO NOTHING`, p.TenantID, hospital, subject, p.Subject).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not change MediCore access")
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospital, "subject": subject, "granted": !revoke})
}

func (h *encounterHandlers) grantAccess(c *gin.Context)  { h.changeAccess(c, false) }
func (h *encounterHandlers) revokeAccess(c *gin.Context) { h.changeAccess(c, true) }

// myAccess answers whether the caller holds a grant at this hospital: 200 if
// so, the same 404 as an unknown hospital if not.
func (h *encounterHandlers) myAccess(c *gin.Context) {
	var hospitalID uuid.UUID
	err := h.inHospital(c, func(_ *gorm.DB, _ authn.Principal, hospital uuid.UUID) error {
		hospitalID = hospital
		return nil
	})
	if err != nil {
		encounterFailure(c, err)
		return
	}
	respond.OK(c, gin.H{"hospital_id": hospitalID, "granted": true})
}
