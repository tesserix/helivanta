package pharmacy

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

// The daily sales report (#1386 C-3, #1060): posted sales and credit notes
// summed per outlet day and currency. Both are insert-only, so the sums are
// read afresh and cannot drift; nothing is cached or closed.

type dailyRow struct {
	Day            string `gorm:"column:day" json:"day"`
	Currency       string `gorm:"column:currency" json:"currency"`
	Sales          int64  `gorm:"column:sales" json:"sales"`
	SalesMinor     int64  `gorm:"column:sales_minor" json:"sales_minor"`
	SalesTaxable   int64  `gorm:"column:sales_taxable_minor" json:"sales_taxable_minor"`
	SalesCGST      int64  `gorm:"column:sales_cgst_minor" json:"sales_cgst_minor"`
	SalesSGST      int64  `gorm:"column:sales_sgst_minor" json:"sales_sgst_minor"`
	DiscountMinor  int64  `gorm:"column:discount_minor" json:"discount_minor"`
	UnitsSold      int64  `gorm:"column:units_sold" json:"units_sold"`
	CreditNotes    int64  `gorm:"column:credit_notes" json:"credit_notes"`
	CreditsMinor   int64  `gorm:"column:credits_minor" json:"credits_minor"`
	CreditsTaxable int64  `gorm:"column:credits_taxable_minor" json:"credits_taxable_minor"`
	CreditsCGST    int64  `gorm:"column:credits_cgst_minor" json:"credits_cgst_minor"`
	CreditsSGST    int64  `gorm:"column:credits_sgst_minor" json:"credits_sgst_minor"`
	UnitsReturned  int64  `gorm:"column:units_returned" json:"units_returned"`
	NetMinor       int64  `gorm:"column:net_minor" json:"net_minor"`
	CashMinor      int64  `gorm:"column:cash_minor" json:"cash_minor"`
	UPIMinor       int64  `gorm:"column:upi_minor" json:"upi_minor"`
	// UPI the system saw settle, apart from typed-in references (#1386 C-4b).
	UPIVerifiedMinor int64 `gorm:"column:upi_verified_minor" json:"upi_verified_minor"`
	CardMinor        int64 `gorm:"column:card_minor" json:"card_minor"`
	CashRefundMinor  int64 `gorm:"column:cash_refund_minor" json:"cash_refund_minor"`
	UPIRefundMinor   int64 `gorm:"column:upi_refund_minor" json:"upi_refund_minor"`
	CardRefundMinor  int64 `gorm:"column:card_refund_minor" json:"card_refund_minor"`
}

type dailyReport struct {
	From string     `json:"from"`
	To   string     `json:"to"`
	Days []dailyRow `json:"days"`
}

// dailyTotals groups each document by its own outlet date (sale_date,
// credit_note_date), so a return counts on the day it was taken. Cash is
// amount_minor, what was applied to the sale; change is not income.
const dailyTotals = `WITH
 s AS (SELECT sale_date AS day, currency, count(*) AS sales, sum(total_minor) AS sales_minor, sum(taxable_minor) AS sales_taxable_minor,
   sum(cgst_minor) AS sales_cgst_minor, sum(sgst_minor) AS sales_sgst_minor
  FROM pharmacy_sales WHERE tenant_id=@tenant AND outlet_id=@outlet AND sale_date BETWEEN CAST(@since AS date) AND CAST(@until AS date) GROUP BY 1,2),
 sl AS (SELECT s.sale_date AS day, s.currency, sum(l.discount_minor) AS discount_minor, sum(l.quantity_units) AS units_sold
  FROM pharmacy_sale_lines l JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id
  WHERE s.tenant_id=@tenant AND s.outlet_id=@outlet AND s.sale_date BETWEEN CAST(@since AS date) AND CAST(@until AS date) GROUP BY 1,2),
 st AS (SELECT s.sale_date AS day, s.currency, sum(t.amount_minor) FILTER (WHERE t.kind='cash') AS cash_minor,
   sum(t.amount_minor) FILTER (WHERE t.kind='upi') AS upi_minor, sum(t.amount_minor) FILTER (WHERE t.kind='upi_verified') AS upi_verified_minor, sum(t.amount_minor) FILTER (WHERE t.kind='card') AS card_minor
  FROM pharmacy_sale_tenders t JOIN pharmacy_sales s ON s.tenant_id=t.tenant_id AND s.outlet_id=t.outlet_id AND s.id=t.sale_id
  WHERE s.tenant_id=@tenant AND s.outlet_id=@outlet AND s.sale_date BETWEEN CAST(@since AS date) AND CAST(@until AS date) GROUP BY 1,2),
 c AS (SELECT credit_note_date AS day, currency, count(*) AS credit_notes, sum(total_minor) AS credits_minor, sum(taxable_minor) AS credits_taxable_minor,
   sum(cgst_minor) AS credits_cgst_minor, sum(sgst_minor) AS credits_sgst_minor
  FROM pharmacy_credit_notes WHERE tenant_id=@tenant AND outlet_id=@outlet AND credit_note_date BETWEEN CAST(@since AS date) AND CAST(@until AS date) GROUP BY 1,2),
 cl AS (SELECT n.credit_note_date AS day, n.currency, sum(l.quantity_units) AS units_returned
  FROM pharmacy_credit_note_lines l JOIN pharmacy_credit_notes n ON n.tenant_id=l.tenant_id AND n.outlet_id=l.outlet_id AND n.id=l.credit_note_id
  WHERE n.tenant_id=@tenant AND n.outlet_id=@outlet AND n.credit_note_date BETWEEN CAST(@since AS date) AND CAST(@until AS date) GROUP BY 1,2),
 cr AS (SELECT n.credit_note_date AS day, n.currency, sum(r.amount_minor) FILTER (WHERE r.kind='cash') AS cash_refund_minor,
   sum(r.amount_minor) FILTER (WHERE r.kind='upi') AS upi_refund_minor, sum(r.amount_minor) FILTER (WHERE r.kind='card') AS card_refund_minor
  FROM pharmacy_credit_note_refunds r JOIN pharmacy_credit_notes n ON n.tenant_id=r.tenant_id AND n.outlet_id=r.outlet_id AND n.id=r.credit_note_id
  WHERE n.tenant_id=@tenant AND n.outlet_id=@outlet AND n.credit_note_date BETWEEN CAST(@since AS date) AND CAST(@until AS date) GROUP BY 1,2),
 k AS (SELECT day, currency FROM s UNION SELECT day, currency FROM c)
SELECT to_char(k.day,'YYYY-MM-DD') AS day, k.currency,
 COALESCE(s.sales,0) AS sales, COALESCE(s.sales_minor,0)::bigint AS sales_minor, COALESCE(s.sales_taxable_minor,0)::bigint AS sales_taxable_minor,
 COALESCE(s.sales_cgst_minor,0)::bigint AS sales_cgst_minor, COALESCE(s.sales_sgst_minor,0)::bigint AS sales_sgst_minor,
 COALESCE(sl.discount_minor,0)::bigint AS discount_minor, COALESCE(sl.units_sold,0)::bigint AS units_sold,
 COALESCE(c.credit_notes,0) AS credit_notes, COALESCE(c.credits_minor,0)::bigint AS credits_minor, COALESCE(c.credits_taxable_minor,0)::bigint AS credits_taxable_minor,
 COALESCE(c.credits_cgst_minor,0)::bigint AS credits_cgst_minor, COALESCE(c.credits_sgst_minor,0)::bigint AS credits_sgst_minor,
 COALESCE(cl.units_returned,0)::bigint AS units_returned,
 (COALESCE(s.sales_minor,0)-COALESCE(c.credits_minor,0))::bigint AS net_minor,
 COALESCE(st.cash_minor,0)::bigint AS cash_minor, COALESCE(st.upi_minor,0)::bigint AS upi_minor, COALESCE(st.upi_verified_minor,0)::bigint AS upi_verified_minor, COALESCE(st.card_minor,0)::bigint AS card_minor,
 COALESCE(cr.cash_refund_minor,0)::bigint AS cash_refund_minor, COALESCE(cr.upi_refund_minor,0)::bigint AS upi_refund_minor,
 COALESCE(cr.card_refund_minor,0)::bigint AS card_refund_minor
FROM k
 LEFT JOIN s USING (day, currency) LEFT JOIN sl USING (day, currency) LEFT JOIN st USING (day, currency)
 LEFT JOIN c USING (day, currency) LEFT JOIN cl USING (day, currency) LEFT JOIN cr USING (day, currency)
ORDER BY k.day, k.currency`

func dailyReportRoutes(s *platform.Router, h *catalogueHandlers) {
	s.GET("/reports/daily", PermSaleRead, h.dailyReport)
}

// dailyReport holds totals only, no patient, prescriber or buyer detail, so
// its reads are not audited (spec C-3 decision 5).
func (h *catalogueHandlers) dailyReport(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	from, to, ok := outletPeriod(c, "the daily report")
	if !ok {
		return
	}
	report := dailyReport{From: from, To: to, Days: []dailyRow{}}
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tx.Raw(dailyTotals, map[string]any{"tenant": p.TenantID, "outlet": outlet, "since": from, "until": to}).Scan(&report.Days).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, report)
}
