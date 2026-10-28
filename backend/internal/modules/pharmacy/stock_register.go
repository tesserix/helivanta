package pharmacy

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var errStockPeriod = errors.New("stock period ends in the future")

func validateStockCutoff(tx *gorm.DB, asOf time.Time) error {
	var valid bool
	if err := tx.Raw(`SELECT ?::timestamptz <= clock_timestamp()`, asOf).Scan(&valid).Error; err != nil {
		return err
	}
	if !valid {
		return errStockPeriod
	}
	return nil
}

type stockWindow struct{ From, AsOf time.Time }

func parseStockWindow(c *gin.Context, required bool) (stockWindow, bool) {
	window := stockWindow{From: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), AsOf: time.Now().UTC()}
	for _, field := range []struct {
		name   string
		target *time.Time
	}{{"from", &window.From}, {"as_of", &window.AsOf}} {
		raw := c.Query(field.name)
		if raw == "" {
			if required && field.name == "as_of" {
				respond.Error(c, http.StatusBadRequest, "invalid_request", "as_of must be an explicit RFC3339 timestamp")
				return window, false
			}
			continue
		}
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || value.Year() < 1970 || value.Year() > 9999 || value.Nanosecond()%1000 != 0 {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "stock period requires RFC3339 timestamps with at most microsecond precision")
			return window, false
		}
		*field.target = value.UTC()
	}
	if (required || c.Query("as_of") != "") && window.From.After(window.AsOf) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "stock period must end at or after its start")
		return window, false
	}
	return window, true
}

type stockPosition struct {
	StorageAddress    StorageAddressFields `json:"storage_address" gorm:"embedded"`
	StockExpiryFields `gorm:"embedded"`
	ID                uuid.UUID `json:"id"`
	CreatedAt         time.Time `json:"created_at"`
	LocationID        uuid.UUID `json:"location_id"`
	BatchID           uuid.UUID `json:"batch_id"`
	ProductID         uuid.UUID `json:"product_id"`
	LocationCode      string    `json:"location_code"`
	LocationName      string    `json:"location_name"`
	SKU               string    `json:"sku"`
	BrandName         string    `json:"brand_name"`
	UnitOfIssue       string    `json:"unit_of_issue"`
	LotNumber         string    `json:"lot_number"`
	ExpiresOn         string    `json:"expires_on"`
	StockStatus       string    `json:"stock_status"`
	ExpiryStatus      string    `json:"expiry_status"`
	ProductStatus     string    `json:"product_status"`
	RegulatoryStatus  string    `json:"regulatory_status"`
	OnHandUnits       int64     `json:"on_hand_units"`
	ReceiptUnits      string    `json:"receipt_units"`
	CorrectionUnits   string    `json:"correction_units"`
	DispensableUnits  int64     `json:"dispensable_units"`
}

func (r stockPosition) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func stockReference(tx *gorm.DB, table string, tenant string, outlet, id uuid.UUID) error {
	if id == uuid.Nil {
		return nil
	}
	var count int64
	if err := tx.Table(table).Where("tenant_id=? AND outlet_id=? AND id=?", tenant, outlet, id).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (h *catalogueHandlers) listStockPositions(c *gin.Context, params pagination.Params) ([]stockPosition, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	location, ok := stockFilter(c, "location_id", false)
	if !ok {
		return nil, nil
	}
	product, ok := stockFilter(c, "product_id", false)
	if !ok {
		return nil, nil
	}
	// One lot: with location_id it names one position, which a reload of
	// the stock register reopens (#1303 E3).
	batch, ok := stockFilter(c, "batch_id", false)
	if !ok {
		return nil, nil
	}
	window, ok := parseStockWindow(c, true)
	if !ok {
		return nil, nil
	}
	expiry := c.DefaultQuery("expiry", "all")
	switch expiry {
	case "all", "expired", "unexpired", "today", "due_30", "due_90", "due_180":
	default:
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid expiry filter")
		return nil, nil
	}
	balance := c.DefaultQuery("balance", "all")
	if (balance != "all" && balance != "on_hand") || len(c.QueryArray("expiry")) > 1 || len(c.QueryArray("balance")) > 1 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid or repeated stock filter")
		return nil, nil
	}
	var rows []stockPosition
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := validateStockCutoff(tx, window.AsOf); err != nil {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_locations", p.TenantID, outlet, location); err != nil {
			return err
		}
		if err := stockReference(tx, "pharmacy_catalogue_products", p.TenantID, outlet, product); err != nil {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_batches", p.TenantID, outlet, batch); err != nil {
			return err
		}
		source := stockPositionsSource(tx, p.TenantID, outlet, window.AsOf)
		if location != uuid.Nil {
			source = source.Where("m.location_id=?", location)
		}
		if product != uuid.Nil {
			source = source.Where("b.product_id=?", product)
		}
		if batch != uuid.Nil {
			source = source.Where("m.batch_id=?", batch)
		}
		q := tenantdb.ApplyKeyset(tx, params).Table("(?) AS positions", source)
		return stockExpiryScope(expiry, balance)(q).Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

type stockReconciliationTotals struct {
	OpeningUnits    int64  `json:"opening_units"`
	ReceiptUnits    string `json:"receipt_units"`
	CorrectionUnits string `json:"correction_units"`
	ClosingUnits    int64  `json:"closing_units"`
	MovementCount   int64  `json:"movement_count"`
}

type stockReconciliation struct {
	LocationID uuid.UUID `json:"location_id"`
	BatchID    uuid.UUID `json:"batch_id"`
	From       time.Time `json:"from"`
	AsOf       time.Time `json:"as_of"`
	stockReconciliationTotals
}

func (h *catalogueHandlers) getStockReconciliation(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	location, ok := stockFilter(c, "location_id", true)
	if !ok {
		return
	}
	batch, ok := stockFilter(c, "batch_id", true)
	if !ok {
		return
	}
	window, ok := parseStockWindow(c, true)
	if !ok {
		return
	}
	// Scan resets its destination, so totals land in their own struct and never touch the echoed period.
	result := stockReconciliation{LocationID: location, BatchID: batch, From: window.From, AsOf: window.AsOf}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := validateStockCutoff(tx, window.AsOf); err != nil {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_locations", p.TenantID, outlet, location); err != nil {
			return err
		}
		if err := stockReference(tx, "pharmacy_stock_batches", p.TenantID, outlet, batch); err != nil {
			return err
		}
		return tx.Raw(`SELECT COALESCE(sum(quantity_units) FILTER(WHERE created_at < ?),0)::bigint AS opening_units,
   COALESCE(sum(quantity_units) FILTER(WHERE created_at >= ? AND kind='receipt'),0)::text AS receipt_units,
   COALESCE(sum(quantity_units) FILTER(WHERE created_at >= ? AND kind='correction'),0)::text AS correction_units,
   COALESCE(sum(quantity_units),0)::bigint AS closing_units,
   count(*) FILTER(WHERE created_at >= ?) AS movement_count
   FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=? AND location_id=? AND batch_id=? AND created_at<=?`, window.From, window.From, window.From, window.From, p.TenantID, outlet, location, batch, window.AsOf).Scan(&result.stockReconciliationTotals).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

// stockPositionsSource is the stock register's rows: one per batch at a
// location, as of asOf, with its expiry on the outlet's own date. Shared by
// the register and the overview's counts (#576).
func stockPositionsSource(tx *gorm.DB, tenant string, outlet uuid.UUID, asOf time.Time) *gorm.DB {
	// The hash identifies a read-model bucket only; it never authorises an object or creates a stock master.
	q := tx.Table("pharmacy_stock_movements m").Select(`md5(m.location_id::text||':'||m.batch_id::text)::uuid AS id,
   b.created_at,m.location_id,m.batch_id,b.product_id,l.code AS location_code,l.name AS location_name,l.zone_code,l.aisle_code,l.rack_code,l.shelf_code,
   m.outlet_id,o.code AS outlet_code,o.name AS outlet_name,p.currency,p.pack_size,b.purchase_cost_minor,
   p.sku,p.brand_name,p.unit_of_issue,b.lot_number,to_char(b.expires_on,'YYYY-MM-DD') AS expires_on,l.stock_status,
   CASE WHEN b.expires_on < snapshot.local_date THEN 'expired' ELSE 'unexpired' END AS expiry_status,
   o.timezone AS expiry_time_zone,to_char(snapshot.local_date,'YYYY-MM-DD') AS expiry_local_date,
   b.expires_on-snapshot.local_date AS days_to_expiry,
   CASE WHEN b.expires_on < snapshot.local_date THEN 'expired'
        WHEN b.expires_on = snapshot.local_date THEN 'today'
        WHEN b.expires_on-snapshot.local_date <= 30 THEN 'days_30'
        WHEN b.expires_on-snapshot.local_date <= 90 THEN 'days_90'
        WHEN b.expires_on-snapshot.local_date <= 180 THEN 'days_180'
        ELSE 'later' END AS expiry_band,
   p.status AS product_status,p.regulatory_status,
   sum(m.quantity_units)::bigint AS on_hand_units,
   COALESCE(sum(m.quantity_units) FILTER (WHERE m.kind='receipt'),0)::text AS receipt_units,
   COALESCE(sum(m.quantity_units) FILTER (WHERE m.kind='correction'),0)::text AS correction_units,
   0::bigint AS dispensable_units`).
		// Unique-key lookup bounds prevent repeated outlet-wide scans under fresh-table RLS estimates.
		Joins("JOIN LATERAL (SELECT created_at,product_id,lot_number,expires_on,purchase_cost_minor FROM pharmacy_stock_batches WHERE tenant_id=m.tenant_id AND outlet_id=m.outlet_id AND id=m.batch_id LIMIT 1) b ON true").
		Joins("JOIN LATERAL (SELECT code,name,zone_code,aisle_code,rack_code,shelf_code,stock_status FROM pharmacy_stock_locations WHERE tenant_id=m.tenant_id AND outlet_id=m.outlet_id AND id=m.location_id LIMIT 1) l ON true").
		Joins("JOIN LATERAL (SELECT sku,brand_name,unit_of_issue,status,regulatory_status,currency,pack_size FROM pharmacy_catalogue_products WHERE tenant_id=m.tenant_id AND outlet_id=m.outlet_id AND id=b.product_id LIMIT 1) p ON true").
		Joins("JOIN pharmacy_outlets o ON o.tenant_id=m.tenant_id AND o.id=m.outlet_id").
		Joins("CROSS JOIN LATERAL (SELECT timezone(o.timezone,?::timestamptz)::date AS local_date) snapshot", asOf).
		Where("m.tenant_id=? AND m.created_at<=?", tenant, asOf).
		Group("m.outlet_id,o.code,o.name,p.currency,p.pack_size,b.purchase_cost_minor,b.created_at,m.location_id,m.batch_id,b.product_id,l.code,l.name,l.zone_code,l.aisle_code,l.rack_code,l.shelf_code,p.sku,p.brand_name,p.unit_of_issue,b.lot_number,b.expires_on,l.stock_status,o.timezone,snapshot.local_date,p.status,p.regulatory_status")
	if outlet != uuid.Nil {
		q = q.Where("m.outlet_id=?", outlet)
	}
	return q
}

// stockExpiryScope is the register's expiry and balance filter, applied to
// stockPositionsSource rows; values are validated by the caller. due_N is
// cumulative: not yet expired and expiring within N days.
func stockExpiryScope(expiry, balance string) func(*gorm.DB) *gorm.DB {
	due := map[string]int{"today": 0, "due_30": 30, "due_90": 90, "due_180": 180}
	return func(q *gorm.DB) *gorm.DB {
		if days, ok := due[expiry]; ok {
			q = q.Where("days_to_expiry BETWEEN 0 AND ?", days)
		} else if expiry != "all" {
			q = q.Where("expiry_status=?", expiry)
		}
		if balance == "on_hand" {
			q = q.Where("on_hand_units>0")
		}
		return q
	}
}
