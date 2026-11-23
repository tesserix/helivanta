package pharmacy

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (h *catalogueHandlers) getOperatingStocktake(c *gin.Context) {
	p, pharmacy, outlet, ok := stocktakeWorkScope(c)
	if !ok {
		return
	}
	id, ok := stocktakeID(c)
	if !ok || !stocktakeRead(c, false) {
		return
	}
	var result stocktakeHeaderRow
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		var err error
		result, err = readCurrentStocktakeHeader(tx.Where("authority_kind='operating'"), p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		if err := tx.Raw(`SELECT CASE WHEN COALESCE(bool_or(kind='start'),false)THEN 'starter'
 WHEN COALESCE(bool_or(kind IN('count','submit')),false)THEN 'counter' ELSE 'independent' END
 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=? AND outlet_id=? AND stocktake_id=?
 AND kind IN('start','count','submit')AND(staff_id=? OR actor_subject=?)`,
			p.TenantID, outlet, id, use.grant.Identity.StaffID, p.Subject).Scan(&result.ReviewRelationship).Error; err != nil {
			return err
		}
		if err := use.stocktakeEvidence(tx, id, "record_read", nil); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake", id, "record_read")
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return
	}
	respond.OK(c, Stocktake{StocktakeSummary: result.StocktakeSummary, CountedRows: result.CountedRows, DifferenceRows: result.DifferenceRows, PositiveDifferenceRows: result.PositiveDifferenceRows, ReviewRelationship: result.ReviewRelationship})
}

func (h *catalogueHandlers) listOperatingStocktakes(c *gin.Context, params pagination.Params) ([]stocktakeHeaderRow, error) {
	p, pharmacy, outlet, ok := stocktakeWorkScope(c)
	if !ok || !stocktakeRead(c, true) {
		return nil, nil
	}
	rows := []stocktakeHeaderRow{}
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(stocktakeCurrentHeaderColumns).Where("tenant_id=? AND outlet_id=? AND sealed AND authority_kind='operating'", p.TenantID, outlet).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if err := finishStocktakeHeader(&rows[i]); err != nil {
				return err
			}
		}
		if err := use.stocktakeEvidence(tx, uuid.Nil, "list_read", nil); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktakes", outlet, "list_read")
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) exportOperatingStocktakeRegister(c *gin.Context) {
	p, pharmacy, outlet, ok := stocktakeWorkScope(c)
	if !ok {
		return
	}
	id, ok := stocktakeID(c)
	if !ok || !stocktakeRead(c, false) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var header stocktakeHeaderRow
	var data []byte
	err := h.operatingStock(ctx, p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		if _, err := readStocktakeHeader(tx.Where("authority_kind='operating'"), p.TenantID, outlet, id); err != nil {
			return err
		}
		var err error
		header, data, err = loadStocktakeRegister(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		if err := use.stocktakeEvidence(tx, id, "register_read", nil); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_register", id, "exported")
	})
	respondStocktakeRegister(c, ctx, p, outlet, id, header, data, err, stocktakeOperatingError)
}
