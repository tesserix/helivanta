package pharmacy

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

func (h *catalogueHandlers) putOperatingStocktakeCount(c *gin.Context) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return
	}
	take, ok := stocktakeID(c)
	if !ok {
		return
	}
	command, ok := stocktakeCommandID(c)
	if !ok {
		return
	}
	var in StocktakeCountInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeStocktakeCount(&in) {
		respond.Error(c, 400, "invalid_request", "Provide exact counted quantities, the current version and a reason.")
		return
	}
	hash, err := stocktakeHash(p.Subject, in)
	if err != nil {
		stocktakeError(c, err)
		return
	}
	var result StocktakeCountReceipt
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	err = h.operatingStock(ctx, p, pharmacy, outlet, PermStocktakeCount, func(tx *gorm.DB, use operatingStockUse) error {
		var parent struct{ ID uuid.UUID }
		if err := tx.Table("pharmacy_stocktakes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id=? AND outlet_id=? AND id=? AND sealed AND authority_kind='operating'", p.TenantID, outlet, take).Take(&parent).Error; err != nil {
			return err
		}
		saved, savedHash, err := readStocktakeCount(tx, p.TenantID, outlet, take, command)
		if err == nil {
			var evidence int64
			if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND command_id=? AND kind='count' AND staff_id=? AND actor_subject=?", p.TenantID, outlet, take, command, use.grant.Identity.StaffID, p.Subject).Count(&evidence).Error; err != nil {
				return err
			}
			if savedHash != hash || saved.RecordedBy != p.Subject || evidence != 1 {
				return errStocktakeIdentity
			}
			result = saved
			return stocktakeReadAudit(c, tx, p, "stocktake_count", command, "command_recovered")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := use.stocktakeEvidence(tx, take, "count", &command); err != nil {
			return err
		}
		if err := recordStocktakeCount(tx, p, outlet, take, command, in, hash); err != nil {
			return err
		}
		// Completeness must be checked before the reviewed operating purpose is restored.
		if err := tx.Exec("SET CONSTRAINTS pharmacy_stocktake_command_commit_guard IMMEDIATE").Error; err != nil {
			return err
		}
		result, _, err = readStocktakeCount(tx, p.TenantID, outlet, take, command)
		return err
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) getOperatingStocktakeCount(c *gin.Context) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return
	}
	take, ok := stocktakeID(c)
	if !ok {
		return
	}
	command, ok := stocktakeCommandID(c)
	if !ok || !stocktakeRead(c, false) {
		return
	}
	var result StocktakeCountReceipt
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		var evidence int64
		if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND command_id=? AND kind='count'", p.TenantID, outlet, take, command).Count(&evidence).Error; err != nil {
			return err
		}
		if evidence != 1 {
			return gorm.ErrRecordNotFound
		}
		var err error
		result, _, err = readStocktakeCount(tx, p.TenantID, outlet, take, command)
		if err != nil {
			return err
		}
		if err := use.stocktakeEvidence(tx, take, "count_read", &command); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_count", command, "command_recovered")
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return
	}
	respond.OK(c, result)
}
