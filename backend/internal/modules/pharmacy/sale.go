package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Counter sale (#1386 B-1): one command takes OTC stock off the shelf
// earliest expiry first, at batch MRP, and posts a numbered GST invoice.

// errNotSellable names why a sale cannot be posted; the message is shown to
// the cashier as is, and nothing of the sale is kept.
type errNotSellable struct{ reason string }

func (e errNotSellable) Error() string { return e.reason }

// maxSaleLines bounds one invoice, so reading it back needs no cursor.
const maxSaleLines = 200

var invoicePrefixPattern = regexp.MustCompile(`^[A-Z0-9]{1,4}$`)

type invoiceProfileInput struct {
	LegalName     string `json:"legal_name" binding:"required,max=200"`
	Address       string `json:"address" binding:"required,max=500"`
	InvoicePrefix string `json:"invoice_prefix" binding:"required"`
}

type invoiceProfile struct {
	ID            uuid.UUID `gorm:"column:id" json:"id"`
	LegalName     string    `gorm:"column:legal_name" json:"legal_name"`
	Address       string    `gorm:"column:address" json:"address"`
	InvoicePrefix string    `gorm:"column:invoice_prefix" json:"invoice_prefix"`
	RecordedBy    string    `gorm:"column:recorded_by" json:"recorded_by"`
	CreatedAt     time.Time `gorm:"column:created_at" json:"created_at"`
}

const invoiceProfileColumns = "id,legal_name,address,invoice_prefix,recorded_by,created_at"

type saleLineInput struct {
	ProductID     uuid.UUID `json:"product_id" binding:"required"`
	QuantityUnits int64     `json:"quantity_units" binding:"required,min=1,max=1000000"`
	// The prescription line a G, H or H1 line dispenses (B-2b decision 2).
	Prescription *saleCitation `json:"prescription" binding:"omitempty"`
	// A percentage off the line in basis points (B-3 decision 1).
	DiscountBps int64 `json:"discount_bps" binding:"min=0,max=10000"`
}

type saleCitation struct {
	PrescriptionID uuid.UUID `json:"prescription_id" binding:"required"`
	LineNumber     int       `json:"line_number" binding:"required,min=1,max=20"`
}

// saleBuyer is the optional walk-in buyer (decision 7): pharmacy-owned and
// never published.
type saleBuyer struct {
	Name  *string `json:"name" binding:"omitempty"`
	Phone *string `json:"phone" binding:"omitempty"`
}

var buyerPhonePattern = regexp.MustCompile(`^\+?[0-9]{6,15}$`)

func (b *saleBuyer) valid() bool {
	if b == nil {
		return true
	}
	return (b.Name == nil || prescriptionText(*b.Name, 200)) && (b.Phone == nil || buyerPhonePattern.MatchString(*b.Phone)) && (b.Name != nil || b.Phone != nil)
}

type saleTenderInput struct {
	Kind          string  `json:"kind" binding:"required,oneof=cash upi card"`
	AmountMinor   int64   `json:"amount_minor" binding:"required,min=1,max=1000000000000"`
	TenderedMinor *int64  `json:"tendered_minor" binding:"omitempty,min=1,max=1000000000000"`
	Reference     *string `json:"reference" binding:"omitempty,max=64"`
}

type saleInput struct {
	Lines   []saleLineInput   `json:"lines" binding:"required,min=1,max=50,dive"`
	Tenders []saleTenderInput `json:"tenders" binding:"required,min=1,max=5,dive"`
	Buyer   *saleBuyer        `json:"buyer"`
	// Required exactly when a line is discounted (B-3 decision 2).
	DiscountReason *string `json:"discount_reason"`
}

type saleQuoteInput struct {
	Lines []saleLineInput `json:"lines" binding:"required,min=1,max=50,dive"`
}

func distinctProducts(lines []saleLineInput) bool {
	seen := map[uuid.UUID]bool{}
	for _, l := range lines {
		if seen[l.ProductID] {
			return false
		}
		seen[l.ProductID] = true
	}
	return true
}

func (in saleInput) digest() string {
	parts := []string{}
	for _, l := range in.Lines {
		parts = append(parts, "l", l.ProductID.String(), strconv.FormatInt(l.QuantityUnits, 10))
		if l.Prescription != nil {
			parts = append(parts, "rx", l.Prescription.PrescriptionID.String(), strconv.Itoa(l.Prescription.LineNumber))
		}
		if l.DiscountBps > 0 {
			parts = append(parts, "d", strconv.FormatInt(l.DiscountBps, 10))
		}
	}
	if in.DiscountReason != nil {
		parts = append(parts, "dr", *in.DiscountReason)
	}
	if in.Buyer != nil {
		name, phone := "", ""
		if in.Buyer.Name != nil {
			name = "\x1e" + *in.Buyer.Name
		}
		if in.Buyer.Phone != nil {
			phone = "\x1e" + *in.Buyer.Phone
		}
		parts = append(parts, "b", name, phone)
	}
	for _, t := range in.Tenders {
		tendered, reference := "", ""
		if t.TenderedMinor != nil {
			tendered = strconv.FormatInt(*t.TenderedMinor, 10)
		}
		if t.Reference != nil {
			reference = *t.Reference
		}
		parts = append(parts, "t", t.Kind, strconv.FormatInt(t.AmountMinor, 10), tendered, reference)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// validTenders checks what the schema cannot say per row: one cash tender,
// with what was handed over; a reference for UPI and card.
func (in *saleInput) validTenders() bool {
	cash := 0
	for i := range in.Tenders {
		t := &in.Tenders[i]
		if t.Kind == "cash" {
			cash++
			if t.TenderedMinor == nil || *t.TenderedMinor < t.AmountMinor || t.Reference != nil {
				return false
			}
			continue
		}
		if t.TenderedMinor != nil || t.Reference == nil {
			return false
		}
		ref := strings.TrimSpace(*t.Reference)
		if ref == "" {
			return false
		}
		t.Reference = &ref
	}
	return cash <= 1
}

type saleRecord struct {
	ID              uuid.UUID    `gorm:"column:id" json:"id"`
	InvoiceNumber   string       `gorm:"column:invoice_number" json:"invoice_number"`
	FinancialYear   string       `gorm:"column:financial_year" json:"financial_year"`
	SaleDate        string       `gorm:"column:sale_date" json:"sale_date"`
	SellerLegalName string       `gorm:"column:seller_legal_name" json:"seller_legal_name"`
	SellerAddress   string       `gorm:"column:seller_address" json:"seller_address"`
	SellerGSTIN     string       `gorm:"column:seller_gstin" json:"seller_gstin"`
	PlaceOfSupply   string       `gorm:"column:place_of_supply" json:"place_of_supply"`
	Currency        string       `gorm:"column:currency" json:"currency"`
	TotalMinor      int64        `gorm:"column:total_minor" json:"total_minor"`
	TaxableMinor    int64        `gorm:"column:taxable_minor" json:"taxable_minor"`
	CGSTMinor       int64        `gorm:"column:cgst_minor" json:"cgst_minor"`
	SGSTMinor       int64        `gorm:"column:sgst_minor" json:"sgst_minor"`
	ActorSubject    string       `gorm:"column:actor_subject" json:"actor_subject"`
	BuyerName       *string      `gorm:"column:buyer_name" json:"buyer_name"`
	BuyerPhone      *string      `gorm:"column:buyer_phone" json:"buyer_phone"`
	DiscountReason  *string      `gorm:"column:discount_reason" json:"discount_reason"`
	DiscountMinor   int64        `gorm:"-" json:"discount_minor"`
	CreatedAt       time.Time    `gorm:"column:created_at" json:"created_at"`
	Lines           []saleLine   `gorm:"-" json:"lines,omitempty"`
	Tenders         []saleTender `gorm:"-" json:"tenders,omitempty"`
	ClassOrigin     string       `gorm:"-" json:"class_origin,omitempty"`
	CeilingChecked  bool         `gorm:"-" json:"nppa_ceiling_checked"`
}

func (saleRecord) TableName() string                 { return "pharmacy_sales" }
func (r saleRecord) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const saleColumns = "id,invoice_number,financial_year,to_char(sale_date,'YYYY-MM-DD') AS sale_date,seller_legal_name,seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,actor_subject,buyer_name,buyer_phone,discount_reason,created_at"

type saleLine struct {
	LineNumber         int        `gorm:"column:line_number" json:"line_number"`
	ProductID          uuid.UUID  `gorm:"column:product_id" json:"product_id"`
	BatchID            uuid.UUID  `gorm:"column:batch_id" json:"batch_id"`
	LocationID         uuid.UUID  `gorm:"column:location_id" json:"location_id"`
	LotNumber          string     `gorm:"column:lot_number" json:"lot_number"`
	ExpiresOn          string     `gorm:"column:expires_on" json:"expires_on"`
	RegulatoryClass    string     `gorm:"column:regulatory_class" json:"regulatory_class"`
	ClassificationCode string     `gorm:"column:classification_code" json:"classification_code"`
	SupplyNature       string     `gorm:"column:supply_nature" json:"supply_nature"`
	RatePPM            int64      `gorm:"column:rate_ppm" json:"rate_ppm"`
	MRPMinor           int64      `gorm:"column:mrp_minor" json:"mrp_minor"`
	PackSize           int64      `gorm:"column:pack_size" json:"pack_size"`
	QuantityUnits      int64      `gorm:"column:quantity_units" json:"quantity_units"`
	AmountMinor        int64      `gorm:"column:amount_minor" json:"amount_minor"`
	TaxableMinor       int64      `gorm:"column:taxable_minor" json:"taxable_minor"`
	CGSTMinor          int64      `gorm:"column:cgst_minor" json:"cgst_minor"`
	SGSTMinor          int64      `gorm:"column:sgst_minor" json:"sgst_minor"`
	PrescriptionID     *uuid.UUID `gorm:"column:prescription_id" json:"prescription_id"`
	PrescriptionLine   *int       `gorm:"column:prescription_line" json:"prescription_line"`
	ListAmountMinor    int64      `gorm:"column:list_amount_minor" json:"list_amount_minor"`
	DiscountBps        int64      `gorm:"column:discount_bps" json:"discount_bps"`
	DiscountMinor      int64      `gorm:"column:discount_minor" json:"discount_minor"`
	declaration        uuid.UUID
}

type saleTender struct {
	TenderNumber  int     `gorm:"column:tender_number" json:"tender_number"`
	Kind          string  `gorm:"column:kind" json:"kind"`
	AmountMinor   int64   `gorm:"column:amount_minor" json:"amount_minor"`
	TenderedMinor *int64  `gorm:"column:tendered_minor" json:"tendered_minor"`
	ChangeMinor   *int64  `gorm:"column:change_minor" json:"change_minor"`
	Reference     *string `gorm:"column:reference" json:"reference"`
}

// priceLine prices qty units of a pack at mrp (spec B3, B4). The amount is
// rounded down on the whole quantity, so it never exceeds MRP; GST is
// inside it, split into equal halves rounded half up.
func priceLine(mrp, pack, qty int64, nature string, ratePPM int64) (amount, taxable, half int64) {
	// big.Int: amount × rate can pass int64 at the schema's bounds.
	amount = new(big.Int).Quo(new(big.Int).Mul(big.NewInt(mrp), big.NewInt(qty)), big.NewInt(pack)).Int64()
	return taxInside(amount, nature, ratePPM)
}

// discountLine takes bps of a list amount off, rounded down so the discount
// stays within its percentage, and returns the discount and what is charged
// (B-3 decision 1; the schema checks the same rounding).
func discountLine(list, bps int64) (discount, amount int64) {
	discount = new(big.Int).Quo(new(big.Int).Mul(big.NewInt(list), big.NewInt(bps)), big.NewInt(10000)).Int64()
	return discount, list - discount
}

// taxInside splits a tax-inclusive amount into taxable value and equal CGST
// and SGST halves (spec B4). A discount recorded on the invoice is outside
// the value of supply (CGST Act s.15(3)(a)), so it runs on the discounted amount.
func taxInside(amount int64, nature string, ratePPM int64) (int64, int64, int64) {
	var half int64
	if nature == "taxable" {
		den := big.NewInt(2 * (1_000_000 + ratePPM))
		num := new(big.Int).Mul(big.NewInt(2*ratePPM), big.NewInt(amount))
		half = num.Add(num, den).Quo(num, new(big.Int).Mul(den, big.NewInt(2))).Int64()
	}
	return amount, amount - 2*half, half
}

// financialYear is the Indian April–March year of an outlet date, as 2627.
func financialYear(day string) (string, error) {
	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return "", err
	}
	start := d.Year()
	if d.Month() < time.April {
		start--
	}
	return fmt.Sprintf("%02d%02d", start%100, (start+1)%100), nil
}

func saleRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/invoice-profile/:id", PermTaxManage, h.putInvoiceProfile)
	s.GET("/invoice-profile", PermSaleRead, h.getInvoiceProfile)
	s.GET("/sales/availability", PermSaleRead, h.getSaleAvailability)
	s.POST("/sales/quote", PermSaleCreate, h.postSaleQuote)
	s.PUT("/sales/:id", PermSaleCreate, h.putSale)
	s.GET("/sales/:id", PermSaleRead, h.getSale)
	platform.ListRoute(s, "/sales", PermSaleRead, h.listSales)
	discountRoutes(s, h)
}

func (h *catalogueHandlers) putInvoiceProfile(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in invoiceProfileInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.LegalName, in.Address = strings.TrimSpace(in.LegalName), strings.TrimSpace(in.Address)
	if in.LegalName == "" || in.Address == "" || !invoicePrefixPattern.MatchString(in.InvoicePrefix) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "an invoice profile needs a legal name, an address and a prefix of 1 to 4 capital letters or digits")
		return
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{in.LegalName, in.Address, in.InvoicePrefix}, "\x1f")))
	digest := hex.EncodeToString(sum[:])
	var row invoiceProfile
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":invoice-profile:"+id.String()); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, RecordedBy string }
		if err := tx.Raw(`SELECT intent_sha256, recorded_by FROM pharmacy_outlet_invoice_profiles WHERE tenant_id=? AND outlet_id=? AND id=?`,
			p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 && (existing[0].IntentSHA256 != digest || existing[0].RecordedBy != p.Subject) {
			return errStockIdentity
		}
		if len(existing) == 0 {
			created = true
			if err := tx.Exec(`INSERT INTO pharmacy_outlet_invoice_profiles (id,tenant_id,outlet_id,legal_name,address,invoice_prefix,intent_sha256,recorded_by)
				VALUES (?,?,?,?,?,?,?,?)`, id, p.TenantID, outlet, in.LegalName, in.Address, in.InvoicePrefix, digest, p.Subject).Error; err != nil {
				return err
			}
		}
		return tx.Raw(`SELECT `+invoiceProfileColumns+` FROM pharmacy_outlet_invoice_profiles WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, id).Scan(&row).Error
	})
	switch {
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, row)
	default:
		respond.OK(c, row)
	}
}

func currentInvoiceProfile(tx *gorm.DB, tenant string, outlet uuid.UUID) (*invoiceProfile, error) {
	var rows []invoiceProfile
	if err := tx.Raw(`SELECT `+invoiceProfileColumns+` FROM pharmacy_outlet_invoice_profiles WHERE tenant_id=? AND outlet_id=? ORDER BY seq DESC LIMIT 1`,
		tenant, outlet).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (h *catalogueHandlers) getInvoiceProfile(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	var row *invoiceProfile
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) (err error) {
		row, err = currentInvoiceProfile(tx, p.TenantID, outlet)
		return err
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, gin.H{"profile": row})
}

type sellableBucket struct {
	LocationID uuid.UUID `gorm:"column:location_id"`
	BatchID    uuid.UUID `gorm:"column:batch_id"`
	LotNumber  string    `gorm:"column:lot_number"`
	ExpiresOn  string    `gorm:"column:expires_on"`
	MRPMinor   int64     `gorm:"column:mrp_minor"`
	OnHand     int64     `gorm:"column:on_hand"`
}

func sellableBuckets(tx *gorm.DB, tenant string, outlet, product uuid.UUID, today string) ([]sellableBucket, error) {
	var rows []sellableBucket
	err := tx.Raw(`SELECT location_id, batch_id, lot_number, to_char(expires_on,'YYYY-MM-DD') AS expires_on, mrp_minor, on_hand
		FROM pharmacy_sellable_buckets(?, ?, ?, ?::date)`, tenant, outlet, product, today).Scan(&rows).Error
	return rows, err
}

func (h *catalogueHandlers) getSaleAvailability(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := stockFilter(c, "product_id", true)
	if !ok {
		return
	}
	var units int64
	var nearest, class *string
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, product).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		buckets, err := sellableBuckets(tx, p.TenantID, outlet, product, today)
		if err != nil {
			return err
		}
		for i, b := range buckets {
			units += b.OnHand
			if i == 0 {
				nearest = &buckets[0].ExpiresOn
			}
		}
		// The class in force tells the counter whether a line needs a
		// prescription; the sale checks it again (B-2b).
		var declared []struct{ RegulatoryClass string }
		if err := tx.Raw(`SELECT regulatory_class FROM pharmacy_product_schedule_in_force(?, ?, ?)`, p.TenantID, outlet, product).Scan(&declared).Error; err != nil {
			return err
		}
		if len(declared) == 1 {
			class = &declared[0].RegulatoryClass
		}
		return nil
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, gin.H{"product_id": product, "sellable_units": units, "nearest_expiry": nearest, "regulatory_class": class})
}

// saleSeller is everything the invoice says about the outlet, checked
// before any stock is touched (plan preconditions 1–3).
type saleSeller struct {
	today, fy, gstin, state, currency string
	profile                           invoiceProfile
}

func saleSellerFor(tx *gorm.DB, tenant string, outlet uuid.UUID) (saleSeller, error) {
	var o pharmacyOutlet
	if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", tenant, outlet).First(&o).Error; err != nil {
		return saleSeller{}, err
	}
	today, err := outletToday(o.Timezone)
	if err != nil {
		return saleSeller{}, err
	}
	fy, err := financialYear(today)
	if err != nil {
		return saleSeller{}, err
	}
	country, ok := countryprofile.Lookup(o.CountryCode)
	if !ok {
		return saleSeller{}, errNotSellable{"this outlet's country has no sale rules"}
	}
	var licensed bool
	if err := tx.Raw(`SELECT pharmacy_outlet_retail_licence_in_force(?, ?, ?::date)`, tenant, outlet, today).Scan(&licensed).Error; err != nil {
		return saleSeller{}, err
	}
	if !licensed {
		return saleSeller{}, errNotSellable{"the outlet has no reviewed retail drug licence valid today"}
	}
	var reg []struct{ IdentifierKind, Identifier, JurisdictionCode string }
	if err := tx.Raw(`SELECT identifier_kind, identifier, jurisdiction_code FROM pharmacy_outlet_tax_registrations
		WHERE tenant_id=? AND outlet_id=? AND effective_from <= ?::date ORDER BY effective_from DESC, seq DESC LIMIT 1`, tenant, outlet, today).Scan(&reg).Error; err != nil {
		return saleSeller{}, err
	}
	if len(reg) == 0 || reg[0].IdentifierKind != "gstin" {
		return saleSeller{}, errNotSellable{"the outlet has no GSTIN in force today"}
	}
	// A union territory levies UTGST, not SGST; until that head is modelled
	// an invoice there would name the wrong tax, so it is not issued.
	scheme, ok := countryprofile.Tax(o.CountryCode)
	if !ok {
		return saleSeller{}, errNotSellable{"this outlet's country has no tax scheme"}
	}
	if j, ok := scheme.Jurisdiction(reg[0].JurisdictionCode); !ok || j.Kind != "state" {
		return saleSeller{}, errNotSellable{"counter invoices for a union territory (UTGST) are not supported yet"}
	}
	profile, err := currentInvoiceProfile(tx, tenant, outlet)
	if err != nil {
		return saleSeller{}, err
	}
	if profile == nil {
		return saleSeller{}, errNotSellable{"the outlet has no invoice details recorded"}
	}
	return saleSeller{today: today, fy: fy, gstin: reg[0].Identifier, state: reg[0].JurisdictionCode, currency: country.Currency, profile: *profile}, nil
}

// saleProduct is what a line needs of its product, read under FOR SHARE so
// a status change waits for the sale.
type saleProduct struct {
	ID       uuid.UUID
	Status   string
	PackSize int64
	Currency string
	SKU      string
	tax      productTax
	class    struct {
		DeclarationID   uuid.UUID
		RegulatoryClass string
		NDPS            bool
	}
}

func saleProducts(tx *gorm.DB, tenant string, outlet uuid.UUID, seller saleSeller, ids []uuid.UUID) (map[uuid.UUID]*saleProduct, error) {
	var rows []saleProduct
	if err := tx.Raw(`SELECT id, status, pack_size, currency, sku FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id IN ? ORDER BY id FOR SHARE`,
		tenant, outlet, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != len(ids) {
		return nil, gorm.ErrRecordNotFound
	}
	byID := make(map[uuid.UUID]*saleProduct, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
	}
	var taxes []struct {
		ProductID          uuid.UUID
		ClassificationCode string
		SupplyNature       string
		RatePPM            int64 `gorm:"column:rate_ppm"`
		EffectiveFrom      string
	}
	if err := tx.Raw(classificationInForce, tenant, outlet, ids, seller.today).Scan(&taxes).Error; err != nil {
		return nil, err
	}
	for _, t := range taxes {
		byID[t.ProductID].tax = productTax{t.ClassificationCode, t.SupplyNature, t.RatePPM, t.EffectiveFrom}
	}
	for _, id := range ids {
		pr := byID[id]
		var class []struct {
			DeclarationID   uuid.UUID
			RegulatoryClass string
			NDPS            bool
		}
		if err := tx.Raw(`SELECT declaration_id, regulatory_class, ndps FROM pharmacy_product_schedule_in_force(?, ?, ?)`, tenant, outlet, id).Scan(&class).Error; err != nil {
			return nil, err
		}
		switch {
		case pr.Status != "active":
			return nil, errNotSellable{pr.SKU + " is " + pr.Status}
		case pr.Currency != seller.currency:
			return nil, errNotSellable{pr.SKU + " is priced in " + pr.Currency + ", not the outlet's " + seller.currency}
		case pr.tax.SupplyNature == "":
			return nil, errNotSellable{pr.SKU + " has no tax classification in force"}
		case len(class) == 0:
			return nil, errNotSellable{pr.SKU + " has no approved schedule class"}
		case class[0].RegulatoryClass == "X" || class[0].NDPS:
			return nil, errNotSellable{pr.SKU + " is Schedule X or NDPS, which cannot be sold until their controls exist"}
		}
		pr.class = class[0]
	}
	return byID, nil
}

// allocate fills each line from sellable buckets, earliest expiry first
// (spec B2). Candidate buckets are locked first in one global order, with
// the key the quantity guard uses, so two tills serialise without deadlock
// and the balances read after are the ones the movements will see.
func allocate(tx *gorm.DB, tenant string, outlet uuid.UUID, today string, in []saleLineInput, products map[uuid.UUID]*saleProduct) ([]saleLine, error) {
	candidates := map[uuid.UUID][]sellableBucket{}
	keys := []string{}
	for _, l := range in {
		buckets, err := sellableBuckets(tx, tenant, outlet, l.ProductID, today)
		if err != nil {
			return nil, err
		}
		candidates[l.ProductID] = buckets
		for _, b := range buckets {
			// Canonical text, as the quantity guard hashes it.
			keys = append(keys, uuid.MustParse(tenant).String()+":"+outlet.String()+":"+b.LocationID.String()+":"+b.BatchID.String())
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := stockLock(tx, k); err != nil {
			return nil, err
		}
	}
	lines := []saleLine{}
	short := []string{}
	for _, l := range in {
		locked := map[[2]uuid.UUID]bool{}
		for _, b := range candidates[l.ProductID] {
			locked[[2]uuid.UUID{b.LocationID, b.BatchID}] = true
		}
		current, err := sellableBuckets(tx, tenant, outlet, l.ProductID, today)
		if err != nil {
			return nil, err
		}
		pr := products[l.ProductID]
		need := l.QuantityUnits
		for _, b := range current {
			if need == 0 {
				break
			}
			if !locked[[2]uuid.UUID{b.LocationID, b.BatchID}] {
				continue
			}
			take := min(need, b.OnHand)
			need -= take
			list, _, _ := priceLine(b.MRPMinor, pr.PackSize, take, pr.tax.SupplyNature, pr.tax.RatePPM)
			if list == 0 {
				return nil, errNotSellable{fmt.Sprintf("%s: %d units of a pack of %d price to nothing; sell more units", pr.SKU, take, pr.PackSize)}
			}
			discount, charged := discountLine(list, l.DiscountBps)
			if charged == 0 {
				return nil, errNotSellable{pr.SKU + ": the discount leaves nothing to charge"}
			}
			amount, taxable, half := taxInside(charged, pr.tax.SupplyNature, pr.tax.RatePPM)
			lines = append(lines, saleLine{LineNumber: len(lines) + 1, ProductID: l.ProductID, BatchID: b.BatchID, LocationID: b.LocationID,
				LotNumber: b.LotNumber, ExpiresOn: b.ExpiresOn, RegulatoryClass: pr.class.RegulatoryClass, ClassificationCode: pr.tax.ClassificationCode,
				SupplyNature: pr.tax.SupplyNature, RatePPM: pr.tax.RatePPM, MRPMinor: b.MRPMinor, PackSize: pr.PackSize, QuantityUnits: take,
				AmountMinor: amount, TaxableMinor: taxable, CGSTMinor: half, SGSTMinor: half, declaration: pr.class.DeclarationID,
				ListAmountMinor: list, DiscountBps: l.DiscountBps, DiscountMinor: discount})
		}
		if need > 0 {
			short = append(short, fmt.Sprintf("%s (%d more units needed)", pr.SKU, need))
		}
	}
	if len(short) > 0 {
		return nil, errNotSellable{"not enough sellable stock: " + strings.Join(short, ", ")}
	}
	if len(lines) > maxSaleLines {
		return nil, errNotSellable{fmt.Sprintf("the sale would print %d batch lines; sell it as more than one invoice", len(lines))}
	}
	return lines, nil
}

// priceSale runs every sale check and allocates the lines: what a quote
// shows and what a posted sale records come from one path.
func priceSale(tx *gorm.DB, tenant string, outlet uuid.UUID, in []saleLineInput) (saleSeller, []saleLine, error) {
	seller, err := saleSellerFor(tx, tenant, outlet)
	if err != nil {
		return seller, nil, err
	}
	ids := make([]uuid.UUID, len(in))
	for i, l := range in {
		ids[i] = l.ProductID
	}
	products, err := saleProducts(tx, tenant, outlet, seller, ids)
	if err != nil {
		return seller, nil, err
	}
	if err := checkCitations(tx, tenant, outlet, seller.today, in, products); err != nil {
		return seller, nil, err
	}
	if err := checkDiscounts(tx, tenant, outlet, in, products); err != nil {
		return seller, nil, err
	}
	lines, err := allocate(tx, tenant, outlet, seller.today, in, products)
	if err != nil {
		return seller, nil, err
	}
	cited := map[uuid.UUID]*saleCitation{}
	for _, l := range in {
		cited[l.ProductID] = l.Prescription
	}
	for i := range lines {
		if c := cited[lines[i].ProductID]; c != nil {
			id, n := c.PrescriptionID, c.LineNumber
			lines[i].PrescriptionID, lines[i].PrescriptionLine = &id, &n
		}
	}
	return seller, lines, nil
}

// checkCitations holds every G, H and H1 line to the prescription line it
// cites (B-2b decision 2, Rule 65(9)–(11A)), with the reason a cashier can act
// on. The cited lines are locked in one order first, as the sale guard locks
// them, so two tills dispensing one prescription serialise; the guard then
// checks the same limits again as the authority.
func checkCitations(tx *gorm.DB, tenant string, outlet uuid.UUID, today string, in []saleLineInput, products map[uuid.UUID]*saleProduct) error {
	cited := []saleLineInput{}
	for _, l := range in {
		pr := products[l.ProductID]
		switch {
		case pr.class.RegulatoryClass == "none" && l.Prescription != nil:
			return errNotSellable{pr.SKU + " is sold without a prescription; remove the prescription from its line"}
		case pr.class.RegulatoryClass != "none" && l.Prescription == nil:
			return errNotSellable{pr.SKU + " is Schedule " + pr.class.RegulatoryClass + " and needs a prescription"}
		case l.Prescription != nil:
			cited = append(cited, l)
		}
	}
	sort.Slice(cited, func(i, j int) bool {
		a, b := cited[i].Prescription, cited[j].Prescription
		if a.PrescriptionID != b.PrescriptionID {
			return a.PrescriptionID.String() < b.PrescriptionID.String()
		}
		return a.LineNumber < b.LineNumber
	})
	for i := 1; i < len(cited); i++ {
		if *cited[i].Prescription == *cited[i-1].Prescription {
			return errNotSellable{"each prescription line is dispensed on one sale line"}
		}
	}
	for _, l := range cited {
		c, pr := l.Prescription, products[l.ProductID]
		var rx []struct {
			ProductID          uuid.UUID
			DispensingsAllowed int
			QuantityUnits      int64
			PrescribedOn       string
			PrescriberAddress  *string
			PatientAddress     *string
		}
		if err := tx.Raw(`SELECT l.product_id, l.dispensings_allowed, l.quantity_units, to_char(p.prescribed_on,'YYYY-MM-DD') AS prescribed_on, p.prescriber_address, p.patient_address
			FROM pharmacy_prescription_lines l JOIN pharmacy_prescriptions p ON p.tenant_id=l.tenant_id AND p.outlet_id=l.outlet_id AND p.id=l.prescription_id
			WHERE l.tenant_id=? AND l.outlet_id=? AND l.prescription_id=? AND l.line_number=? FOR UPDATE OF l`, tenant, outlet, c.PrescriptionID, c.LineNumber).Scan(&rx).Error; err != nil {
			return err
		}
		if len(rx) == 0 {
			return errNotSellable{"the prescription line cited for " + pr.SKU + " is not recorded at this outlet"}
		}
		var left []struct {
			UnitsRemaining       int64
			DispensingsRemaining int
		}
		if err := tx.Raw(`SELECT units_remaining, dispensings_remaining FROM (`+prescriptionLinesQuery+` WHERE l.tenant_id=? AND l.outlet_id=? AND l.prescription_id=? AND l.line_number=?) q`,
			tenant, outlet, c.PrescriptionID, c.LineNumber).Scan(&left).Error; err != nil {
			return err
		}
		switch {
		case rx[0].ProductID != l.ProductID:
			return errNotSellable{"the prescription line is for another product; a prescribed medicine is not substituted"}
		case rx[0].PrescribedOn > today:
			return errNotSellable{"the prescription is dated after today"}
		case (pr.class.RegulatoryClass == "H" || pr.class.RegulatoryClass == "H1") && (rx[0].PrescriberAddress == nil || rx[0].PatientAddress == nil):
			return errNotSellable{"a Schedule H or H1 prescription needs the prescriber's and the patient's address; record the prescription again with them"}
		case left[0].DispensingsRemaining < 1:
			return errNotSellable{"the prescription for " + pr.SKU + " has been dispensed as often as the prescriber allowed"}
		case left[0].UnitsRemaining < l.QuantityUnits:
			return errNotSellable{fmt.Sprintf("the prescription allows %d more units of %s", left[0].UnitsRemaining, pr.SKU)}
		}
	}
	return nil
}

func saleTotals(lines []saleLine) (total, taxable, half int64) {
	for _, l := range lines {
		total, taxable, half = total+l.AmountMinor, taxable+l.TaxableMinor, half+l.CGSTMinor
	}
	return total, taxable, half
}

// postSaleQuote prices a basket as a sale would, without posting it, so
// the cashier knows what to collect. Stock can move before the sale is
// posted; the posted sale then refuses tenders that no longer match.
func (h *catalogueHandlers) postSaleQuote(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	var in saleQuoteInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !distinctProducts(in.Lines) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "each product appears on one line")
		return
	}
	if !mayDiscount(c, in.Lines) {
		return
	}
	var quote struct {
		Currency       string     `json:"currency"`
		TotalMinor     int64      `json:"total_minor"`
		TaxableMinor   int64      `json:"taxable_minor"`
		CGSTMinor      int64      `json:"cgst_minor"`
		SGSTMinor      int64      `json:"sgst_minor"`
		Lines          []saleLine `json:"lines"`
		CeilingChecked bool       `json:"nppa_ceiling_checked"`
	}
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		seller, lines, err := priceSale(tx, p.TenantID, outlet, in.Lines)
		if err != nil {
			return err
		}
		quote.Currency, quote.Lines = seller.currency, lines
		quote.TotalMinor, quote.TaxableMinor, quote.CGSTMinor = saleTotals(lines)
		quote.SGSTMinor = quote.CGSTMinor
		return nil
	})
	var refusal errNotSellable
	switch {
	case errors.As(err, &refusal):
		respond.Error(c, http.StatusConflict, "not_sellable", refusal.reason)
	case err != nil:
		stockError(c, err)
	default:
		respond.OK(c, quote)
	}
}

func (h *catalogueHandlers) putSale(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in saleInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !distinctProducts(in.Lines) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "each product appears on one line")
		return
	}
	if !in.validTenders() {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "at most one cash tender, with the amount handed over; UPI and card need a reference")
		return
	}
	if !in.Buyer.valid() {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a buyer has a name of up to 200 characters, a phone of 6 to 15 digits, or both")
		return
	}
	if !mayDiscount(c, in.Lines) {
		return
	}
	if in.DiscountReason != nil && !prescriptionText(*in.DiscountReason, 200) || in.DiscountReason != nil && len([]rune(*in.DiscountReason)) < 5 ||
		discounted(in.Lines) != (in.DiscountReason != nil) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a discounted sale gives a reason of 5 to 200 characters, and only a discounted sale gives one")
		return
	}
	var buyerName, buyerPhone *string
	if in.Buyer != nil {
		buyerName, buyerPhone = in.Buyer.Name, in.Buyer.Phone
	}
	digest := in.digest()
	var result saleRecord
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":"+outlet.String()+":sale:"+id.String()); err != nil {
			return err
		}
		var existing []struct{ IntentSHA256, ActorSubject string }
		if err := tx.Raw(`SELECT intent_sha256, actor_subject FROM pharmacy_sales WHERE tenant_id=? AND outlet_id=? AND id=?`,
			p.TenantID, outlet, id).Scan(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if existing[0].IntentSHA256 != digest || existing[0].ActorSubject != p.Subject {
				return errStockIdentity
			}
			return loadSale(tx, p.TenantID, outlet, id, &result)
		}
		created = true
		seller, lines, err := priceSale(tx, p.TenantID, outlet, in.Lines)
		if err != nil {
			return err
		}
		total, taxable, half := saleTotals(lines)
		var paid int64
		for _, t := range in.Tenders {
			paid += t.AmountMinor
		}
		if paid != total {
			return errNotSellable{fmt.Sprintf("the tenders come to %d but the invoice is %d (minor units)", paid, total)}
		}
		// The series row is created once per year and stepped under its row
		// lock; a refused sale rolls the step back, leaving no gap.
		if err := tx.Exec(`INSERT INTO pharmacy_invoice_series (tenant_id,outlet_id,financial_year) VALUES (?,?,?) ON CONFLICT DO NOTHING`,
			p.TenantID, outlet, seller.fy).Error; err != nil {
			return err
		}
		var number int64
		if err := tx.Raw(`UPDATE pharmacy_invoice_series SET last_number=last_number+1 WHERE tenant_id=? AND outlet_id=? AND financial_year=? RETURNING last_number`,
			p.TenantID, outlet, seller.fy).Scan(&number).Error; err != nil {
			return err
		}
		invoice := seller.profile.InvoicePrefix + "/" + seller.fy + "/" + strconv.FormatInt(number, 10)
		if len(invoice) > 16 {
			return errNotSellable{"this year's invoice series is full; record a shorter invoice prefix"}
		}
		if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,
			seller_legal_name,seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject,buyer_name,buyer_phone,discount_reason)
			VALUES (?,?,?,?,?,?,?::date,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, p.TenantID, outlet, seller.fy, number, invoice, seller.today, seller.profile.ID,
			seller.profile.LegalName, seller.profile.Address, seller.gstin, seller.state, seller.currency, total, taxable, half, half, digest, p.Subject, buyerName, buyerPhone, in.DiscountReason).Error; err != nil {
			return err
		}
		for _, l := range lines {
			if err := tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,
				regulatory_class,schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,
				prescription_id,prescription_line,discount_bps,discount_minor)
				VALUES (?,?,?,?,?,?,?,?,?::date,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, outlet, id, l.LineNumber, l.ProductID, l.BatchID, l.LocationID, l.LotNumber, l.ExpiresOn,
				l.RegulatoryClass, l.declaration, l.ClassificationCode, l.SupplyNature, l.RatePPM, l.MRPMinor, l.PackSize, l.QuantityUnits, l.AmountMinor,
				l.TaxableMinor, l.CGSTMinor, l.SGSTMinor, l.PrescriptionID, l.PrescriptionLine, l.DiscountBps, l.DiscountMinor).Error; err != nil {
				return err
			}
			movement := stockMovement{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ActorSubject: p.Subject,
				MovementFields: movementInput{LocationID: l.LocationID, BatchID: l.BatchID, Kind: "sale", QuantityUnits: -l.QuantityUnits,
					Reason: "sale " + invoice, SourceReference: "sale:" + id.String() + ":" + strconv.Itoa(l.LineNumber)}}
			if err := tx.Create(&movement).Error; err != nil {
				return err
			}
			data, err := json.Marshal(pharmacycontract.StockMovementRecordedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(),
				MovementID: movement.ID.String(), LocationID: l.LocationID.String(), BatchID: l.BatchID.String()})
			if err != nil {
				return err
			}
			if err := h.bus.Publish(tx, pharmacycontract.SubjectStockMovementRecorded, events.Event{Type: "StockMovementRecorded", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
				return err
			}
		}
		for i, t := range in.Tenders {
			var change *int64
			if t.TenderedMinor != nil {
				v := *t.TenderedMinor - t.AmountMinor
				change = &v
			}
			if err := tx.Exec(`INSERT INTO pharmacy_sale_tenders (tenant_id,outlet_id,sale_id,tender_number,kind,amount_minor,tendered_minor,change_minor,reference)
				VALUES (?,?,?,?,?,?,?,?,?)`, p.TenantID, outlet, id, i+1, t.Kind, t.AmountMinor, t.TenderedMinor, change, t.Reference).Error; err != nil {
				return err
			}
		}
		posted := pharmacycontract.SalePostedData{PharmacyID: currentPharmacy(c.Request.Context()).String(), OutletID: outlet.String(), SaleID: id.String(),
			InvoiceNumber: invoice, Currency: seller.currency, TotalMinor: total}
		for _, l := range lines {
			posted.Lines = append(posted.Lines, pharmacycontract.SalePostedLine{ProductID: l.ProductID.String(), BatchID: l.BatchID.String(), QuantityUnits: l.QuantityUnits})
		}
		data, err := json.Marshal(posted)
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, pharmacycontract.SubjectSalePosted, events.Event{Type: "SalePosted", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return loadSale(tx, p.TenantID, outlet, id, &result)
	})
	var refusal errNotSellable
	switch {
	case errors.As(err, &refusal):
		respond.Error(c, http.StatusConflict, "not_sellable", refusal.reason)
	case prescriptionLimitError(err):
		respond.Error(c, http.StatusConflict, "not_sellable", "the prescription no longer allows this sale; price the basket again")
	case violated(err, "pharmacy_sale_line_discount_ceiling"):
		respond.Error(c, http.StatusConflict, "not_sellable", "the outlet's discount ceiling changed; price the basket again")
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, result)
	default:
		respond.OK(c, result)
	}
}

func loadSale(tx *gorm.DB, tenant string, outlet, id uuid.UUID, out *saleRecord) error {
	var rows []saleRecord
	if err := tx.Raw(`SELECT `+saleColumns+` FROM pharmacy_sales WHERE tenant_id=? AND outlet_id=? AND id=?`, tenant, outlet, id).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return gorm.ErrRecordNotFound
	}
	*out = rows[0]
	if err := tx.Raw(`SELECT line_number,product_id,batch_id,location_id,lot_number,to_char(expires_on,'YYYY-MM-DD') AS expires_on,regulatory_class,
		classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,prescription_id,prescription_line,list_amount_minor,discount_bps,discount_minor
		FROM pharmacy_sale_lines WHERE tenant_id=? AND outlet_id=? AND sale_id=? ORDER BY line_number`, tenant, outlet, id).Scan(&out.Lines).Error; err != nil {
		return err
	}
	if err := tx.Raw(`SELECT tender_number,kind,amount_minor,tendered_minor,change_minor,reference FROM pharmacy_sale_tenders
		WHERE tenant_id=? AND outlet_id=? AND sale_id=? ORDER BY tender_number`, tenant, outlet, id).Scan(&out.Tenders).Error; err != nil {
		return err
	}
	// Decision 2: the class on the invoice is the outlet's declaration, and
	// NPPA ceiling prices (#1371) are not checked; the invoice says both.
	for _, l := range out.Lines {
		out.DiscountMinor += l.DiscountMinor
	}
	out.ClassOrigin = "declared_by_outlet"
	out.CeilingChecked = false
	return nil
}

func (h *catalogueHandlers) getSale(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var result saleRecord
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return loadSale(tx, p.TenantID, outlet, id, &result)
	})
	if err != nil {
		stockError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *catalogueHandlers) listSales(c *gin.Context, params pagination.Params) ([]saleRecord, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []saleRecord
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, params).Select(saleColumns).Where("tenant_id=? AND outlet_id=?", p.TenantID, outlet).Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

// prescriptionLimitError is the sale guard refusing a line its handler check
// passed, so a refusal the database makes still reaches the cashier as one.
func prescriptionLimitError(err error) bool {
	return violated(err, "pharmacy_sale_line_prescription_limit")
}

// violated reports whether err is the named constraint refusing a write.
func violated(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.ConstraintName == constraint
}

// checkDiscounts holds every discount to the outlet's ceiling in force, with
// a reason the cashier can act on; the sale-line guard checks it again.
func checkDiscounts(tx *gorm.DB, tenant string, outlet uuid.UUID, in []saleLineInput, products map[uuid.UUID]*saleProduct) error {
	var ceiling int64
	if err := tx.Raw(`SELECT pharmacy_outlet_discount_ceiling_in_force(?, ?)`, tenant, outlet).Scan(&ceiling).Error; err != nil {
		return err
	}
	for _, l := range in {
		if l.DiscountBps > ceiling {
			return errNotSellable{fmt.Sprintf("the discount on %s is above this outlet's ceiling of %s%%", products[l.ProductID].SKU, percent(ceiling))}
		}
	}
	return nil
}

// percent writes basis points as a percentage without trailing zeros.
func percent(bps int64) string {
	whole, frac := bps/100, bps%100
	switch {
	case frac == 0:
		return strconv.FormatInt(whole, 10)
	case frac%10 == 0:
		return fmt.Sprintf("%d.%d", whole, frac/10)
	default:
		return fmt.Sprintf("%d.%02d", whole, frac)
	}
}

// discounted reports whether any line asks for a discount.
func discounted(lines []saleLineInput) bool {
	for _, l := range lines {
		if l.DiscountBps > 0 {
			return true
		}
	}
	return false
}

// mayDiscount is the permission a discount needs beyond the route's own
// (decision 3): checked before any pricing, so a quote cannot show a
// discount its caller cannot give. A missing permission set fails closed.
func mayDiscount(c *gin.Context, lines []saleLineInput) bool {
	if !discounted(lines) {
		return true
	}
	set, ok := authz.PermissionsFrom(c)
	if !ok || !set.Has(PermSaleDiscount) {
		respond.Forbidden(c, "a discount needs the pharmacy.sale.discount permission")
		return false
	}
	return true
}
