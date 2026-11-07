package supply

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	"gorm.io/gorm"
)

func (h *handlers) stockProductByBarcode(c *gin.Context) {
	p, party, premise, ok := stockScope(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, errors.New("provide one product barcode without query parameters"))
		return
	}
	var reference stockproduct.Reference
	err := h.stockTransaction(c, p, party, premise, PermOperatingRead, func(tx *gorm.DB) error {
		if err := lockStockAccess(tx, p.TenantID, party); err != nil {
			return err
		}
		if _, err := stockParent(tx, p, party, premise, []string{"administrator", "inventory", "warehouse", "auditor", "regulator"}); err != nil {
			return err
		}
		source, ok := h.stockProducts.(stockproduct.BarcodeLookup)
		if !ok {
			return stockproduct.ErrUnavailable
		}
		var err error
		reference, err = source.LookupStockProduct(c.Request.Context(), c.Param("gtin"))
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'stock_product_reference_read','supply_premises',?)`, p.TenantID, p.Subject, premise).Error
	})
	switch {
	case errors.Is(err, stockproduct.ErrInvalidBarcode):
		respond.BadRequest(c, err)
	case errors.Is(err, stockproduct.ErrIneligible):
		respond.NotFound(c, "current stock product")
	case err != nil:
		wholesaleStockError(c, err)
	default:
		respond.OK(c, reference)
	}
}
