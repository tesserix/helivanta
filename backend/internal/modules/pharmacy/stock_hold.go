package pharmacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

var errStockHoldAudit = errors.New("stock hold read evidence unavailable")

type stockHoldRow struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	OutletID             uuid.UUID
	BatchID              uuid.UUID
	FromLocationID       uuid.UUID
	QuarantineLocationID uuid.UUID
	QuantityUnits        int64
	Reason               string
	PhysicalDisposition  string
	OutMovementID        uuid.UUID
	InMovementID         uuid.UUID
	IntentSHA256         string
	ActorSubject         string
	CreatedAt            time.Time `gorm:"autoCreateTime:false;default:now()"`
}

const stockHoldColumns = "id,tenant_id,outlet_id,batch_id,from_location_id,quarantine_location_id,quantity_units,reason,physical_disposition,out_movement_id,in_movement_id,intent_sha256,actor_subject,created_at"

func (stockHoldRow) TableName() string { return "pharmacy_stock_holds" }

func (r stockHoldRow) receipt() StockHoldReceipt {
	return StockHoldReceipt{StockHoldInput: StockHoldInput{BatchID: r.BatchID, FromLocationID: r.FromLocationID,
		QuarantineLocationID: r.QuarantineLocationID, QuantityUnits: &r.QuantityUnits, Reason: r.Reason, PhysicalDisposition: r.PhysicalDisposition},
		ID: r.ID, OutletID: r.OutletID, OutMovementID: r.OutMovementID, InMovementID: r.InMovementID,
		HeldBy: r.ActorSubject, HeldAt: r.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func stockHoldRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/holds/:hold", PermStockQuarantine, h.putStockHold)
	s.GET("/holds/:hold", PermStockRead, h.getStockHold)
}

func stockHoldID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("hold"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "stock hold")
		return uuid.Nil, false
	}
	return id, true
}

func validStockHold(in *StockHoldInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	return in.BatchID.Version() == 7 && in.BatchID.Variant() == uuid.RFC4122 &&
		in.FromLocationID.Version() == 7 && in.FromLocationID.Variant() == uuid.RFC4122 &&
		in.QuarantineLocationID.Version() == 7 && in.QuarantineLocationID.Variant() == uuid.RFC4122 &&
		in.FromLocationID != in.QuarantineLocationID && in.QuantityUnits != nil &&
		*in.QuantityUnits >= 1 && *in.QuantityUnits <= 1000000000 && stocktakeText(in.Reason, 500) && in.PhysicalDisposition == "moved_to_quarantine"
}

func (h *catalogueHandlers) putStockHold(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	id, ok := stockHoldID(c)
	if !ok {
		return
	}
	var in StockHoldInput
	if !decodeStockCommand(c, &in) {
		return
	}
	if !validStockHold(&in) || c.Request.URL.RawQuery != "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "A stock hold needs exact batches, two different locations, units, a reason and physical quarantine confirmation.")
		return
	}
	raw, err := json.Marshal(struct {
		Actor string
		Input StockHoldInput
	}{p.Subject, in})
	if err != nil {
		stockHoldError(c, err)
		return
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result stockHoldRow
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":hold:"+id.String()); err != nil {
			return err
		}
		err := tx.Select(stockHoldColumns).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&result).Error
		if err == nil {
			if result.ActorSubject != p.Subject || result.IntentSHA256 != digest {
				return errStockIdentity
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_batches", p.TenantID, outlet, in.BatchID); err != nil {
			return err
		}
		var locations []stockLocation
		if err := tx.Select("id,stock_status").Where("tenant_id=? AND outlet_id=? AND id IN ?", p.TenantID, outlet, []uuid.UUID{in.FromLocationID, in.QuarantineLocationID}).Find(&locations).Error; err != nil {
			return err
		}
		if len(locations) != 2 {
			return gorm.ErrRecordNotFound
		}
		for _, location := range locations {
			if location.ID == in.FromLocationID && location.StockStatus != "sellable" || location.ID == in.QuarantineLocationID && location.StockStatus != "quarantine" {
				return errStockIdentity
			}
		}
		if err := lockStockTransferBuckets(tx, p.TenantID, outlet, in.BatchID, in.FromLocationID, in.QuarantineLocationID); err != nil {
			return err
		}
		result = stockHoldRow{ID: id, TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, BatchID: in.BatchID, FromLocationID: in.FromLocationID,
			QuarantineLocationID: in.QuarantineLocationID, QuantityUnits: *in.QuantityUnits, Reason: in.Reason, PhysicalDisposition: in.PhysicalDisposition,
			OutMovementID: uuid.Must(uuid.NewV7()), InMovementID: uuid.Must(uuid.NewV7()), IntentSHA256: digest, ActorSubject: p.Subject}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		for _, leg := range []struct {
			id, location uuid.UUID
			quantity     int64
			side         string
		}{
			{result.OutMovementID, result.FromLocationID, -result.QuantityUnits, "out"},
			{result.InMovementID, result.QuarantineLocationID, result.QuantityUnits, "in"},
		} {
			_, _, err := h.recordMovement(tx, p, currentPharmacy(ctx), outlet, leg.id, movementInput{LocationID: leg.location, BatchID: in.BatchID, Kind: "quarantine_hold", QuantityUnits: leg.quantity, Reason: in.Reason, SourceReference: "hold:" + id.String() + ":" + leg.side})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		stockHoldError(c, err)
		return
	}
	respond.OK(c, result.receipt())
}

func (h *catalogueHandlers) getStockHold(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	id, ok := stockHoldID(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Stock hold recovery does not accept a body or query.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var result stockHoldRow
	err := h.transaction(ctx, p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Select(stockHoldColumns).Where("tenant_id=? AND outlet_id=? AND id=? AND actor_subject=?", p.TenantID, outlet, id, p.Subject).Take(&result).Error; err != nil {
			return err
		}
		if err := auditRegisterRead(c, tx, "stock_holds", p.TenantID, p.Subject, id, "recover"); err != nil {
			return errors.Join(errStockHoldAudit, err)
		}
		return nil
	})
	if err != nil {
		stockHoldError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	respond.OK(c, result.receipt())
}

func stockHoldError(c *gin.Context, err error) {
	if errors.Is(err, errStockHoldAudit) {
		respond.Error(c, http.StatusServiceUnavailable, "stock_hold_evidence_unavailable", "Stock hold read evidence is unavailable.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.TableName == "audit_events" || pg.TableName == "outbox_events" {
			respond.Error(c, http.StatusServiceUnavailable, "stock_hold_evidence_unavailable", "Stock hold evidence could not be retained. Recover the original command before retrying.")
			return
		}
		if strings.HasPrefix(pg.ConstraintName, "pharmacy_stock_hold") {
			respond.Conflict(c, "Stock hold evidence changed or could not be completed.")
			return
		}
	}
	stockError(c, err)
}

func lockStockTransferBuckets(tx *gorm.DB, tenant string, outlet, batch, from, to uuid.UUID) error {
	keys := []string{tenant + ":" + outlet.String() + ":" + from.String() + ":" + batch.String(), tenant + ":" + outlet.String() + ":" + to.String() + ":" + batch.String()}
	sort.Strings(keys)
	for _, key := range keys {
		if err := stockLock(tx, key); err != nil {
			return err
		}
	}
	return nil
}
