package pharmacy

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func operatingStocktakeRoutes(g *platform.Router, h *catalogueHandlers) {
	s := g.Group("/pharmacies/:pharmacy/operating-locations/:outlet/stocktakes")
	s.PUT("/:stocktake", authz.Public, h.putOperatingStocktake)
	platform.ListRoute(s, "", authz.Public, h.listOperatingStocktakes)
	s.GET("/:stocktake", authz.Public, h.getOperatingStocktake)
	s.GET("/:stocktake/register.csv", authz.Public, h.exportOperatingStocktakeRegister)
	platform.ListRoute(s, "/:stocktake/lines", authz.Public, h.listOperatingStocktakeLines)
	s.PUT("/:stocktake/counts/:command", authz.Public, h.putOperatingStocktakeCount)
	s.GET("/:stocktake/counts/:command", authz.Public, h.getOperatingStocktakeCount)
	s.PUT("/:stocktake/decisions/:command", authz.Public, h.putOperatingStocktakeDecision)
	s.PUT("/:stocktake/cancellations/:command", authz.Public, h.putOperatingStocktakeCancellation)
	s.GET("/:stocktake/decisions/:command", authz.Public, h.getOperatingStocktakeDecision)
}

func stocktakeOperatingError(c *gin.Context, err error) {
	var failure operatingStockFailure
	var audit operatingAuditFailure
	if errors.Is(err, errStocktakeParticipantSource) {
		respond.Error(c, 503, "authorization_unavailable", "Stocktake participant identity could not be verified.")
		return
	}
	if errors.As(err, &failure) && !errors.As(err, &audit) {
		stocktakeError(c, failure.err)
		return
	}
	operatingStockError(c, err)
}

func (u operatingStockUse) stocktakeEvidence(tx *gorm.DB, take uuid.UUID, kind string, command *uuid.UUID) error {
	id, err := uuid.NewV7()
	if err != nil {
		return operatingAuditFailure{err}
	}
	if kind == "start" {
		id = take
	} else if (kind == "count" || kind == "submit" || kind == "cancel" || kind == "approve" || kind == "reject") && command != nil {
		id = *command
	}
	g := u.grant
	var resource *uuid.UUID
	if take != uuid.Nil {
		resource = &take
	}
	if err := tx.Exec(`INSERT INTO pharmacy_stocktake_operating_evidence(tenant_id,outlet_id,id,pharmacy_id,stocktake_id,kind,command_id,permission,duty_id,snapshot_version,authority_epoch,staff_id,staff_version,link_id,link_version,actor_subject)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.tenant, u.outlet, id, u.pharmacy, resource, kind, command, g.Permission, g.DutyID, g.SnapshotVersion, g.AuthorityEpoch,
		g.Identity.StaffID, g.Identity.StaffVersion, g.Identity.LinkID, g.Identity.LinkVersion, u.subject).Error; err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "pharmacy_stocktake_operating_evidence_pkey" {
			return errStocktakeIdentity
		}
		return operatingAuditFailure{err}
	}
	return nil
}

func (h *catalogueHandlers) putOperatingStocktake(c *gin.Context) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return
	}
	id, ok := stocktakeID(c)
	if !ok {
		return
	}
	var in StocktakeStartInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !normalizeStocktakeStart(&in) {
		respond.Error(c, 400, "invalid_request", "Choose all products or an exact product selection, a quarantine location and a reason.")
		return
	}
	hash, err := stocktakeHash(p.Subject, in)
	if err != nil {
		stocktakeError(c, err)
		return
	}
	var result stocktakeHeaderRow
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	err = h.operatingStock(ctx, p, pharmacy, outlet, PermStocktakeCount, func(tx *gorm.DB, use operatingStockUse) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":stocktake-start"); err != nil {
			return err
		}
		saved, err := readStocktakeHeader(tx, p.TenantID, outlet, id)
		if err == nil {
			var retained int64
			if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='start' AND staff_id=? AND actor_subject=?", p.TenantID, outlet, id, id, use.grant.Identity.StaffID, p.Subject).Count(&retained).Error; err != nil {
				return err
			}
			if saved.StartedBy != p.Subject || saved.IntentSHA256 != hash || retained != 1 {
				return errStocktakeIdentity
			}
			result = saved
			return stocktakeReadAudit(c, tx, p, "stocktake", id, "command_recovered")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := use.stocktakeEvidence(tx, id, "start", nil); err != nil {
			return err
		}
		if err := startStocktake(tx, p, outlet, id, in, hash); err != nil {
			return err
		}
		// The receiving frame is restored before commit; validate the sealed snapshot while its reviewed purpose is held.
		if err := tx.Exec("SET CONSTRAINTS pharmacy_stocktake_commit_guard IMMEDIATE").Error; err != nil {
			return err
		}
		result, err = readStocktakeHeader(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return
	}
	respond.OK(c, Stocktake{StocktakeSummary: result.StocktakeSummary, ReviewRelationship: "starter"})
}

func (h *catalogueHandlers) listOperatingStocktakeLines(c *gin.Context, params pagination.Params) ([]stocktakeLineRow, error) {
	p, pharmacy, outlet, ok := operatingStockScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := stocktakeID(c)
	if !ok || !stocktakeRead(c, true) {
		return nil, nil
	}
	rows := []stocktakeLineRow{}
	err := h.operatingStock(c.Request.Context(), p, pharmacy, outlet, PermStocktakeRead, func(tx *gorm.DB, use operatingStockUse) error {
		if _, err := readStocktakeHeader(tx.Where("authority_kind='operating'"), p.TenantID, outlet, id); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(stocktakeLineColumns+",observed.counted_units,observed.counted_by,observed.count_reason,observed.counted_at,observed.counted_units-book_units difference_units").Joins(stocktakeObservedJoin).Where("tenant_id=? AND outlet_id=? AND stocktake_id=?", p.TenantID, outlet, id).Find(&rows).Error; err != nil {
			return err
		}
		if err := use.stocktakeEvidence(tx, id, "lines_read", nil); err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_lines", id, "list_read")
	})
	if err != nil {
		stocktakeOperatingError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].SnapshottedAt = rows[i].CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return rows, nil
}

func stocktakeCommandUnused(tx *gorm.DB, tenant string, outlet, command uuid.UUID) error {
	var existing int64
	if err := tx.Table("pharmacy_stocktake_commands").Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, command).Count(&existing).Error; err != nil {
		return err
	}
	if existing != 0 {
		return errStocktakeIdentity
	}
	return nil
}
