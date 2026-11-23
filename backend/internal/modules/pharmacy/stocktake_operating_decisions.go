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

func (h *catalogueHandlers) putOperatingStocktakeDecision(c *gin.Context) {
	h.writeOperatingStocktakeDecision(c, false)
}

func (h *catalogueHandlers) putOperatingStocktakeCancellation(c *gin.Context) {
	h.writeOperatingStocktakeDecision(c, true)
}

func (h *catalogueHandlers) writeOperatingStocktakeDecision(c *gin.Context, cancelOnly bool) {
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
	var in StocktakeDecisionInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeStocktakeDecision(&in) {
		respond.Error(c, 400, "invalid_request", "Provide the current version and a reason for the decision.")
		return
	}
	if cancelOnly && in.Decision != "cancel" {
		respond.Error(c, 400, "invalid_request", "This continuation only cancels an existing stocktake.")
		return
	}
	permission := PermStocktakeCount
	review := in.Decision == "approve" || in.Decision == "reject"
	if review {
		permission = PermStocktakeReview
	}
	hash, err := stocktakeHash(p.Subject, in)
	if err != nil {
		stocktakeError(c, err)
		return
	}
	var result StocktakeDecisionReceipt
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	err = h.operatingStock(ctx, p, pharmacy, outlet, permission, func(tx *gorm.DB, use operatingStockUse) error {
		var parent struct{ ID uuid.UUID }
		if err := tx.Table("pharmacy_stocktakes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id=? AND outlet_id=? AND id=? AND sealed AND authority_kind='operating'", p.TenantID, outlet, take).Take(&parent).Error; err != nil {
			return err
		}
		saved, savedHash, err := readStocktakeDecision(tx, p.TenantID, outlet, take, command)
		if err == nil {
			var evidence int64
			if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND command_id=? AND kind=? AND staff_id=? AND actor_subject=?", p.TenantID, outlet, take, command, in.Decision, use.grant.Identity.StaffID, p.Subject).Count(&evidence).Error; err != nil {
				return err
			}
			if savedHash != hash || saved.RecordedBy != p.Subject || evidence != 1 {
				return errStocktakeIdentity
			}
			result = saved
			return stocktakeReadAudit(c, tx, p, "stocktake_decision", command, "command_recovered")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := stocktakeCommandUnused(tx, p.TenantID, outlet, command); err != nil {
			return err
		}
		if review {
			if err := h.reviewStocktakeParticipants(tx, use, take); err != nil {
				return err
			}
		}
		if err := use.stocktakeEvidence(tx, take, in.Decision, &command); err != nil {
			return err
		}
		if err := postStocktakeDecision(tx, p, pharmacy, outlet, take, command, in, hash); err != nil {
			return err
		}
		// Completeness must be checked before the reviewed operating purpose is restored.
		if err := tx.Exec("SET CONSTRAINTS pharmacy_stocktake_command_commit_guard IMMEDIATE").Error; err != nil {
			return err
		}
		result, _, err = readStocktakeDecision(tx, p.TenantID, outlet, take, command)
		return err
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) getOperatingStocktakeDecision(c *gin.Context) {
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
	var result StocktakeDecisionReceipt
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		var evidence int64
		if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND command_id=? AND kind IN('submit','cancel','approve','reject')", p.TenantID, outlet, take, command).Count(&evidence).Error; err != nil {
			return err
		}
		if evidence != 1 {
			return gorm.ErrRecordNotFound
		}
		var err error
		result, _, err = readStocktakeDecision(tx, p.TenantID, outlet, take, command)
		if err != nil {
			return err
		}
		if err := use.stocktakeEvidence(tx, take, "decision_read", &command); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_decision", command, "command_recovered")
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return
	}
	respond.OK(c, result)
}
