package pharmacy

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type licenceRenewalRow struct {
	LicenceRenewalNotice
	CreatedAt time.Time `json:"-"`
}

func (r licenceRenewalRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.LicenceID }

// These are SupplyConnect's operational reminder bands, not statutory renewal
// deadlines. Dates are evaluated in the outlet's timezone at one observation.
func licenceRenewalNotice(id uuid.UUID, from, until, zone string, instant time.Time) (LicenceRenewalNotice, error) {
	if id == uuid.Nil || zone == "" || instant.IsZero() {
		return LicenceRenewalNotice{}, errors.New("licence renewal context unavailable")
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return LicenceRenewalNotice{}, err
	}
	issued, err := time.Parse(time.DateOnly, from)
	if err != nil || issued.Year() < 1900 {
		return LicenceRenewalNotice{}, errors.New("licence validity dates unavailable")
	}
	expires, err := time.Parse(time.DateOnly, until)
	if err != nil || expires.Before(issued) || expires.Year() > 9999 {
		return LicenceRenewalNotice{}, errors.New("licence validity dates unavailable")
	}
	localDate := instant.In(location).Format(time.DateOnly)
	today, err := time.Parse(time.DateOnly, localDate)
	if err != nil {
		return LicenceRenewalNotice{}, err
	}
	// Unix seconds avoid time.Duration's 292-year saturation for bounded dates.
	days := (expires.Unix() - today.Unix()) / 86400
	status := "current"
	switch {
	case today.Before(issued):
		status = "not_yet_valid"
	case days < 0:
		status = "expired"
	case days == 0:
		status = "expires_today"
	case days <= 30:
		status = "renewal_due_30"
	case days <= 90:
		status = "renewal_due_90"
	case days <= 180:
		status = "renewal_due_180"
	}
	return LicenceRenewalNotice{LicenceID: id, DateStatus: status, DaysRemaining: days, LocalDate: localDate, TimeZone: zone, ObservedAt: instant.UTC().Format(time.RFC3339Nano)}, nil
}

func (h *catalogueHandlers) listLicenceRenewals(c *gin.Context, params pagination.Params) ([]licenceRenewalRow, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var result []licenceRenewalRow
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		var location pharmacyOutlet
		if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", p.TenantID, outlet).First(&location).Error; err != nil {
			return err
		}
		var rows []outletLicence
		q := tenantdb.ApplyKeyset(tx.Table("(?) AS licences", tx.Raw(licenceQuery+` WHERE l.tenant_id=? AND l.outlet_id=?`, p.TenantID, outlet)), params)
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		observed := clock().UTC()
		result = make([]licenceRenewalRow, 0, len(rows))
		for _, row := range rows {
			notice, err := licenceRenewalNotice(row.ID, row.ValidFrom, row.ValidUntil, location.Timezone, observed)
			if err != nil {
				return err
			}
			result = append(result, licenceRenewalRow{LicenceRenewalNotice: notice, CreatedAt: row.CreatedAt})
		}
		return nil
	})
	if err != nil {
		licenceError(c, err)
		return nil, nil
	}
	return result, nil
}
