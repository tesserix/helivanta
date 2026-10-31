package workforce

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"gorm.io/gorm"
)

func (h *handlers) documentRecords(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, ok := documentStaff(c)
	if !ok {
		return
	}
	choices := []documentRecordChoice{{RecordKind: "enrollment", RecordID: staffID, Label: "Staff enrollment", Categories: documentCategoryBindings["enrollment"]}}
	err := h.documentTransaction(c, staffDocuments{}, p, hospital, false, func(tx *gorm.DB) error {
		if _, err := documentOwner(tx, p, hospital, staffID); err != nil {
			return err
		}
		var credentials []struct {
			ID     uuid.UUID
			Type   string
			Issuer string
		}
		if err := tx.Table("workforce_enrollment_credentials").Select("id,type,issuer").Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID).Order("type,id").Limit(20).Find(&credentials).Error; err != nil {
			return err
		}
		for _, record := range credentials {
			choices = append(choices, documentRecordChoice{RecordKind: "credential", RecordID: record.ID, Label: record.Type + " · " + record.Issuer, Categories: documentCategoryBindings["credential"]})
		}
		var qualifications []struct {
			ID            uuid.UUID
			Qualification string
			Institution   string
		}
		if err := tx.Table("workforce_enrollment_qualifications").Select("id,qualification,institution").Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID).Order("qualification,id").Limit(20).Find(&qualifications).Error; err != nil {
			return err
		}
		for _, record := range qualifications {
			choices = append(choices, documentRecordChoice{RecordKind: "qualification", RecordID: record.ID, Label: record.Qualification + " · " + record.Institution, Categories: documentCategoryBindings["qualification"]})
		}
		return nil
	})
	if documentUploadError(c, err) {
		return
	}
	respond.OK(c, gin.H{"records": choices, "uploads_available": h.documents != nil})
}
