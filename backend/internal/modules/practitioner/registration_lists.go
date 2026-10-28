package practitioner

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r registration) PageKey() (time.Time, uuid.UUID)         { return r.CreatedAt, r.ID }
func (r registrationRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func auditIDs(tx *gorm.DB, p authn.Principal, table string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read',?,value::uuid FROM jsonb_array_elements_text(?::jsonb)", p.TenantID, p.Subject, table, string(data)).Error
}
func (h *handlers) listRegistrations(c *gin.Context, params pagination.Params) ([]registration, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	practitioner, ok := resourceID(c, "practitioner")
	if !ok {
		return nil, nil
	}
	var rows []registration
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var owner record
		if e := read(tx, p, practitioner, &owner); e != nil {
			return e
		}
		if e := auditRead(tx, p, practitioner); e != nil {
			return e
		}
		source := tx.Table("practitioner_registrations r").Select(registrationColumns).Joins("JOIN practitioner_registration_revisions v ON v.tenant_id=r.tenant_id AND v.registration_id=r.id AND v.version=r.version").Where("r.tenant_id=? AND r.practitioner_id=?", p.TenantID, practitioner)
		if e := tenantdb.ApplyKeyset(tx.Table("(?) AS registration_page", source), params).Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_registrations", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) listRegistrationRevisions(c *gin.Context, params pagination.Params) ([]registrationRevision, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	practitioner, ok := resourceID(c, "practitioner")
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "registration")
	if !ok {
		return nil, nil
	}
	var rows []registrationRevision
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var current registration
		if e := readRegistration(tx, p, practitioner, id, &current); e != nil {
			return e
		}
		if e := registrationAudit(tx, p, id); e != nil {
			return e
		}
		q := tx.Table("practitioner_registration_revisions").Select(registrationRevisionColumns).Where("tenant_id=? AND practitioner_id=? AND registration_id=?", p.TenantID, practitioner, id)
		if e := tenantdb.ApplyKeyset(q, params).Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_registration_revisions", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
