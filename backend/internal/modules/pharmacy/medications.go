package pharmacy

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type medication struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Name      string    `json:"name"`
	Strength  string    `json:"strength"`
	CreatedAt time.Time `json:"created_at"`
}

func (medication) TableName() string { return "pharmacy_medications" }

// PageKey is the keyset position of this row, satisfying platform.Keyed.
func (m medication) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }

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

// list returns one page of this tenant's medications, newest first.
//
// It returns whatever ApplyKeyset yields — up to Limit+1 rows — and does
// not trim, does not decide has_more and does not build a cursor.
// platform.ListRoute owns all three, so they cannot be got wrong per
// endpoint.
func (h *medicationHandlers) list(c *gin.Context, p pagination.Params) ([]medication, error) {
	principal, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []medication
	err := h.db.WithTenant(c.Request.Context(), principal.TenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, p).Find(&rows).Error
	})
	return rows, err
}
