package pharmacy

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/tenantdb"
)

type medication struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Name      string    `json:"name"`
	Strength  string    `json:"strength"`
	CreatedAt time.Time `json:"created_at"`
}

func (medication) TableName() string { return "pharmacy_medications" }

type createMedicationRequest struct {
	Name     string `json:"name" binding:"required,max=200"`
	Strength string `json:"strength" binding:"max=100"`
}

type medicationHandlers struct {
	db *tenantdb.DB
}

// create adds a medication to the tenant's formulary.
func (h *medicationHandlers) create(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var req createMedicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	row := medication{TenantID: tenantUUID, Name: req.Name, Strength: req.Strength}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Create(&row).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not create medication")
		return
	}
	respond.Created(c, gin.H{"id": row.ID.String()})
}

// list returns this tenant's medications, newest first.
func (h *medicationHandlers) list(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var rows []medication
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not list medications")
		return
	}
	respond.OK(c, gin.H{"data": rows})
}
