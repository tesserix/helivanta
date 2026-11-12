package pharmacy

import (
	_ "embed"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
)

//go:embed business_profile.sql
var businessProfileSchema string

func (h *catalogueHandlers) getBusinessProfile(c *gin.Context) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return
	}
	id, ok := registryID(c, "pharmacy")
	if !ok {
		return
	}
	var row pharmacyOrganization
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, id, registryManager(c)); err != nil {
			return err
		}
		if err := tx.Select("id,onboarding_profile").Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; err != nil {
			return err
		}
		if row.OnboardingProfile != nil && row.OnboardingProfile.Validate() != nil {
			return errors.New("stored organisation profile is invalid")
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'business_profile_read','pharmacy_organization',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		catalogueError(c, err)
		return
	}
	respond.OK(c, customercontract.BusinessProfileView{Profile: row.OnboardingProfile})
}
