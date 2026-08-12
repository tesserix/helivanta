package pharmacy

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type dispense struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	Medication  string     `json:"medication"`
	Status      string     `gorm:"default:pending" json:"status"`
	DispensedAt *time.Time `json:"dispensed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (dispense) TableName() string { return "pharmacy_dispenses" }

type dispenseRequest struct {
	Medication string `json:"medication" binding:"required,max=200"`
}

type dispenseRecordedData struct {
	DispenseID string `json:"dispense_id"`
	VisitID    string `json:"visit_id"`
}

type dispenseHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// list returns this tenant's dispenses, newest first.
func (h *dispenseHandlers) list(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var rows []dispense
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not list dispenses")
		return
	}
	respond.OK(c, gin.H{"data": rows})
}

// fulfil marks a pending dispense dispensed. The guarded UPDATE is what
// makes concurrent fulfilment safe: the loser matches zero rows and gets
// a 409 rather than double-dispensing.
func (h *dispenseHandlers) fulfil(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "dispense")
		return
	}
	var req dispenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	var status int
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var row dispense
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Status != "pending" {
			status = http.StatusConflict
			return nil
		}
		now := time.Now().UTC()
		result := tx.Model(&dispense{}).Where("id = ? AND status = 'pending'", id).
			Updates(map[string]any{"status": "dispensed", "medication": req.Medication, "dispensed_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// Lost the race to a concurrent dispense between the
			// pre-check above and this guarded UPDATE.
			status = http.StatusConflict
			return nil
		}
		data, err := json.Marshal(dispenseRecordedData{DispenseID: row.ID.String(), VisitID: row.VisitID.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, SubjectDispenseRecorded, events.Event{
			Type: "DispenseRecorded", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "dispense")
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "could not dispense")
		return
	}
	if status == http.StatusConflict {
		respond.Conflict(c, "already dispensed")
		return
	}
	respond.OK(c, gin.H{"id": id.String(), "status": "dispensed"})
}
