package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Returns at the counter (#1386 C-1). A return is a GST credit note against
// one posted sale (CGST Act s.34, Rule 53(1A)): it reverses each returned
// batch line pro rata, refunds the total, and receives the units into
// quarantine, where only a release can put them back on a shelf.

type creditNoteLineInput struct {
	SaleLine             int       `json:"sale_line" binding:"required,min=1,max=1000"`
	QuantityUnits        int64     `json:"quantity_units" binding:"required,min=1,max=1000000"`
	QuarantineLocationID uuid.UUID `json:"quarantine_location_id" binding:"required"`
}

type creditNoteRecipient struct {
	Name    string `json:"name" binding:"required"`
	Address string `json:"address" binding:"required"`
}

type creditNoteInput struct {
	SaleID    uuid.UUID             `json:"sale_id" binding:"required"`
	Lines     []creditNoteLineInput `json:"lines" binding:"required,min=1,max=200,dive"`
	Refunds   []saleTenderInput     `json:"refunds" binding:"required,min=1,max=5,dive"`
	Reason    string                `json:"reason" binding:"required"`
	Recipient creditNoteRecipient   `json:"recipient" binding:"required"`
}

func (in creditNoteInput) digest() string {
	parts := []string{in.SaleID.String(), in.Reason, in.Recipient.Name, in.Recipient.Address}
	for _, l := range in.Lines {
		parts = append(parts, "l", strconv.Itoa(l.SaleLine), strconv.FormatInt(l.QuantityUnits, 10), l.QuarantineLocationID.String())
	}
	for _, t := range in.Refunds {
		reference := ""
		if t.Reference != nil {
			reference = *t.Reference
		}
		parts = append(parts, "r", t.Kind, strconv.FormatInt(t.AmountMinor, 10), reference)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// validRefunds: cash carries no reference; UPI and card carry one.
func (in *creditNoteInput) validRefunds() bool {
	for i := range in.Refunds {
		t := &in.Refunds[i]
		if t.TenderedMinor != nil {
			return false
		}
		if t.Kind == "cash" {
			if t.Reference != nil {
				return false
			}
			continue
		}
		if t.Reference == nil || strings.TrimSpace(*t.Reference) == "" {
			return false
		}
		ref := strings.TrimSpace(*t.Reference)
		t.Reference = &ref
	}
	return true
}

type creditNote struct {
	ID               uuid.UUID          `gorm:"column:id" json:"id"`
	SaleID           uuid.UUID          `gorm:"column:sale_id" json:"sale_id"`
	CreditNoteNumber string             `gorm:"column:credit_note_number" json:"credit_note_number"`
	FinancialYear    string             `gorm:"column:financial_year" json:"financial_year"`
	CreditNoteDate   string             `gorm:"column:credit_note_date" json:"credit_note_date"`
	InvoiceNumber    string             `gorm:"column:invoice_number" json:"invoice_number"`
	InvoiceDate      string             `gorm:"column:invoice_date" json:"invoice_date"`
	SellerLegalName  string             `gorm:"column:seller_legal_name" json:"seller_legal_name"`
	SellerAddress    string             `gorm:"column:seller_address" json:"seller_address"`
	SellerGSTIN      string             `gorm:"column:seller_gstin" json:"seller_gstin"`
	PlaceOfSupply    string             `gorm:"column:place_of_supply" json:"place_of_supply"`
	Currency         string             `gorm:"column:currency" json:"currency"`
	TotalMinor       int64              `gorm:"column:total_minor" json:"total_minor"`
	TaxableMinor     int64              `gorm:"column:taxable_minor" json:"taxable_minor"`
	CGSTMinor        int64              `gorm:"column:cgst_minor" json:"cgst_minor"`
	SGSTMinor        int64              `gorm:"column:sgst_minor" json:"sgst_minor"`
	Reason           string             `gorm:"column:reason" json:"reason"`
	RecipientName    string             `gorm:"column:recipient_name" json:"recipient_name"`
	RecipientAddress string             `gorm:"column:recipient_address" json:"recipient_address"`
	ActorSubject     string             `gorm:"column:actor_subject" json:"actor_subject"`
	CreatedAt        time.Time          `gorm:"column:created_at" json:"created_at"`
	Lines            []creditNoteLine   `gorm:"-" json:"lines,omitempty"`
	Refunds          []creditNoteRefund `gorm:"-" json:"refunds,omitempty"`
}

func (creditNote) TableName() string                 { return "pharmacy_credit_notes" }
func (r creditNote) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// creditNoteColumns joins the sale for the invoice reference Rule 53(1A)(g)
// requires and the seller the credit note names.
const creditNoteColumns = //nolint:gosec // SQL column names, not a credential value
`n.id, n.sale_id, n.credit_note_number, n.financial_year, to_char(n.credit_note_date,'YYYY-MM-DD') AS credit_note_date,
 s.invoice_number, to_char(s.sale_date,'YYYY-MM-DD') AS invoice_date, s.seller_legal_name, s.seller_address, s.seller_gstin, s.place_of_supply,
 n.currency, n.total_minor, n.taxable_minor, n.cgst_minor, n.sgst_minor, n.reason, n.recipient_name, n.recipient_address, n.actor_subject, n.created_at`

const creditNoteFrom = ` FROM pharmacy_credit_notes n JOIN pharmacy_sales s ON s.tenant_id=n.tenant_id AND s.outlet_id=n.outlet_id AND s.id=n.sale_id`

type creditNoteLine struct {
	LineNumber         int       `gorm:"column:line_number" json:"line_number"`
	SaleLine           int       `gorm:"column:sale_line" json:"sale_line"`
	ProductID          uuid.UUID `gorm:"column:product_id" json:"product_id"`
	BatchID            uuid.UUID `gorm:"column:batch_id" json:"batch_id"`
	LotNumber          string    `gorm:"column:lot_number" json:"lot_number"`
	LocationID         uuid.UUID `gorm:"column:location_id" json:"location_id"`
	ClassificationCode string    `gorm:"column:classification_code" json:"classification_code"`
	RatePPM            int64     `gorm:"column:rate_ppm" json:"rate_ppm"`
	QuantityUnits      int64     `gorm:"column:quantity_units" json:"quantity_units"`
	AmountMinor        int64     `gorm:"column:amount_minor" json:"amount_minor"`
	TaxableMinor       int64     `gorm:"column:taxable_minor" json:"taxable_minor"`
	CGSTMinor          int64     `gorm:"column:cgst_minor" json:"cgst_minor"`
	SGSTMinor          int64     `gorm:"column:sgst_minor" json:"sgst_minor"`
}

type creditNoteRefund struct {
	RefundNumber int     `gorm:"column:refund_number" json:"refund_number"`
	Kind         string  `gorm:"column:kind" json:"kind"`
	AmountMinor  int64   `gorm:"column:amount_minor" json:"amount_minor"`
	Reference    *string `gorm:"column:reference" json:"reference"`
}

type errNotReturnable struct{ reason string }

func (e errNotReturnable) Error() string { return e.reason }

func creditNoteRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/credit-notes/:id", PermSaleReturn, h.putCreditNote)
	s.GET("/credit-notes/:id", PermSaleRead, h.getCreditNote)
	platform.ListRoute(s, "/credit-notes", PermSaleRead, h.listCreditNotes)
}

// proRata is the cumulative pro-rata credit of v for k of q units, rounded
// down, so the steps of successive returns sum to exactly v (decision 2).
func proRata(v, k, q int64) int64 {
	return new(big.Int).Quo(new(big.Int).Mul(big.NewInt(v), big.NewInt(k)), big.NewInt(q)).Int64()
}

// creditStep is a return of r units after k already returned, of a sale
// line of q units charging amount with tax half half.
func creditStep(amount, half, q, k, r int64) (credit, taxable, creditHalf int64) {
	credit = proRata(amount, k+r, q) - proRata(amount, k, q)
	creditHalf = proRata(half, k+r, q) - proRata(half, k, q)
	return credit, credit - 2*creditHalf, creditHalf
}

// returnDeadline is the last day a credit note may be issued for a sale of
// financial year fy (CGST Act s.34(2)): 30 November after the year ends.
func returnDeadline(fy string) (string, error) {
	end, err := strconv.Atoi(fy[2:])
	if err != nil || len(fy) != 4 {
		return "", fmt.Errorf("financial year %q is not YYZZ", fy)
	}
	return fmt.Sprintf("%04d-11-30", 2000+end), nil
}

func (h *catalogueHandlers) putCreditNote(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in creditNoteInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	in.Recipient.Name, in.Recipient.Address = strings.TrimSpace(in.Recipient.Name), strings.TrimSpace(in.Recipient.Address)
	seen := map[int]bool{}
	for _, l := range in.Lines {
		if seen[l.SaleLine] {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "each sale line appears once")
			return
		}
		seen[l.SaleLine] = true
	}
	if !prescriptionText(in.Reason, 200) || len([]rune(in.Reason)) < 5 || !prescriptionText(in.Recipient.Name, 200) ||
		!prescriptionText(in.Recipient.Address, 500) || !in.validRefunds() {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a return gives a reason of 5 to 200 characters, the name and address of the person refunded, and refunds in cash or by UPI or card with a reference")
		return
	}
	digest := in.digest()
	var result creditNote
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":credit-note:"+id.String()); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, ActorSubject string }
		if err := tx.Raw(`SELECT intent_sha256, actor_subject FROM pharmacy_credit_notes WHERE tenant_id=? AND outlet_id=? AND id=?`,
			p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].IntentSHA256 != digest || existing[0].ActorSubject != p.Subject {
				return errStockIdentity
			}
			return loadCreditNote(tx, p.TenantID, outlet, id, &result)
		}
		created = true
		return h.issueCreditNote(c, tx, outlet, id, in, digest, &result)
	})
	var refusal errNotReturnable
	switch {
	case errors.As(err, &refusal):
		respond.Error(c, http.StatusConflict, "not_returnable", refusal.reason)
	case creditLineLimit(err):
		respond.Error(c, http.StatusConflict, "not_returnable", "those units have already been returned; check what is left to return")
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, result)
	default:
		respond.OK(c, result)
	}
}

func (h *catalogueHandlers) issueCreditNote(c *gin.Context, tx *gorm.DB, outlet, id uuid.UUID, in creditNoteInput, digest string, out *creditNote) error {
	p, _, _ := pharmacyPrincipal(c)
	var sales []struct {
		FinancialYear string
		SaleDate      string
		Currency      string
	}
	if err := tx.Raw(`SELECT financial_year, to_char(sale_date,'YYYY-MM-DD') AS sale_date, currency FROM pharmacy_sales WHERE tenant_id=? AND outlet_id=? AND id=?`,
		p.TenantID, outlet, in.SaleID).Scan(&sales).Error; err != nil {
		return err
	}
	if len(sales) == 0 {
		return gorm.ErrRecordNotFound
	}
	sale := sales[0]
	today, err := outletLocalDate(tx, p.TenantID, outlet)
	if err != nil {
		return err
	}
	deadline, err := returnDeadline(sale.FinancialYear)
	if err != nil {
		return err
	}
	if today > deadline {
		return errNotReturnable{"credit notes for this sale's financial year closed on " + deadline}
	}
	profile, err := currentInvoiceProfile(tx, p.TenantID, outlet)
	if err != nil {
		return err
	}
	if profile == nil {
		return errNotReturnable{"record the outlet's invoice details before issuing a credit note"}
	}
	fy, err := financialYear(today)
	if err != nil {
		return err
	}
	// Sale lines are locked in order, as the line guard locks them, so two
	// returns of one sale serialise and each counts what the other returned.
	lines := append([]creditNoteLineInput{}, in.Lines...)
	sort.Slice(lines, func(i, j int) bool { return lines[i].SaleLine < lines[j].SaleLine })
	type built struct {
		in                    creditNoteLineInput
		productID, batchID    uuid.UUID
		amount, taxable, half int64
	}
	var steps []built
	var total, taxable, half int64
	for _, l := range lines {
		var rows []struct {
			ProductID     uuid.UUID
			BatchID       uuid.UUID
			SKU           string
			QuantityUnits int64
			AmountMinor   int64
			CGSTMinor     int64
		}
		if err := tx.Raw(`SELECT l.product_id, l.batch_id, p.sku, l.quantity_units, l.amount_minor, l.cgst_minor FROM pharmacy_sale_lines l
			JOIN pharmacy_catalogue_products p ON p.tenant_id=l.tenant_id AND p.outlet_id=l.outlet_id AND p.id=l.product_id
			WHERE l.tenant_id=? AND l.outlet_id=? AND l.sale_id=? AND l.line_number=? FOR UPDATE OF l`, p.TenantID, outlet, in.SaleID, l.SaleLine).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return errNotReturnable{fmt.Sprintf("the sale has no line %d", l.SaleLine)}
		}
		sold := rows[0]
		var returned int64
		if err := tx.Raw(`SELECT COALESCE(sum(quantity_units),0) FROM pharmacy_credit_note_lines WHERE tenant_id=? AND outlet_id=? AND sale_id=? AND sale_line=?`,
			p.TenantID, outlet, in.SaleID, l.SaleLine).Scan(&returned).Error; err != nil {
			return err
		}
		if returned+l.QuantityUnits > sold.QuantityUnits {
			return errNotReturnable{fmt.Sprintf("%s: %d of %d units are left to return", sold.SKU, sold.QuantityUnits-returned, sold.QuantityUnits)}
		}
		var status string
		if err := tx.Raw(`SELECT stock_status FROM pharmacy_stock_locations WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, l.QuarantineLocationID).Scan(&status).Error; err != nil {
			return err
		}
		if status != "quarantine" {
			return errNotReturnable{"returned medicine goes to a quarantine location; a pharmacist releases it to sell it again"}
		}
		amount, tax, h := creditStep(sold.AmountMinor, sold.CGSTMinor, sold.QuantityUnits, returned, l.QuantityUnits)
		steps = append(steps, built{in: l, productID: sold.ProductID, batchID: sold.BatchID, amount: amount, taxable: tax, half: h})
		total, taxable, half = total+amount, taxable+tax, half+h
	}
	if total == 0 {
		return errNotReturnable{"these units credit nothing; return more units"}
	}
	var refunded int64
	for _, t := range in.Refunds {
		refunded += t.AmountMinor
	}
	if refunded != total {
		return errNotReturnable{fmt.Sprintf("the refunds come to %d but the credit note is %d (minor units)", refunded, total)}
	}
	if err := tx.Exec(`INSERT INTO pharmacy_credit_note_series (tenant_id,outlet_id,financial_year) VALUES (?,?,?) ON CONFLICT DO NOTHING`, p.TenantID, outlet, fy).Error; err != nil {
		return err
	}
	var number int64
	if err := tx.Raw(`UPDATE pharmacy_credit_note_series SET last_number=last_number+1 WHERE tenant_id=? AND outlet_id=? AND financial_year=? RETURNING last_number`,
		p.TenantID, outlet, fy).Scan(&number).Error; err != nil {
		return err
	}
	numberText := profile.InvoicePrefix + "/" + fy + "/C" + strconv.FormatInt(number, 10)
	if len(numberText) > 16 {
		return errNotReturnable{"this year's credit note series is full; record a shorter invoice prefix"}
	}
	if err := tx.Exec(`INSERT INTO pharmacy_credit_notes (id,tenant_id,outlet_id,sale_id,financial_year,sequence_number,credit_note_number,credit_note_date,currency,
		total_minor,taxable_minor,cgst_minor,sgst_minor,reason,recipient_name,recipient_address,intent_sha256,actor_subject)
		VALUES (?,?,?,?,?,?,?,?::date,?,?,?,?,?,?,?,?,?,?)`, id, p.TenantID, outlet, in.SaleID, fy, number, numberText, today, sale.Currency,
		total, taxable, half, half, in.Reason, in.Recipient.Name, in.Recipient.Address, digest, p.Subject).Error; err != nil {
		return err
	}
	issued := pharmacycontract.CreditNoteIssuedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), CreditNoteID: id.String(),
		SaleID: in.SaleID.String(), CreditNoteNumber: numberText, Currency: sale.Currency, TotalMinor: total}
	for i, st := range steps {
		n := i + 1
		if err := tx.Exec(`INSERT INTO pharmacy_credit_note_lines (tenant_id,outlet_id,credit_note_id,line_number,sale_id,sale_line,location_id,quantity_units,
			amount_minor,taxable_minor,cgst_minor,sgst_minor) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, outlet, id, n, in.SaleID, st.in.SaleLine,
			st.in.QuarantineLocationID, st.in.QuantityUnits, st.amount, st.taxable, st.half, st.half).Error; err != nil {
			return err
		}
		movement := stockMovement{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ActorSubject: p.Subject,
			MovementFields: movementInput{LocationID: st.in.QuarantineLocationID, BatchID: st.batchID, Kind: "return", QuantityUnits: st.in.QuantityUnits,
				Reason: "return " + numberText, SourceReference: "return:" + id.String() + ":" + strconv.Itoa(n)}}
		if err := tx.Create(&movement).Error; err != nil {
			return err
		}
		data, err := json.Marshal(pharmacycontract.StockMovementRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(),
			MovementID: movement.ID.String(), LocationID: st.in.QuarantineLocationID.String(), BatchID: st.batchID.String()})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, pharmacycontract.SubjectStockMovementRecorded, events.Event{Type: "StockMovementRecorded", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		issued.Lines = append(issued.Lines, pharmacycontract.CreditNoteIssuedLine{ProductID: st.productID.String(), BatchID: st.batchID.String(), QuantityUnits: st.in.QuantityUnits})
	}
	for i, t := range in.Refunds {
		if err := tx.Exec(`INSERT INTO pharmacy_credit_note_refunds (tenant_id,outlet_id,credit_note_id,refund_number,kind,amount_minor,reference) VALUES (?,?,?,?,?,?,?)`,
			p.TenantID, outlet, id, i+1, t.Kind, t.AmountMinor, t.Reference).Error; err != nil {
			return err
		}
	}
	data, err := json.Marshal(issued)
	if err != nil {
		return err
	}
	if err := h.bus.Publish(tx, pharmacycontract.SubjectCreditNoteIssued, events.Event{Type: "CreditNoteIssued", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
		return err
	}
	return loadCreditNote(tx, p.TenantID, outlet, id, out)
}

func loadCreditNote(tx *gorm.DB, tenant string, outlet, id uuid.UUID, out *creditNote) error {
	var rows []creditNote
	if err := tx.Raw(`SELECT `+creditNoteColumns+creditNoteFrom+` WHERE n.tenant_id=? AND n.outlet_id=? AND n.id=?`, tenant, outlet, id).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return gorm.ErrRecordNotFound
	}
	*out = rows[0]
	if err := tx.Raw(`SELECT c.line_number, c.sale_line, s.product_id, s.batch_id, s.lot_number, c.location_id, s.classification_code, s.rate_ppm,
		c.quantity_units, c.amount_minor, c.taxable_minor, c.cgst_minor, c.sgst_minor
		FROM pharmacy_credit_note_lines c JOIN pharmacy_sale_lines s ON s.tenant_id=c.tenant_id AND s.outlet_id=c.outlet_id AND s.sale_id=c.sale_id AND s.line_number=c.sale_line
		WHERE c.tenant_id=? AND c.outlet_id=? AND c.credit_note_id=? ORDER BY c.line_number`, tenant, outlet, id).Scan(&out.Lines).Error; err != nil {
		return err
	}
	return tx.Raw(`SELECT refund_number, kind, amount_minor, reference FROM pharmacy_credit_note_refunds WHERE tenant_id=? AND outlet_id=? AND credit_note_id=? ORDER BY refund_number`,
		tenant, outlet, id).Scan(&out.Refunds).Error
}

func (h *catalogueHandlers) getCreditNote(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var result creditNote
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return loadCreditNote(tx, p.TenantID, outlet, id, &result)
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

// listCreditNotes pages an outlet's credit notes, optionally those of one sale.
func (h *catalogueHandlers) listCreditNotes(c *gin.Context, params pagination.Params) ([]creditNote, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var sale *uuid.UUID
	if raw := c.Query("sale_id"); raw != "" {
		v, err := uuid.Parse(raw)
		if err != nil {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "sale_id is a sale id")
			return nil, nil
		}
		sale = &v
	}
	var rows []creditNote
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		q := tenantdb.ApplyKeyset(tx.Table("pharmacy_credit_notes n").Joins("JOIN pharmacy_sales s ON s.tenant_id=n.tenant_id AND s.outlet_id=n.outlet_id AND s.id=n.sale_id"), params).
			Select(creditNoteColumns).Where("n.tenant_id=? AND n.outlet_id=?", p.TenantID, outlet)
		if sale != nil {
			q = q.Where("n.sale_id=?", *sale)
		}
		return q.Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

// creditLineLimit is the line guard refusing a return its handler check passed.
func creditLineLimit(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.ConstraintName == "pharmacy_credit_note_line_limit"
}
