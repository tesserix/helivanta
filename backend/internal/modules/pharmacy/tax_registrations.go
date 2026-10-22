package pharmacy

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tax"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// clock is the module's time source; tests pin it through SetClock.
var clock = time.Now

var (
	errTaxSchemeUnavailable = errors.New("tax scheme unavailable")
	errTaxBackdated         = errors.New("tax change before today")
)

// outletToday is the outlet's own calendar date. Tax setup can never start
// before it: a sale snapshots its tax, and backdating would rewrite what
// the setup "was" on days already traded.
func outletToday(timezone string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", err
	}
	return clock().In(loc).Format(time.DateOnly), nil
}

// outletTaxContext loads the outlet's scheme and local date, refusing a
// country without a scheme rather than guessing one.
func outletTaxContext(tx *gorm.DB, tenant string, pharmacy, outlet uuid.UUID) (countryprofile.TaxScheme, string, error) {
	var o pharmacyOutlet
	if err := tx.Select(outletColumns).Where("tenant_id=? AND pharmacy_id=? AND id=?", tenant, pharmacy, outlet).First(&o).Error; err != nil {
		return countryprofile.TaxScheme{}, "", err
	}
	scheme, ok := countryprofile.Tax(o.CountryCode)
	if !ok {
		return countryprofile.TaxScheme{}, "", errTaxSchemeUnavailable
	}
	today, err := outletToday(o.Timezone)
	return scheme, today, err
}

func checkEffectiveFrom(effective, today string) error {
	if _, err := time.Parse(time.DateOnly, effective); err != nil {
		return errInvalidTaxDate
	}
	if effective < today {
		return errTaxBackdated
	}
	return nil
}

var errInvalidTaxDate = errors.New("invalid effective date")

// optionalReason trims a reason; blank is refused rather than stored.
func optionalReason(reason *string) (*string, bool) {
	if reason == nil {
		return nil, true
	}
	r := strings.TrimSpace(*reason)
	return &r, r != ""
}

func taxError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errTaxSchemeUnavailable):
		respond.Error(c, http.StatusConflict, "tax_scheme_unavailable", "tax setup is not available for this outlet's country yet")
	case errors.Is(err, errTaxBackdated):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "effective_from cannot be before today at the outlet")
	case errors.Is(err, errInvalidTaxDate):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "effective_from must be a date")
	case errors.Is(err, tax.ErrFormat), errors.Is(err, tax.ErrKind):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "identifier format is not valid")
	case errors.Is(err, tax.ErrChecksum):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "identifier check character is wrong")
	case errors.Is(err, tax.ErrJurisdictionMismatch):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "identifier does not belong to the jurisdiction")
	default:
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.ConstraintName == "pharmacy_tax_correction_reason" {
			respond.Error(c, http.StatusBadRequest, "invalid_request", "a correction for the same date needs a reason")
			return
		}
		catalogueError(c, err)
	}
}

type taxRegistration struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"-"`
	OutletID         uuid.UUID `json:"outlet_id"`
	JurisdictionCode string    `json:"jurisdiction_code"`
	IdentifierKind   string    `json:"identifier_kind"`
	Identifier       string    `json:"identifier"`
	EffectiveFrom    string    `json:"effective_from"`
	Reason           *string   `json:"reason"`
	RecordedBy       string    `json:"recorded_by"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	InForce          bool      `json:"in_force" gorm:"->;-:migration"`
	Replaced         bool      `json:"replaced" gorm:"->;-:migration"`
}

func (taxRegistration) TableName() string                 { return "pharmacy_outlet_tax_registrations" }
func (r taxRegistration) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

// The row in force on a date is the latest effective_from on or before it,
// and the latest recorded (seq) among equal dates; replaced means a later
// row shares the date.
const registrationColumns = `r.id,r.tenant_id,r.outlet_id,r.jurisdiction_code,r.identifier_kind,r.identifier,
 to_char(r.effective_from,'YYYY-MM-DD') AS effective_from,r.reason,r.recorded_by,r.created_at,
 r.id = (SELECT f.id FROM pharmacy_outlet_tax_registrations f WHERE f.tenant_id=r.tenant_id AND f.outlet_id=r.outlet_id
  AND f.effective_from <= ?::date ORDER BY f.effective_from DESC,f.seq DESC LIMIT 1) AS in_force,
 EXISTS (SELECT 1 FROM pharmacy_outlet_tax_registrations l WHERE l.tenant_id=r.tenant_id AND l.outlet_id=r.outlet_id
  AND l.effective_from=r.effective_from AND l.seq>r.seq) AS replaced`

func taxRegistrationRoutes(g *platform.Router, h *catalogueHandlers) {
	// Registration is outlet administration: reads and writes are the
	// owner's (pharmacy.tax.manage), scoped like the outlet registry.
	platform.ListRoute(g, "/pharmacies/:pharmacy/outlets/:outlet/tax-registrations", PermTaxManage, h.listTaxRegistrations)
	g.POST("/pharmacies/:pharmacy/outlets/:outlet/tax-registrations", PermTaxManage, h.postTaxRegistration)
	g.GET("/pharmacies/:pharmacy/outlets/:outlet/tax-registrations/in-force", PermTaxManage, h.getTaxRegistrationInForce)
}

// getTaxRegistrationInForce answers {"registration": row} or
// {"registration": null} when nothing is in force today.
func (h *catalogueHandlers) getTaxRegistrationInForce(c *gin.Context) {
	tenant, pharmacy, outlet, ok := h.registrationScope(c)
	if !ok {
		return
	}
	p, _, _ := pharmacyPrincipal(c)
	var rows []taxRegistration
	err := h.db.WithTenant(c.Request.Context(), tenant, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, pharmacy, registryManager(c)); err != nil {
			return err
		}
		// outletTaxContext also proves the outlet belongs to this pharmacy.
		_, today, err := outletTaxContext(tx, tenant, pharmacy, outlet)
		if errors.Is(err, errTaxSchemeUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		return tx.Table("pharmacy_outlet_tax_registrations AS r").Select(registrationColumns, today).
			Where("r.tenant_id=? AND r.outlet_id=? AND r.effective_from <= ?::date", tenant, outlet, today).
			Order("r.effective_from DESC, r.seq DESC").Limit(1).Find(&rows).Error
	})
	if err != nil {
		taxError(c, err)
		return
	}
	var row *taxRegistration
	if len(rows) == 1 {
		row = &rows[0]
	}
	respond.OK(c, gin.H{"registration": row})
}

func (h *catalogueHandlers) registrationScope(c *gin.Context) (string, uuid.UUID, uuid.UUID, bool) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return "", uuid.Nil, uuid.Nil, false
	}
	pharmacy, ok := registryID(c, "pharmacy")
	if !ok {
		return "", uuid.Nil, uuid.Nil, false
	}
	outlet, ok := registryID(c, "outlet")
	if !ok {
		return "", uuid.Nil, uuid.Nil, false
	}
	return p.TenantID, pharmacy, outlet, true
}

func (h *catalogueHandlers) listTaxRegistrations(c *gin.Context, params pagination.Params) ([]taxRegistration, error) {
	tenant, pharmacy, outlet, ok := h.registrationScope(c)
	if !ok {
		return nil, nil
	}
	p, _, _ := pharmacyPrincipal(c)
	var rows []taxRegistration
	err := h.db.WithTenant(c.Request.Context(), tenant, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, pharmacy, registryManager(c)); err != nil {
			return err
		}
		_, today, err := outletTaxContext(tx, tenant, pharmacy, outlet)
		if err != nil && !errors.Is(err, errTaxSchemeUnavailable) {
			return err
		}
		if today == "" {
			// No scheme: there is nothing in force to compute against.
			today = "0001-01-01"
		}
		q := tenantdb.ApplyKeyset(tx.Table("pharmacy_outlet_tax_registrations AS r"), params)
		return q.Select(registrationColumns, today).Where("r.tenant_id=? AND r.outlet_id=?", tenant, outlet).Find(&rows).Error
	})
	if err != nil {
		taxError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *catalogueHandlers) postTaxRegistration(c *gin.Context) {
	tenant, pharmacy, outlet, ok := h.registrationScope(c)
	if !ok {
		return
	}
	p, _, _ := pharmacyPrincipal(c)
	var in taxRegistrationInput
	if !decodeCatalogue(c, &in) {
		return
	}
	reason, ok := optionalReason(in.Reason)
	if !ok {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "reason cannot be blank")
		return
	}
	var row taxRegistration
	err := h.db.WithTenant(tenantdb.WithActor(c.Request.Context(), p.Subject), tenant, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, pharmacy, registryManager(c)); err != nil {
			return err
		}
		scheme, today, err := outletTaxContext(tx, tenant, pharmacy, outlet)
		if err != nil {
			return err
		}
		if err := checkEffectiveFrom(in.EffectiveFrom, today); err != nil {
			return err
		}
		identifier := tax.NormalizeRegistration(in.Identifier)
		if err := tax.ValidateRegistration(scheme, in.JurisdictionCode, scheme.RegistrationKind, identifier); err != nil {
			return err
		}
		row = taxRegistration{ID: uuid.Must(uuid.NewV7()), TenantID: uuid.MustParse(tenant), OutletID: outlet,
			JurisdictionCode: in.JurisdictionCode, IdentifierKind: scheme.RegistrationKind, Identifier: identifier,
			EffectiveFrom: in.EffectiveFrom, Reason: reason, RecordedBy: p.Subject}
		if err := tx.Omit("CreatedAt", "InForce", "Replaced").Create(&row).Error; err != nil {
			return err
		}
		return tx.Table("pharmacy_outlet_tax_registrations AS r").Select(registrationColumns, today).Where("r.tenant_id=? AND r.id=?", tenant, row.ID).Take(&row).Error
	})
	if err != nil {
		taxError(c, err)
		return
	}
	respond.Created(c, row)
}
