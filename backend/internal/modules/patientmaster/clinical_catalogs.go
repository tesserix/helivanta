package patientmaster

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r sharingNotice) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

type clinicalRecipientChoice struct {
	ID                    uuid.UUID `json:"id"`
	HospitalID            uuid.UUID `json:"hospital_id"`
	OrganizationReference string    `json:"organization_reference"`
	DisplayName           string    `json:"display_name"`
	Version               int64     `json:"version"`
	ValidUntil            time.Time `json:"valid_until"`
	CreatedAt             time.Time `json:"created_at"`
}

func (r clinicalRecipientChoice) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func clinicalCatalogScope(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string) error {
	if !validReviewUUID(id, true) {
		return gorm.ErrRecordNotFound
	}
	if err := clinicalScope(tx, p, hospital, id); err != nil {
		return err
	}
	var authority consentAuthority
	if err := tx.Select(consentAuthorityColumns+",registration_updated_at").Where("tenant_id=? AND hospital_id=? AND id=? AND subject=? AND status='accepted' AND version=2 AND valid_until>clock_timestamp()", p.TenantID, hospital, id, p.Subject).Take(&authority).Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Table("patients").Where("tenant_id=? AND hospital_id=? AND id=? AND updated_at=?", p.TenantID, hospital, authority.PatientID, authority.RegistrationUpdatedAt).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return gorm.ErrRecordNotFound
	}
	return consentAuthorityReadAudit(tx, p, authority)
}
func clinicalCatalogAudit(tx *gorm.DB, p authn.Principal, kind string, ids []uuid.UUID) error {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read',?,value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, kind, string(encoded)).Error
}
func (h *patientHandlers) listClinicalNotices(c *gin.Context, params pagination.Params) ([]sharingNotice, error) {
	return h.listClinicalNoticesFor(c, params, false)
}
func (h *patientHandlers) listProxyClinicalNotices(c *gin.Context, params pagination.Params) ([]sharingNotice, error) {
	return h.listClinicalNoticesFor(c, params, true)
}
func (h *patientHandlers) listClinicalNoticesFor(c *gin.Context, params pagination.Params, proxy bool) ([]sharingNotice, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []sharingNotice
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		capacity, err := clinicalCatalogCapacity(tx, p, hospital, c.Param("authority"), proxy)
		if err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Select(sharingNoticeColumns).Where("tenant_id=? AND hospital_id=? AND status='approved' AND purpose='care_coordination' AND authority_kind=?", p.TenantID, hospital, capacity).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return clinicalCatalogAudit(tx, p, "patient_sharing_notices", ids)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *patientHandlers) listClinicalRecipients(c *gin.Context, params pagination.Params) ([]clinicalRecipientChoice, error) {
	return h.listClinicalRecipientsFor(c, params, false)
}
func (h *patientHandlers) listProxyClinicalRecipients(c *gin.Context, params pagination.Params) ([]clinicalRecipientChoice, error) {
	return h.listClinicalRecipientsFor(c, params, true)
}
func (h *patientHandlers) listClinicalRecipientsFor(c *gin.Context, params pagination.Params, proxy bool) ([]clinicalRecipientChoice, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []clinicalRecipientChoice
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if _, err := clinicalCatalogCapacity(tx, p, hospital, c.Param("authority"), proxy); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx.Table("patient_sharing_recipients"), params).Select("id,hospital_id,organization_reference,display_name,version,valid_until,created_at").Where("tenant_id=? AND hospital_id=? AND status='approved' AND valid_until>clock_timestamp()", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return clinicalCatalogAudit(tx, p, "patient_sharing_recipients", ids)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return nil, nil
	}
	return rows, nil
}

type clinicalConsentSummary struct {
	ID            uuid.UUID  `json:"id"`
	HospitalID    uuid.UUID  `json:"hospital_id"`
	Status        string     `json:"status"`
	Version       int64      `json:"version"`
	ValidUntil    time.Time  `json:"valid_until"`
	CapturedAt    time.Time  `json:"captured_at"`
	CreatedAt     time.Time  `json:"created_at"`
	WithdrawnAt   *time.Time `json:"withdrawn_at"`
	QuarantinedAt *time.Time `json:"quarantined_at"`
}

func (r clinicalConsentSummary) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (h *patientHandlers) listMyClinicalConsents(c *gin.Context, params pagination.Params) ([]clinicalConsentSummary, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []clinicalConsentSummary
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := clinicalScope(tx, p, hospital, ""); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx.Table("patient_clinical_consents"), params).Select("id,hospital_id,status,version,valid_until,captured_at,created_at,withdrawn_at,quarantined_at").Where("tenant_id=? AND hospital_id=? AND subject=?", p.TenantID, hospital, p.Subject).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return clinicalCatalogAudit(tx, p, "patient_clinical_consents", ids)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return nil, nil
	}
	return rows, nil
}

func clinicalCatalogCapacity(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, proxy bool) (string, error) {
	if !proxy {
		return "", clinicalCatalogScope(tx, p, hospital, id)
	}
	if !validReviewUUID(id, true) {
		return "", gorm.ErrRecordNotFound
	}
	if err := clinicalScope(tx, p, hospital, id); err != nil {
		return "", err
	}
	authority, err := clinicalSourceAuthority(tx, p, hospital, id, true)
	return authority.Kind, err
}
