package workforce

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

var (
	errPharmacyZoneInvalid = errors.New("pharmacy time zone is not a supported IANA zone")
	errPharmacyZoneVersion = errors.New("pharmacy time zone version conflict")
)

func loadPharmacyTimeZone(tx *gorm.DB, tenant string, pharmacy uuid.UUID) (pharmacyTimeZone, error) {
	var rows []pharmacyTimeZone
	if err := tx.Raw(`SELECT version,timezone AS time_zone,updated_by_subject,updated_at FROM workforce_pharmacy_time_zones WHERE tenant_id=? AND hospital_id=?`, tenant, pharmacy).Scan(&rows).Error; err != nil {
		return pharmacyTimeZone{}, err
	}
	if len(rows) == 0 {
		return pharmacyTimeZone{}, nil
	}
	return rows[0], nil
}

func (h *handlers) getPharmacyTimeZone(c *gin.Context, t credentialTarget) {
	var result pharmacyTimeZone
	err := t.transaction(manageCredentials, "", func(tx *gorm.DB) error {
		var err error
		result, err = loadPharmacyTimeZone(tx, t.p.TenantID, t.owner.id)
		return err
	})
	if err != nil {
		respondPharmacyZoneError(c, err)
		return
	}
	respond.OK(c, result)
}

// putPharmacyTimeZone publishes no event: each reminder workflow re-reads the zone within its 24-hour sleep cap.
func (h *handlers) putPharmacyTimeZone(c *gin.Context, t credentialTarget) {
	p, pharmacy := t.p, t.owner.id
	var in pharmacyTimeZoneInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	if !validTimezone(in.TimeZone) {
		respondPharmacyZoneError(c, errPharmacyZoneInvalid)
		return
	}
	var result pharmacyTimeZone
	err := t.transaction(manageCredentials, "time-zone", func(tx *gorm.DB) error {
		current, err := loadPharmacyTimeZone(tx, p.TenantID, pharmacy)
		if err != nil {
			return err
		}
		if current.TimeZone != nil && *current.TimeZone == in.TimeZone {
			result = current
			return nil
		}
		if current.Version != *in.ExpectedVersion {
			return errPharmacyZoneVersion
		}
		version := current.Version + 1
		write := tx.Exec(`UPDATE workforce_pharmacy_time_zones SET timezone=?,version=?,updated_by_subject=?,updated_at=now() WHERE tenant_id=? AND hospital_id=? AND version=?`,
			in.TimeZone, version, p.Subject, p.TenantID, pharmacy, current.Version)
		if current.Version == 0 {
			write = tx.Exec(`INSERT INTO workforce_pharmacy_time_zones(tenant_id,hospital_id,timezone,version,updated_by_subject) VALUES(?,?,?,?,?)`,
				p.TenantID, pharmacy, in.TimeZone, version, p.Subject)
		}
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected != 1 {
			return errPharmacyZoneVersion
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.pharmacy_time_zone.changed',?,jsonb_build_object('previous',?::text,'time_zone',?::text,'version',?::bigint))`,
			p.TenantID, pharmacy, p.Subject, pharmacy, current.TimeZone, in.TimeZone, version).Error; err != nil {
			return err
		}
		result, err = loadPharmacyTimeZone(tx, p.TenantID, pharmacy)
		return err
	})
	if err != nil {
		respondPharmacyZoneError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondPharmacyZoneError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, errPharmacyZoneInvalid):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a supported IANA time zone such as Asia/Kolkata.")
	case errors.Is(err, errPharmacyZoneVersion), isPG && pg.ConstraintName == "workforce_pharmacy_time_zone_version":
		respond.ConflictWithDetail(c, "The pharmacy's time zone changed since you opened it. Reload before saving.", gin.H{"reason": "version_conflict"})
	default:
		respondIntakeError(c, err)
	}
}
