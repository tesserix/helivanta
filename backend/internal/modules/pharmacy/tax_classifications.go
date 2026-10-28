package pharmacy

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tax"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var ratePercentPattern = regexp.MustCompile(`^(100(\.0{1,2})?|[0-9]{1,2}(\.[0-9]{1,2})?)$`)

var errTaxClassification = errors.New("invalid tax classification")

// ratePPM turns a percentage typed to 0.01% ("5", "0.25") into parts per
// million without passing through a float.
func ratePPM(percent string) (int64, bool) {
	if !ratePercentPattern.MatchString(percent) {
		return 0, false
	}
	whole, frac, _ := strings.Cut(percent, ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}
	f, err := strconv.ParseInt((frac + "00")[:2], 10, 64)
	if err != nil {
		return 0, false
	}
	return w*10000 + f*100, true
}

// productTax is the classification in force on the outlet's date.
type productTax struct {
	ClassificationCode string `json:"classification_code"`
	SupplyNature       string `json:"supply_nature"`
	RatePPM            int64  `json:"rate_ppm"`
	EffectiveFrom      string `json:"effective_from"`
}

type taxClassification struct {
	ID                 uuid.UUID `json:"id"`
	TenantID           uuid.UUID `json:"-"`
	OutletID           uuid.UUID `json:"outlet_id"`
	ProductID          uuid.UUID `json:"product_id"`
	ClassificationCode string    `json:"classification_code"`
	SupplyNature       string    `json:"supply_nature"`
	RatePPM            int64     `json:"rate_ppm" gorm:"column:rate_ppm"`
	ExemptionBasis     *string   `json:"exemption_basis"`
	EffectiveFrom      string    `json:"effective_from"`
	Reason             *string   `json:"reason"`
	RecordedBy         string    `json:"recorded_by"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	InForce            bool      `json:"in_force" gorm:"->;-:migration"`
	Replaced           bool      `json:"replaced" gorm:"->;-:migration"`
}

func (taxClassification) TableName() string                 { return "pharmacy_product_tax_classifications" }
func (r taxClassification) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const classificationColumns = `r.id,r.tenant_id,r.outlet_id,r.product_id,r.classification_code,r.supply_nature,r.rate_ppm,r.exemption_basis,
 to_char(r.effective_from,'YYYY-MM-DD') AS effective_from,r.reason,r.recorded_by,r.created_at,
 r.id = (SELECT f.id FROM pharmacy_product_tax_classifications f WHERE f.tenant_id=r.tenant_id AND f.outlet_id=r.outlet_id
  AND f.product_id=r.product_id AND f.effective_from <= ?::date ORDER BY f.effective_from DESC,f.seq DESC LIMIT 1) AS in_force,
 EXISTS (SELECT 1 FROM pharmacy_product_tax_classifications l WHERE l.tenant_id=r.tenant_id AND l.outlet_id=r.outlet_id
  AND l.product_id=r.product_id AND l.effective_from=r.effective_from AND l.seq>r.seq) AS replaced`

// classificationInForce is the row a product is taxed by on a date, the
// same rule the history flags use.
const classificationInForce = `SELECT DISTINCT ON (product_id) product_id,classification_code,supply_nature,rate_ppm,
 to_char(effective_from,'YYYY-MM-DD') AS effective_from FROM pharmacy_product_tax_classifications
 WHERE tenant_id=? AND outlet_id=? AND product_id IN ? AND effective_from <= ?::date
 ORDER BY product_id,effective_from DESC,seq DESC`

func taxClassificationRoutes(s *platform.Router, h *catalogueHandlers) {
	platform.ListRoute(s, "/catalogue/products/:id/tax-classifications", PermCatalogueRead, h.listTaxClassifications)
	s.POST("/catalogue/products/:id/tax-classifications", PermTaxManage, h.postTaxClassification)
}

// outletLocalDate is the outlet's date for in-force lookups; a country
// without a scheme still has a calendar, so this never refuses.
func outletLocalDate(tx *gorm.DB, tenant string, outlet uuid.UUID) (string, error) {
	var o pharmacyOutlet
	if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", tenant, outlet).First(&o).Error; err != nil {
		return "", err
	}
	return outletToday(o.Timezone)
}

// attachProductTax fills one product's classification in force today.
func attachProductTax(tx *gorm.DB, tenant string, outlet uuid.UUID, row *catalogueProduct) error {
	rows := []catalogueProduct{*row}
	if err := attachTax(tx, tenant, outlet, rows); err != nil {
		return err
	}
	*row = rows[0]
	return nil
}

// attachTax fills each product's classification in force today.
func attachTax(tx *gorm.DB, tenant string, outlet uuid.UUID, rows []catalogueProduct) error {
	if len(rows) == 0 {
		return nil
	}
	today, err := outletLocalDate(tx, tenant, outlet)
	if err != nil {
		return err
	}
	return attachTaxOn(tx, tenant, outlet, today, rows)
}

// attachTaxOn fills each product's classification in force on today, the
// outlet's date already read by the caller: a list filtered on that date
// must badge its rows on the same one (#576).
func attachTaxOn(tx *gorm.DB, tenant string, outlet uuid.UUID, today string, rows []catalogueProduct) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	var found []struct {
		ProductID          uuid.UUID
		ClassificationCode string
		SupplyNature       string
		RatePPM            int64 `gorm:"column:rate_ppm"`
		EffectiveFrom      string
	}
	if err := tx.Raw(classificationInForce, tenant, outlet, ids, today).Scan(&found).Error; err != nil {
		return err
	}
	byProduct := make(map[uuid.UUID]productTax, len(found))
	for _, f := range found {
		byProduct[f.ProductID] = productTax{f.ClassificationCode, f.SupplyNature, f.RatePPM, f.EffectiveFrom}
	}
	for i := range rows {
		if t, ok := byProduct[rows[i].ID]; ok {
			rows[i].Tax = &t
		}
	}
	return nil
}

func (h *catalogueHandlers) listTaxClassifications(c *gin.Context, params pagination.Params) ([]taxClassification, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := catalogueID(c)
	if !ok {
		return nil, nil
	}
	var rows []taxClassification
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Select("id").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&catalogueProduct{}).Error; err != nil {
			return err
		}
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx.Table("pharmacy_product_tax_classifications AS r"), params)
		return q.Select(classificationColumns, today).Where("r.tenant_id=? AND r.outlet_id=? AND r.product_id=?", p.TenantID, outlet, id).Find(&rows).Error
	})
	if err != nil {
		taxError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) postTaxClassification(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in taxClassificationInput
	if !decodeCatalogue(c, &in) {
		return
	}
	reason, okReason := optionalReason(in.Reason)
	basis, okBasis := optionalReason(in.ExemptionBasis)
	rate, okRate := ratePPM(in.RatePercent)
	nature := tax.Nature(in.SupplyNature)
	// An exemption must give its basis and nothing else may carry one.
	if !okReason || !okBasis || !okRate || !tax.ValidRate(nature, rate) || (nature == tax.Exempt) != (basis != nil) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "invalid rate, supply nature, exemption basis or reason")
		return
	}
	var row taxClassification
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var product catalogueProduct
		if err := tx.Select("id,status").Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).First(&product).Error; err != nil {
			return err
		}
		if product.Status == "withdrawn" {
			return errCatalogueWithdrawn
		}
		scheme, today, err := outletTaxContext(tx, p.TenantID, currentPharmacy(c.Request.Context()), outlet)
		if err != nil {
			return err
		}
		if err := checkEffectiveFrom(in.EffectiveFrom, today); err != nil {
			return err
		}
		code := strings.ToUpper(strings.TrimSpace(in.ClassificationCode))
		if !regexp.MustCompile(scheme.ClassificationPattern).MatchString(code) {
			return errTaxClassification
		}
		row = taxClassification{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.MustParse(p.TenantID), OutletID: outlet, ProductID: id,
			ClassificationCode: code, SupplyNature: in.SupplyNature, RatePPM: rate, ExemptionBasis: basis,
			EffectiveFrom: in.EffectiveFrom, Reason: reason, RecordedBy: p.Subject}
		if err := tx.Omit("CreatedAt", "InForce", "Replaced").Create(&row).Error; err != nil {
			return err
		}
		return tx.Table("pharmacy_product_tax_classifications AS r").Select(classificationColumns, today).Where("r.tenant_id=? AND r.outlet_id=? AND r.id=?", p.TenantID, outlet, row.ID).Take(&row).Error
	})
	if errors.Is(err, errTaxClassification) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "classification code does not match the country's scheme")
		return
	}
	if err != nil {
		taxError(c, err)
		return
	}
	respond.Created(c, row)
}
