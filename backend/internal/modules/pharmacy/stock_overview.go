package pharmacy

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/csvsafe"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Product identity remains outlet-owned. A shared SKU or name is not evidence
// that two outlets hold the same formulation, strength or pack.
type stockOverviewPosition struct {
	Position            stockPosition `gorm:"embedded" json:"-"`
	StockOverviewFields `gorm:"embedded"`
}

func (r stockOverviewPosition) PageKey() (time.Time, uuid.UUID) { return r.Position.PageKey() }

func (r stockOverviewPosition) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		stockPosition
		StockOverviewFields
	}{r.Position, r.StockOverviewFields})
}

type stockOverviewFilter struct {
	asOf            time.Time
	expiry, balance string
	outlet          uuid.UUID
}

func stockOverviewRoutes(g *platform.Router, h *catalogueHandlers) {
	s := g.Group("/pharmacies/:pharmacy/stock")
	platform.ListRoute(s, "/positions", PermStockRead, h.listStockOverview)
	s.GET("/positions.csv", PermStockRead, h.exportStockOverview)
}

func parseStockOverview(c *gin.Context, export bool) (stockOverviewFilter, bool) {
	var f stockOverviewFilter
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.Error(c, 400, "invalid_request", "invalid stock filters")
		return f, false
	}
	for key, values := range query {
		allowed := key == "as_of" || key == "expiry" || key == "balance" || key == "outlet_id" || (!export && (key == "limit" || key == "cursor"))
		if !allowed || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "unsupported, empty or repeated stock filter")
			return f, false
		}
	}
	window, ok := parseStockWindow(c, true)
	if !ok {
		return f, false
	}
	f.asOf = window.AsOf
	f.expiry = c.DefaultQuery("expiry", "all")
	switch f.expiry {
	case "all", "expired", "unexpired", "today", "due_30", "due_90", "due_180":
	default:
		respond.Error(c, 400, "invalid_request", "invalid expiry filter")
		return f, false
	}
	f.balance = c.DefaultQuery("balance", "all")
	if f.balance != "all" && f.balance != "on_hand" {
		respond.Error(c, 400, "invalid_request", "invalid balance filter")
		return f, false
	}
	if c.Query("outlet_id") != "" {
		f.outlet, err = uuid.Parse(c.Query("outlet_id"))
		if err != nil || f.outlet == uuid.Nil {
			respond.Error(c, 400, "invalid_request", "invalid outlet filter")
			return f, false
		}
	}
	return f, true
}

func (h *catalogueHandlers) readStockOverview(c *gin.Context, params pagination.Params, export bool) ([]stockOverviewPosition, error) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	pharmacy, ok := registryID(c, "pharmacy")
	if !ok {
		return nil, nil
	}
	f, ok := parseStockOverview(c, export)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var rows []stockOverviewPosition
	var excluded bool
	var values []StockOverviewValuationFields
	err := h.db.WithTenantSnapshot(ctx, p.TenantID, func(tx *gorm.DB) error {
		// Stock permission never makes a directory manager an outlet reader.
		if err := pharmacyVisible(tx, p, pharmacy, false); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return errPharmacyAccessUnavailable
		}
		if err := tx.Exec(`SELECT set_config('statement_timeout','8s',true),set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_overview',?,true)`, p.Subject, pharmacy.String()).Error; err != nil {
			return errPharmacyAccessUnavailable
		}
		if f.outlet != uuid.Nil {
			if _, err := outletAccess(tx, p, f.outlet, pharmacy, false); err != nil {
				return err
			}
		}
		if err := validateStockCutoff(tx, f.asOf); err != nil {
			return err
		}
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM pharmacy_outlets o WHERE o.tenant_id=? AND o.pharmacy_id=? AND NOT EXISTS (SELECT 1 FROM pharmacy_outlet_access a WHERE a.tenant_id=o.tenant_id AND a.outlet_id=o.id AND a.subject=?))`, p.TenantID, pharmacy, p.Subject).Scan(&excluded).Error; err != nil {
			return errPharmacyAccessUnavailable
		}
		source := stockPositionsSource(tx, p.TenantID, f.outlet, f.asOf)
		// SQL-level scope is explicit in addition to the four read-only RLS policies.
		source = source.Where("o.pharmacy_id=? AND EXISTS (SELECT 1 FROM pharmacy_outlet_access a WHERE a.tenant_id=m.tenant_id AND a.outlet_id=m.outlet_id AND a.subject=?)", pharmacy, p.Subject)
		filtered := stockExpiryScope(f.expiry, f.balance)(tx.Table("(?) AS positions", source))
		// Compute complete filtered product totals BEFORE the keyset/limit. Numeric
		// arithmetic avoids bigint overflow. Missing cost remains visible, never zero.
		valued := tx.Table("(?) AS filtered", filtered).Select(`filtered.*,
   sum(on_hand_units::numeric) OVER (PARTITION BY outlet_id,product_id,unit_of_issue)::text AS product_on_hand_units,
   floor(COALESCE(sum(on_hand_units::numeric*purchase_cost_minor) OVER (PARTITION BY outlet_id,product_id,unit_of_issue,currency),0)/pack_size)::text AS known_purchase_value_minor,
   COALESCE(sum(on_hand_units::numeric) FILTER (WHERE purchase_cost_minor IS NULL) OVER (PARTITION BY outlet_id,product_id,unit_of_issue),0)::text AS uncosted_units`)
		// Each product total occurs on several positions: deduplicate by the owning
		// product BEFORE adding values within a currency. Never sum units here.
		products := tx.Table("(?) AS product_values", valued).Select("DISTINCT outlet_id,product_id,unit_of_issue,currency,known_purchase_value_minor,uncosted_units")
		if err := tx.Table("(?) AS unique_products", products).Select("currency,sum(known_purchase_value_minor::numeric)::text AS known_purchase_value_minor,count(*) FILTER (WHERE uncosted_units::numeric>0)::text AS uncosted_products").Group("currency").Order("currency").Limit(21).Scan(&values).Error; err != nil {
			return err
		}
		if len(values) > 20 {
			return fmt.Errorf("stock overview currency bound exceeded")
		}
		// The position hash includes outlet scope: IDs are only unique within an outlet.
		const columns = `md5(outlet_id::text||':'||id::text)::uuid AS id,created_at,location_id,batch_id,product_id,location_code,location_name,zone_code,aisle_code,rack_code,shelf_code,sku,brand_name,unit_of_issue,lot_number,expires_on,stock_status,expiry_status,product_status,regulatory_status,on_hand_units,receipt_units,correction_units,dispensable_units,expiry_time_zone,expiry_local_date,days_to_expiry,expiry_band,outlet_id,outlet_code,outlet_name,currency,product_on_hand_units,known_purchase_value_minor,uncosted_units`
		keyed := tx.Table("(?) AS valued", valued).Select(columns)
		return tenantdb.ApplyKeyset(tx, params).Table("(?) AS overview", keyed).Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	coverage := "all_accessible"
	if excluded {
		coverage = "restricted_outlets_excluded"
	}
	c.Header("X-Pharmacy-Stock-Coverage", coverage)
	if values == nil {
		values = []StockOverviewValuationFields{}
	}
	valueHeader, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	c.Header("X-Pharmacy-Stock-Valuation", string(valueHeader))
	return rows, nil
}

func (h *catalogueHandlers) listStockOverview(c *gin.Context, params pagination.Params) ([]stockOverviewPosition, error) {
	rows, err := h.readStockOverview(c, params, false)
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

func (h *catalogueHandlers) exportStockOverview(c *gin.Context) {
	rows, err := h.readStockOverview(c, pagination.Params{Limit: stockExportRowLimit}, true)
	if err != nil {
		stockError(c, err)
		return
	}
	if c.IsAborted() {
		return
	}
	if len(rows) > stockExportRowLimit {
		respond.Error(c, http.StatusRequestEntityTooLarge, "stock_export_too_large", "stock export exceeds 1000 rows; narrow the filters")
		return
	}
	// Reuse the outlet CSV's exact fields and neutralisation, adding scope and
	// complete product totals. No page is truncated or exported from UI cache.
	positions := make([]stockPosition, len(rows))
	for i := range rows {
		positions[i] = rows[i].Position
	}
	asOf, _ := time.Parse(time.RFC3339Nano, c.Query("as_of"))
	data, err := encodeOverviewCSV(rows, positions, asOf, c.Writer.Header().Get("X-Pharmacy-Stock-Coverage"))
	if errors.Is(err, csvsafe.ErrTooLarge) {
		respond.Error(c, 413, "stock_export_too_large", "stock export exceeds 8 MiB; narrow the filters")
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "could not encode stock CSV")
		return
	}
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelInfo, "pharmacy.stock.overview_exported", "combined stock CSV generated", "pharmacy_id", c.Param("pharmacy"), "tenant_id", p.TenantID, "subject", p.Subject, "as_of", asOf.UTC(), "row_count", len(rows), "sha256", digest)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="pharmacy-stock-overview-%s-%s.csv"`, c.Param("pharmacy"), asOf.UTC().Format("20060102t150405z")))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Export-Row-Count", strconv.Itoa(len(rows)))
	c.Header("X-Export-Sha256", digest)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func encodeOverviewCSV(rows []stockOverviewPosition, positions []stockPosition, asOf time.Time, coverage string) ([]byte, error) {
	data, err := encodeStockCSV(positions, asOf)
	if err != nil {
		return nil, err
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		return nil, err
	}
	w := csvsafe.NewWriter(stockExportByteLimit)
	prefix := []string{"outlet_id", "outlet_code", "outlet_name", "coverage", "currency", "product_on_hand_units", "product_known_purchase_value_minor", "product_uncosted_units"}
	if err := w.Write(append(prefix, records[0]...)); err != nil {
		return nil, err
	}
	for i, row := range rows {
		prefix = []string{row.OutletID.String(), csvsafe.Text(row.OutletCode), csvsafe.Text(row.OutletName), coverage, row.Currency, row.ProductOnHandUnits, row.KnownPurchaseValueMinor, row.UncostedUnits}
		if err := w.Write(append(prefix, records[i+1]...)); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}
