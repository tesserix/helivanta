package pharmacy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/csvsafe"
)

var errStocktakeExportIncomplete = errors.New("stocktake export did not include every snapshot bucket")

const stocktakeAdjustmentJoin = `LEFT JOIN LATERAL(
 SELECT a.movement_id,a.location_id adjustment_location_id,l.code adjustment_location_code,
 a.quantity_units adjustment_units,c.actor_subject approved_by,c.created_at approved_at,c.reason approval_reason
 FROM pharmacy_stocktake_adjustments a
 JOIN pharmacy_stocktake_commands c ON c.tenant_id=a.tenant_id AND c.outlet_id=a.outlet_id AND c.id=a.command_id AND c.stocktake_id=a.stocktake_id
 JOIN pharmacy_stock_locations l ON l.tenant_id=a.tenant_id AND l.outlet_id=a.outlet_id AND l.id=a.location_id
 WHERE a.tenant_id=pharmacy_stocktake_lines.tenant_id AND a.outlet_id=pharmacy_stocktake_lines.outlet_id AND a.stocktake_id=pharmacy_stocktake_lines.stocktake_id AND a.line_id=pharmacy_stocktake_lines.id
)adjustment ON true`

func (h *catalogueHandlers) exportStocktakeRegister(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
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
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		var err error
		header, data, err = loadStocktakeRegister(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_register", id, "exported")
	})
	respondStocktakeRegister(c, ctx, p, outlet, id, header, data, err, stocktakeError)
}

func loadStocktakeRegister(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (header stocktakeHeaderRow, data []byte, err error) {
	var parent struct{ ID uuid.UUID }
	if err := tx.Table("pharmacy_stocktakes").Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("tenant_id=? AND outlet_id=? AND id=? AND sealed", tenant, outlet, id).Take(&parent).Error; err != nil {
		return header, nil, err
	}
	header, err = readCurrentStocktakeHeader(tx, tenant, outlet, id)
	if err != nil {
		return header, nil, err
	}
	var rows []stocktakeRegisterRow
	columns := stocktakeLineColumns + `,observed.counted_units,observed.counted_by,observed.count_reason,observed.counted_at,observed.counted_units-book_units difference_units,
 adjustment.movement_id,adjustment.adjustment_location_id,adjustment.adjustment_location_code,adjustment.adjustment_units,adjustment.approved_by,adjustment.approved_at,adjustment.approval_reason`
	if err := tx.Select(columns).Joins(stocktakeObservedJoin).Joins(stocktakeAdjustmentJoin).Where("tenant_id=? AND outlet_id=? AND stocktake_id=?", tenant, outlet, id).Order("id").Limit(10001).Find(&rows).Error; err != nil {
		return header, nil, err
	}
	if len(rows) > 10000 {
		return header, nil, csvsafe.ErrTooLarge
	}
	if int64(len(rows)) != header.SnapshotRows {
		return header, nil, errStocktakeExportIncomplete
	}
	data, err = encodeStocktakeCSV(Stocktake{StocktakeSummary: header.StocktakeSummary, CountedRows: header.CountedRows, DifferenceRows: header.DifferenceRows, PositiveDifferenceRows: header.PositiveDifferenceRows, ReviewRelationship: header.ReviewRelationship}, rows)
	if err != nil {
		return header, nil, err
	}

	return header, data, nil
}

func respondStocktakeRegister(c *gin.Context, ctx context.Context, p authn.Principal, outlet, id uuid.UUID, header stocktakeHeaderRow, data []byte, err error, report func(*gin.Context, error)) {
	if errors.Is(err, csvsafe.ErrTooLarge) {
		respond.Error(c, http.StatusRequestEntityTooLarge, "stocktake_export_too_large", "The stocktake register exceeds 10000 rows or 8 MiB. No partial file was exported; use the paged record to examine the retained observations.")
		return
	}
	if errors.Is(err, errStocktakeExportIncomplete) {
		respond.Error(c, http.StatusServiceUnavailable, "stocktake_export_unavailable", "The complete stocktake register could not be loaded.")
		return
	}
	if err != nil {
		report(c, err)
		return
	}
	if err := ctx.Err(); err != nil {
		report(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Export-Row-Count", strconv.FormatInt(header.SnapshotRows, 10))
	c.Header("X-Export-Sha256", fmt.Sprintf("%x", sha256.Sum256(data)))
	c.Header("X-Stocktake-Tenant-Id", p.TenantID)
	c.Header("X-Stocktake-Outlet-Id", outlet.String())
	c.Header("X-Stocktake-Id", id.String())
	c.Header("X-Stocktake-Version", strconv.FormatInt(header.Version, 10))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="stocktake-%d-register.csv"`, header.Number))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
