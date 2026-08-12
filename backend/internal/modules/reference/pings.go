package reference

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type ping struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func (ping) TableName() string { return "reference_pings" }

type pingRequest struct {
	Message string `json:"message" binding:"required,max=500"`
}

type pingedData struct {
	PingID string `json:"ping_id"`
}

type pingHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// create records a ping and publishes pinged for the receipt consumer.
func (h *pingHandlers) create(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var req pingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	row := ping{TenantID: tenantUUID, Message: req.Message}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pingedData{PingID: row.ID.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, SubjectPinged, events.Event{
			Type: "ReferencePinged", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not record ping")
		return
	}
	respond.Accepted(c, gin.H{"id": row.ID.String()})
}

// list returns this tenant's pings, newest first.
func (h *pingHandlers) list(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var rows []ping
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not list pings")
		return
	}
	respond.OK(c, gin.H{"data": rows})
}

// get loads a single ping by id, scoped to the caller's tenant.
func (h *pingHandlers) get(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "ping")
		return
	}
	var row ping
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.First(&row, "id = ?", id).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// RLS filters cross-tenant rows → identical 404 (issue #2).
		respond.NotFound(c, "ping")
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "could not load ping")
		return
	}
	respond.OK(c, row)
}
