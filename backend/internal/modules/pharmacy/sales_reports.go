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
// Service charges are not products: the daily report shows them.
const itemSalesRows = `(SELECT c.id, c.created_at, c.sku, c.brand_name, c.generic_name, c.manufacturer, c.pack_size, c.unit_of_issue, c.currency,
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
  WHERE s.tenant_id=? AND s.outlet_id=? AND s.sale_date BETWEEN ?::date AND ?::date GROUP BY l.product_id) s ON s.product_id=c.id
 LEFT JOIN (SELECT sl.product_id, sum(cl.quantity_units) AS units_returned, sum(cl.amount_minor) AS credits_minor, sum(cl.taxable_minor) AS credits_taxable_minor,
   sum(cl.cgst_minor) AS credits_cgst_minor, sum(cl.sgst_minor) AS credits_sgst_minor
  FROM pharmacy_credit_note_lines cl
  JOIN pharmacy_credit_notes n ON n.tenant_id=cl.tenant_id AND n.outlet_id=cl.outlet_id AND n.id=cl.credit_note_id
  JOIN pharmacy_sale_lines sl ON sl.tenant_id=cl.tenant_id AND sl.outlet_id=cl.outlet_id AND sl.sale_id=cl.sale_id AND sl.line_number=cl.sale_line
  WHERE n.tenant_id=? AND n.outlet_id=? AND n.credit_note_date BETWEEN ?::date AND ?::date GROUP BY sl.product_id) r ON r.product_id=c.id
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

var receiptKinds = map[string]bool{"cash": true, "upi": true, "upi_verified": true, "card": true, "admission_account": true}

func (h *catalogueHandlers) listItemSales(c *gin.Context, params pagination.Params) ([]itemSalesRow, error) {
	return salesReportPage[itemSalesRow](c, h, params, "sales by item", "product_id", itemSalesRows, func(tenant string, outlet uuid.UUID, f salesReportFilter) []any {
		period := []any{tenant, outlet, f.since, f.until}
		return append(append(period, period...), tenant, outlet, f.product, f.product)
	})
}

func (h *catalogueHandlers) listReceipts(c *gin.Context, params pagination.Params) ([]receiptEntry, error) {
	return salesReportPage[receiptEntry](c, h, params, "the receipts register", "kind", receiptRows, func(tenant string, outlet uuid.UUID, f salesReportFilter) []any {
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
}

func salesReportArgs(c *gin.Context, what, filter string) (salesReportFilter, bool) {
	from, to, ok := outletPeriod(c, what)
	if !ok {
		return salesReportFilter{}, false
	}
	args := salesReportFilter{since: from, until: to}
	switch v := c.Query(filter); {
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

// salesReportPage checks the outlet scope before the query string, so a
// wrong tenant or outlet is refused before anything else is read.
func salesReportPage[T any](c *gin.Context, h *catalogueHandlers, params pagination.Params, what, filter, query string,
	args func(string, uuid.UUID, salesReportFilter) []any) ([]T, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	f, ok := salesReportArgs(c, what, filter)
	if !ok {
		return nil, nil
	}
	var rows []T
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyOrderedKeyset(tx.Table(query, args(p.TenantID, outlet, f)...), params,
			tenantdb.Keyset{Column: "created_at", Direction: pagination.OldestFirst}).Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}
