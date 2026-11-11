package workforce

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

type businessProfileRow struct {
	Profile *customercontract.OrganizationProfile `gorm:"column:onboarding_profile;serializer:json"`
}

func (h *handlers) getBusinessProfile(c *gin.Context) {
	p, id, ok := profileScope(c)
	if !ok {
		return
	}
	var row businessProfileRow
	err := h.transaction(c.Request.Context(), p, id, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, id, true); err != nil {
			return err
		}
		if err := tx.Table("workforce_hospitals").Select("onboarding_profile").Where("tenant_id=? AND id=? AND establishment_kind='hospital'", p.TenantID, id).Take(&row).Error; err != nil {
			return err
		}
		if row.Profile != nil && row.Profile.Validate() != nil {
			return errors.New("stored organisation profile is invalid")
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'business_profile_read','workforce_hospital',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, customercontract.BusinessProfileView{Profile: row.Profile})
}
