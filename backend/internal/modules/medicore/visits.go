package medicore

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type visit struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID `json:"-"`
	PatientName string    `json:"patient_name"`
	Department  string    `json:"department"`
	Status      string    `gorm:"default:open" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

func (visit) TableName() string { return "medicore_visits" }

type createVisitRequest struct {
	PatientName string `json:"patient_name" binding:"required,max=200"`
	Department  string `json:"department" binding:"required,oneof=OPD IPD"`
}

// VisitCreatedData is the v1 payload of visit_created.
type VisitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
	Department  string `json:"department"`
}

type visitHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// create opens a visit and publishes visit_created for downstream intake.
func (h *visitHandlers) create(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var req createVisitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	row := visit{TenantID: tenantUUID, PatientName: req.PatientName, Department: req.Department}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		data, err := json.Marshal(VisitCreatedData{
			VisitID: row.ID.String(), PatientName: row.PatientName, Department: row.Department,
		})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, SubjectVisitCreated, events.Event{
			Type: "VisitCreated", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not create visit")
		return
	}
	respond.Accepted(c, gin.H{"id": row.ID.String()})
}

// list returns this tenant's visits, newest first.
func (h *visitHandlers) list(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var rows []visit
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not list visits")
		return
	}
	respond.OK(c, gin.H{"data": rows})
}
