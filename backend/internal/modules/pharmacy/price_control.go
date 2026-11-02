package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

// NPPA ceiling prices (#1371): whether a product is a price-controlled
// scheduled formulation and its notified ceiling per unit of issue, before
// GST, declared by one person and reviewed by another.

type priceDeclaration struct {
	ID             uuid.UUID  `gorm:"column:id" json:"id"`
	ProductID      uuid.UUID  `gorm:"column:product_id" json:"product_id"`
	Controlled     bool       `gorm:"column:controlled" json:"controlled"`
	CeilingMinor   *int64     `gorm:"column:ceiling_minor" json:"ceiling_minor"`
	OrderReference *string    `gorm:"column:order_reference" json:"order_reference"`
	OrderDate      *string    `gorm:"column:order_date" json:"order_date"`
	EffectiveFrom  string     `gorm:"column:effective_from" json:"effective_from"`
	Source         string     `gorm:"column:source" json:"source"`
	DeclaredBy     string     `gorm:"column:declared_by" json:"declared_by"`
	CreatedAt      time.Time  `gorm:"column:created_at" json:"created_at"`
	Decision       *string    `gorm:"column:decision" json:"decision"`
	Reason         *string    `gorm:"column:reason" json:"decision_reason"`
	ReviewedBy     *string    `gorm:"column:reviewed_by" json:"reviewed_by"`
	ReviewedAt     *time.Time `gorm:"column:reviewed_at" json:"reviewed_at"`
}

const priceDeclarationQuery = `SELECT d.id, d.product_id, d.controlled, d.ceiling_minor, d.order_reference, to_char(d.order_date,'YYYY-MM-DD') AS order_date,
	to_char(d.effective_from,'YYYY-MM-DD') AS effective_from, d.source, d.declared_by, d.created_at,
	r.decision, r.reason, r.reviewed_by, r.created_at AS reviewed_at
	FROM pharmacy_product_price_declarations d LEFT JOIN pharmacy_product_price_reviews r
	ON r.tenant_id = d.tenant_id AND r.outlet_id = d.outlet_id AND r.declaration_id = d.id`

// priceInForce is pharmacy_product_price_in_force: the one rule the counter,
// the record and the sale-line guard all read.
type priceInForce struct {
	State         string     `gorm:"column:state"`
	DeclarationID *uuid.UUID `gorm:"column:declaration_id"`
	Controlled    *bool      `gorm:"column:controlled"`
	CeilingMinor  *int64     `gorm:"column:ceiling_minor"`
}

func readPriceInForce(tx *gorm.DB, tenant string, outlet, product uuid.UUID, day string) (priceInForce, error) {
	var row priceInForce
	err := tx.Raw(`SELECT state, declaration_id, controlled, ceiling_minor FROM pharmacy_product_price_in_force(?, ?, ?, ?::date)`,
		tenant, outlet, product, day).Scan(&row).Error
	return row, err
}

func priceControlRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/catalogue/products/:id/price-control/:declaration", PermPriceControlDeclare, h.putPriceDeclaration)
	s.POST("/catalogue/products/:id/price-control/:declaration/review", PermPriceControlReview, h.reviewPriceDeclaration)
	s.GET("/catalogue/products/:id/price-control", PermCatalogueRead, h.getPriceControl)
}

func priceControlError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.ConstraintName == "pharmacy_price_four_eyes":
		respond.Error(c, http.StatusConflict, "four_eyes", "a price control is reviewed by someone other than who declared it")
	case errors.Is(err, errDeclarationReviewed):
		respond.Conflict(c, "this declaration has already been reviewed")
	case errors.Is(err, errStockIdentity):
		respond.Conflict(c, "this declaration id was already used with different details")
	default:
		catalogueError(c, err)
	}
}

// validPriceInput says what a declaration of either kind must carry; the
// table's form constraint says the same.
func validPriceInput(in *priceControlInput) string {
	in.Source = strings.TrimSpace(in.Source)
	if in.OrderReference != nil {
		*in.OrderReference = strings.TrimSpace(*in.OrderReference)
	}
	if _, err := time.Parse(time.DateOnly, in.EffectiveFrom); err != nil {
		return "effective_from is a date"
	}
	if in.Source == "" {
		return "a declaration names its source"
	}
	if in.PriceControl == "not_controlled" {
		if in.CeilingMinor != nil || in.OrderReference != nil || in.OrderDate != nil {
			return "a product not under price control carries no ceiling or order"
		}
		return ""
	}
	if in.CeilingMinor == nil || in.OrderReference == nil || *in.OrderReference == "" || in.OrderDate == nil {
		return "a price-controlled product needs its ceiling, the NPPA order and the order's date"
	}
	order, err := time.Parse(time.DateOnly, *in.OrderDate)
	if err != nil {
		return "order_date is a date"
	}
	if order.Format(time.DateOnly) > in.EffectiveFrom {
		return "a ceiling takes effect on or after its order's date"
	}
	return ""
}

func optionalText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *catalogueHandlers) putPriceDeclaration(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	id, ok := declarationID(c)
	if !ok {
		return
	}
	var in priceControlInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if why := validPriceInput(&in); why != "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", why)
		return
	}
	ceiling := ""
	if in.CeilingMinor != nil {
		ceiling = strconv.FormatInt(*in.CeilingMinor, 10)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{product.String(), in.PriceControl, ceiling, optionalText(in.OrderReference),
		optionalText(in.OrderDate), in.EffectiveFrom, in.Source}, "\x1f")))
	digest := hex.EncodeToString(sum[:])
	var result priceDeclaration
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_catalogue_products WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, product).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		insert := tx.Exec(`INSERT INTO pharmacy_product_price_declarations (id, tenant_id, outlet_id, product_id, controlled, ceiling_minor, order_reference, order_date,
			effective_from, source, intent_sha256, declared_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?::date, ?::date, ?, ?, ?) ON CONFLICT DO NOTHING`,
			id, p.TenantID, outlet, product, in.PriceControl == "controlled", in.CeilingMinor, in.OrderReference, in.OrderDate, in.EffectiveFrom, in.Source, digest, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		created = insert.RowsAffected == 1
		if !created {
			var won []struct{ IntentSHA256, DeclaredBy string }
			if err := tx.Raw(`SELECT intent_sha256, declared_by FROM pharmacy_product_price_declarations WHERE tenant_id=? AND outlet_id=? AND id=?`,
				p.TenantID, outlet, id).Scan(&won).Error; err != nil {
				return err
			}
			if len(won) != 1 || won[0].IntentSHA256 != digest || won[0].DeclaredBy != p.Subject {
				return errStockIdentity
			}
		}
		return tx.Raw(priceDeclarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	switch {
	case err != nil:
		priceControlError(c, err)
	case created:
		respond.Created(c, result)
	default:
		respond.OK(c, result)
	}
}

func (h *catalogueHandlers) reviewPriceDeclaration(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	id, ok := declarationID(c)
	if !ok {
		return
	}
	var in licenceReviewInput
	if !decodeCatalogue(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a review needs a reason")
		return
	}
	var result priceDeclaration
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_product_price_declarations WHERE tenant_id=? AND outlet_id=? AND product_id=? AND id=?`,
			p.TenantID, outlet, product, id).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		insert := tx.Exec(`INSERT INTO pharmacy_product_price_reviews (tenant_id, outlet_id, declaration_id, decision, reason, reviewed_by)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, p.TenantID, outlet, id, in.Decision, in.Reason, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return errDeclarationReviewed
		}
		return tx.Raw(priceDeclarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		priceControlError(c, err)
		return
	}
	respond.OK(c, result)
}

type priceControlState struct {
	// State is undeclared (sold under its MRP alone), blocked (not sold) or
	// in_force, on the outlet's date today.
	State   string             `json:"state"`
	Today   string             `json:"today"`
	InForce *priceDeclaration  `json:"in_force"`
	Reason  string             `json:"reason"`
	History []priceDeclaration `json:"history"`
}

func (h *catalogueHandlers) getPriceControl(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	product, ok := scheduleProduct(c)
	if !ok {
		return
	}
	out := priceControlState{History: []priceDeclaration{}}
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
		out.Today = today
		if err := tx.Raw(priceDeclarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.product_id=? ORDER BY d.seq DESC LIMIT ?`,
			p.TenantID, outlet, product, historyLimit).Scan(&out.History).Error; err != nil {
			return err
		}
		now, err := readPriceInForce(tx, p.TenantID, outlet, product, today)
		if err != nil {
			return err
		}
		out.State = now.State
		switch {
		case now.State == "undeclared":
			out.Reason = "no price control has been declared; it sells under its MRP alone"
		case now.State == "in_force":
			// Read by id: the one in force can be older than the history shown.
			var row priceDeclaration
			if err := tx.Raw(priceDeclarationQuery+` WHERE d.tenant_id=? AND d.outlet_id=? AND d.id=?`, p.TenantID, outlet, *now.DeclarationID).Scan(&row).Error; err != nil {
				return err
			}
			out.InForce = &row
		case out.History[0].Decision == nil:
			out.Reason = "the latest declaration awaits review by a second person"
		case *out.History[0].Decision == "rejected":
			out.Reason = "the latest declaration was rejected"
		default:
			out.Reason = "no approved declaration has taken effect yet"
		}
		return nil
	})
	if err != nil {
		priceControlError(c, err)
		return
	}
	respond.OK(c, out)
}

// withinCeiling lowers a line's tax-inclusive list amount to the most whose
// value before GST, as the counter splits it, is within the ceiling for the
// line's units, both before and after its discount (DPCO para 26: the lower
// of label and price list). It reports whether the ceiling, not the MRP,
// set the amount. Searching the counter's own split keeps the check and the
// invoice from disagreeing by a rounding paisa.
func withinCeiling(list, ceiling, bps int64, nature string, ratePPM int64) (int64, bool) {
	fits := func(amount int64) bool {
		_, charged := discountLine(amount, bps)
		_, before, _ := taxInside(amount, nature, ratePPM)
		_, after, _ := taxInside(charged, nature, ratePPM)
		return before <= ceiling && after <= ceiling
	}
	if fits(list) {
		return list, false
	}
	// The value before GST is the amount less at most one paisa of rounding
	// either way, so nothing three paise past ceiling × (1 + rate) fits.
	guess := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(ceiling), big.NewInt(1_000_000+ratePPM)), big.NewInt(1_000_000)).Int64()
	amount := min(list, guess+3)
	for amount > 0 && !fits(amount) {
		amount--
	}
	return amount, true
}

// ceilingChecked: every line sold under a price-control declaration, so the
// invoice can say the NPPA ceiling was checked.
func ceilingChecked(lines []saleLine) bool {
	for _, l := range lines {
		if l.PriceDeclarationID == nil {
			return false
		}
	}
	return len(lines) > 0
}
