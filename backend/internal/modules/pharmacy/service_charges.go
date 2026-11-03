package pharmacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
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

// Service charges on pharmacy bills (#1388): an outlet's delivery or handling
// charge, versioned, each version reviewed by a second person. How a charge is
// taxed is the tenant accountant's recorded decision, never the software's.

var (
	chargeCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,19}$`)
	sacCodePattern    = regexp.MustCompile(`^99[0-9]{4}$`)
	errChargeCode     = errors.New("a service charge's code is fixed")
)

type serviceChargeVersion struct {
	ID             uuid.UUID  `gorm:"column:id" json:"id"`
	ChargeID       uuid.UUID  `gorm:"column:charge_id" json:"charge_id"`
	Code           string     `gorm:"column:code" json:"code"`
	Name           string     `gorm:"column:name" json:"name"`
	Channel        string     `gorm:"column:channel" json:"channel"`
	AmountMinor    int64      `gorm:"column:amount_minor" json:"amount_minor"`
	Status         string     `gorm:"column:status" json:"status"`
	TaxTreatment   string     `gorm:"column:tax_treatment" json:"tax_treatment"`
	SACCode        *string    `gorm:"column:sac_code" json:"sac_code"`
	SupplyNature   *string    `gorm:"column:supply_nature" json:"supply_nature"`
	RatePPM        *int64     `gorm:"column:rate_ppm" json:"rate_ppm"`
	ExemptionBasis *string    `gorm:"column:exemption_basis" json:"exemption_basis"`
	TaxBasis       string     `gorm:"column:tax_basis" json:"tax_basis"`
	EffectiveFrom  string     `gorm:"column:effective_from" json:"effective_from"`
	DeclaredBy     string     `gorm:"column:declared_by" json:"declared_by"`
	CreatedAt      time.Time  `gorm:"column:created_at" json:"created_at"`
	Decision       *string    `gorm:"column:decision" json:"decision"`
	Reason         *string    `gorm:"column:reason" json:"decision_reason"`
	ReviewedBy     *string    `gorm:"column:reviewed_by" json:"reviewed_by"`
	ReviewedAt     *time.Time `gorm:"column:reviewed_at" json:"reviewed_at"`
}

const serviceChargeVersionQuery = `SELECT v.id, v.charge_id, c.code, v.name, v.channel, v.amount_minor,
	CASE WHEN v.available THEN 'available' ELSE 'withdrawn' END AS status, v.tax_treatment, v.sac_code, v.supply_nature, v.rate_ppm,
	v.exemption_basis, v.tax_basis, to_char(v.effective_from,'YYYY-MM-DD') AS effective_from, v.declared_by, v.created_at,
	r.decision, r.reason, r.reviewed_by, r.created_at AS reviewed_at
	FROM pharmacy_service_charge_versions v
	JOIN pharmacy_service_charges c ON c.tenant_id = v.tenant_id AND c.outlet_id = v.outlet_id AND c.id = v.charge_id
	LEFT JOIN pharmacy_service_charge_reviews r ON r.tenant_id = v.tenant_id AND r.outlet_id = v.outlet_id AND r.version_id = v.id`

// serviceCharge is a charge as the counter and its manager see it today: the
// version billed (nil when none is in force) and the latest version, which
// may await review.
type serviceCharge struct {
	ID      uuid.UUID              `json:"id"`
	Code    string                 `json:"code"`
	Today   string                 `json:"today"`
	InForce *serviceChargeVersion  `json:"in_force"`
	Latest  *serviceChargeVersion  `json:"latest"`
	History []serviceChargeVersion `json:"history,omitempty"`
}

func serviceChargeRoutes(s *platform.Router, h *catalogueHandlers) {
	s.PUT("/service-charges/:charge/versions/:version", PermServiceChargeDeclare, h.putServiceChargeVersion)
	s.POST("/service-charges/:charge/versions/:version/review", PermServiceChargeReview, h.reviewServiceChargeVersion)
	// Unpaginated: the schema holds an outlet to 50 charges.
	s.GET("/service-charges", PermSaleRead, h.listServiceCharges)
	s.GET("/service-charges/:charge", PermSaleRead, h.getServiceCharge)
}

func serviceChargeIDs(c *gin.Context, withVersion bool) (charge, version uuid.UUID, ok bool) {
	parse := func(name string) (uuid.UUID, bool) {
		id, err := uuid.Parse(c.Param(name))
		return id, err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122
	}
	if charge, ok = parse("charge"); !ok {
		respond.NotFound(c, "service charge")
		return
	}
	if withVersion {
		if version, ok = parse("version"); !ok {
			respond.NotFound(c, "service charge version")
		}
	}
	return
}

func serviceChargeError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.ConstraintName == "pharmacy_service_charge_four_eyes":
		respond.Error(c, http.StatusConflict, "four_eyes", "a service charge is reviewed by someone other than who declared it")
	case errors.As(err, &pg) && pg.ConstraintName == "pharmacy_service_charge_review_latest":
		respond.Conflict(c, "a newer version replaced this one; review the latest")
	case errors.As(err, &pg) && pg.ConstraintName == "pharmacy_service_charge_code":
		respond.Conflict(c, "another service charge of this outlet already uses this code")
	case errors.As(err, &pg) && pg.ConstraintName == "pharmacy_service_charge_limit":
		respond.Conflict(c, "an outlet has at most 50 service charges")
	case errors.Is(err, errChargeCode):
		respond.Conflict(c, "a service charge's code never changes; a new version repeats it")
	case errors.Is(err, errDeclarationReviewed):
		respond.Conflict(c, "this version has already been reviewed")
	case errors.Is(err, errStockIdentity):
		respond.Conflict(c, "this version id was already used with different details")
	default:
		catalogueError(c, err)
	}
}

// validServiceChargeInput says what a version must carry; the table's form
// constraint says the same.
func validServiceChargeInput(in *serviceChargeInput) string {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.TaxBasis = strings.TrimSpace(in.TaxBasis)
	if in.SacCode != nil {
		*in.SacCode = strings.TrimSpace(*in.SacCode)
	}
	if in.ExemptionBasis != nil {
		*in.ExemptionBasis = strings.TrimSpace(*in.ExemptionBasis)
	}
	switch {
	case !chargeCodePattern.MatchString(in.Code):
		return "a code is 2 to 20 capital letters, digits or underscores, starting with a letter"
	case !prescriptionText(in.Name, 80):
		return "a name is 1 to 80 characters"
	case len([]rune(in.TaxBasis)) < 5 || !prescriptionText(in.TaxBasis, 500):
		return "a tax basis of 5 to 500 characters says who decided the treatment and on what authority"
	}
	if _, err := time.Parse(time.DateOnly, in.EffectiveFrom); err != nil {
		return "effective_from is a date"
	}
	if in.TaxTreatment == "principal_supply" {
		if in.SacCode != nil || in.SupplyNature != nil || in.RatePpm != nil || in.ExemptionBasis != nil {
			return "a charge taxed as the medicines carries no SAC, supply nature, rate or exemption of its own"
		}
		return ""
	}
	if in.SacCode == nil || !sacCodePattern.MatchString(*in.SacCode) || in.SupplyNature == nil || in.RatePpm == nil {
		return "a service of its own needs its SAC (99 and four digits), supply nature and rate"
	}
	if *in.RatePpm%100 != 0 || (*in.SupplyNature == "taxable") != (*in.RatePpm >= 100) {
		return "a taxable service has a rate of at least 0.01% in steps of 0.01%; a nil-rated or exempt one has none"
	}
	if (*in.SupplyNature == "exempt") != (in.ExemptionBasis != nil) || in.ExemptionBasis != nil && !prescriptionText(*in.ExemptionBasis, 500) {
		return "an exempt service records its exemption basis, and only an exempt one does"
	}
	return ""
}

func (in serviceChargeInput) digest(charge uuid.UUID) string {
	rate := ""
	if in.RatePpm != nil {
		rate = strconv.FormatInt(*in.RatePpm, 10)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{charge.String(), in.Code, in.Name, in.Channel, strconv.FormatInt(in.AmountMinor, 10),
		in.Status, in.TaxTreatment, optionalText(in.SacCode), optionalText(in.SupplyNature), rate, optionalText(in.ExemptionBasis),
		in.TaxBasis, in.EffectiveFrom}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func (h *catalogueHandlers) putServiceChargeVersion(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	charge, id, ok := serviceChargeIDs(c, true)
	if !ok {
		return
	}
	var in serviceChargeInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if why := validServiceChargeInput(&in); why != "" {
		respond.Error(c, http.StatusBadRequest, "invalid_request", why)
		return
	}
	digest := in.digest(charge)
	var result serviceChargeVersion
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		// The first version names the charge; every later one repeats its code.
		if err := tx.Exec(`INSERT INTO pharmacy_service_charges (id, tenant_id, outlet_id, code, created_by) VALUES (?, ?, ?, ?, ?) ON CONFLICT (tenant_id, outlet_id, id) DO NOTHING`,
			charge, p.TenantID, outlet, in.Code, p.Subject).Error; err != nil {
			return err
		}
		var code string
		if err := tx.Raw(`SELECT code FROM pharmacy_service_charges WHERE tenant_id=? AND outlet_id=? AND id=?`, p.TenantID, outlet, charge).Scan(&code).Error; err != nil {
			return err
		}
		if code != in.Code {
			return errChargeCode
		}
		insert := tx.Exec(`INSERT INTO pharmacy_service_charge_versions (id, tenant_id, outlet_id, charge_id, name, channel, amount_minor, available, tax_treatment,
			sac_code, supply_nature, rate_ppm, exemption_basis, tax_basis, effective_from, intent_sha256, declared_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::date, ?, ?) ON CONFLICT DO NOTHING`,
			id, p.TenantID, outlet, charge, in.Name, in.Channel, in.AmountMinor, in.Status == "available", in.TaxTreatment,
			in.SacCode, in.SupplyNature, in.RatePpm, in.ExemptionBasis, in.TaxBasis, in.EffectiveFrom, digest, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		created = insert.RowsAffected == 1
		if !created {
			var won []struct{ IntentSHA256, DeclaredBy string }
			if err := tx.Raw(`SELECT intent_sha256, declared_by FROM pharmacy_service_charge_versions WHERE tenant_id=? AND outlet_id=? AND id=?`,
				p.TenantID, outlet, id).Scan(&won).Error; err != nil {
				return err
			}
			if len(won) != 1 || won[0].IntentSHA256 != digest || won[0].DeclaredBy != p.Subject {
				return errStockIdentity
			}
		}
		return tx.Raw(serviceChargeVersionQuery+` WHERE v.tenant_id=? AND v.outlet_id=? AND v.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	switch {
	case err != nil:
		serviceChargeError(c, err)
	case created:
		respond.Created(c, result)
	default:
		respond.OK(c, result)
	}
}

func (h *catalogueHandlers) reviewServiceChargeVersion(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	charge, id, ok := serviceChargeIDs(c, true)
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
	var result serviceChargeVersion
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var found int64
		if err := tx.Raw(`SELECT count(*) FROM pharmacy_service_charge_versions WHERE tenant_id=? AND outlet_id=? AND charge_id=? AND id=?`,
			p.TenantID, outlet, charge, id).Scan(&found).Error; err != nil {
			return err
		}
		if found == 0 {
			return gorm.ErrRecordNotFound
		}
		insert := tx.Exec(`INSERT INTO pharmacy_service_charge_reviews (tenant_id, outlet_id, version_id, decision, reason, reviewed_by)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, p.TenantID, outlet, id, in.Decision, in.Reason, p.Subject)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return errDeclarationReviewed
		}
		return tx.Raw(serviceChargeVersionQuery+` WHERE v.tenant_id=? AND v.outlet_id=? AND v.id=?`, p.TenantID, outlet, id).Scan(&result).Error
	})
	if err != nil {
		serviceChargeError(c, err)
		return
	}
	respond.OK(c, result)
}

// readServiceCharges is every charge of the outlet (one, when only is set),
// with the version in force on day and the latest version.
func readServiceCharges(tx *gorm.DB, tenant string, outlet uuid.UUID, day string, only *uuid.UUID) ([]serviceCharge, error) {
	var rows []struct {
		ID       uuid.UUID
		Code     string
		InForce  *uuid.UUID `gorm:"column:in_force"`
		LatestID *uuid.UUID `gorm:"column:latest"`
	}
	q := `SELECT c.id, c.code, f.version_id AS in_force,
		(SELECT v.id FROM pharmacy_service_charge_versions v WHERE v.tenant_id=c.tenant_id AND v.outlet_id=c.outlet_id AND v.charge_id=c.id ORDER BY v.seq DESC LIMIT 1) AS latest
		FROM pharmacy_service_charges c LEFT JOIN LATERAL pharmacy_service_charge_in_force(c.tenant_id, c.outlet_id, c.id, ?::date) f ON true
		WHERE c.tenant_id=? AND c.outlet_id=?`
	args := []any{day, tenant, outlet}
	if only != nil {
		q += ` AND c.id=?`
		args = append(args, *only)
	}
	if err := tx.Raw(q+` ORDER BY c.code`, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for _, r := range rows {
		for _, v := range []*uuid.UUID{r.InForce, r.LatestID} {
			if v != nil {
				ids = append(ids, *v)
			}
		}
	}
	versions := map[uuid.UUID]serviceChargeVersion{}
	if len(ids) > 0 {
		var found []serviceChargeVersion
		if err := tx.Raw(serviceChargeVersionQuery+` WHERE v.tenant_id=? AND v.outlet_id=? AND v.id IN ?`, tenant, outlet, ids).Scan(&found).Error; err != nil {
			return nil, err
		}
		for _, v := range found {
			versions[v.ID] = v
		}
	}
	pick := func(id *uuid.UUID) *serviceChargeVersion {
		if id == nil {
			return nil
		}
		v, ok := versions[*id]
		if !ok {
			return nil
		}
		return &v
	}
	out := make([]serviceCharge, 0, len(rows))
	for _, r := range rows {
		out = append(out, serviceCharge{ID: r.ID, Code: r.Code, Today: day, InForce: pick(r.InForce), Latest: pick(r.LatestID)})
	}
	return out, nil
}

func (h *catalogueHandlers) listServiceCharges(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	var out []serviceCharge
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		out, err = readServiceCharges(tx, p.TenantID, outlet, today, nil)
		return err
	})
	if err != nil {
		serviceChargeError(c, err)
		return
	}
	respond.OK(c, gin.H{"items": out})
}

func (h *catalogueHandlers) getServiceCharge(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	charge, _, ok := serviceChargeIDs(c, false)
	if !ok {
		return
	}
	var out serviceCharge
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		today, err := outletLocalDate(tx, p.TenantID, outlet)
		if err != nil {
			return err
		}
		found, err := readServiceCharges(tx, p.TenantID, outlet, today, &charge)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return gorm.ErrRecordNotFound
		}
		out = found[0]
		out.History = []serviceChargeVersion{}
		return tx.Raw(serviceChargeVersionQuery+` WHERE v.tenant_id=? AND v.outlet_id=? AND v.charge_id=? ORDER BY v.seq DESC LIMIT ?`,
			p.TenantID, outlet, charge, historyLimit).Scan(&out.History).Error
	})
	if err != nil {
		serviceChargeError(c, err)
		return
	}
	respond.OK(c, out)
}

// saleChargeLine is a charge as billed: the version applied and its terms,
// copied, so a later version never alters an issued invoice.
type saleChargeLine struct {
	ChargeNumber  int       `gorm:"column:charge_number" json:"charge_number"`
	ChargeID      uuid.UUID `gorm:"column:charge_id" json:"charge_id"`
	VersionID     uuid.UUID `gorm:"column:version_id" json:"version_id"`
	Code          string    `gorm:"column:code" json:"code"`
	Name          string    `gorm:"column:name" json:"name"`
	TaxTreatment  string    `gorm:"column:tax_treatment" json:"tax_treatment"`
	SACCode       *string   `gorm:"column:sac_code" json:"sac_code"`
	SupplyNature  string    `gorm:"column:supply_nature" json:"supply_nature"`
	RatePPM       int64     `gorm:"column:rate_ppm" json:"rate_ppm"`
	AmountMinor   int64     `gorm:"column:amount_minor" json:"amount_minor"`
	TaxableMinor  int64     `gorm:"column:taxable_minor" json:"taxable_minor"`
	CGSTMinor     int64     `gorm:"column:cgst_minor" json:"cgst_minor"`
	SGSTMinor     int64     `gorm:"column:sgst_minor" json:"sgst_minor"`
	CreditedMinor int64     `gorm:"column:credited_minor" json:"credited_minor"`
}

const saleChargeColumns = `SELECT c.charge_number, c.charge_id, c.version_id, c.code, c.name, c.tax_treatment, c.sac_code, c.supply_nature, c.rate_ppm,
	c.amount_minor, c.taxable_minor, c.cgst_minor, c.sgst_minor,
	(SELECT COALESCE(sum(k.amount_minor),0) FROM pharmacy_credit_note_charge_lines k WHERE k.tenant_id=c.tenant_id AND k.outlet_id=c.outlet_id
	 AND k.sale_id=c.sale_id AND k.sale_charge=c.charge_number) AS credited_minor
	FROM pharmacy_sale_charge_lines c`

func distinctCharges(in []saleChargeInput) bool {
	seen := map[uuid.UUID]bool{}
	for _, ch := range in {
		if seen[ch.ChargeID] {
			return false
		}
		seen[ch.ChargeID] = true
	}
	return true
}

// priceCharges bills each charge at the version in force on day for the
// sale's channel. A principal-supply charge is taxed as the medicines, so
// they must share one supply nature and rate; with none to follow it is
// refused rather than guessed (spec decision 1). The schema checks it all again.
func priceCharges(tx *gorm.DB, tenant string, outlet uuid.UUID, day, channel string, in []saleChargeInput, lines []saleLine) ([]saleChargeLine, error) {
	out := []saleChargeLine{}
	for i, ch := range in {
		var codes []string
		if err := tx.Raw(`SELECT code FROM pharmacy_service_charges WHERE tenant_id=? AND outlet_id=? AND id=?`, tenant, outlet, ch.ChargeID).Scan(&codes).Error; err != nil {
			return nil, err
		}
		if len(codes) == 0 {
			return nil, errNotSellable{"a service charge is not one of this outlet's"}
		}
		var now []struct {
			VersionID uuid.UUID
			Available bool
		}
		if err := tx.Raw(`SELECT version_id, available FROM pharmacy_service_charge_in_force(?, ?, ?, ?::date)`, tenant, outlet, ch.ChargeID, day).Scan(&now).Error; err != nil {
			return nil, err
		}
		if len(now) == 0 || !now[0].Available {
			return nil, errNotSellable{"the service charge " + codes[0] + " is not available today"}
		}
		var v serviceChargeVersion
		if err := tx.Raw(serviceChargeVersionQuery+` WHERE v.tenant_id=? AND v.outlet_id=? AND v.id=?`, tenant, outlet, now[0].VersionID).Scan(&v).Error; err != nil {
			return nil, err
		}
		if v.Channel != channel {
			return nil, errNotSellable{"the service charge " + v.Code + " is not for this kind of sale"}
		}
		line := saleChargeLine{ChargeNumber: i + 1, ChargeID: ch.ChargeID, VersionID: v.ID, Code: v.Code, Name: v.Name, TaxTreatment: v.TaxTreatment, SACCode: v.SACCode}
		if v.TaxTreatment == "own_service" {
			line.SupplyNature, line.RatePPM = *v.SupplyNature, *v.RatePPM
		} else {
			for j, l := range lines {
				if j > 0 && (l.SupplyNature != line.SupplyNature || l.RatePPM != line.RatePPM) {
					return nil, errNotSellable{"the service charge " + v.Code + " is taxed as the medicines, which must all have one GST rate"}
				}
				line.SupplyNature, line.RatePPM = l.SupplyNature, l.RatePPM
			}
		}
		line.AmountMinor, line.TaxableMinor, line.CGSTMinor = taxInside(v.AmountMinor, line.SupplyNature, line.RatePPM)
		line.SGSTMinor = line.CGSTMinor
		out = append(out, line)
	}
	return out, nil
}
