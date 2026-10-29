package billing

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	errNotConfigured = errors.New("bed charging is not set up")
	errSetDifferent  = errors.New("already set to something else")
)

type bedCharging struct {
	HospitalID uuid.UUID `json:"hospital_id"`
	Timezone   string    `json:"timezone"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// setBedCharging records the hospital's timezone, once.
func (h *handlers) setBedCharging(c *gin.Context) {
	var in struct {
		Timezone string `json:"timezone"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || !knownZone(in.Timezone) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "timezone must be a named timezone, such as Asia/Kolkata")
		return
	}
	var out bedCharging
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := tx.Exec(`INSERT INTO billing_bed_charging (tenant_id,hospital_id,timezone,created_by) VALUES (?,?,?,?) ON CONFLICT (tenant_id,hospital_id) DO NOTHING`,
			p.TenantID, hospital, in.Timezone, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT hospital_id,timezone,created_by,created_at FROM billing_bed_charging WHERE tenant_id=? AND hospital_id=?`, p.TenantID, hospital).Scan(&out).Error; err != nil {
			return err
		}
		if out.Timezone != in.Timezone {
			return errSetDifferent
		}
		return nil
	})
	switch {
	case errors.Is(err, errSetDifferent):
		respond.Error(c, http.StatusConflict, "already_set", "this hospital's timezone is set to "+out.Timezone+" and cannot change")
	case err != nil:
		failure(c, err)
	default:
		respond.OK(c, out)
	}
}

// knownZone accepts a named zone Go knows; the database checks its own list.
func knownZone(zone string) bool {
	if zone == "" || zone == "Local" || len(zone) > 64 {
		return false
	}
	_, err := time.LoadLocation(zone)
	return err == nil
}

func (h *handlers) bedChargingSetup(c *gin.Context) {
	var out []bedCharging
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return tx.Raw(`SELECT hospital_id,timezone,created_by,created_at FROM billing_bed_charging WHERE tenant_id=? AND hospital_id=?`, p.TenantID, hospital).Scan(&out).Error
	})
	switch {
	case err != nil:
		failure(c, err)
	case len(out) == 0:
		respond.Error(c, http.StatusNotFound, "not_configured", "bed charging is not set up at this hospital")
	default:
		respond.OK(c, out[0])
	}
}

type bedRate struct {
	ID             uuid.UUID `json:"id"`
	HospitalID     uuid.UUID `json:"hospital_id"`
	BedClass       string    `json:"bed_class"`
	EffectiveFrom  string    `json:"effective_from"`
	DailyRateMinor int64     `json:"daily_rate_minor"`
	Currency       string    `json:"currency"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}

func (bedRate) TableName() string                 { return "billing_bed_rates" }
func (r bedRate) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const bedRateColumns = "id,hospital_id,bed_class,to_char(effective_from,'YYYY-MM-DD') AS effective_from,daily_rate_minor,currency,created_by,created_at"

// setBedRate records a class's daily rate from a date. A rate is fixed once
// set: the same again is accepted, another is a conflict.
func (h *handlers) setBedRate(c *gin.Context) {
	class := c.Param("class")
	from, err := time.Parse(time.DateOnly, c.Param("effective_from"))
	var in struct {
		DailyRateMinor int64  `json:"daily_rate_minor"`
		Currency       string `json:"currency"`
	}
	if !medicorecontract.BedClasses[class] || err != nil || c.ShouldBindJSON(&in) != nil || in.DailyRateMinor < 1 || in.DailyRateMinor > 1_000_000_000 || !currencyCode(in.Currency) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a bed class, an effective date (YYYY-MM-DD), a daily rate of 1 to 1000000000 minor units and a currency code are required")
		return
	}
	var out bedRate
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var set int64
		if err := tx.Table("billing_bed_charging").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Count(&set).Error; err != nil {
			return err
		}
		if set == 0 {
			return errNotConfigured
		}
		if err := tx.Exec(`INSERT INTO billing_bed_rates (tenant_id,hospital_id,bed_class,effective_from,daily_rate_minor,currency,created_by) VALUES (?,?,?,?,?,?,?)
 ON CONFLICT (tenant_id,hospital_id,bed_class,effective_from) DO NOTHING`, p.TenantID, hospital, class, from, in.DailyRateMinor, in.Currency, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT `+bedRateColumns+` FROM billing_bed_rates WHERE tenant_id=? AND hospital_id=? AND bed_class=? AND effective_from=?`, p.TenantID, hospital, class, from).Scan(&out).Error; err != nil {
			return err
		}
		if out.DailyRateMinor != in.DailyRateMinor || out.Currency != in.Currency {
			return errSetDifferent
		}
		return nil
	})
	switch {
	case errors.Is(err, errNotConfigured):
		respond.Error(c, http.StatusConflict, "not_configured", "set the hospital's timezone before its bed rates")
	case errors.Is(err, errSetDifferent):
		respond.Error(c, http.StatusConflict, "already_set", "a different rate is set for this class from this date; set a rate from a later date")
	case err != nil:
		failure(c, err)
	default:
		respond.OK(c, out)
	}
}

func currencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func (h *handlers) bedRates(c *gin.Context, params pagination.Params) ([]bedRate, error) {
	var rows []bedRate
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		return tenantdb.ApplyOrderedKeyset(tx, params, tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).
			Select(bedRateColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
