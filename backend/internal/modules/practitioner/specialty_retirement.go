package practitioner

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

type specialtyRetirement struct {
	ID              uuid.UUID `json:"id"`
	SpecialtyID     uuid.UUID `json:"specialty_id"`
	ReviewReference uuid.UUID `json:"review_reference"`
	ReplacementID   *string   `json:"replacement_id"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *specialtyRetirement) AfterFind(*gorm.DB) error { s.CreatedAt = s.CreatedAt.UTC(); return nil }
func specialtyRetirementQuery(tx *gorm.DB, p authn.Principal, id uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_specialty_retirements").Select("id,specialty_id,review_reference,replacement_id::text AS replacement_id,created_at").Where("tenant_id=? AND specialty_id=?", p.TenantID, id)
}
func (h *handlers) retireSpecialty(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	specialty, ok := resourceID(c, "specialty")
	if !ok {
		return
	}
	id, ok := resourceID(c, "retirement")
	if !ok {
		return
	}
	var in *struct {
		ReviewReference string  `json:"review_reference"`
		ReplacementID   *string `json:"replacement_id"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil {
		respond.Error(c, 400, "invalid_request", "Expected a specialty retirement reference")
		return
	}
	reference, valid := canonicalStewardReference(in.ReviewReference)
	if !valid {
		respond.Error(c, 400, "invalid_request", "Expected an opaque review reference")
		return
	}
	if in.ReplacementID != nil {
		replacement, valid := canonicalStewardReference(*in.ReplacementID)
		if !valid || replacement == specialty {
			respond.Error(c, 400, "invalid_request", "Expected a distinct canonical replacement specialty")
			return
		}
	}
	if !h.reauthorize(c, PermSpecialtyManage) {
		return
	}
	var row specialtyRetirement
	created := false
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_specialty_manage','true',true)").Error; err != nil {
			return err
		}
		var locked []uuid.UUID
		if err := tx.Raw("SELECT id FROM practitioner_specialties WHERE tenant_id=? AND (id=? OR id=?::uuid) ORDER BY id FOR UPDATE", p.TenantID, specialty, in.ReplacementID).Scan(&locked).Error; err != nil {
			return err
		}
		if len(locked) == 0 || in.ReplacementID != nil && len(locked) != 2 {
			return gorm.ErrRecordNotFound
		}
		err := specialtyRetirementQuery(tx, p, specialty).Where("id=?", id).Take(&row).Error
		if err == nil {
			if row.ReviewReference != reference || !sameOptionalString(row.ReplacementID, in.ReplacementID) {
				return errIntentConflict
			}
			return auditIDs(tx, p, "practitioner_specialty_retirements", []uuid.UUID{id})
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec("INSERT INTO practitioner_specialty_retirements(tenant_id,id,specialty_id,review_reference,replacement_id,created_by) VALUES(?,?,?,?,?,?)", p.TenantID, id, specialty, reference, in.ReplacementID, p.Subject).Error; err != nil {
			return err
		}
		created = true
		return specialtyRetirementQuery(tx, p, specialty).Where("id=?", id).Take(&row).Error
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *handlers) getSpecialtyRetirement(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	specialty, ok := resourceID(c, "specialty")
	if !ok {
		return
	}
	id, ok := resourceID(c, "retirement")
	if !ok {
		return
	}
	var row specialtyRetirement
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := specialtyRetirementQuery(tx, p, specialty).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_specialty_retirements", []uuid.UUID{id})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
