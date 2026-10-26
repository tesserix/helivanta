package practitioner

import (
	"errors"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

var specialtyCode = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)

type specialty struct {
	Active        bool      `json:"active" gorm:"->"`
	RetirementID  *string   `json:"retirement_id" gorm:"->"`
	ReplacementID *string   `json:"replacement_id" gorm:"->"`
	ID            uuid.UUID `json:"id"`
	Code          string    `json:"code"`
	Display       string    `json:"display"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *specialty) AfterFind(*gorm.DB) error       { s.CreatedAt = s.CreatedAt.UTC(); return nil }
func (s specialty) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }
func specialtyQuery(tx *gorm.DB, p authn.Principal) *gorm.DB {
	return tx.Table("practitioner_specialties").Select("id,code,display,created_at,NOT EXISTS(SELECT 1 FROM practitioner_specialty_retirements r WHERE r.tenant_id=practitioner_specialties.tenant_id AND r.specialty_id=practitioner_specialties.id) AS active,(SELECT r.id::text FROM practitioner_specialty_retirements r WHERE r.tenant_id=practitioner_specialties.tenant_id AND r.specialty_id=practitioner_specialties.id) AS retirement_id,(SELECT r.replacement_id::text FROM practitioner_specialty_retirements r WHERE r.tenant_id=practitioner_specialties.tenant_id AND r.specialty_id=practitioner_specialties.id) AS replacement_id").Where("tenant_id=?", p.TenantID)
}
func (h *handlers) putSpecialty(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "specialty")
	if !ok {
		return
	}
	var in *struct {
		Code    string `json:"code"`
		Display string `json:"display"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil || !specialtyCode.MatchString(in.Code) || !validName(in.Display) {
		respond.Error(c, 400, "invalid_request", "Expected a bounded local specialty code and display")
		return
	}
	var row specialty
	created := false
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_specialty_manage','true',true)").Error; err != nil {
			return err
		}
		result := tx.Exec("INSERT INTO practitioner_specialties(tenant_id,id,code,display,created_by) VALUES(?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, id, in.Code, in.Display, p.Subject)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := specialtyQuery(tx, p).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.Code != in.Code || row.Display != in.Display {
			return errIntentConflict
		}
		if !created {
			return auditIDs(tx, p, "practitioner_specialties", []uuid.UUID{id})
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
func (h *handlers) getSpecialty(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "specialty")
	if !ok {
		return
	}
	var row specialty
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := specialtyQuery(tx, p).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_specialties", []uuid.UUID{id})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listSpecialties(c *gin.Context, params pagination.Params) ([]specialty, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []specialty
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_specialties',?)", p.TenantID, p.Subject, p.TenantID).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(specialtyQuery(tx, p), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_specialties", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

// QualificationFields contains reported facts, not verified credentials.
type QualificationFields struct {
	Degree         string  `json:"degree"`
	AwardingBody   string  `json:"awarding_body"`
	CompletionYear *int64  `json:"completion_year"`
	SpecialtyID    *string `json:"specialty_id"`
}

func (f QualificationFields) valid() bool {
	if !validName(f.Degree) || !validName(f.AwardingBody) || f.CompletionYear != nil && (*f.CompletionYear < 1 || *f.CompletionYear > 9999) {
		return false
	}
	if f.SpecialtyID != nil {
		_, valid := canonicalStewardReference(*f.SpecialtyID)
		return valid
	}
	return true
}
func (f QualificationFields) equals(other QualificationFields) bool {
	years := f.CompletionYear == nil && other.CompletionYear == nil || f.CompletionYear != nil && other.CompletionYear != nil && *f.CompletionYear == *other.CompletionYear
	return f.Degree == other.Degree && f.AwardingBody == other.AwardingBody && years && sameOptionalString(f.SpecialtyID, other.SpecialtyID)
}

type qualificationRevision struct {
	ID                  uuid.UUID `json:"id"`
	PractitionerID      uuid.UUID `json:"practitioner_id"`
	QualificationID     uuid.UUID `json:"qualification_id"`
	ExpectedVersion     int64     `json:"expected_version"`
	Version             int64     `json:"version"`
	QualificationFields `gorm:"embedded"`
	VerificationState   string    `json:"verification_state"`
	CreatedAt           time.Time `json:"created_at"`
}

func (q *qualificationRevision) AfterFind(*gorm.DB) error {
	q.CreatedAt = q.CreatedAt.UTC()
	return nil
}
func (q qualificationRevision) PageKey() (time.Time, uuid.UUID) { return q.CreatedAt, q.ID }

const qualificationRevisionColumns = "id,practitioner_id,qualification_id,expected_version,version,degree,awarding_body,completion_year,specialty_id::text AS specialty_id,verification_state,created_at"

func qualificationRevisionQuery(tx *gorm.DB, p authn.Principal, owner, id uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_qualification_revisions").Select(qualificationRevisionColumns).Where("tenant_id=? AND practitioner_id=? AND qualification_id=?", p.TenantID, owner, id)
}
func (h *handlers) qualificationCommand(c *gin.Context, correction bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	id, ok := resourceID(c, "qualification")
	if !ok {
		return
	}
	command := id
	if correction {
		command, ok = resourceID(c, "revision")
		if !ok {
			return
		}
	}
	var fields QualificationFields
	var expected int64
	if correction {
		var in *struct {
			ExpectedVersion int64 `json:"expected_version"`
			QualificationFields
		}
		if !decode(c, &in) {
			return
		}
		if in == nil || in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740990 || !in.valid() {
			respond.Error(c, 400, "invalid_request", "Expected a bounded complete qualification correction")
			return
		}
		fields, expected = in.QualificationFields, in.ExpectedVersion
	} else {
		var in *QualificationFields
		if !decode(c, &in) {
			return
		}
		if in == nil || !in.valid() {
			respond.Error(c, 400, "invalid_request", "Expected bounded reported qualification facts")
			return
		}
		fields = *in
	}
	var row qualificationRevision
	created := false
	err := h.transaction(c.Request.Context(), p, true, func(tx *gorm.DB) error {
		if correction {
			var head struct{ Version int64 }
			if err := tx.Raw("SELECT version FROM practitioner_qualifications WHERE tenant_id=? AND practitioner_id=? AND id=? FOR UPDATE", p.TenantID, owner, id).Scan(&head).Error; err != nil {
				return err
			}
			if head.Version == 0 {
				return gorm.ErrRecordNotFound
			}
			err := qualificationRevisionQuery(tx, p, owner, id).Where("id=?", command).Take(&row).Error
			if err == nil {
				if row.ExpectedVersion != expected || !row.equals(fields) {
					return errIntentConflict
				}
				return auditIDs(tx, p, "practitioner_qualification_revisions", []uuid.UUID{command})
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if head.Version != expected {
				return errIntentConflict
			}
		} else {
			var parent record
			if err := tx.Select(recordColumns).Where("tenant_id=? AND id=?", p.TenantID, owner).Take(&parent).Error; err != nil {
				return err
			}
			res := tx.Exec("INSERT INTO practitioner_qualifications(tenant_id,practitioner_id,id,created_by) VALUES(?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING", p.TenantID, owner, id, p.Subject)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				if err := qualificationRevisionQuery(tx, p, owner, id).Where("id=?", id).Take(&row).Error; err != nil {
					return err
				}
				if !row.equals(fields) {
					return errIntentConflict
				}
				return auditIDs(tx, p, "practitioner_qualification_revisions", []uuid.UUID{id})
			}
		}
		if fields.SpecialtyID != nil {
			var catalog specialty
			if err := specialtyQuery(tx, p).Where("id=?", *fields.SpecialtyID).Take(&catalog).Error; err != nil {
				return err
			}
			if !catalog.Active {
				return errIntentConflict
			}
		}
		if err := tx.Exec("INSERT INTO practitioner_qualification_revisions(tenant_id,practitioner_id,qualification_id,id,expected_version,version,degree,awarding_body,completion_year,specialty_id,created_by) VALUES(?,?,?,?,?,?,?,?,?,?,?)", p.TenantID, owner, id, command, expected, expected+1, fields.Degree, fields.AwardingBody, fields.CompletionYear, fields.SpecialtyID, p.Subject).Error; err != nil {
			return err
		}
		if correction {
			result := tx.Exec("UPDATE practitioner_qualifications SET version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND practitioner_id=? AND id=? AND version=?", p.TenantID, owner, id, expected)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errIntentConflict
			}
		}
		created = true
		return qualificationRevisionQuery(tx, p, owner, id).Where("id=?", command).Take(&row).Error
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
func (h *handlers) putQualification(c *gin.Context)     { h.qualificationCommand(c, false) }
func (h *handlers) correctQualification(c *gin.Context) { h.qualificationCommand(c, true) }

type qualificationHead struct {
	ID                  uuid.UUID `json:"id"`
	RevisionID          uuid.UUID `json:"revision_id"`
	Version             int64     `json:"version"`
	QualificationFields `gorm:"embedded"`
	VerificationState   string    `json:"verification_state"`
	CreatedAt           time.Time `json:"created_at"`
}

func (q *qualificationHead) AfterFind(*gorm.DB) error       { q.CreatedAt = q.CreatedAt.UTC(); return nil }
func (q qualificationHead) PageKey() (time.Time, uuid.UUID) { return q.CreatedAt, q.ID }
func qualificationHeadQuery(tx *gorm.DB, p authn.Principal, owner uuid.UUID) *gorm.DB {
	return tx.Table("(SELECT h.id,v.id AS revision_id,v.version,v.degree,v.awarding_body,v.completion_year,v.specialty_id::text AS specialty_id,v.verification_state,h.created_at FROM practitioner_qualifications h JOIN practitioner_qualification_revisions v ON v.tenant_id=h.tenant_id AND v.practitioner_id=h.practitioner_id AND v.qualification_id=h.id AND v.version=h.version WHERE h.tenant_id=? AND h.practitioner_id=?) AS qualification_heads", p.TenantID, owner)
}
func (h *handlers) getQualification(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	id, ok := resourceID(c, "qualification")
	if !ok {
		return
	}
	var row qualificationHead
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := qualificationHeadQuery(tx, p, owner).Where("id=?", id).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_qualification_revisions", []uuid.UUID{row.RevisionID})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) getQualificationRevision(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return
	}
	id, ok := resourceID(c, "qualification")
	if !ok {
		return
	}
	command, ok := resourceID(c, "revision")
	if !ok {
		return
	}
	var row qualificationRevision
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := qualificationRevisionQuery(tx, p, owner, id).Where("id=?", command).Take(&row).Error; err != nil {
			return err
		}
		return auditIDs(tx, p, "practitioner_qualification_revisions", []uuid.UUID{row.ID})
	})
	if err != nil {
		failure(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listQualifications(c *gin.Context, params pagination.Params) ([]qualificationHead, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return nil, nil
	}
	var rows []qualificationHead
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var parent record
		if err := tx.Select(recordColumns).Where("tenant_id=? AND id=?", p.TenantID, owner).Take(&parent).Error; err != nil {
			return err
		}
		if err := auditRead(tx, p, owner); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(qualificationHeadQuery(tx, p, owner), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.RevisionID
		}
		return auditIDs(tx, p, "practitioner_qualification_revisions", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) listQualificationRevisions(c *gin.Context, params pagination.Params) ([]qualificationRevision, error) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	owner, ok := resourceID(c, "practitioner")
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "qualification")
	if !ok {
		return nil, nil
	}
	var rows []qualificationRevision
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		var head qualificationHead
		if err := qualificationHeadQuery(tx, p, owner).Where("id=?", id).Take(&head).Error; err != nil {
			return err
		}
		if err := auditIDs(tx, p, "practitioner_qualifications", []uuid.UUID{id}); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(qualificationRevisionQuery(tx, p, owner, id), params).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return auditIDs(tx, p, "practitioner_qualification_revisions", ids)
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}
