package pharmacy

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// outletGrant is one subject's access at an outlet, as its manager sees it (#1718).
type outletGrant struct {
	ID        uuid.UUID `json:"id"`
	Subject   string    `json:"subject"`
	Level     string    `json:"level"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (outletGrant) TableName() string                 { return "pharmacy_outlet_access" }
func (g outletGrant) PageKey() (time.Time, uuid.UUID) { return g.CreatedAt, g.ID }

func outletAccessGrantRoutes(g *platform.Router, deps platform.Deps) {
	h := &catalogueHandlers{db: deps.DB}
	platform.ListRoute(g.Group("/pharmacies/:pharmacy/outlets/:outlet"), "/access-grants", PermAccessManage, h.listGrants)
}

// listGrants needs the manage permission, not an outlet grant, like
// changeAccess: managing grants is how the first one is given.
func (h *catalogueHandlers) listGrants(c *gin.Context, params pagination.Params) ([]outletGrant, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []outletGrant
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("pharmacy_outlets").Where("tenant_id=? AND pharmacy_id=? AND id=?", p.TenantID, currentPharmacy(c.Request.Context()), outlet).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Select("id,subject,level,created_at,updated_at").Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}
