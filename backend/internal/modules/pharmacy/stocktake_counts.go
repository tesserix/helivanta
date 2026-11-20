package pharmacy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

type stocktakeCommand struct {
	ID              uuid.UUID
	StocktakeID     uuid.UUID
	OutletID        uuid.UUID
	ExpectedVersion int64
	Version         int64
	State           string
	Reason          string
	ActorSubject    string
	IntentSHA256    string
	CreatedAt       time.Time
}

func (stocktakeCommand) TableName() string { return "pharmacy_stocktake_commands" }

type stocktakeCountRow struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	OutletID       uuid.UUID
	StocktakeID    uuid.UUID
	CommandID      uuid.UUID
	LineID         uuid.UUID
	CommandVersion int64
	CountedUnits   int64
}

func (stocktakeCountRow) TableName() string { return "pharmacy_stocktake_counts" }

const stocktakeCommandColumns = `id,stocktake_id,outlet_id,expected_version,version,state,reason,actor_subject,intent_sha256,created_at`

func stocktakeCommandID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("command"))
	if err != nil || !stocktakeRecordID(id) {
		respond.NotFound(c, "stocktake command")
		return uuid.Nil, false
	}
	return id, true
}

func readStocktakeCount(tx *gorm.DB, tenant string, outlet, take, command uuid.UUID) (StocktakeCountReceipt, string, error) {
	var row stocktakeCommand
	err := tx.Select(stocktakeCommandColumns).Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id=? AND kind='count'", tenant, outlet, take, command).Take(&row).Error
	if err != nil {
		return StocktakeCountReceipt{}, "", err
	}
	var entries []stocktakeCountRow
	if err := tx.Select("line_id,counted_units").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND command_id=?", tenant, outlet, take, command).Order("line_id").Find(&entries).Error; err != nil {
		return StocktakeCountReceipt{}, "", err
	}
	result := StocktakeCountReceipt{ID: row.ID, StocktakeID: row.StocktakeID, OutletID: row.OutletID, Version: row.Version,
		State: row.State, RecordedBy: row.ActorSubject, RecordedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		StocktakeCountInput: StocktakeCountInput{ExpectedVersion: &row.ExpectedVersion, Reason: row.Reason, Counts: make([]StocktakeCountEntry, len(entries))}}
	for i := range entries {
		result.Counts[i] = StocktakeCountEntry{LineID: entries[i].LineID, CountedUnits: &entries[i].CountedUnits}
	}
	return result, row.IntentSHA256, nil
}

func (h *catalogueHandlers) putStocktakeCount(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
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
	if !decodeStocktake(c, &in) {
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
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		var parent struct{ ID uuid.UUID }
		if err := tx.Table("pharmacy_stocktakes").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id=? AND outlet_id=? AND id=? AND sealed", p.TenantID, outlet, take).Take(&parent).Error; err != nil {
			return err
		}
		saved, savedHash, err := readStocktakeCount(tx, p.TenantID, outlet, take, command)
		if err == nil {
			if savedHash != hash || saved.RecordedBy != p.Subject {
				return errStocktakeIdentity
			}
			result = saved
			return stocktakeReadAudit(c, tx, p, "stocktake_count", command, "command_recovered")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := recordStocktakeCount(tx, p, outlet, take, command, in, hash); err != nil {
			return err
		}
		result, _, err = readStocktakeCount(tx, p.TenantID, outlet, take, command)
		return err
	})
	if err != nil {
		stocktakeError(c, err)
		return
	}
	respond.OK(c, result)
}

func recordStocktakeCount(tx *gorm.DB, p authn.Principal, outlet, take, command uuid.UUID, in StocktakeCountInput, hash string) error {
	ids := make([]uuid.UUID, len(in.Counts))
	for i := range in.Counts {
		ids[i] = in.Counts[i].LineID
	}
	var existing int64
	if err := tx.Table("pharmacy_stocktake_lines").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND id IN ?", p.TenantID, outlet, take, ids).Count(&existing).Error; err != nil {
		return err
	}
	if existing != int64(len(ids)) {
		return gorm.ErrRecordNotFound
	}
	// Acquire all bucket locks before the INSERT's snapshot, in ledger key order.
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(tenant_id::text||':'||outlet_id::text||':'||location_id::text||':'||batch_id::text,0))
 FROM pharmacy_stocktake_lines WHERE tenant_id=? AND outlet_id=? AND stocktake_id=? AND id IN ? ORDER BY location_id,batch_id`, p.TenantID, outlet, take, ids).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO pharmacy_stocktake_commands(tenant_id,outlet_id,id,stocktake_id,kind,expected_version,version,state,item_count,reason,actor_subject,intent_sha256)
 VALUES(?,?,?,?,'count',?,?,'counting',?,?,?,?)`, p.TenantID, outlet, command, take, *in.ExpectedVersion, *in.ExpectedVersion+1, len(in.Counts), in.Reason, p.Subject, hash).Error; err != nil {
		return err
	}
	rows := make([]stocktakeCountRow, len(in.Counts))
	for i, entry := range in.Counts {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create count observation identity: %w", err)
		}
		rows[i] = stocktakeCountRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, StocktakeID: take,
			CommandID: command, CommandVersion: *in.ExpectedVersion + 1, LineID: entry.LineID, CountedUnits: *entry.CountedUnits}
	}
	return tx.CreateInBatches(&rows, 100).Error
}

func (h *catalogueHandlers) getStocktakeCount(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
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
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var err error
		result, _, err = readStocktakeCount(tx, p.TenantID, outlet, take, command)
		if err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_count", command, "command_recovered")
	})
	if err != nil {
		stocktakeError(c, err)
		return
	}
	respond.OK(c, result)
}
