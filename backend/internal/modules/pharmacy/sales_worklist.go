package pharmacy

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// The sales worklist and each sale's summary (#987 slice 1). Both read
// posted, insert-only records afresh. Neither carries the buyer's name or
// phone or the patient's id, only who the sale was for as a kind; an
// admission sale's bed code points to its occupant, so a read showing one
// is audited (#1555).

func salesWorklistRoutes(s *platform.Router, h *catalogueHandlers) {
	platform.ListRoute(s, "/sales/worklist", PermSaleRead, h.listSalesWorklist)
	s.GET("/sales/:id/summary", PermSaleRead, h.getSaleSummary)
}

type saleWorkRow struct {
	ID                   uuid.UUID `gorm:"column:id" json:"id"`
	CreatedAt            time.Time `gorm:"column:created_at" json:"posted_at"`
	InvoiceNumber        string    `gorm:"column:invoice_number" json:"invoice_number"`
	SaleDate             string    `gorm:"column:sale_date" json:"sale_date"`
	Customer             string    `gorm:"column:customer" json:"customer"`
	BedCode              *string   `gorm:"column:admission_bed_code" json:"admission_bed_code"`
	Currency             string    `gorm:"column:currency" json:"currency"`
	TotalMinor           int64     `gorm:"column:total_minor" json:"total_minor"`
	DiscountMinor        int64     `gorm:"column:discount_minor" json:"discount_minor"`
	Units                int64     `gorm:"column:units" json:"units"`
	CreditedMinor        int64     `gorm:"column:credited_minor" json:"credited_minor"`
	NetMinor             int64     `gorm:"column:net_minor" json:"net_minor"`
	AdmissionOutstanding int64     `gorm:"column:admission_outstanding_minor" json:"admission_outstanding_minor"`
}

func (r saleWorkRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// saleCustomer is who a sale was for, as a kind: an admission sale also names
// its registered patient, so admission is decided first.
const saleCustomer = `CASE WHEN s.admission_id IS NOT NULL THEN 'admission' WHEN s.patient_id IS NOT NULL THEN 'patient' ELSE 'walk_in' END`

// saleWorkRows is one row per sale of the outlet dated in [since, until], or
// the one invoice asked for, with every credit note posted so far. What is
// still on the admission account is what was billed to it less what its
// credit notes reversed (#1387).
const saleWorkRows = `(SELECT s.id, s.created_at, s.invoice_number, to_char(s.sale_date,'YYYY-MM-DD') AS sale_date, ` + saleCustomer + ` AS customer,
 s.admission_bed_code, s.currency, s.total_minor,
 (SELECT COALESCE(sum(l.discount_minor),0) FROM pharmacy_sale_lines l WHERE l.tenant_id=s.tenant_id AND l.outlet_id=s.outlet_id AND l.sale_id=s.id)::bigint AS discount_minor,
 (SELECT COALESCE(sum(l.quantity_units),0) FROM pharmacy_sale_lines l WHERE l.tenant_id=s.tenant_id AND l.outlet_id=s.outlet_id AND l.sale_id=s.id)::bigint AS units,
 c.credited::bigint AS credited_minor, (s.total_minor - c.credited)::bigint AS net_minor,
 ((SELECT COALESCE(sum(t.amount_minor),0) FROM pharmacy_sale_tenders t WHERE t.tenant_id=s.tenant_id AND t.outlet_id=s.outlet_id AND t.sale_id=s.id AND t.kind='admission_account')
  - (SELECT COALESCE(sum(r.amount_minor),0) FROM pharmacy_credit_note_refunds r JOIN pharmacy_credit_notes n ON n.tenant_id=r.tenant_id AND n.outlet_id=r.outlet_id AND n.id=r.credit_note_id
     WHERE n.tenant_id=s.tenant_id AND n.outlet_id=s.outlet_id AND n.sale_id=s.id AND r.kind='admission_account'))::bigint AS admission_outstanding_minor
 FROM pharmacy_sales s
 CROSS JOIN LATERAL (SELECT COALESCE(sum(n.total_minor),0) AS credited FROM pharmacy_credit_notes n WHERE n.tenant_id=s.tenant_id AND n.outlet_id=s.outlet_id AND n.sale_id=s.id) c
 WHERE s.tenant_id=? AND s.outlet_id=?
  AND (?::text IS NOT NULL OR s.sale_date BETWEEN ?::date AND ?::date) AND (?::text IS NULL OR s.invoice_number=?::text)
  AND (?::text IS NULL OR ` + saleCustomer + `=?::text)) e`

var saleCustomers = map[string]bool{"walk_in": true, "patient": true, "admission": true}

func (h *catalogueHandlers) listSalesWorklist(c *gin.Context, params pagination.Params) ([]saleWorkRow, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var invoice, customer *string
	// An invoice is found exactly, without a period; otherwise the period
	// bounds the read, as every pharmacy report's does.
	var since, until *string
	if v := c.Query("invoice"); v != "" {
		if len(v) > 16 {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "invoice is an invoice number")
			return nil, nil
		}
		invoice = &v
	} else {
		from, to, ok := outletPeriod(c, "the sales worklist")
		if !ok {
			return nil, nil
		}
		since, until = &from, &to
	}
	if v := c.Query("customer"); v != "" {
		if !saleCustomers[v] {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "customer is walk_in, patient or admission")
			return nil, nil
		}
		customer = &v
	}
	var rows []saleWorkRow
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		// Correlated sums per row; a large outlet gets a clean failure, as
		// the stock overview does, not a held connection.
		if err := tx.Exec(`SELECT set_config('statement_timeout','8s',true)`).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyOrderedKeyset(tx.Table(saleWorkRows, p.TenantID, outlet, invoice, since, until, invoice, invoice, customer, customer), params,
			tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).Find(&rows).Error; err != nil {
			return err
		}
		// A bed code with its date points to the bed's occupant, so a page
		// showing an admission sale is recorded (#1555); others carry no one.
		// Only the rows the page shows: ListRoute reads one more to see if
		// there is a next page and drops it.
		for _, r := range rows[:min(len(rows), params.Limit)] {
			if r.Customer == "admission" {
				return auditRegisterRead(c, tx, "sales_worklist", p.TenantID, p.Subject, outlet, "list_read",
					"from", orAll(since), "to", orAll(until), "customer", orAll(customer), "invoice", orAll(invoice))
			}
		}
		return nil
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

type summaryLine struct {
	LineNumber    int       `gorm:"column:line_number" json:"line_number"`
	ProductID     uuid.UUID `gorm:"column:product_id" json:"product_id"`
	SKU           string    `gorm:"column:sku" json:"sku"`
	BrandName     string    `gorm:"column:brand_name" json:"brand_name"`
	LotNumber     string    `gorm:"column:lot_number" json:"lot_number"`
	Units         int64     `gorm:"column:quantity_units" json:"quantity_units"`
	ReturnedUnits int64     `gorm:"column:returned_units" json:"returned_units"`
	DiscountMinor int64     `gorm:"column:discount_minor" json:"discount_minor"`
	AmountMinor   int64     `gorm:"column:amount_minor" json:"amount_minor"`
	CreditedMinor int64     `gorm:"column:credited_minor" json:"credited_minor"`
}

type summaryCharge struct {
	ChargeNumber  int    `gorm:"column:charge_number" json:"charge_number"`
	Code          string `gorm:"column:code" json:"code"`
	Name          string `gorm:"column:name" json:"name"`
	AmountMinor   int64  `gorm:"column:amount_minor" json:"amount_minor"`
	CreditedMinor int64  `gorm:"column:credited_minor" json:"credited_minor"`
}

type summaryCreditNote struct {
	ID               uuid.UUID        `gorm:"column:id" json:"id"`
	CreditNoteNumber string           `gorm:"column:credit_note_number" json:"credit_note_number"`
	CreditNoteDate   string           `gorm:"column:credit_note_date" json:"credit_note_date"`
	TotalMinor       int64            `gorm:"column:total_minor" json:"total_minor"`
	Refunds          map[string]int64 `gorm:"-" json:"refunds"`
}

type kindAmount struct {
	Owner       uuid.UUID `gorm:"column:owner"`
	Kind        string    `gorm:"column:kind"`
	AmountMinor int64     `gorm:"column:amount_minor"`
}

// summaryCreditNotes bounds the credit notes a summary lists: a line of a
// million units could be returned a unit at a time. Totals always count
// every note; the rest are on the paged credit-notes list. A var only so a
// test can reach the bound.
var summaryCreditNotes = 100

// summaryMidRead is a test seam between a summary's reads; a no-op otherwise.
var summaryMidRead = func() {}

// saleSummary is one sale's statement. The database holds a sale's total to
// its lines and charges and to its tenders, and a credit note's total to its
// lines and charges and to its refunds, so net always equals what was kept
// at the counter plus what remains on the admission account.
type saleSummary struct {
	ID                     uuid.UUID           `gorm:"column:id" json:"id"`
	InvoiceNumber          string              `gorm:"column:invoice_number" json:"invoice_number"`
	SaleDate               string              `gorm:"column:sale_date" json:"sale_date"`
	Customer               string              `gorm:"column:customer" json:"customer"`
	BedCode                *string             `gorm:"column:admission_bed_code" json:"admission_bed_code"`
	Currency               string              `gorm:"column:currency" json:"currency"`
	TotalMinor             int64               `gorm:"column:total_minor" json:"total_minor"`
	TaxableMinor           int64               `gorm:"column:taxable_minor" json:"taxable_minor"`
	CGSTMinor              int64               `gorm:"column:cgst_minor" json:"cgst_minor"`
	SGSTMinor              int64               `gorm:"column:sgst_minor" json:"sgst_minor"`
	DiscountMinor          int64               `gorm:"-" json:"discount_minor"`
	Lines                  []summaryLine       `gorm:"-" json:"lines"`
	Charges                []summaryCharge     `gorm:"-" json:"charges"`
	Payments               map[string]int64    `gorm:"-" json:"payments"`
	CreditNotes            []summaryCreditNote `gorm:"-" json:"credit_notes"`
	MoreCreditNotes        bool                `gorm:"-" json:"more_credit_notes"`
	CreditedMinor          int64               `gorm:"-" json:"credited_minor"`
	NetMinor               int64               `gorm:"-" json:"net_minor"`
	CollectedMinor         int64               `gorm:"-" json:"collected_minor"`
	RefundedMinor          int64               `gorm:"-" json:"refunded_minor"`
	AdmissionBilledMinor   int64               `gorm:"-" json:"admission_billed_minor"`
	AdmissionReversedMinor int64               `gorm:"-" json:"admission_reversed_minor"`
}

func (h *catalogueHandlers) getSaleSummary(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	pharmacy := currentPharmacy(c.Request.Context())
	if pharmacy == uuid.Nil {
		respond.NotFound(c, "pharmacy resource")
		return
	}
	var s saleSummary
	// One snapshot for every read, so a return posted meanwhile is wholly in
	// or wholly out and the statement's identities hold in the response, not
	// only in the database. The snapshot needs no lock on the grant; it is
	// writable only for the read's own audit row.
	err := h.db.WithTenantRecordedSnapshot(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if _, err := outletAccess(tx, p, outlet, pharmacy, false); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('statement_timeout','8s',true)`).Error; err != nil {
			return err
		}
		if err := loadSaleSummary(tx, p.TenantID, outlet, id, &s); err != nil {
			return err
		}
		if s.Customer != "admission" {
			return nil
		}
		// The bed code points to its occupant: recorded in the same snapshot (#1555).
		return auditRegisterRead(c, tx, "sale", p.TenantID, p.Subject, id, "summary_read")
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, s)
}

func loadSaleSummary(tx *gorm.DB, tenant string, outlet, id uuid.UUID, s *saleSummary) error {
	res := tx.Raw(`SELECT s.id, s.invoice_number, to_char(s.sale_date,'YYYY-MM-DD') AS sale_date, `+saleCustomer+` AS customer, s.admission_bed_code,
 s.currency, s.total_minor, s.taxable_minor, s.cgst_minor, s.sgst_minor FROM pharmacy_sales s WHERE s.tenant_id=? AND s.outlet_id=? AND s.id=?`, tenant, outlet, id).Scan(s)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	if err := tx.Raw(`SELECT l.line_number, l.product_id, c.sku, c.brand_name, l.lot_number, l.quantity_units, l.discount_minor, l.amount_minor,
 COALESCE(r.units,0)::bigint AS returned_units, COALESCE(r.amount,0)::bigint AS credited_minor
 FROM pharmacy_sale_lines l
 JOIN pharmacy_catalogue_products c ON c.tenant_id=l.tenant_id AND c.outlet_id=l.outlet_id AND c.id=l.product_id
 LEFT JOIN (SELECT sale_line, sum(quantity_units) AS units, sum(amount_minor) AS amount FROM pharmacy_credit_note_lines
   WHERE tenant_id=? AND outlet_id=? AND sale_id=? GROUP BY sale_line) r ON r.sale_line=l.line_number
 WHERE l.tenant_id=? AND l.outlet_id=? AND l.sale_id=? ORDER BY l.line_number`, tenant, outlet, id, tenant, outlet, id).Scan(&s.Lines).Error; err != nil {
		return err
	}
	summaryMidRead()
	if err := tx.Raw(`SELECT g.charge_number, g.code, g.name, g.amount_minor, COALESCE(r.amount,0)::bigint AS credited_minor
 FROM pharmacy_sale_charge_lines g
 LEFT JOIN (SELECT sale_charge, sum(amount_minor) AS amount FROM pharmacy_credit_note_charge_lines
   WHERE tenant_id=? AND outlet_id=? AND sale_id=? GROUP BY sale_charge) r ON r.sale_charge=g.charge_number
 WHERE g.tenant_id=? AND g.outlet_id=? AND g.sale_id=? ORDER BY g.charge_number`, tenant, outlet, id, tenant, outlet, id).Scan(&s.Charges).Error; err != nil {
		return err
	}
	var tenders []kindAmount
	if err := tx.Raw(`SELECT sale_id AS owner, kind, sum(amount_minor)::bigint AS amount_minor FROM pharmacy_sale_tenders
 WHERE tenant_id=? AND outlet_id=? AND sale_id=? GROUP BY sale_id, kind`, tenant, outlet, id).Scan(&tenders).Error; err != nil {
		return err
	}
	if err := tx.Raw(`SELECT COALESCE(sum(total_minor),0)::bigint FROM pharmacy_credit_notes WHERE tenant_id=? AND outlet_id=? AND sale_id=?`,
		tenant, outlet, id).Scan(&s.CreditedMinor).Error; err != nil {
		return err
	}
	var refundTotals []kindAmount
	if err := tx.Raw(`SELECT r.kind, sum(r.amount_minor)::bigint AS amount_minor FROM pharmacy_credit_note_refunds r
 JOIN pharmacy_credit_notes n ON n.tenant_id=r.tenant_id AND n.outlet_id=r.outlet_id AND n.id=r.credit_note_id
 WHERE n.tenant_id=? AND n.outlet_id=? AND n.sale_id=? GROUP BY r.kind`, tenant, outlet, id).Scan(&refundTotals).Error; err != nil {
		return err
	}
	if err := tx.Raw(`SELECT id, credit_note_number, to_char(credit_note_date,'YYYY-MM-DD') AS credit_note_date, total_minor FROM pharmacy_credit_notes
 WHERE tenant_id=? AND outlet_id=? AND sale_id=? ORDER BY created_at, id LIMIT ?`, tenant, outlet, id, summaryCreditNotes+1).Scan(&s.CreditNotes).Error; err != nil {
		return err
	}
	if len(s.CreditNotes) > summaryCreditNotes {
		s.CreditNotes, s.MoreCreditNotes = s.CreditNotes[:summaryCreditNotes], true
	}
	listed := make([]uuid.UUID, 0, len(s.CreditNotes))
	for _, n := range s.CreditNotes {
		listed = append(listed, n.ID)
	}
	var refunds []kindAmount
	if len(listed) > 0 {
		if err := tx.Raw(`SELECT credit_note_id AS owner, kind, sum(amount_minor)::bigint AS amount_minor FROM pharmacy_credit_note_refunds
 WHERE tenant_id=? AND outlet_id=? AND credit_note_id IN ? GROUP BY credit_note_id, kind`, tenant, outlet, listed).Scan(&refunds).Error; err != nil {
			return err
		}
	}
	if s.Lines == nil {
		s.Lines = []summaryLine{}
	}
	if s.Charges == nil {
		s.Charges = []summaryCharge{}
	}
	if s.CreditNotes == nil {
		s.CreditNotes = []summaryCreditNote{}
	}
	for _, l := range s.Lines {
		s.DiscountMinor += l.DiscountMinor
	}
	s.Payments = map[string]int64{}
	for _, t := range tenders {
		s.Payments[t.Kind] = t.AmountMinor
		if t.Kind == "admission_account" {
			s.AdmissionBilledMinor += t.AmountMinor
		} else {
			s.CollectedMinor += t.AmountMinor
		}
	}
	notes := map[uuid.UUID]*summaryCreditNote{}
	for i := range s.CreditNotes {
		n := &s.CreditNotes[i]
		n.Refunds = map[string]int64{}
		notes[n.ID] = n
	}
	for _, r := range refunds {
		n, ok := notes[r.Owner]
		if !ok {
			return errors.New("sale summary: a refund outside its sale's credit notes")
		}
		n.Refunds[r.Kind] = r.AmountMinor
	}
	for _, r := range refundTotals {
		if r.Kind == "admission_account" {
			s.AdmissionReversedMinor += r.AmountMinor
		} else {
			s.RefundedMinor += r.AmountMinor
		}
	}
	s.NetMinor = s.TotalMinor - s.CreditedMinor
	return nil
}

// orAll is an absent filter as the audit log writes it.
func orAll(v *string) string {
	if v == nil {
		return "all"
	}
	return *v
}
