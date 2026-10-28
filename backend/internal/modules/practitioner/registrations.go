package practitioner

import (
	"errors"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"gorm.io/gorm"
)

var councilSystem = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)

// RegistrationDates holds reported dates, not proof of current registration.
type RegistrationDates struct {
	IssuedOn  *string `json:"issued_on"`
	ExpiresOn *string `json:"expires_on"`
}

func validDate(s *string) bool {
	if s == nil {
		return true
	}
	d, err := time.Parse("2006-01-02", *s)
	return err == nil && d.Year() > 0 && d.Format("2006-01-02") == *s
}
func (d RegistrationDates) valid() bool {
	return validDate(d.IssuedOn) && validDate(d.ExpiresOn) && (d.IssuedOn == nil || d.ExpiresOn == nil || *d.ExpiresOn >= *d.IssuedOn)
}
func (d RegistrationDates) equals(other RegistrationDates) bool {
	return sameOptionalString(d.IssuedOn, other.IssuedOn) && sameOptionalString(d.ExpiresOn, other.ExpiresOn)
}

type registrationInput struct {
	CouncilSystem      string `json:"council_system"`
	RegistrationNumber string `json:"registration_number"`
	RegistrationDates
}
type registration struct {
	ID                 uuid.UUID `json:"id"`
	PractitionerID     uuid.UUID `json:"practitioner_id"`
	CouncilSystem      string    `json:"council_system"`
	RegistrationNumber string    `json:"registration_number"`
	RevisionID         uuid.UUID `json:"revision_id"`
	Version            int64     `json:"version"`
	VerificationState  string    `json:"verification_state"`
	RegistrationDates  `gorm:"embedded"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (r *registration) AfterFind(*gorm.DB) error {
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return nil
}

const registrationColumns = "r.id,r.practitioner_id,r.council_system,r.registration_number,r.version,v.id AS revision_id,v.issued_on::text AS issued_on,v.expires_on::text AS expires_on,v.verification_state,r.created_at,r.updated_at"

type registrationRevision struct {
	ID                uuid.UUID `json:"id"`
	PractitionerID    uuid.UUID `json:"practitioner_id"`
	RegistrationID    uuid.UUID `json:"registration_id"`
	ExpectedVersion   int64     `json:"expected_version"`
	Version           int64     `json:"version"`
	VerificationState string    `json:"verification_state"`
	RegistrationDates `gorm:"embedded"`
	CreatedAt         time.Time `json:"created_at"`
}

func (r *registrationRevision) AfterFind(*gorm.DB) error { r.CreatedAt = r.CreatedAt.UTC(); return nil }

const registrationRevisionColumns = "id,practitioner_id,registration_id,expected_version,version,issued_on::text AS issued_on,expires_on::text AS expires_on,verification_state,created_at"

func readRegistration(tx *gorm.DB, p authn.Principal, practitioner, id uuid.UUID, row *registration) error {
	return tx.Table("practitioner_registrations r").Select(registrationColumns).Joins("JOIN practitioner_registration_revisions v ON v.tenant_id=r.tenant_id AND v.registration_id=r.id AND v.version=r.version").Where("r.tenant_id=? AND r.practitioner_id=? AND r.id=?", p.TenantID, practitioner, id).Take(row).Error
}
func readRevision(tx *gorm.DB, p authn.Principal, practitioner, registration, id uuid.UUID, row *registrationRevision) error {
	return tx.Table("practitioner_registration_revisions").Select(registrationRevisionColumns).Where("tenant_id=? AND practitioner_id=? AND registration_id=? AND id=?", p.TenantID, practitioner, registration, id).Take(row).Error
}
func registrationAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_registrations',?)", p.TenantID, p.Subject, id).Error
}
func (h *handlers) putRegistration(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	practitioner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	id, ok := resourceID(c, "registration")
	if !ok {
		return
	}
	var in *registrationInput
	if !decode(c, &in) {
		return
	}
	if in == nil || !councilSystem.MatchString(in.CouncilSystem) || !validName(in.RegistrationNumber) || len(in.RegistrationNumber) > 100 || !in.valid() {
		respond.Error(c, 400, "invalid_request", "Invalid council registration claim")
		return
	}
	var row registration
	created := false
	err := h.transaction(c.Request.Context(), p, true, func(tx *gorm.DB) error {
		var owner record
		if e := read(tx, p, practitioner, &owner); e != nil {
			return e
		}
		res := tx.Exec("INSERT INTO practitioner_registrations(tenant_id,practitioner_id,id,council_system,registration_number,created_by) VALUES(?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, practitioner, id, in.CouncilSystem, in.RegistrationNumber, p.Subject)
		if res.Error != nil {
			return res.Error
		}
		created = res.RowsAffected == 1
		if created {
			if e := tx.Exec("INSERT INTO practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id,expected_version,version,issued_on,expires_on,created_by) VALUES(?,?,?,?,0,1,?::date,?::date,?)", p.TenantID, practitioner, id, id, in.IssuedOn, in.ExpiresOn, p.Subject).Error; e != nil {
				return e
			}
		}
		if e := readRegistration(tx, p, practitioner, id, &row); e != nil {
			return e
		}
		var original registrationRevision
		if e := readRevision(tx, p, practitioner, id, id, &original); e != nil {
			return e
		}
		if row.CouncilSystem != in.CouncilSystem || row.RegistrationNumber != in.RegistrationNumber || !original.equals(in.RegistrationDates) {
			return errIntentConflict
		}
		if !created {
			return registrationAudit(tx, p, id)
		}
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *handlers) getRegistration(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	practitioner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	id, ok := resourceID(c, "registration")
	if !ok {
		return
	}
	var row registration
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := readRegistration(tx, p, practitioner, id, &row); e != nil {
			return e
		}
		return registrationAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}

// Existing command replay is independent of the current registration head.
func existingRevision(tx *gorm.DB, p authn.Principal, practitioner, registration, id uuid.UUID, row *registrationRevision) (bool, error) {
	err := readRevision(tx, p, practitioner, registration, id, row)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

type renewalInput struct {
	ExpectedVersion int64 `json:"expected_version"`
	RegistrationDates
}

func revisionAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_registration_revisions',?)", p.TenantID, p.Subject, id).Error
}
func (h *handlers) putRegistrationRevision(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	practitioner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	registration, ok := resourceID(c, "registration")
	if !ok {
		return
	}
	id, ok := resourceID(c, "revision")
	if !ok {
		return
	}
	var in *renewalInput
	if !decode(c, &in) {
		return
	}
	if in == nil || in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740990 || !in.valid() {
		respond.Error(c, 400, "invalid_request", "Invalid registration revision")
		return
	}
	var row registrationRevision
	created := false
	err := h.transaction(c.Request.Context(), p, true, func(tx *gorm.DB) error {
		var head struct{ Version int64 }
		if e := tx.Raw("SELECT version FROM practitioner_registrations WHERE tenant_id=? AND practitioner_id=? AND id=? FOR UPDATE", p.TenantID, practitioner, registration).Scan(&head).Error; e != nil {
			return e
		}
		if head.Version == 0 {
			return gorm.ErrRecordNotFound
		}
		found, e := existingRevision(tx, p, practitioner, registration, id, &row)
		if e != nil {
			return e
		}
		if found {
			if row.ExpectedVersion != in.ExpectedVersion || !row.equals(in.RegistrationDates) {
				return errIntentConflict
			}
			return revisionAudit(tx, p, id)
		}
		if head.Version != in.ExpectedVersion {
			return errIntentConflict
		}
		if e := tx.Exec("INSERT INTO practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id,expected_version,version,issued_on,expires_on,created_by) VALUES(?,?,?,?,?,?,?::date,?::date,?)", p.TenantID, practitioner, registration, id, in.ExpectedVersion, in.ExpectedVersion+1, in.IssuedOn, in.ExpiresOn, p.Subject).Error; e != nil {
			return e
		}
		result := tx.Exec("UPDATE practitioner_registrations SET version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND practitioner_id=? AND id=? AND version=?", p.TenantID, practitioner, registration, in.ExpectedVersion)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errIntentConflict
		}
		if e := readRevision(tx, p, practitioner, registration, id, &row); e != nil {
			return e
		}
		created = true
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *handlers) getRegistrationRevision(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	practitioner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	registration, ok := resourceID(c, "registration")
	if !ok {
		return
	}
	id, ok := resourceID(c, "revision")
	if !ok {
		return
	}
	var row registrationRevision
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if e := readRevision(tx, p, practitioner, registration, id, &row); e != nil {
			return e
		}
		return revisionAudit(tx, p, id)
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
