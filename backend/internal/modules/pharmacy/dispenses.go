package pharmacy

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (d dispense) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

type dispenseRequest struct {
	Medication string `json:"medication" binding:"required,max=200"`
}

type dispenseHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// list returns one page of this tenant's dispenses, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and does
// not trim, does not decide has_more and does not build a cursor.
// platform.ListRoute owns all three, so they cannot be got wrong per
// endpoint.
func (h *dispenseHandlers) list(c *gin.Context, p pagination.Params) ([]dispense, error) {
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []dispense
	err := h.db.WithTenant(c.Request.Context(), principal.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, p).Find(&rows).Error
	})
	return rows, err
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
		data, err := json.Marshal(pharmacycontract.DispenseRecordedData{DispenseID: row.ID.String(), VisitID: row.VisitID.String()})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectDispenseRecorded, events.Event{
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
