package pharmacy

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (h *catalogueHandlers) readStocktakeChoices(c *gin.Context, kind string, read func(*gorm.DB, authn.Principal, uuid.UUID) error) bool {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok || !stocktakeRead(c, true) {
		return false
	}
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		if err := read(tx, p, outlet); err != nil {
			return err
		}
		if err := use.stocktakeEvidence(tx, uuid.Nil, "list_read", nil); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, kind, outlet, "list_read")
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return false
	}
	return true
}

func (h *catalogueHandlers) listStocktakeProductChoices(c *gin.Context, page pagination.Params) ([]receivingProduct, error) {
	rows := []receivingProduct{}
	h.readStocktakeChoices(c, "stocktake_product_choices", func(tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		return tenantdb.ApplyKeyset(tx, page).Select(receivingProductColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	return rows, nil
}

func (h *catalogueHandlers) listStocktakeQuarantineChoices(c *gin.Context, page pagination.Params) ([]stockLocation, error) {
	rows := []stockLocation{}
	h.readStocktakeChoices(c, "stocktake_quarantine_choices", func(tx *gorm.DB, p authn.Principal, outlet uuid.UUID) error {
		return tenantdb.ApplyKeyset(tx, page).Select(locationColumns).Where("tenant_id=? AND outlet_id=? AND stock_status='quarantine'", p.TenantID, outlet).Find(&rows).Error
	})
	return rows, nil
}
