package practitioner

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (s stewardAppointment) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }

type stewardEvent struct {
	ID              uuid.UUID `json:"id"`
	AppointmentID   uuid.UUID `json:"appointment_id"`
	Version         int64     `json:"version"`
	Status          string    `json:"status"`
	ActorSubject    string    `json:"actor_subject"`
	ReviewReference uuid.UUID `json:"review_reference"`
	CreatedAt       time.Time `json:"created_at"`
}

func (e stewardEvent) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }
func (e *stewardEvent) AfterFind(*gorm.DB) error       { e.CreatedAt = e.CreatedAt.UTC(); return nil }
func (h *handlers) listManagedStewards(c *gin.Context, p pagination.Params) ([]stewardAppointment, error) {
	return h.listStewards(c, p, true)
}
func (h *handlers) listOwnStewards(c *gin.Context, p pagination.Params) ([]stewardAppointment, error) {
	return h.listStewards(c, p, false)
}
func (h *handlers) listStewards(c *gin.Context, params pagination.Params, managed bool) ([]stewardAppointment, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []stewardAppointment
	err := h.stewardTransaction(c.Request.Context(), p, managed, func(tx *gorm.DB) error {
		if e := auditIDs(tx, p, "practitioner_steward_directory", []uuid.UUID{uuid.MustParse(p.TenantID)}); e != nil {
			return e
		}
		q := tx.Select(stewardColumns).Where("tenant_id=?", p.TenantID)
		if !managed {
			q = q.Where("subject=?", p.Subject)
		}
		if e := tenantdb.ApplyKeyset(q, params).Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_steward_appointments", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) managedStewardHistory(c *gin.Context, p pagination.Params) ([]stewardEvent, error) {
	return h.stewardHistory(c, p, true)
}
func (h *handlers) ownStewardHistory(c *gin.Context, p pagination.Params) ([]stewardEvent, error) {
	return h.stewardHistory(c, p, false)
}
func (h *handlers) stewardHistory(c *gin.Context, params pagination.Params, managed bool) ([]stewardEvent, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "appointment")
	if !ok {
		return nil, nil
	}
	var rows []stewardEvent
	err := h.stewardTransaction(c.Request.Context(), p, managed, func(tx *gorm.DB) error {
		var parent stewardAppointment
		if e := loadSteward(tx, p, id, managed, false, &parent); e != nil {
			return e
		}
		if e := stewardAudit(tx, p, id); e != nil {
			return e
		}
		q := tx.Table("practitioner_steward_events").Select("id,appointment_id,version,status,actor_subject,review_reference,created_at").Where("tenant_id=? AND appointment_id=?", p.TenantID, id)
		if e := tenantdb.ApplyKeyset(q, params).Find(&rows).Error; e != nil {
			return e
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_steward_events", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
