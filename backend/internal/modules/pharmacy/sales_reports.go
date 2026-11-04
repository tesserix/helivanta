package pharmacy

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Sales by item and the receipts and refunds register (#1060). Like the
// daily report they read posted, insert-only records afresh and hold no
// patient, prescriber or buyer, so they are not audited; each document
// counts on its own outlet date.

type itemSalesRow struct {
	ProductID      uuid.UUID `gorm:"column:id" json:"product_id"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"-"`
	SKU            string    `gorm:"column:sku" json:"sku"`
	BrandName      string    `gorm:"column:brand_name" json:"brand_name"`
	GenericName    string    `gorm:"column:generic_name" json:"generic_name"`
	Manufacturer   string    `gorm:"column:manufacturer" json:"manufacturer"`
	PackSize       int       `gorm:"column:pack_size" json:"pack_size"`
	UnitOfIssue    string    `gorm:"column:unit_of_issue" json:"unit_of_issue"`
	Currency       string    `gorm:"column:currency" json:"currency"`
	UnitsSold      int64     `gorm:"column:units_sold" json:"units_sold"`
	SalesMinor     int64     `gorm:"column:sales_minor" json:"sales_minor"`
	DiscountMinor  int64     `gorm:"column:discount_minor" json:"discount_minor"`
	SalesTaxable   int64     `gorm:"column:sales_taxable_minor" json:"sales_taxable_minor"`
	SalesCGST      int64     `gorm:"column:sales_cgst_minor" json:"sales_cgst_minor"`
	SalesSGST      int64     `gorm:"column:sales_sgst_minor" json:"sales_sgst_minor"`
	MRPLow         *int64    `gorm:"column:mrp_low_minor" json:"mrp_low_minor"`
	MRPHigh        *int64    `gorm:"column:mrp_high_minor" json:"mrp_high_minor"`
	UnitsReturned  int64     `gorm:"column:units_returned" json:"units_returned"`
	CreditsMinor   int64     `gorm:"column:credits_minor" json:"credits_minor"`
	CreditsTaxable int64     `gorm:"column:credits_taxable_minor" json:"credits_taxable_minor"`
	CreditsCGST    int64     `gorm:"column:credits_cgst_minor" json:"credits_cgst_minor"`
	CreditsSGST    int64     `gorm:"column:credits_sgst_minor" json:"credits_sgst_minor"`
	NetUnits       int64     `gorm:"column:net_units" json:"net_units"`
	NetMinor       int64     `gorm:"column:net_minor" json:"net_minor"`
}

func (r itemSalesRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ProductID }

// itemSalesRows is one row per product with a sale line or a credit-note line
// in [since, until]. A stocked product's currency and pack size cannot change
// (pharmacy_stock_product_identity_guard), so a row never mixes either.
// Service charges are not products: the daily report shows them. A category
// filter counts a sale line under the category in force on its sale date and
// a credit-note line under its sale line's, so a return always cancels the
// sale it returns (#988).
var itemSalesRows = `(SELECT c.id, c.created_at, c.sku, c.brand_name, c.generic_name, c.manufacturer, c.pack_size, c.unit_of_issue, c.currency,
 COALESCE(s.units_sold,0)::bigint AS units_sold, COALESCE(s.sales_minor,0)::bigint AS sales_minor, COALESCE(s.discount_minor,0)::bigint AS discount_minor,
 COALESCE(s.sales_taxable_minor,0)::bigint AS sales_taxable_minor, COALESCE(s.sales_cgst_minor,0)::bigint AS sales_cgst_minor,
 COALESCE(s.sales_sgst_minor,0)::bigint AS sales_sgst_minor, s.mrp_low_minor, s.mrp_high_minor,
 COALESCE(r.units_returned,0)::bigint AS units_returned, COALESCE(r.credits_minor,0)::bigint AS credits_minor,
 COALESCE(r.credits_taxable_minor,0)::bigint AS credits_taxable_minor, COALESCE(r.credits_cgst_minor,0)::bigint AS credits_cgst_minor,
 COALESCE(r.credits_sgst_minor,0)::bigint AS credits_sgst_minor,
 (COALESCE(s.units_sold,0)-COALESCE(r.units_returned,0))::bigint AS net_units, (COALESCE(s.sales_minor,0)-COALESCE(r.credits_minor,0))::bigint AS net_minor
 FROM pharmacy_catalogue_products c
 LEFT JOIN (SELECT l.product_id, sum(l.quantity_units) AS units_sold, sum(l.amount_minor) AS sales_minor, sum(l.discount_minor) AS discount_minor,
   sum(l.taxable_minor) AS sales_taxable_minor, sum(l.cgst_minor) AS sales_cgst_minor, sum(l.sgst_minor) AS sales_sgst_minor,
   min(l.mrp_minor) AS mrp_low_minor, max(l.mrp_minor) AS mrp_high_minor
  FROM pharmacy_sale_lines l JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id
  WHERE s.tenant_id=? AND s.outlet_id=? AND s.sale_date BETWEEN ?::date AND ?::date AND ` + categoryOn("s", "l.product_id", "s.sale_date") + ` GROUP BY l.product_id) s ON s.product_id=c.id
 LEFT JOIN (SELECT sl.product_id, sum(cl.quantity_units) AS units_returned, sum(cl.amount_minor) AS credits_minor, sum(cl.taxable_minor) AS credits_taxable_minor,
   sum(cl.cgst_minor) AS credits_cgst_minor, sum(cl.sgst_minor) AS credits_sgst_minor
  FROM pharmacy_credit_note_lines cl
  JOIN pharmacy_credit_notes n ON n.tenant_id=cl.tenant_id AND n.outlet_id=cl.outlet_id AND n.id=cl.credit_note_id
  JOIN pharmacy_sale_lines sl ON sl.tenant_id=cl.tenant_id AND sl.outlet_id=cl.outlet_id AND sl.sale_id=cl.sale_id AND sl.line_number=cl.sale_line
  JOIN pharmacy_sales os ON os.tenant_id=sl.tenant_id AND os.outlet_id=sl.outlet_id AND os.id=sl.sale_id
  WHERE n.tenant_id=? AND n.outlet_id=? AND n.credit_note_date BETWEEN ?::date AND ?::date AND ` + categoryOn("os", "sl.product_id", "os.sale_date") + ` GROUP BY sl.product_id) r ON r.product_id=c.id
 WHERE c.tenant_id=? AND c.outlet_id=? AND (s.product_id IS NOT NULL OR r.product_id IS NOT NULL)
  AND (?::uuid IS NULL OR c.id=?::uuid)) e`

type receiptEntry struct {
	ID             uuid.UUID `gorm:"column:id" json:"id"`
	PostedAt       time.Time `gorm:"column:created_at" json:"posted_at"`
	Day            string    `gorm:"column:day" json:"day"`
	Direction      string    `gorm:"column:direction" json:"direction"`
	DocumentID     uuid.UUID `gorm:"column:document_id" json:"document_id"`
	DocumentNumber string    `gorm:"column:document_number" json:"document_number"`
	Kind           string    `gorm:"column:kind" json:"kind"`
	Reference      *string   `gorm:"column:reference" json:"reference"`
	AmountMinor    int64     `gorm:"column:amount_minor" json:"amount_minor"`
	Currency       string    `gorm:"column:currency" json:"currency"`
}

func (e receiptEntry) PageKey() (time.Time, uuid.UUID) { return e.PostedAt, e.ID }

// receiptRows is every tender on a sale and every refund on a credit note
// in [since, until], at its document's time. Cash is the amount applied to
// the sale; change handed back is not income.
const receiptRows = `(SELECT t.id, s.created_at, to_char(s.sale_date,'YYYY-MM-DD') AS day, 'receipt' AS direction, s.id AS document_id,
 s.invoice_number AS document_number, t.kind, t.reference, t.amount_minor, s.currency
 FROM pharmacy_sale_tenders t JOIN pharmacy_sales s ON s.tenant_id=t.tenant_id AND s.outlet_id=t.outlet_id AND s.id=t.sale_id
 WHERE s.tenant_id=? AND s.outlet_id=? AND s.sale_date BETWEEN ?::date AND ?::date
  AND (?::text IS NULL OR t.kind=?::text)
 UNION ALL
 SELECT r.id, n.created_at, to_char(n.credit_note_date,'YYYY-MM-DD'), 'refund', n.id, n.credit_note_number, r.kind, r.reference, r.amount_minor, n.currency
 FROM pharmacy_credit_note_refunds r JOIN pharmacy_credit_notes n ON n.tenant_id=r.tenant_id AND n.outlet_id=r.outlet_id AND n.id=r.credit_note_id
 WHERE n.tenant_id=? AND n.outlet_id=? AND n.credit_note_date BETWEEN ?::date AND ?::date
  AND (?::text IS NULL OR r.kind=?::text)) e`

// patientSaleLine is one line of a sale to a registered patient (#1060,
// legacy 442). It carries the patient's purchases, so every read is audited.
type patientSaleLine struct {
	ID            uuid.UUID `gorm:"column:id" json:"id"`
	CreatedAt     time.Time `gorm:"column:created_at" json:"-"`
	SaleDate      string    `gorm:"column:sale_date" json:"sale_date"`
	SaleID        uuid.UUID `gorm:"column:sale_id" json:"sale_id"`
	InvoiceNumber string    `gorm:"column:invoice_number" json:"invoice_number"`
	LineNumber    int       `gorm:"column:line_number" json:"line_number"`
	BedCode       *string   `gorm:"column:admission_bed_code" json:"admission_bed_code"`
	ProductID     uuid.UUID `gorm:"column:product_id" json:"product_id"`
	SKU           string    `gorm:"column:sku" json:"sku"`
	BrandName     string    `gorm:"column:brand_name" json:"brand_name"`
	GenericName   string    `gorm:"column:generic_name" json:"generic_name"`
	PackSize      int       `gorm:"column:pack_size" json:"pack_size"`
	UnitOfIssue   string    `gorm:"column:unit_of_issue" json:"unit_of_issue"`
	BatchID       uuid.UUID `gorm:"column:batch_id" json:"batch_id"`
	LotNumber     string    `gorm:"column:lot_number" json:"lot_number"`
	ExpiresOn     string    `gorm:"column:expires_on" json:"expires_on"`
	Units         int64     `gorm:"column:quantity_units" json:"quantity_units"`
	MRPMinor      int64     `gorm:"column:mrp_minor" json:"mrp_minor"`
	DiscountMinor int64     `gorm:"column:discount_minor" json:"discount_minor"`
	AmountMinor   int64     `gorm:"column:amount_minor" json:"amount_minor"`
	Currency      string    `gorm:"column:currency" json:"currency"`
	ReturnedUnits int64     `gorm:"column:returned_units" json:"returned_units"`
}

func (l patientSaleLine) PageKey() (time.Time, uuid.UUID) { return l.CreatedAt, l.ID }

// patientSaleLines is every line of the patient's sales dated in [since,
// until], with the units credited back on notes dated up to until. Walk-in
// buyers are not patients and never match.
const patientSaleLines = `(SELECT l.id, s.created_at, to_char(s.sale_date,'YYYY-MM-DD') AS sale_date, s.id AS sale_id, s.invoice_number, l.line_number,
 s.admission_bed_code, l.product_id, c.sku, c.brand_name, c.generic_name, c.pack_size, c.unit_of_issue, l.batch_id, l.lot_number,
 to_char(l.expires_on,'YYYY-MM-DD') AS expires_on, l.quantity_units, l.mrp_minor, l.discount_minor, l.amount_minor, s.currency,
 (SELECT COALESCE(sum(r.quantity_units),0) FROM pharmacy_credit_note_lines r
   JOIN pharmacy_credit_notes n ON n.tenant_id=r.tenant_id AND n.outlet_id=r.outlet_id AND n.id=r.credit_note_id
  WHERE r.tenant_id=l.tenant_id AND r.outlet_id=l.outlet_id AND r.sale_id=l.sale_id AND r.sale_line=l.line_number AND n.credit_note_date <= ?::date)::bigint AS returned_units
 FROM pharmacy_sale_lines l
 JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id
 JOIN pharmacy_catalogue_products c ON c.tenant_id=l.tenant_id AND c.outlet_id=l.outlet_id AND c.id=l.product_id
 WHERE s.tenant_id=? AND s.outlet_id=? AND s.patient_id=?::uuid AND s.sale_date BETWEEN ?::date AND ?::date) e`

// stockSalesRow is a product's sales in the period beside its stock now
// (#1060, legacy 755). Units only: no money, patient or buyer.
type stockSalesRow struct {
	ProductID     uuid.UUID `gorm:"column:id" json:"product_id"`
	CreatedAt     time.Time `gorm:"column:created_at" json:"-"`
	SKU           string    `gorm:"column:sku" json:"sku"`
	BrandName     string    `gorm:"column:brand_name" json:"brand_name"`
	GenericName   string    `gorm:"column:generic_name" json:"generic_name"`
	PackSize      int       `gorm:"column:pack_size" json:"pack_size"`
	UnitOfIssue   string    `gorm:"column:unit_of_issue" json:"unit_of_issue"`
	UnitsSold     int64     `gorm:"column:units_sold" json:"units_sold"`
	UnitsReturned int64     `gorm:"column:units_returned" json:"units_returned"`
	OnHand        int64     `gorm:"column:on_hand_units" json:"on_hand_units"`
	Sellable      int64     `gorm:"column:sellable_units" json:"sellable_units"`
}

func (r stockSalesRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ProductID }

// stockSalesRows is every product that has ever held stock (a sale takes
// stock, so every product sold), with units sold and returned by document
// date, as sales by item counts them, units on hand from all its movements,
// and units a sale may take today through the counter's own predicate
// (pharmacy_sellable_buckets), so flagged, expired and unpriced batches on a
// sellable shelf are never shown as sellable. A category filter lists a
// product by its category today, as its stock is now, and counts its sales
// line by line as sales by item does (#988).
var stockSalesRows = `(SELECT c.id, c.created_at, c.sku, c.brand_name, c.generic_name, c.pack_size, c.unit_of_issue,
 COALESCE(s.units,0)::bigint AS units_sold, COALESCE(r.units,0)::bigint AS units_returned, k.on_hand::bigint AS on_hand_units, v.sellable::bigint AS sellable_units
 FROM pharmacy_catalogue_products c
 JOIN (SELECT b.product_id, sum(m.quantity_units) AS on_hand
   FROM pharmacy_stock_movements m
   JOIN pharmacy_stock_batches b ON b.tenant_id=m.tenant_id AND b.outlet_id=m.outlet_id AND b.id=m.batch_id
  WHERE m.tenant_id=? AND m.outlet_id=? GROUP BY b.product_id) k ON k.product_id=c.id
 CROSS JOIN LATERAL (SELECT COALESCE(sum(on_hand),0) AS sellable FROM pharmacy_sellable_buckets(c.tenant_id, c.outlet_id, c.id, ?::date)) v
 LEFT JOIN (SELECT l.product_id, sum(l.quantity_units) AS units
  FROM pharmacy_sale_lines l JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id
  WHERE s.tenant_id=? AND s.outlet_id=? AND s.sale_date BETWEEN ?::date AND ?::date AND ` + categoryOn("s", "l.product_id", "s.sale_date") + ` GROUP BY l.product_id) s ON s.product_id=c.id
 LEFT JOIN (SELECT sl.product_id, sum(cl.quantity_units) AS units
  FROM pharmacy_credit_note_lines cl
  JOIN pharmacy_credit_notes n ON n.tenant_id=cl.tenant_id AND n.outlet_id=cl.outlet_id AND n.id=cl.credit_note_id
  JOIN pharmacy_sale_lines sl ON sl.tenant_id=cl.tenant_id AND sl.outlet_id=cl.outlet_id AND sl.sale_id=cl.sale_id AND sl.line_number=cl.sale_line
  JOIN pharmacy_sales os ON os.tenant_id=sl.tenant_id AND os.outlet_id=sl.outlet_id AND os.id=sl.sale_id
  WHERE n.tenant_id=? AND n.outlet_id=? AND n.credit_note_date BETWEEN ?::date AND ?::date AND ` + categoryOn("os", "sl.product_id", "os.sale_date") + ` GROUP BY sl.product_id) r ON r.product_id=c.id
 WHERE c.tenant_id=? AND c.outlet_id=? AND ` + categoryOn("c", "c.id", "?::date") + `) e`

// categoryOn is true when no category filter is set, or when the category in
// force for product on day matches it; rel holds the tenant and outlet. Its
// arguments are salesReportFilter.categoryArgs.
func categoryOn(rel, product, day string) string {
	return `(?::uuid IS NULL OR EXISTS (SELECT 1 FROM pharmacy_product_category_on(` + rel + `.tenant_id, ` + rel + `.outlet_id, ` + product + `, ` + day + `) pc
  WHERE pc.category_id=?::uuid AND (?::uuid IS NULL OR pc.subcategory_id=?::uuid)))`
}

var receiptKinds = map[string]bool{"cash": true, "upi": true, "upi_verified": true, "card": true, "admission_account": true}

func (h *catalogueHandlers) listItemSales(c *gin.Context, params pagination.Params) ([]itemSalesRow, error) {
	return salesReportPage[itemSalesRow](c, h, params, "sales by item", "product_id", true, itemSalesRows, func(tenant string, outlet uuid.UUID, f salesReportFilter) []any {
		period := append([]any{tenant, outlet, f.since, f.until}, f.categoryArgs()...)
		return append(append(period, period...), tenant, outlet, f.product, f.product)
	})
}

func (h *catalogueHandlers) listPatientSales(c *gin.Context, params pagination.Params) ([]patientSaleLine, error) {
	return salesReportPage[patientSaleLine](c, h, params, "patient-wise sales", "patient_id", false, patientSaleLines, func(tenant string, outlet uuid.UUID, f salesReportFilter) []any {
		return []any{f.until, tenant, outlet, f.patient, f.since, f.until}
	})
}

func (h *catalogueHandlers) listStockSales(c *gin.Context, params pagination.Params) ([]stockSalesRow, error) {
	return salesReportPage[stockSalesRow](c, h, params, "the sales stock report", "", true, stockSalesRows, func(tenant string, outlet uuid.UUID, f salesReportFilter) []any {
		period := append([]any{tenant, outlet, f.since, f.until}, f.categoryArgs()...)
		return append(append(append(append([]any{tenant, outlet, f.today}, period...), period...), tenant, outlet), f.categoryArgs(f.today)...)
	})
}

func (h *catalogueHandlers) listReceipts(c *gin.Context, params pagination.Params) ([]receiptEntry, error) {
	return salesReportPage[receiptEntry](c, h, params, "the receipts register", "kind", false, receiptRows, func(tenant string, outlet uuid.UUID, f salesReportFilter) []any {
		half := []any{tenant, outlet, f.since, f.until, f.kind, f.kind}
		return append(half, half...)
	})
}

// salesReportFilter is the period and the one optional filter; an absent
// filter is NULL, which the query reads as all.
type salesReportFilter struct {
	since, until string
	product      *uuid.UUID
	kind         *string
	// patient is required, never "all": patient-wise sales are one patient's.
	patient *uuid.UUID
	// today is the outlet's date, read in the transaction.
	today string
	// category, and optionally its subcategory, of sales by item and sales
	// and stock (#988).
	category, subcategory *uuid.UUID
}

// categoryArgs are categoryOn's arguments; day is given when categoryOn's
// day is a placeholder.
func (f salesReportFilter) categoryArgs(day ...any) []any {
	return append(append([]any{f.category}, day...), f.category, f.subcategory, f.subcategory)
}

func salesReportArgs(c *gin.Context, what, filter string, categories bool) (salesReportFilter, bool) {
	from, to, ok := outletPeriod(c, what)
	if !ok {
		return salesReportFilter{}, false
	}
	args := salesReportFilter{since: from, until: to}
	if categories && !reportCategory(c, &args) {
		return args, false
	}
	switch v := c.Query(filter); {
	case filter == "patient_id":
		// Fail closed: without a patient this would be every patient's purchases.
		id, err := uuid.Parse(v)
		if err != nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "patient_id is required and is an id")
			return args, false
		}
		args.patient = &id
	case v == "":
	case filter == "product_id":
		id, err := uuid.Parse(v)
		if err != nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "product_id is an id")
			return args, false
		}
		args.product = &id
	case !receiptKinds[v]:
		respond.Error(c, http.StatusBadRequest, "invalid_request", "kind is cash, upi, upi_verified, card or admission_account")
		return args, false
	default:
		args.kind = &v
	}
	return args, true
}

// reportCategory reads category_id and subcategory_id. A subcategory alone
// is refused, not resolved to its parent: fail closed rather than guess. An
// id that is no category of this outlet simply matches nothing.
func reportCategory(c *gin.Context, args *salesReportFilter) bool {
	for _, q := range []struct {
		name string
		into **uuid.UUID
	}{{"category_id", &args.category}, {"subcategory_id", &args.subcategory}} {
		v := c.Query(q.name)
		if v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", q.name+" is an id")
			return false
		}
		*q.into = &id
	}
	if args.subcategory != nil && args.category == nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "subcategory_id needs its category_id")
		return false
	}
	return true
}

// salesReportPage refuses malformed scope ids before reading the query
// string; the outlet grant is checked in the transaction, as for the daily
// report.
func salesReportPage[T any](c *gin.Context, h *catalogueHandlers, params pagination.Params, what, filter string, categories bool, query string,
	args func(string, uuid.UUID, salesReportFilter) []any) ([]T, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	f, ok := salesReportArgs(c, what, filter, categories)
	if !ok {
		return nil, nil
	}
	var rows []T
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		// Each page sums whole tables before its keyset; a large outlet gets
		// a clean failure, as the stock overview does, not a held connection.
		if err := tx.Exec(`SELECT set_config('statement_timeout','8s',true)`).Error; err != nil {
			return err
		}
		var err error
		if f.today, err = outletLocalDate(tx, p.TenantID, outlet); err != nil {
			return err
		}
		if err := tenantdb.ApplyOrderedKeyset(tx.Table(query, args(p.TenantID, outlet, f)...), params,
			tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}).Find(&rows).Error; err != nil {
			return err
		}
		// A patient's purchases are disclosed only with their audit row, in
		// the same transaction: no row, no data.
		if f.patient == nil {
			return nil
		}
		return auditRegisterRead(c, tx, "patient_sales", p.TenantID, p.Subject, outlet, "report_read", "from", f.since, "to", f.until, "patient_id", f.patient.String())
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}
