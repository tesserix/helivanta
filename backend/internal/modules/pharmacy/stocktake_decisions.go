package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

var errStocktakePostingLimit = errors.New("stocktake differences exceed posting limits")

type stocktakeAdjustmentPlan struct {
	LineID        uuid.UUID
	BatchID       uuid.UUID
	LocationID    uuid.UUID
	QuantityUnits int64
}

type stocktakeAdjustmentRow struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	OutletID      uuid.UUID
	StocktakeID   uuid.UUID
	CommandID     uuid.UUID
	LineID        uuid.UUID
	MovementID    uuid.UUID
	LocationID    uuid.UUID
	QuantityUnits int64
}

func (stocktakeAdjustmentRow) TableName() string { return "pharmacy_stocktake_adjustments" }

func readStocktakeDecision(tx *gorm.DB, tenant string, outlet, take, command uuid.UUID) (StocktakeDecisionReceipt, string, error) {
	var row stocktakeCommand
	err := tx.Select(stocktakeCommandColumns).Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind<>'count'", tenant, outlet, take, command).Take(&row).Error
	if err != nil {
		return StocktakeDecisionReceipt{}, "", err
	}
	return StocktakeDecisionReceipt{ID: row.ID, StocktakeID: row.StocktakeID, OutletID: row.OutletID, Version: row.Version,
		State: row.State, AdjustmentCount: row.ItemCount, RecordedBy: row.ActorSubject, RecordedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		StocktakeDecisionInput: StocktakeDecisionInput{Decision: row.Kind, ExpectedVersion: &row.ExpectedVersion, Reason: row.Reason, ExtraUnitsDisposition: row.ExtraUnitsDisposition}}, row.IntentSHA256, nil
}

func (h *catalogueHandlers) putStocktakeDecision(c *gin.Context) {
	h.writeStocktakeDecision(c, false)
}

func (h *catalogueHandlers) putStocktakeCancellation(c *gin.Context) {
	h.writeStocktakeDecision(c, true)
}

func (h *catalogueHandlers) writeStocktakeDecision(c *gin.Context, cancelOnly bool) {
	p, outlet, ok := stockCommandScope(c)
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
		respond.Error(c, 400, "invalid_request", "Provide the current version, a reason and the appropriate quarantine evidence.")
		return
	}
	if cancelOnly && in.Decision != "cancel" {
		respond.Error(c, 400, "invalid_request", "This continuation only cancels an existing stocktake.")
		return
	}
	hash, err := stocktakeHash(p.Subject, in)
	if err != nil {
		stocktakeError(c, err)
		return
	}
	var result StocktakeDecisionReceipt
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		var parent struct{ ID uuid.UUID }
		if err := tx.Table("pharmacy_stocktakes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id=? AND outlet_id=? AND id=? AND sealed", p.TenantID, outlet, take).Take(&parent).Error; err != nil {
			return err
		}
		saved, savedHash, err := readStocktakeDecision(tx, p.TenantID, outlet, take, command)
		if err == nil {
			if savedHash != hash || saved.RecordedBy != p.Subject {
				return errStocktakeIdentity
			}
			result = saved
			return stocktakeReadAudit(c, tx, p, "stocktake_decision", command, "command_recovered")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := postStocktakeDecision(tx, p, currentPharmacy(c.Request.Context()), outlet, take, command, in, hash); err != nil {
			return err
		}
		result, _, err = readStocktakeDecision(tx, p.TenantID, outlet, take, command)
		return err
	})
	if err != nil {
		stocktakeError(c, err)
		return
	}
	respond.OK(c, result)
}

func postStocktakeDecision(tx *gorm.DB, p authn.Principal, pharmacy, outlet, take, command uuid.UUID, in StocktakeDecisionInput, hash string) error {
	var plans []stocktakeAdjustmentPlan
	if in.Decision == "approve" {
		if err := tx.Exec(`SET LOCAL plan_cache_mode=force_custom_plan`).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT d.line_id,d.batch_id,CASE WHEN d.quantity_units>0 THEN s.quarantine_location_id ELSE d.from_location_id END location_id,d.quantity_units
 FROM pharmacy_stocktake_differences(?,?,?)d JOIN pharmacy_stocktakes s ON s.tenant_id=? AND s.outlet_id=? AND s.id=?
 ORDER BY location_id,d.batch_id,d.line_id LIMIT 501`, p.TenantID, outlet, take, p.TenantID, outlet, take).Scan(&plans).Error; err != nil {
			return err
		}
		if len(plans) > 500 {
			return errStocktakePostingLimit
		}
		for _, plan := range plans {
			if plan.QuantityUnits < -1000000000 || plan.QuantityUnits > 1000000000 {
				return errStocktakePostingLimit
			}
		}
	}
	state := map[string]string{"submit": "submitted", "approve": "posted", "reject": "rejected", "cancel": "cancelled"}[in.Decision]
	if err := tx.Exec(`INSERT INTO pharmacy_stocktake_commands(tenant_id,outlet_id,id,stocktake_id,kind,expected_version,version,state,item_count,extra_units_disposition,reason,actor_subject,intent_sha256)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, outlet, command, take, in.Decision, *in.ExpectedVersion, *in.ExpectedVersion+1, state, len(plans), in.ExtraUnitsDisposition, in.Reason, p.Subject, hash).Error; err != nil {
		return err
	}
	if len(plans) == 0 {
		return nil
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?::uuid::text||':'||?::uuid::text||':'||d.location_id::text||':'||d.batch_id::text,0))
 FROM(SELECT DISTINCT CASE WHEN x.quantity_units>0 THEN s.quarantine_location_id ELSE x.from_location_id END location_id,x.batch_id
 FROM pharmacy_stocktake_differences(?,?,?)x JOIN pharmacy_stocktakes s ON s.tenant_id=? AND s.outlet_id=? AND s.id=?)d
 ORDER BY d.location_id,d.batch_id`, p.TenantID, outlet, p.TenantID, outlet, take, p.TenantID, outlet, take).Error; err != nil {
		return err
	}
	links := make([]stocktakeAdjustmentRow, len(plans))
	movements := make([]stockMovement, len(plans))
	publications := make([]events.Event, len(plans))
	for i, plan := range plans {
		movementID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create stocktake movement identity: %w", err)
		}
		linkID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create stocktake evidence identity: %w", err)
		}
		links[i] = stocktakeAdjustmentRow{ID: linkID, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, StocktakeID: take, CommandID: command, LineID: plan.LineID, MovementID: movementID, LocationID: plan.LocationID, QuantityUnits: plan.QuantityUnits}
		movements[i] = stockMovement{ID: movementID, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ActorSubject: p.Subject,
			MovementFields: movementInput{LocationID: plan.LocationID, BatchID: plan.BatchID, Kind: "correction", QuantityUnits: plan.QuantityUnits, Reason: in.Reason, SourceReference: "stocktake:" + command.String() + ":" + plan.LineID.String()}}
		data, err := json.Marshal(pharmacycontract.StockMovementRecordedData{PharmacyID: pharmacy.String(), OutletID: outlet.String(), MovementID: movementID.String(), LocationID: plan.LocationID.String(), BatchID: plan.BatchID.String()})
		if err != nil {
			return err
		}
		publications[i] = events.Event{Type: "StockMovementRecorded", Version: 1, TenantID: p.TenantID, Data: data}
	}
	if err := withStocktakeAdjustmentTimestamp(tx, p, outlet, take, command, func() error { return tx.CreateInBatches(&links, 500).Error }); err != nil {
		return err
	}
	if err := tx.CreateInBatches(&movements, 500).Error; err != nil {
		return err
	}
	return events.EnqueueBatch(tx, pharmacycontract.SubjectStockMovementRecorded, publications)
}

func (h *catalogueHandlers) getStocktakeDecision(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
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
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var err error
		result, _, err = readStocktakeDecision(tx, p.TenantID, outlet, take, command)
		if err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_decision", command, "command_recovered")
	})
	if err != nil {
		stocktakeError(c, err)
		return
	}
	respond.OK(c, result)
}
