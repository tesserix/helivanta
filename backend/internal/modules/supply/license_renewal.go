package supply

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authn"
)

type licenseRead struct {
	license
	Renewal LicenseRenewalFields `json:"renewal"`
}

func licenceRenewalReads(tx *gorm.DB, p authn.Principal, partyID uuid.UUID, rows []license) ([]licenseRead, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.PremiseID)
	}
	var premises []premise
	if err := tx.Select("id,time_zone,status").Where("tenant_id=? AND party_id=? AND id IN ?", p.TenantID, partyID, ids).Find(&premises).Error; err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]premise, len(premises))
	for _, row := range premises {
		byID[row.ID] = row
	}
	var observation struct{ ObservedAt time.Time }
	if err := tx.Raw("SELECT clock_timestamp() AS observed_at").Scan(&observation).Error; err != nil {
		return nil, err
	}
	result := make([]licenseRead, 0, len(rows))
	for _, row := range rows {
		location, ok := byID[row.PremiseID]
		if !ok {
			return nil, gorm.ErrRecordNotFound
		}
		renewal, err := licenceRenewal(row.IssuedOn, row.ExpiresOn, location.TimeZone, observation.ObservedAt)
		if err != nil {
			return nil, err
		}
		renewal.PremiseStatus = location.Status
		result = append(result, licenseRead{license: row, Renewal: renewal})
	}
	return result, nil
}

func licenceRenewal(issuedOn, expiresOn, zone string, instant time.Time) (LicenseRenewalFields, error) {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return LicenseRenewalFields{}, fmt.Errorf("load licence premise timezone: %w", err)
	}
	issued, err := time.Parse(time.DateOnly, issuedOn)
	if err != nil {
		return LicenseRenewalFields{}, fmt.Errorf("parse licence issue date: %w", err)
	}
	expires, err := time.Parse(time.DateOnly, expiresOn)
	if err != nil || expires.Before(issued) {
		return LicenseRenewalFields{}, fmt.Errorf("invalid licence validity dates")
	}
	day := instant.In(location).Format(time.DateOnly)
	today, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return LicenseRenewalFields{}, fmt.Errorf("parse licence local date: %w", err)
	}
	days := int((expires.Unix() - today.Unix()) / 86400)
	status := "current"
	switch {
	case issued.After(today):
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
	return LicenseRenewalFields{DateStatus: status, DaysRemaining: days, LocalDate: day, TimeZone: zone, ObservedAt: instant.UTC().Format(time.RFC3339Nano)}, nil
}
