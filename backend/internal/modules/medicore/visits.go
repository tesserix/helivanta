package medicore

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/hms/internal/modules/medicore/contract"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/pagination"
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

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (v visit) PageKey() (time.Time, uuid.UUID) { return v.CreatedAt, v.ID }

type createVisitRequest struct {
	PatientName string `json:"patient_name" binding:"required,max=200"`
	Department  string `json:"department" binding:"required,oneof=OPD IPD"`
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
		data, err := json.Marshal(medicorecontract.VisitCreatedData{
			VisitID: row.ID.String(), PatientName: row.PatientName, Department: row.Department,
		})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, medicorecontract.SubjectVisitCreated, events.Event{
			Type: "VisitCreated", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not create visit")
		return
	}
	respond.Accepted(c, gin.H{"id": row.ID.String()})
}

// list returns one page of this tenant's visits, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and does
// not trim, does not decide has_more and does not build a cursor.
// platform.ListRoute owns all three, so they cannot be got wrong per
// endpoint.
func (h *visitHandlers) list(c *gin.Context, p pagination.Params) ([]visit, error) {
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []visit
	err := h.db.WithTenant(c.Request.Context(), principal.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, p).Find(&rows).Error
	})
	return rows, err
}
