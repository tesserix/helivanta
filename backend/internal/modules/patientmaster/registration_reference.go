package patientmaster

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

func (h *patientHandlers) registrationReference(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	principal, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	selectors := c.Request.URL.Query()["hospital_id"]
	if len(selectors) != 1 {
		respond.NotFound(c, "patient")
		return
	}
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	if hospital.String() != selectors[0] {
		respond.NotFound(c, "patient")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil || id.String() != c.Param("id") || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "patient")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	var reference struct {
		ID         uuid.UUID
		HospitalID uuid.UUID
	}
	err = h.hospitalTransaction(ctx, principal, hospital, false, func(tx *gorm.DB) error {
		if err := tx.Table("patients").Select("id,hospital_id").Where("tenant_id=? AND hospital_id=? AND id=?", principal.TenantID, hospital, id).Take(&reference).Error; err != nil {
			return err
		}
		return patientAudit(tx, principal, hospital, "read", []patient{{ID: reference.ID}})
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "patient")
		return
	}
	if err != nil {
		// A reference is withheld whenever its current visibility or audit cannot be proved.
		requestid.Logger(c).Error("patient registration reference unavailable")
		respond.Error(c, 503, "registration_reference_unavailable", "Current registration visibility cannot be confirmed; retry the reference read")
		return
	}
	respond.OK(c, patientmastercontract.RegistrationReference{PatientID: reference.ID.String(), HospitalID: reference.HospitalID.String(), RecordAccessAuthorized: false})
}
