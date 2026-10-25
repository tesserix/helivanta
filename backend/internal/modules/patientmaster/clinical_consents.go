package patientmaster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

var errClinicalConflict = errors.New("clinical declaration state or scope changed")
var errClinicalUnavailable = errors.New("clinical consent dependency unavailable")
var errClinicalIntegrity = errors.New("clinical declaration integrity failed")

type clinicalGrantInput struct {
	AuthorityID              string    `json:"authority_id"`
	ExpectedAuthorityVersion int64     `json:"expected_authority_version"`
	NoticeID                 string    `json:"notice_id"`
	ExpectedNoticeVersion    int64     `json:"expected_notice_version"`
	NoticeHash               string    `json:"notice_hash"`
	RecipientID              string    `json:"recipient_id"`
	ExpectedRecipientVersion int64     `json:"expected_recipient_version"`
	Purpose                  string    `json:"purpose"`
	ResourceTypes            []string  `json:"resource_types"`
	ValidUntil               time.Time `json:"valid_until"`
	Affirmed                 *bool     `json:"affirmed"`
}

func (in clinicalGrantInput) valid() bool {
	if !validReviewUUID(in.AuthorityID, true) || !validReviewUUID(in.NoticeID, true) || !validReviewUUID(in.RecipientID, true) || in.ExpectedAuthorityVersion != 2 || in.ExpectedNoticeVersion != 2 || in.ExpectedRecipientVersion != 2 || in.Purpose != "care_coordination" || in.Affirmed == nil || !*in.Affirmed || in.ValidUntil.IsZero() || in.ValidUntil.Year() > 9999 || in.ValidUntil.Nanosecond()%1000 != 0 || len(in.NoticeHash) != 64 || len(in.ResourceTypes) < 1 || len(in.ResourceTypes) > 3 || !slices.IsSorted(in.ResourceTypes) {
		return false
	}
	for i, kind := range in.ResourceTypes {
		if kind != "diagnostic_report" && kind != "prescription" && kind != "discharge_summary" {
			return false
		}
		if i > 0 && kind == in.ResourceTypes[i-1] {
			return false
		}
	}
	return true
}

type clinicalConsentBody struct {
	SchemaVersion         int                     `json:"schema_version"`
	ID                    string                  `json:"id"`
	TenantID              string                  `json:"tenant_id"`
	HospitalID            string                  `json:"hospital_id"`
	PatientID             string                  `json:"patient_id"`
	Subject               string                  `json:"subject"`
	AuthorityID           string                  `json:"authority_id"`
	AuthorityVersion      int64                   `json:"authority_version"`
	ReviewRequestID       string                  `json:"review_request_id"`
	DecisionID            string                  `json:"decision_id"`
	RegistrationUpdatedAt time.Time               `json:"registration_updated_at"`
	NoticeID              string                  `json:"notice_id"`
	NoticeVersion         int64                   `json:"notice_version"`
	NoticeHash            string                  `json:"notice_hash"`
	Notice                SharingNoticeContent    `json:"notice"`
	RecipientID           string                  `json:"recipient_id"`
	RecipientVersion      int64                   `json:"recipient_version"`
	Recipient             SharingRecipientContent `json:"recipient"`
	Purpose               string                  `json:"purpose"`
	ResourceTypes         []string                `json:"resource_types"`
	ValidUntil            time.Time               `json:"valid_until"`
	CapturedAt            time.Time               `json:"captured_at"`
	Fingerprint           string                  `json:"fingerprint"`
}
type clinicalConsent struct {
	ID              uuid.UUID
	HospitalID      uuid.UUID
	PatientID       uuid.UUID
	Subject         string
	AuthorityID     uuid.UUID
	ReviewRequestID uuid.UUID
	NoticeID        uuid.UUID
	RecipientID     uuid.UUID
	Purpose         string
	Fingerprint     string
	Body            string
	HMAC            string
	Status          string
	Version         int64
	ValidUntil      time.Time
	CapturedAt      time.Time
	WithdrawnAt     *time.Time
	WithdrawnBy     *string
	QuarantinedAt   *time.Time
	QuarantinedBy   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (clinicalConsent) TableName() string { return "patient_clinical_consents" }

const clinicalConsentColumns = "id,hospital_id,patient_id,subject,authority_id,review_request_id,notice_id,recipient_id,purpose,fingerprint,body,hmac,status,version,valid_until,captured_at,withdrawn_at,withdrawn_by,quarantined_at,quarantined_by,created_at,updated_at"

func clinicalKey(tenant string) string { return "helivanta-consent-" + tenant }
func clinicalScope(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, authority string) error {
	return tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.patient_consent_subject',?,true),set_config('app.patient_clinical_recipient',?,true),set_config('app.patient_hospital',?,true),set_config('app.patient_clinical_authority',?,true)`, p.Subject, p.Subject, p.Subject, hospital.String(), authority).Error
}
func readOwnClinicalConsent(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, row *clinicalConsent) error {
	return tx.Select(clinicalConsentColumns).Where("tenant_id=? AND hospital_id=? AND id=? AND subject=?", p.TenantID, hospital, id, p.Subject).Take(row).Error
}
func clinicalReadAudit(tx *gorm.DB, p authn.Principal, row clinicalConsent) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','patient_clinical_consents',?)`, p.TenantID, p.Subject, row.ID).Error
}
func clinicalConsentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errClinicalConflict):
		respond.Error(c, 409, "clinical_consent_conflict", "Clinical declaration scope or state changed")
	case errors.Is(err, errClinicalIntegrity):
		respond.Error(c, 409, "clinical_consent_integrity_failed", "Clinical declaration unavailable due to an integrity incident")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "clinical consent")
	default:
		respond.Error(c, 503, "clinical_consent_unavailable", "Clinical consent unavailable; reconcile submitted commands before retrying")
	}
}
func (h *patientHandlers) clinicalSnapshot(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, in clinicalGrantInput, fingerprint string) (clinicalConsentBody, error) {
	var body clinicalConsentBody
	if err := clinicalScope(tx, p, hospital, in.AuthorityID); err != nil {
		return body, err
	}
	var authority consentAuthority
	if err := tx.Select(consentAuthorityColumns+",registration_updated_at").Where("tenant_id=? AND hospital_id=? AND id=? AND subject=?", p.TenantID, hospital, in.AuthorityID, p.Subject).Take(&authority).Error; err != nil {
		return body, err
	}
	var now time.Time
	if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
		return body, err
	}
	if authority.Status != "accepted" || authority.Version != in.ExpectedAuthorityVersion || !authority.ValidUntil.After(now) || !in.ValidUntil.After(now) || in.ValidUntil.After(authority.ValidUntil) {
		return body, errClinicalConflict
	}
	var matches int64
	if err := tx.Table("patients").Where("tenant_id=? AND hospital_id=? AND id=? AND updated_at=?", p.TenantID, hospital, authority.PatientID, authority.RegistrationUpdatedAt).Count(&matches).Error; err != nil {
		return body, err
	}
	if matches != 1 {
		return body, errClinicalConflict
	}
	var notice sharingNotice
	if err := readSharingNotice(tx, p, hospital, in.NoticeID, &notice); err != nil {
		return body, err
	}
	if notice.Status != "approved" || notice.Version != in.ExpectedNoticeVersion || notice.ContentHash != in.NoticeHash || notice.Purpose != in.Purpose {
		return body, errClinicalConflict
	}
	encodedNotice, err := json.Marshal(notice.SharingNoticeContent)
	if err != nil {
		return body, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(encodedNotice)) != notice.ContentHash {
		return body, errClinicalIntegrity
	}
	var recipient sharingRecipient
	if err := readSharingRecipient(tx, p, hospital, in.RecipientID, &recipient); err != nil {
		return body, err
	}
	if recipient.Status != "approved" || recipient.Version != in.ExpectedRecipientVersion || !recipient.ValidUntil.After(now) || in.ValidUntil.After(recipient.ValidUntil) {
		return body, errClinicalConflict
	}
	encodedRecipient, err := json.Marshal(recipient.SharingRecipientContent)
	if err != nil {
		return body, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(encodedRecipient)) != recipient.ContentHash {
		return body, errClinicalIntegrity
	}
	body = clinicalConsentBody{SchemaVersion: 1, ID: id, TenantID: p.TenantID, HospitalID: hospital.String(), PatientID: authority.PatientID.String(), Subject: p.Subject, AuthorityID: in.AuthorityID, AuthorityVersion: authority.Version, ReviewRequestID: authority.ReviewRequestID.String(), DecisionID: authority.DecisionID.String(), RegistrationUpdatedAt: authority.RegistrationUpdatedAt.UTC(), NoticeID: in.NoticeID, NoticeVersion: notice.Version, NoticeHash: notice.ContentHash, Notice: notice.SharingNoticeContent, RecipientID: in.RecipientID, RecipientVersion: recipient.Version, Recipient: recipient.SharingRecipientContent, Purpose: in.Purpose, ResourceTypes: in.ResourceTypes, ValidUntil: in.ValidUntil, CapturedAt: now.UTC(), Fingerprint: fingerprint}
	return body, nil
}
func decodeClinicalConsent(row clinicalConsent, p authn.Principal) (clinicalConsentBody, error) {
	var body clinicalConsentBody
	decoder := json.NewDecoder(bytes.NewBufferString(row.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.SchemaVersion != 1 || body.ID != row.ID.String() || body.TenantID != p.TenantID || body.HospitalID != row.HospitalID.String() || body.PatientID != row.PatientID.String() || body.Subject != row.Subject || body.AuthorityID != row.AuthorityID.String() || body.ReviewRequestID != row.ReviewRequestID.String() || body.NoticeID != row.NoticeID.String() || body.RecipientID != row.RecipientID.String() || body.Purpose != row.Purpose || body.Fingerprint != row.Fingerprint || !body.ValidUntil.Equal(row.ValidUntil) || !body.CapturedAt.Equal(row.CapturedAt) {
		return body, errClinicalIntegrity
	}
	return body, nil
}
func (h *patientHandlers) verifyClinicalConsent(ctx context.Context, p authn.Principal, row clinicalConsent) (clinicalConsentBody, error) {
	body, err := decodeClinicalConsent(row, p)
	if err != nil {
		return body, err
	}
	if h.transit == nil {
		return body, errClinicalUnavailable
	}
	valid, err := h.transit.VerifyHMAC(ctx, clinicalKey(p.TenantID), []byte(row.Body), row.HMAC)
	if err != nil {
		return body, errClinicalUnavailable
	}
	if !valid {
		return body, errClinicalIntegrity
	}
	return body, nil
}
func clinicalReceipt(row clinicalConsent, body clinicalConsentBody) gin.H {
	return gin.H{"id": row.ID, "hospital_id": row.HospitalID, "patient_id": row.PatientID, "authority_id": row.AuthorityID, "notice_id": row.NoticeID, "notice_hash": body.NoticeHash, "notice": body.Notice, "recipient": gin.H{"id": row.RecipientID, "organization_reference": body.Recipient.OrganizationReference, "display_name": body.Recipient.DisplayName}, "purpose": row.Purpose, "resource_types": body.ResourceTypes, "valid_until": row.ValidUntil, "captured_at": row.CapturedAt, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt, "status": row.Status, "version": row.Version, "withdrawn_at": row.WithdrawnAt, "quarantined_at": row.QuarantinedAt, "hmac": row.HMAC, "integrity_algorithm": "HMAC-SHA-256", "key_version": 1, "hmac_scope": "immutable_declaration", "eligibility": "requires_fresh_consent_and_record_owner_checks"}
}
func (h *patientHandlers) publishClinicalConsent(tx *gorm.DB, p authn.Principal, row clinicalConsent) error {
	data, err := json.Marshal(patientmastercontract.ClinicalConsentChangedData{ConsentID: row.ID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, patientmastercontract.SubjectClinicalConsentChanged, events.Event{Type: patientmastercontract.SubjectClinicalConsentChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
func (h *patientHandlers) putClinicalConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id := c.Param("consent")
	if !validReviewUUID(id, true) {
		respond.NotFound(c, "clinical consent")
		return
	}
	var in clinicalGrantInput
	if !decodePatientInput(c, &in) {
		return
	}
	in.ValidUntil = in.ValidUntil.UTC()
	if !in.valid() {
		respond.BadRequest(c, errors.New("invalid clinical declaration"))
		return
	}
	encoded, err := json.Marshal(struct {
		Tenant, Hospital, Subject string
		Input                     clinicalGrantInput
	}{p.TenantID, hospital.String(), p.Subject, in})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	var row clinicalConsent
	var body clinicalConsentBody
	existing := false
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := clinicalScope(tx, p, hospital, in.AuthorityID); err != nil {
			return err
		}
		err := readOwnClinicalConsent(tx, p, hospital, id, &row)
		if err == nil {
			if row.Fingerprint != fingerprint {
				return errClinicalConflict
			}
			existing = true
			return clinicalReadAudit(tx, p, row)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var snapshotErr error
		body, snapshotErr = h.clinicalSnapshot(tx, p, hospital, id, in, fingerprint)
		if snapshotErr != nil {
			return snapshotErr
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'clinical_declaration_prepared','patient_consent_authorities',?)`, p.TenantID, p.Subject, in.AuthorityID).Error
	})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	if existing {
		verified, err := h.verifiedClinicalConsent(ctx, p, row)
		if err != nil {
			clinicalConsentError(c, err)
			return
		}
		respond.OK(c, clinicalReceipt(row, verified))
		return
	}
	if h.transit == nil || h.claims == nil {
		clinicalConsentError(c, errClinicalUnavailable)
		return
	}
	payload, err := json.Marshal(body)
	if err != nil || len(payload) > 16384 {
		clinicalConsentError(c, errClinicalUnavailable)
		return
	}
	if err := h.transit.EnsureHMACKey(ctx, clinicalKey(p.TenantID)); err != nil {
		clinicalConsentError(c, errClinicalUnavailable)
		return
	}
	signature, err := h.transit.HMAC(ctx, clinicalKey(p.TenantID), payload)
	if err != nil {
		clinicalConsentError(c, errClinicalUnavailable)
		return
	}
	current, err := h.claims.Current(ctx, mediconnectcontract.ClaimCheckRequest{TenantID: p.TenantID, RequestID: body.ReviewRequestID, HospitalID: body.HospitalID, PatientID: body.PatientID, Subject: p.Subject})
	if err != nil {
		clinicalConsentError(c, errClinicalUnavailable)
		return
	}
	if !current {
		clinicalConsentError(c, errClinicalConflict)
		return
	}
	created := false
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		checked, err := h.clinicalSnapshot(tx, p, hospital, id, in, fingerprint)
		if err != nil {
			return err
		}
		checked.CapturedAt = body.CapturedAt
		comparable, err := json.Marshal(checked)
		if err != nil {
			return err
		}
		if !bytes.Equal(comparable, payload) {
			return errClinicalConflict
		}
		result := tx.Exec(`INSERT INTO patient_clinical_consents(tenant_id,hospital_id,id,patient_id,subject,authority_id,review_request_id,notice_id,recipient_id,purpose,fingerprint,body,hmac,valid_until,captured_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, p.TenantID, hospital, id, body.PatientID, p.Subject, in.AuthorityID, body.ReviewRequestID, in.NoticeID, in.RecipientID, in.Purpose, fingerprint, string(payload), signature, in.ValidUntil, body.CapturedAt)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readOwnClinicalConsent(tx, p, hospital, id, &row); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errClinicalConflict
			}
			return err
		}
		if row.Fingerprint != fingerprint {
			return errClinicalConflict
		}
		if created {
			return h.publishClinicalConsent(tx, p, row)
		}
		return clinicalReadAudit(tx, p, row)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	if !created {
		body, err = h.verifiedClinicalConsent(ctx, p, row)
		if err != nil {
			clinicalConsentError(c, err)
			return
		}
	}
	if created {
		respond.Created(c, clinicalReceipt(row, body))
	} else {
		respond.OK(c, clinicalReceipt(row, body))
	}
}

func (h *patientHandlers) getClinicalConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("consent"), true) {
		respond.NotFound(c, "clinical consent")
		return
	}
	var row clinicalConsent
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := clinicalScope(tx, p, hospital, ""); err != nil {
			return err
		}
		if err := readOwnClinicalConsent(tx, p, hospital, c.Param("consent"), &row); err != nil {
			return err
		}
		return clinicalReadAudit(tx, p, row)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	body, err := h.verifiedClinicalConsent(c.Request.Context(), p, row)
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	respond.OK(c, clinicalReceipt(row, body))
}

func (h *patientHandlers) withdrawClinicalConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("consent"), true) {
		respond.NotFound(c, "clinical consent")
		return
	}
	var in struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decodePatientInput(c, &in) {
		return
	}
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 3 {
		respond.BadRequest(c, errors.New("invalid clinical withdrawal"))
		return
	}
	var row clinicalConsent
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := clinicalScope(tx, p, hospital, ""); err != nil {
			return err
		}
		if err := readOwnClinicalConsent(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, hospital, c.Param("consent"), &row); err != nil {
			return err
		}
		if in.ExpectedVersion > row.Version {
			return errClinicalConflict
		}
		if row.WithdrawnAt != nil {
			return clinicalReadAudit(tx, p, row)
		}
		status := "withdrawn"
		if row.Status == "quarantined" {
			status = "quarantined"
		}
		result := tx.Exec(`UPDATE patient_clinical_consents SET status=?,version=version+1,withdrawn_at=clock_timestamp(),withdrawn_by=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND subject=? AND version=? AND withdrawn_at IS NULL`, status, p.Subject, p.TenantID, hospital, row.ID, p.Subject, row.Version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errClinicalConflict
		}
		if err := readOwnClinicalConsent(tx, p, hospital, c.Param("consent"), &row); err != nil {
			return err
		}
		return h.publishClinicalConsent(tx, p, row)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	respond.OK(c, gin.H{"id": row.ID, "hospital_id": row.HospitalID, "status": row.Status, "version": row.Version, "withdrawal_recorded": row.WithdrawnAt != nil, "withdrawn_at": row.WithdrawnAt, "quarantined_at": row.QuarantinedAt})
}

func recipientClinicalScope(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string) error {
	if err := clinicalScope(tx, p, hospital, ""); err != nil {
		return err
	}
	return tx.Exec(`SELECT set_config('app.patient_clinical_consent',?,true)`, id).Error
}
func readRecipientClinicalConsent(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, id string, row *clinicalConsent) error {
	return tx.Select(clinicalConsentColumns).Where(`tenant_id=? AND hospital_id=? AND id=? AND EXISTS(SELECT 1 FROM patient_sharing_recipients r WHERE r.tenant_id=patient_clinical_consents.tenant_id AND r.hospital_id=patient_clinical_consents.hospital_id AND r.id=patient_clinical_consents.recipient_id AND r.grantee_subject=?)`, p.TenantID, hospital, id, p.Subject).Take(row).Error
}
func clinicalRecipientState(tx *gorm.DB, p authn.Principal, row clinicalConsent, body clinicalConsentBody) (bool, error) {
	var count int64
	err := tx.Raw(`SELECT count(*) FROM patient_consent_authorities a
 JOIN patient_link_reviews r ON r.tenant_id=a.tenant_id AND r.request_id=a.review_request_id
 JOIN patients p ON p.tenant_id=a.tenant_id AND p.hospital_id=a.hospital_id AND p.id=a.patient_id
 JOIN patient_sharing_notices n ON n.tenant_id=a.tenant_id AND n.hospital_id=a.hospital_id AND n.id=?
 JOIN patient_sharing_recipients g ON g.tenant_id=a.tenant_id AND g.hospital_id=a.hospital_id AND g.id=?
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.id=? AND a.subject=? AND a.patient_id=?
 AND a.status='accepted' AND a.version=? AND a.valid_until>clock_timestamp()
 AND a.decision_id=? AND a.registration_updated_at=? AND p.updated_at=a.registration_updated_at
 AND r.status='approved' AND r.decision_id=a.decision_id AND r.relationship='self'
 AND r.confirmed_patient_id=a.patient_id AND r.requester_subject=a.subject AND r.registration_updated_at=a.registration_updated_at
 AND n.status='approved' AND n.version=? AND n.content_hash=?
 AND g.status='approved' AND g.version=? AND g.grantee_subject=? AND g.valid_until>clock_timestamp()`, row.NoticeID, row.RecipientID, p.TenantID, row.HospitalID, row.AuthorityID, row.Subject, row.PatientID, body.AuthorityVersion, body.DecisionID, body.RegistrationUpdatedAt, body.NoticeVersion, body.NoticeHash, body.RecipientVersion, p.Subject).Scan(&count).Error
	return count == 1, err
}
func (h *patientHandlers) decideClinicalConsent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id := c.Param("consent")
	if !validReviewUUID(id, true) {
		respond.NotFound(c, "clinical consent")
		return
	}
	var in struct {
		Purpose      string `json:"purpose"`
		ResourceType string `json:"resource_type"`
	}
	if !decodePatientInput(c, &in) {
		return
	}
	if in.Purpose != "care_coordination" || (in.ResourceType != "diagnostic_report" && in.ResourceType != "prescription" && in.ResourceType != "discharge_summary") {
		respond.BadRequest(c, errors.New("invalid consent decision scope"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	var row clinicalConsent
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := recipientClinicalScope(tx, p, hospital, id); err != nil {
			return err
		}
		if err := readRecipientClinicalConsent(tx, p, hospital, id, &row); err != nil {
			return err
		}
		return clinicalReadAudit(tx, p, row)
	})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	reason := "inactive"
	var body clinicalConsentBody
	if row.Status == "granted" {
		body, err = h.verifiedClinicalConsent(ctx, p, row)
		if err != nil {
			clinicalConsentError(c, err)
			return
		}
		if in.Purpose != body.Purpose || !slices.Contains(body.ResourceTypes, in.ResourceType) {
			reason = "scope_mismatch"
		} else {
			if h.claims == nil {
				clinicalConsentError(c, errClinicalUnavailable)
				return
			}
			current, err := h.claims.Current(ctx, mediconnectcontract.ClaimCheckRequest{TenantID: p.TenantID, RequestID: body.ReviewRequestID, HospitalID: body.HospitalID, PatientID: body.PatientID, Subject: body.Subject})
			if err != nil {
				clinicalConsentError(c, errClinicalUnavailable)
				return
			}
			reason = "claim_inactive"
			if current {
				reason = "eligible"
			}
		}
	}
	decisionID, err := uuid.NewV7()
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := recipientClinicalScope(tx, p, hospital, id); err != nil {
			return err
		}
		var checked clinicalConsent
		if err := readRecipientClinicalConsent(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, hospital, id, &checked); err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
			return err
		}
		if checked.Status != "granted" || !checked.ValidUntil.After(now) {
			reason = "inactive"
		} else if checked.Version != row.Version || checked.Body != row.Body || checked.HMAC != row.HMAC {
			return errClinicalConflict
		} else if reason == "eligible" {
			current, err := clinicalRecipientState(tx, p, checked, body)
			if err != nil {
				return err
			}
			if !current {
				reason = "owner_state_changed"
			}
		}
		decision := "deny"
		if reason == "eligible" {
			decision = "allow"
		}
		return tx.Exec(`INSERT INTO patient_clinical_consent_decisions(tenant_id,hospital_id,id,consent_id,consent_version,actor_subject,purpose,resource_type,decision,reason) VALUES (?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, decisionID, checked.ID, checked.Version, p.Subject, in.Purpose, in.ResourceType, decision, reason).Error
	})
	if err != nil {
		clinicalConsentError(c, err)
		return
	}
	decision := "deny"
	if reason == "eligible" {
		decision = "allow"
	}
	respond.OK(c, gin.H{"decision_id": decisionID, "consent_gate": decision, "record_access_authorized": false})
}

func (h *patientHandlers) verifiedClinicalConsent(ctx context.Context, p authn.Principal, row clinicalConsent) (clinicalConsentBody, error) {
	body, err := h.verifyClinicalConsent(ctx, p, row)
	if !errors.Is(err, errClinicalIntegrity) {
		return body, err
	}
	incidentID, idErr := uuid.NewV7()
	if idErr != nil {
		return body, idErr
	}
	incidentErr := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := recipientClinicalScope(tx, p, row.HospitalID, row.ID.String()); err != nil {
			return err
		}
		var checked clinicalConsent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(clinicalConsentColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, row.HospitalID, row.ID).Take(&checked).Error; err != nil {
			return err
		}
		if checked.Status == "quarantined" {
			return clinicalReadAudit(tx, p, checked)
		}
		if checked.Body != row.Body || checked.HMAC != row.HMAC {
			return errClinicalConflict
		}
		if err := tx.Exec(`SELECT set_config('app.patient_clinical_integrity',?,true)`, row.ID.String()).Error; err != nil {
			return err
		}
		observed := fmt.Sprintf("%x", sha256.Sum256([]byte(row.Body+"\n"+row.HMAC)))
		if err := tx.Exec(`INSERT INTO patient_clinical_integrity_incidents(tenant_id,hospital_id,id,consent_id,observed_version,actor_subject,observed_hash) VALUES (?,?,?,?,?,?,?)`, p.TenantID, row.HospitalID, incidentID, row.ID, row.Version, p.Subject, observed).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE patient_clinical_consents SET status='quarantined',version=version+1,quarantined_at=clock_timestamp(),quarantined_by=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=?`, p.Subject, p.TenantID, row.HospitalID, row.ID).Error; err != nil {
			return err
		}
		checked.Version++
		return h.publishClinicalConsent(tx, p, checked)
	})
	if incidentErr != nil {
		return body, incidentErr
	}
	return body, errClinicalIntegrity
}
