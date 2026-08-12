package lab

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

type order struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	TestName    string     `gorm:"default:CBC" json:"test_name"`
	Status      string     `gorm:"default:pending" json:"status"`
	ResultValue *string    `json:"result_value"`
	ResultedAt  *time.Time `json:"resulted_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (order) TableName() string { return "lab_orders" }

type resultRequest struct {
	ResultValue string `json:"result_value" binding:"required,max=500"`
}

type resultReadyData struct {
	OrderID string `json:"order_id"`
	VisitID string `json:"visit_id"`
}

type orderHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// list returns this tenant's orders, newest first.
func (h *orderHandlers) list(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var rows []order
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not list orders")
		return
	}
	respond.OK(c, gin.H{"data": rows})
}

// result records a pending order's result. The guarded UPDATE is what
// makes concurrent submission safe: the loser matches zero rows and gets
// a 409 rather than overwriting the result.
func (h *orderHandlers) result(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.NotFound(c, "order")
		return
	}
	var req resultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	var status int
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var row order
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Status != "pending" {
			status = http.StatusConflict
			return nil
		}
		now := time.Now().UTC()
		result := tx.Model(&order{}).Where("id = ? AND status = 'pending'", id).
			Updates(map[string]any{"status": "completed", "result_value": req.ResultValue, "resulted_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// Lost the race to a concurrent result submission between
			// the pre-check above and this guarded UPDATE.
			status = http.StatusConflict
			return nil
		}
		data, err := json.Marshal(resultReadyData{OrderID: row.ID.String(), VisitID: row.VisitID.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, SubjectResultReady, events.Event{
			Type: "ResultReady", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "order")
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "could not record result")
		return
	}
	if status == http.StatusConflict {
		respond.Conflict(c, "result already recorded")
		return
	}
	respond.OK(c, gin.H{"id": id.String(), "status": "completed"})
}
