package pharmacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	errStocktakeIdentity   = errors.New("stocktake command identity changed")
	errStocktakeLimit      = errors.New("stocktake snapshot exceeds its bound")
	errStocktakeAudit      = errors.New("stocktake read evidence unavailable")
	errStocktakeQuarantine = errors.New("stocktake destination is not quarantine")
)

type stocktakeHeaderRow struct {
	StocktakeSummary
	IntentSHA256           string    `json:"-"`
	ProductsJSON           string    `json:"-"`
	CreatedAt              time.Time `json:"-"`
	CountedRows            int64     `json:"-"`
	DifferenceRows         int64     `json:"-"`
	PositiveDifferenceRows int64     `json:"-"`
	ReviewRelationship     string    `json:"-"`
}

func (stocktakeHeaderRow) TableName() string                 { return "pharmacy_stocktakes" }
func (r stocktakeHeaderRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type stocktakeLineRow struct {
	StocktakeLine
	TenantID  uuid.UUID `json:"-"`
	OutletID  uuid.UUID `json:"-"`
	CreatedAt time.Time `json:"-"`
}

func (stocktakeLineRow) TableName() string                 { return "pharmacy_stocktake_lines" }
func (r stocktakeLineRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const stocktakeHeaderBaseColumns = `id,outlet_id,number,selection,array_to_json(product_ids)::text products_json,
 quarantine_location_id,reason,actor_subject started_by,created_at,intent_sha256,snapshot_rows`

const stocktakeHeaderColumns = stocktakeHeaderBaseColumns + `,'counting'::text state,0::bigint version`

const stocktakeCurrentHeaderColumns = stocktakeHeaderBaseColumns + `,
 COALESCE((SELECT state FROM pharmacy_stocktake_commands c WHERE c.tenant_id=pharmacy_stocktakes.tenant_id AND c.outlet_id=pharmacy_stocktakes.outlet_id AND c.stocktake_id=pharmacy_stocktakes.id ORDER BY version DESC LIMIT 1),'counting') state,
 COALESCE((SELECT MAX(version) FROM pharmacy_stocktake_commands c WHERE c.tenant_id=pharmacy_stocktakes.tenant_id AND c.outlet_id=pharmacy_stocktakes.outlet_id AND c.stocktake_id=pharmacy_stocktakes.id),0) version`

const stocktakeProgressJoin = `LEFT JOIN LATERAL(
 SELECT COUNT(*) counted_rows,COUNT(*)FILTER(WHERE observed.counted_units<>l.book_units) difference_rows,
 COUNT(*)FILTER(WHERE observed.counted_units>l.book_units) positive_difference_rows
 FROM pharmacy_stocktake_lines l JOIN LATERAL(
  SELECT counted_units FROM pharmacy_stocktake_counts e WHERE e.tenant_id=l.tenant_id AND e.outlet_id=l.outlet_id AND e.stocktake_id=l.stocktake_id AND e.line_id=l.id ORDER BY command_version DESC LIMIT 1
 )observed ON true
 WHERE l.tenant_id=pharmacy_stocktakes.tenant_id AND l.outlet_id=pharmacy_stocktakes.outlet_id AND l.stocktake_id=pharmacy_stocktakes.id
)progress ON true`

const stocktakeRelationshipColumn = `CASE
 WHEN pharmacy_stocktakes.actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),'')THEN 'starter'
 WHEN EXISTS(SELECT 1 FROM pharmacy_stocktake_commands c WHERE c.tenant_id=pharmacy_stocktakes.tenant_id AND c.outlet_id=pharmacy_stocktakes.outlet_id AND c.stocktake_id=pharmacy_stocktakes.id AND c.kind='count'AND c.actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''))THEN 'counter'
 ELSE 'independent'END review_relationship`

const stocktakeObservedJoin = `LEFT JOIN LATERAL(
 SELECT e.counted_units,c.actor_subject counted_by,c.reason count_reason,
 to_char(c.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') counted_at
 FROM pharmacy_stocktake_counts e JOIN pharmacy_stocktake_commands c ON c.tenant_id=e.tenant_id AND c.outlet_id=e.outlet_id AND c.id=e.command_id
 WHERE e.tenant_id=pharmacy_stocktake_lines.tenant_id AND e.outlet_id=pharmacy_stocktake_lines.outlet_id AND e.stocktake_id=pharmacy_stocktake_lines.stocktake_id AND e.line_id=pharmacy_stocktake_lines.id
 ORDER BY e.command_version DESC LIMIT 1
)observed ON true`

const stocktakeLineColumns = `id,stocktake_id,product_id,product_version,sku,brand_name,unit_of_issue,batch_id,lot_number,
 expires_on::text,location_id,location_code,location_name,stock_status,book_units,movement_count,regulatory_class,
 ndps_status,schedule_declaration_id,created_at`

const stocktakeSnapshotQuery = `WITH products AS (
 SELECT id,tenant_id,outlet_id,version,sku,brand_name,unit_of_issue FROM pharmacy_catalogue_products
 WHERE tenant_id=? AND outlet_id=? AND (? OR id=ANY(?::uuid[]))
),book AS (
 SELECT p.id product_id,p.version product_version,p.sku,p.brand_name,p.unit_of_issue,
 b.id batch_id,b.lot_number,b.expires_on::text expires_on,
 l.id location_id,l.code location_code,l.name location_name,l.stock_status,
 SUM(m.quantity_units)::bigint book_units,COUNT(*)::bigint movement_count,
 c.declaration_id schedule_declaration_id,c.regulatory_class,
 CASE WHEN c.declaration_id IS NULL THEN 'unknown' WHEN c.ndps THEN 'yes' ELSE 'no' END ndps_status
 FROM products p JOIN pharmacy_stock_batches b ON b.tenant_id=p.tenant_id AND b.outlet_id=p.outlet_id AND b.product_id=p.id
 JOIN pharmacy_stock_movements m ON m.tenant_id=b.tenant_id AND m.outlet_id=b.outlet_id AND m.batch_id=b.id
 JOIN pharmacy_stock_locations l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.id=m.location_id
 LEFT JOIN LATERAL pharmacy_product_schedule_in_force(p.tenant_id,p.outlet_id,p.id)c ON true
 GROUP BY p.id,p.version,p.sku,p.brand_name,p.unit_of_issue,b.id,b.lot_number,b.expires_on,
 l.id,l.code,l.name,l.stock_status,c.declaration_id,c.regulatory_class,c.ndps
 ORDER BY p.id,b.id,l.id LIMIT 10001
)
SELECT statement_timestamp() captured_at,(SELECT COUNT(*) FROM products) product_count,
 COALESCE(jsonb_agg(to_jsonb(book)),'[]'::jsonb) body FROM book`

func stocktakeRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/stocktakes/:stocktake", PermStockCorrect, h.putStocktake)
	s.GET("/stocktakes/:stocktake", PermStockRead, h.getStocktake)
	platform.ListRoute(s, "/stocktakes", PermStockRead, h.listStocktakes)
	platform.ListRoute(s, "/stocktakes/:stocktake/lines", PermStockRead, h.listStocktakeLines)
	s.PUT("/stocktakes/:stocktake/counts/:command", PermStockCorrect, h.putStocktakeCount)
	s.GET("/stocktakes/:stocktake/counts/:command", PermStockRead, h.getStocktakeCount)
	s.PUT("/stocktakes/:stocktake/decisions/:command", PermStockCorrect, h.putStocktakeDecision)
	s.PUT("/stocktakes/:stocktake/cancellations/:command", PermStockCorrect, h.putStocktakeCancellation)
	s.GET("/stocktakes/:stocktake/decisions/:command", PermStockRead, h.getStocktakeDecision)
	s.GET("/stocktakes/:stocktake/register.csv", PermStockRead, h.exportStocktakeRegister)
}

func stocktakeID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("stocktake"))
	if err != nil || !stocktakeRecordID(id) {
		respond.NotFound(c, "stocktake")
		return uuid.Nil, false
	}
	return id, true
}

func stocktakeRead(c *gin.Context, paged bool) bool {
	if c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Stocktake reads do not accept a body.")
		return false
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || !paged && c.Request.URL.RawQuery != "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a valid stocktake query.")
		return false
	}
	for key, values := range query {
		if !paged || key != "limit" && key != "cursor" || len(values) != 1 || values[0] == "" {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "Use only bounded stocktake pagination parameters.")
			return false
		}
	}
	return true
}

func stocktakeError(c *gin.Context, err error) {
	if errors.Is(err, errStocktakeIndependence) {
		respond.Conflict(c, "Review requires independent Staff and verifiable participant identity history.")
		return
	}
	if errors.Is(err, errStocktakePostingLimit) {
		respond.Error(c, 409, "stocktake_posting_limit", "These differences exceed posting limits. Retain the observations and ask the responsible stock owner to review them.")
		return
	}
	if errors.Is(err, errStocktakeAudit) {
		respond.Error(c, http.StatusServiceUnavailable, "audit_unavailable", "Stocktake read evidence is unavailable.")
		return
	}
	if errors.Is(err, errStocktakeIdentity) {
		respond.Conflict(c, "This stocktake command already has different details or an original actor.")
		return
	}
	if errors.Is(err, errStocktakeLimit) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "A stocktake supports at most 10000 batch/location buckets. Choose fewer products.")
		return
	}
	if errors.Is(err, errStocktakeQuarantine) {
		respond.Conflict(c, "Extra stock found in a count needs a quarantine destination.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.TableName == "audit_events" || pg.TableName == "outbox_events") {
		respond.Error(c, http.StatusServiceUnavailable, "stocktake_evidence_unavailable", "Stocktake evidence could not be retained. Recover the original command before retrying.")
		return
	}
	if errors.As(err, &pg) && strings.HasPrefix(pg.ConstraintName, "pharmacy_stocktake") {
		respond.Conflict(c, "The stocktake snapshot or state changed. Review the existing count before continuing.")
		return
	}
	stockError(c, err)
}

func stocktakeReadAudit(c *gin.Context, tx *gorm.DB, p authn.Principal, kind string, resource uuid.UUID, action string) error {
	if err := auditRegisterRead(c, tx, kind, p.TenantID, p.Subject, resource, action); err != nil {
		return errors.Join(errStocktakeAudit, err)
	}
	return nil
}

func stocktakeHash(actor string, in any) (string, error) {
	raw, err := json.Marshal(struct {
		Actor string `json:"actor"`
		Input any    `json:"input"`
	}{actor, in})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func stocktakeUUIDArray(ids []uuid.UUID) string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = id.String()
	}
	return "{" + strings.Join(values, ",") + "}"
}

func readStocktakeHeader(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (stocktakeHeaderRow, error) {
	var row stocktakeHeaderRow
	err := tx.Select(stocktakeHeaderColumns).Where("tenant_id=? AND outlet_id=? AND id=? AND sealed", tenant, outlet, id).Take(&row).Error
	if err != nil {
		return row, err
	}
	err = finishStocktakeHeader(&row)
	return row, err
}

func finishStocktakeHeader(row *stocktakeHeaderRow) error {
	row.StartedAt = row.CreatedAt.UTC().Format(time.RFC3339Nano)
	row.ProductIds = []uuid.UUID{}
	if err := json.Unmarshal([]byte(row.ProductsJSON), &row.ProductIds); err != nil {
		return fmt.Errorf("decode retained stocktake selection: %w", err)
	}
	return nil
}

func (h *catalogueHandlers) putStocktake(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
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
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Choose all products or an exact product selection, a quarantine location and a reason.")
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
	err = h.transaction(ctx, p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":stocktake-start"); err != nil {
			return err
		}
		saved, err := readStocktakeHeader(tx, p.TenantID, outlet, id)
		if err == nil {
			if saved.StartedBy != p.Subject || saved.IntentSHA256 != hash {
				return errStocktakeIdentity
			}
			result = saved
			return stocktakeReadAudit(c, tx, p, "stocktake", id, "command_recovered")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := startStocktake(tx, p, outlet, id, in, hash); err != nil {
			return err
		}
		result, err = readStocktakeHeader(tx, p.TenantID, outlet, id)
		return err
	})
	if err != nil {
		stocktakeError(c, err)
		return
	}
	respond.OK(c, Stocktake{StocktakeSummary: result.StocktakeSummary, ReviewRelationship: "starter"})
}

func readCurrentStocktakeHeader(tx *gorm.DB, tenant string, outlet, id uuid.UUID) (stocktakeHeaderRow, error) {
	var result stocktakeHeaderRow
	if err := tx.Select(stocktakeCurrentHeaderColumns+",progress.counted_rows,progress.difference_rows,progress.positive_difference_rows,"+stocktakeRelationshipColumn).Joins(stocktakeProgressJoin).Where("tenant_id=? AND outlet_id=? AND id=? AND sealed", tenant, outlet, id).Take(&result).Error; err != nil {
		return result, err
	}
	err := finishStocktakeHeader(&result)
	return result, err
}

func startStocktake(tx *gorm.DB, p authn.Principal, outlet, id uuid.UUID, in StocktakeStartInput, hash string) error {
	var target stockLocation
	if err := tx.Select("id,stock_status").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, in.QuarantineLocationID).Take(&target).Error; err != nil {
		return err
	}
	if target.StockStatus != "quarantine" {
		return errStocktakeQuarantine
	}
	var capture struct {
		CapturedAt   time.Time
		ProductCount int64
		Body         json.RawMessage
	}
	if err := tx.Raw(stocktakeSnapshotQuery, p.TenantID, outlet, in.Selection == "all", stocktakeUUIDArray(in.ProductIds)).Scan(&capture).Error; err != nil {
		return err
	}
	if in.Selection == "selected" && capture.ProductCount != int64(len(in.ProductIds)) {
		return gorm.ErrRecordNotFound
	}
	rows := []stocktakeLineRow{}
	if err := json.Unmarshal(capture.Body, &rows); err != nil {
		return fmt.Errorf("decode stocktake book snapshot: %w", err)
	}
	if len(rows) > 10000 {
		return errStocktakeLimit
	}
	if err := tx.Exec(`INSERT INTO pharmacy_stocktakes(tenant_id,outlet_id,id,number,selection,product_ids,quarantine_location_id,reason,actor_subject,intent_sha256,snapshot_rows,created_at)
 VALUES(?,?,?,0,?,?::uuid[],?,?,?,?,?,?)`, p.TenantID, outlet, id, in.Selection, stocktakeUUIDArray(in.ProductIds), in.QuarantineLocationID, in.Reason, p.Subject, hash, len(rows), capture.CapturedAt).Error; err != nil {
		return err
	}
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil {
		return err
	}
	for i := range rows {
		lineID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create stocktake line identity: %w", err)
		}
		rows[i].ID, rows[i].StocktakeID = lineID, id
		rows[i].TenantID, rows[i].OutletID, rows[i].CreatedAt = tenant, outlet, capture.CapturedAt
	}
	if len(rows) > 0 {
		columns := []string{"tenant_id", "outlet_id", "id", "stocktake_id", "product_id", "product_version", "sku", "brand_name", "unit_of_issue", "batch_id", "lot_number", "expires_on", "location_id", "location_code", "location_name", "stock_status", "book_units", "movement_count", "regulatory_class", "ndps_status", "schedule_declaration_id", "created_at"}
		if err := tx.Select(columns).CreateInBatches(&rows, 500).Error; err != nil {
			return err
		}
	}
	return tx.Exec("UPDATE pharmacy_stocktakes SET sealed=true WHERE tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Error
}

func (h *catalogueHandlers) getStocktake(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	id, ok := stocktakeID(c)
	if !ok || !stocktakeRead(c, false) {
		return
	}
	var result stocktakeHeaderRow
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var err error
		result, err = readCurrentStocktakeHeader(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake", id, "record_read")
	})
	if err != nil {
		stocktakeError(c, err)
		return
	}
	respond.OK(c, Stocktake{StocktakeSummary: result.StocktakeSummary, CountedRows: result.CountedRows, DifferenceRows: result.DifferenceRows, PositiveDifferenceRows: result.PositiveDifferenceRows, ReviewRelationship: result.ReviewRelationship})
}

func (h *catalogueHandlers) listStocktakes(c *gin.Context, params pagination.Params) ([]stocktakeHeaderRow, error) {
	p, outlet, ok := stockCommandScope(c)
	if !ok || !stocktakeRead(c, true) {
		return nil, nil
	}
	rows := []stocktakeHeaderRow{}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(stocktakeCurrentHeaderColumns).Where("tenant_id=? AND outlet_id=? AND sealed", p.TenantID, outlet).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if err := finishStocktakeHeader(&rows[i]); err != nil {
				return err
			}
		}
		return stocktakeReadAudit(c, tx, p, "stocktakes", outlet, "list_read")
	})
	if err != nil {
		stocktakeError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) listStocktakeLines(c *gin.Context, params pagination.Params) ([]stocktakeLineRow, error) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := stocktakeID(c)
	if !ok || !stocktakeRead(c, true) {
		return nil, nil
	}
	rows := []stocktakeLineRow{}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if _, err := readStocktakeHeader(tx, p.TenantID, outlet, id); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(stocktakeLineColumns+",observed.counted_units,observed.counted_by,observed.count_reason,observed.counted_at,observed.counted_units-book_units difference_units").Joins(stocktakeObservedJoin).Where("tenant_id=? AND outlet_id=? AND stocktake_id=?", p.TenantID, outlet, id).Find(&rows).Error; err != nil {
			return err
		}
		return stocktakeReadAudit(c, tx, p, "stocktake_lines", id, "list_read")
	})
	if err != nil {
		stocktakeError(c, err)
		return nil, nil
	}
	for i := range rows {
		rows[i].SnapshottedAt = rows[i].CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return rows, nil
}
