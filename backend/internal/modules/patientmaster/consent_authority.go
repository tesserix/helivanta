package patientmaster

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var errConsentAuthorityConflict = errors.New("consent authority state conflicts")

type consentAuthority struct {
	ID                    uuid.UUID  `json:"id"`
	HospitalID            uuid.UUID  `json:"hospital_id"`
	PatientID             uuid.UUID  `json:"patient_id"`
	ReviewRequestID       uuid.UUID  `json:"review_request_id"`
	DecisionID            uuid.UUID  `json:"decision_id"`
	Subject               string     `json:"-"`
	Fingerprint           string     `json:"-"`
	RegistrationUpdatedAt time.Time  `json:"-"`
	Status                string     `json:"status"`
	EffectiveStatus       string     `json:"effective_status" gorm:"->"`
	Version               int64      `json:"version"`
	ValidUntil            time.Time  `json:"valid_until"`
	AcceptedAt            *time.Time `json:"accepted_at"`
	RevokedAt             *time.Time `json:"revoked_at"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (consentAuthority) TableName() string { return "patient_consent_authorities" }

const consentAuthorityColumns = `id,hospital_id,patient_id,review_request_id,decision_id,subject,fingerprint,status,version,valid_until,accepted_at,revoked_at,created_at,updated_at,CASE WHEN status<>'revoked' AND valid_until<=clock_timestamp() THEN 'expired' ELSE status END AS effective_status`

type consentAuthorityProposal struct {
	ReviewRequestID    string    `json:"review_request_id"`
	ExpectedDecisionID string    `json:"expected_decision_id"`
	CapacityConfirmed  *bool     `json:"capacity_confirmed"`
	PolicyReference    string    `json:"policy_reference"`
	EvidenceReference  string    `json:"evidence_reference"`
	ValidUntil         time.Time `json:"valid_until"`
}

func (in consentAuthorityProposal) valid() bool {
	return validReviewUUID(in.ReviewRequestID, true) && validReviewUUID(in.ExpectedDecisionID, true) && validReviewUUID(in.PolicyReference, true) && validReviewUUID(in.EvidenceReference, true) && in.CapacityConfirmed != nil && *in.CapacityConfirmed && !in.ValidUntil.IsZero() && in.ValidUntil.Year() <= 9999 && in.ValidUntil.Nanosecond()%1000 == 0
}
func readConsentAuthority(tx *gorm.DB, tenant string, hospital uuid.UUID, id string, row *consentAuthority, locking bool) error {
	q := tx.Select(consentAuthorityColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id)
	if locking {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q.Take(row).Error
}
func consentAuthorityReadAudit(tx *gorm.DB, p authn.Principal, row consentAuthority) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','patient_consent_authorities',?)`, p.TenantID, p.Subject, row.ID).Error
}
func (h *patientHandlers) publishConsentAuthority(tx *gorm.DB, p authn.Principal, row consentAuthority) error {
	data, err := json.Marshal(patientmastercontract.ConsentAuthorityChangedData{AuthorityID: row.ID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, patientmastercontract.SubjectConsentAuthorityChanged, events.Event{Type: patientmastercontract.SubjectConsentAuthorityChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
func consentAuthorityError(c *gin.Context, err error) {
	if errors.Is(err, errConsentAuthorityConflict) {
		respond.Error(c, 409, "consent_authority_conflict", "Consent authority state or input changed")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		respond.Error(c, 503, "consent_authority_unavailable", "Consent authority unavailable; reconcile a submitted command before retrying")
		return
	}
	patientError(c, err)
}
func (h *patientHandlers) proposeConsentAuthority(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("authority"), true) {
		respond.NotFound(c, "consent authority")
		return
	}
	var in consentAuthorityProposal
	if !decodePatientInput(c, &in) {
		return
	}
	if !in.valid() {
		respond.BadRequest(c, errors.New("invalid consent authority proposal"))
		return
	}
	in.ValidUntil = in.ValidUntil.UTC()
	encoded, err := json.Marshal(in)
	if err != nil {
		patientError(c, err)
		return
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row consentAuthority
	created := false
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		err := readConsentAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false)
		if err == nil {
			if row.Fingerprint != fingerprint {
				return errConsentAuthorityConflict
			}
			return consentAuthorityReadAudit(tx, p, row)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var review linkReview
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("request_id,patient_id,requester_subject,relationship,status,decision_id,registration_updated_at").Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, in.ReviewRequestID).Take(&review).Error; err != nil {
			return err
		}
		if review.Status != "approved" || review.Relationship != "self" || review.RequesterSubject == p.Subject || review.DecisionID == nil || review.DecisionID.String() != in.ExpectedDecisionID || review.ReviewedRegistrationUpdatedAt == nil {
			return errConsentAuthorityConflict
		}
		var registration struct{ UpdatedAt time.Time }
		if err := tx.Table("patients").Select("updated_at").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, review.PatientID).Take(&registration).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
			return err
		}
		if !registration.UpdatedAt.Equal(*review.ReviewedRegistrationUpdatedAt) || !in.ValidUntil.After(now) {
			return errConsentAuthorityConflict
		}
		result := tx.Exec(`INSERT INTO patient_consent_authorities(id,tenant_id,hospital_id,patient_id,review_request_id,decision_id,subject,reviewer_subject,policy_reference,evidence_reference,registration_updated_at,fingerprint,valid_until) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING`, c.Param("authority"), p.TenantID, hospital, review.PatientID, review.RequestID, review.DecisionID, review.RequesterSubject, p.Subject, in.PolicyReference, in.EvidenceReference, registration.UpdatedAt, fingerprint, in.ValidUntil)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := reviewReadAudit(tx, p, []linkReview{review}); err != nil {
			return err
		}
		if err := patientAudit(tx, p, hospital, "read", []patient{{ID: review.PatientID}}); err != nil {
			return err
		}
		if err := readConsentAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false); err != nil {
			return err
		}
		if row.Fingerprint != fingerprint {
			return errConsentAuthorityConflict
		}
		if created {
			if err := h.publishConsentAuthority(tx, p, row); err != nil {
				return err
			}
		}
		return consentAuthorityReadAudit(tx, p, row)
	})
	if err != nil {
		consentAuthorityError(c, err)
		return
	}
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

func (r consentAuthority) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *patientHandlers) consentSubjectTransaction(ctx context.Context, p authn.Principal, hospital uuid.UUID, fn func(*gorm.DB) error) error {
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.patient_consent_subject',?,true),set_config('app.patient_hospital',?,true)`, p.Subject, p.Subject, hospital.String()).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func (h *patientHandlers) getMyConsentAuthority(c *gin.Context)       { h.getConsentAuthority(c, false) }
func (h *patientHandlers) getReviewedConsentAuthority(c *gin.Context) { h.getConsentAuthority(c, true) }
func (h *patientHandlers) getConsentAuthority(c *gin.Context, staff bool) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("authority"), true) {
		respond.NotFound(c, "consent authority")
		return
	}
	var row consentAuthority
	read := func(tx *gorm.DB) error {
		if err := readConsentAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false); err != nil {
			return err
		}
		return consentAuthorityReadAudit(tx, p, row)
	}
	var err error
	if staff {
		err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, read)
	} else {
		err = h.consentSubjectTransaction(c.Request.Context(), p, hospital, read)
	}
	if err != nil {
		consentAuthorityError(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *patientHandlers) listMyConsentAuthorities(c *gin.Context, params pagination.Params) ([]consentAuthority, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []consentAuthority
	err := h.consentSubjectTransaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(consentAuthorityColumns).Where("tenant_id=? AND hospital_id=? AND subject=?", p.TenantID, hospital, p.Subject).Find(&rows).Error; err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read','patient_consent_authorities',value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, string(encoded)).Error
	})
	if err != nil {
		consentAuthorityError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *patientHandlers) acceptConsentAuthority(c *gin.Context) {
	h.changeConsentAuthority(c, true, false)
}
func (h *patientHandlers) revokeMyConsentAuthority(c *gin.Context) {
	h.changeConsentAuthority(c, false, false)
}
func (h *patientHandlers) revokeReviewedConsentAuthority(c *gin.Context) {
	h.changeConsentAuthority(c, false, true)
}
func (h *patientHandlers) changeConsentAuthority(c *gin.Context, accept, staff bool) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("authority"), true) {
		respond.NotFound(c, "consent authority")
		return
	}
	var version int64
	if accept {
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
			Accepted        *bool `json:"accepted"`
		}
		if !decodePatientInput(c, &in) {
			return
		}
		if in.Accepted == nil || !*in.Accepted {
			respond.BadRequest(c, errors.New("explicit acceptance required"))
			return
		}
		version = in.ExpectedVersion
	} else {
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !decodePatientInput(c, &in) {
			return
		}
		version = in.ExpectedVersion
	}
	if version < 1 || version > 3 {
		respond.BadRequest(c, errors.New("invalid expected version"))
		return
	}
	target := "revoked"
	if accept {
		target = "accepted"
	}
	var row consentAuthority
	change := func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		if err := readConsentAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, true); err != nil {
			return err
		}
		if row.Status == target && row.Version == version+1 {
			return consentAuthorityReadAudit(tx, p, row)
		}
		if row.Status == "revoked" || row.Version != version {
			return errConsentAuthorityConflict
		}
		var result *gorm.DB
		if accept {
			if row.Status != "proposed" {
				return errConsentAuthorityConflict
			}
			var review linkReview
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("request_id,status,decision_id").Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, row.ReviewRequestID).Take(&review).Error; err != nil {
				return err
			}
			var now time.Time
			if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
				return err
			}
			if review.Status != "approved" || review.DecisionID == nil || *review.DecisionID != row.DecisionID || !row.ValidUntil.After(now) {
				return errConsentAuthorityConflict
			}
			if err := reviewReadAudit(tx, p, []linkReview{review}); err != nil {
				return err
			}
			result = tx.Exec(`UPDATE patient_consent_authorities SET status='accepted',version=version+1,accepted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND status='proposed' AND version=?`, p.TenantID, hospital, row.ID, version)
		} else {
			result = tx.Exec(`UPDATE patient_consent_authorities SET status='revoked',version=version+1,revoked_at=clock_timestamp(),revoked_by=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND status<>'revoked' AND version=?`, p.Subject, p.TenantID, hospital, row.ID, version)
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errConsentAuthorityConflict
		}
		if err := readConsentAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false); err != nil {
			return err
		}
		if err := h.publishConsentAuthority(tx, p, row); err != nil {
			return err
		}
		return consentAuthorityReadAudit(tx, p, row)
	}
	var err error
	if staff {
		err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, change)
	} else {
		err = h.consentSubjectTransaction(c.Request.Context(), p, hospital, change)
	}
	if err != nil {
		consentAuthorityError(c, err)
		return
	}
	respond.OK(c, row)
}
