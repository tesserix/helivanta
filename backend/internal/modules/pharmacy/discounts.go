package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

// The outlet's discount ceiling (#1386 B-3, decision 3): the owner's
// commercial decision, recorded with a reason. The latest record is in force;
// an outlet with none allows no discount.

type discountCeilingInput struct {
	MaxBps int64  `json:"max_bps" binding:"min=0,max=10000"`
	Reason string `json:"reason" binding:"required"`
}

type discountCeiling struct {
	ID         uuid.UUID `gorm:"column:id" json:"id"`
	MaxBps     int64     `gorm:"column:max_bps" json:"max_bps"`
	Reason     string    `gorm:"column:reason" json:"reason"`
	RecordedBy string    `gorm:"column:recorded_by" json:"recorded_by"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
}

const discountCeilingColumns = "id,max_bps,reason,recorded_by,created_at"

func discountRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/discount-ceiling/:id", PermDiscountManage, h.putDiscountCeiling)
	s.GET("/discount-ceiling", PermSaleRead, h.getDiscountCeiling)
}

func (h *catalogueHandlers) putDiscountCeiling(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in discountCeilingInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !prescriptionText(in.Reason, 200) || len([]rune(in.Reason)) < 5 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a discount ceiling is 0 to 10000 basis points, with a reason of 5 to 200 characters")
		return
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{strconv.FormatInt(in.MaxBps, 10), in.Reason}, "\x1f")))
	digest := hex.EncodeToString(sum[:])
	var row discountCeiling
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		// One lock per outlet, not per record: "latest" is decided by seq, so
		// concurrent records must commit in the order they are numbered.
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":discount-ceiling"); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, RecordedBy string }
		if err := tx.Raw(`SELECT intent_sha256, recorded_by FROM pharmacy_outlet_discount_ceilings WHERE tenant_id=? AND outlet_id=? AND id=?`,
			p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 && (existing[0].IntentSHA256 != digest || existing[0].RecordedBy != p.Subject) {
			return errStockIdentity
		}
		if len(existing) == 0 {
			created = true
			if err := tx.Exec(`INSERT INTO pharmacy_outlet_discount_ceilings (id,tenant_id,outlet_id,max_bps,reason,intent_sha256,recorded_by) VALUES (?,?,?,?,?,?,?)`,
				id, p.TenantID, outlet, in.MaxBps, in.Reason, digest, p.Subject).Error; err != nil {
				return err
			}
		}
		return tx.Raw(`SELECT `+discountCeilingColumns+` FROM pharmacy_outlet_discount_ceilings WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&row).Error
	})
	switch {
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, row)
	default:
		respond.OK(c, row)
	}
}

func (h *catalogueHandlers) getDiscountCeiling(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	var rows []discountCeiling
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT `+discountCeilingColumns+` FROM pharmacy_outlet_discount_ceilings WHERE tenant_id=? AND outlet_id=? ORDER BY seq DESC LIMIT 1`,
			p.TenantID, outlet).Scan(&rows).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	var ceiling *discountCeiling
	if len(rows) == 1 {
		ceiling = &rows[0]
	}
	respond.OK(c, gin.H{"ceiling": ceiling})
}
