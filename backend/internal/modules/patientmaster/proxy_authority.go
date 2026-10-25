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
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var errProxyAuthorityConflict = errors.New("proxy authority state conflicts")

type proxyAuthority struct {
	Kind                   string     `json:"kind"`
	CountryCode            string     `json:"country_code"`
	RecordAccessAuthorized bool       `json:"record_access_authorized" gorm:"-"`
	ParentalEndAt          *time.Time `json:"-"`
	ID                     uuid.UUID  `json:"id"`
	HospitalID             uuid.UUID  `json:"hospital_id"`
	PatientID              uuid.UUID  `json:"patient_id"`
	ReviewRequestID        uuid.UUID  `json:"review_request_id"`
	DecisionID             uuid.UUID  `json:"decision_id"`
	Subject                string     `json:"-"`
	Fingerprint            string     `json:"-"`
	RegistrationUpdatedAt  time.Time  `json:"-"`
	Status                 string     `json:"status"`
	EffectiveStatus        string     `json:"effective_status" gorm:"->"`
	Version                int64      `json:"version"`
	ValidUntil             time.Time  `json:"valid_until"`
	AcceptedAt             *time.Time `json:"accepted_at"`
	RevokedAt              *time.Time `json:"revoked_at"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (proxyAuthority) TableName() string { return "patient_proxy_authorities" }

const proxyAuthorityColumns = `id,hospital_id,kind,country_code,parental_end_at,registration_updated_at,patient_id,review_request_id,decision_id,subject,fingerprint,status,version,valid_until,accepted_at,revoked_at,created_at,updated_at,CASE WHEN status<>'revoked' AND valid_until<=clock_timestamp() THEN 'expired' ELSE status END AS effective_status`

type proxyAuthorityProposal struct {
	Kind                       string    `json:"kind"`
	CountryCode                string    `json:"country_code"`
	ReviewRequestID            string    `json:"review_request_id"`
	ExpectedDecisionID         string    `json:"expected_decision_id"`
	PolicyReference            string    `json:"policy_reference"`
	IdentityAgeReference       string    `json:"identity_age_reference"`
	AuthorityEvidenceReference string    `json:"authority_evidence_reference"`
	AppointmentReference       *string   `json:"appointment_reference,omitempty"`
	IdentityAgeConfirmed       *bool     `json:"identity_age_confirmed"`
	AuthorityConfirmed         *bool     `json:"authority_confirmed"`
	ValidUntil                 time.Time `json:"valid_until"`
}

func (in proxyAuthorityProposal) valid() bool {
	_, supported := countryprofile.Lookup(in.CountryCode)
	if !supported || !validReviewUUID(in.ReviewRequestID, true) || !validReviewUUID(in.ExpectedDecisionID, true) || !validReviewUUID(in.PolicyReference, true) || !validReviewUUID(in.IdentityAgeReference, true) || !validReviewUUID(in.AuthorityEvidenceReference, true) || in.IdentityAgeConfirmed == nil || !*in.IdentityAgeConfirmed || in.AuthorityConfirmed == nil || !*in.AuthorityConfirmed || in.ValidUntil.IsZero() || in.ValidUntil.Year() > 9999 || in.ValidUntil.Nanosecond()%1000 != 0 {
		return false
	}
	return (in.Kind == "parental" && in.AppointmentReference == nil) || (in.Kind == "appointed_guardian" && in.AppointmentReference != nil && validReviewUUID(*in.AppointmentReference, true))
}
func proxyParentalEnd(dob time.Time, zone string) (time.Time, error) {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	year, month, day := dob.Date()
	year += registrationAdultAgeYears
	// Clamp leap-day births conservatively; a review may choose an earlier expiry.
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	return time.Date(year, month, min(day, last), 0, 0, 0, 0, location).UTC(), nil
}
func readProxyAuthority(tx *gorm.DB, tenant string, hospital uuid.UUID, id string, row *proxyAuthority, locking bool) error {
	q := tx.Select(proxyAuthorityColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id)
	if locking {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q.Take(row).Error
}
func proxyAuthorityReadAudit(tx *gorm.DB, p authn.Principal, row proxyAuthority) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','patient_proxy_authorities',?)`, p.TenantID, p.Subject, row.ID).Error
}
func (h *patientHandlers) publishProxyAuthority(tx *gorm.DB, p authn.Principal, row proxyAuthority) error {
	data, err := json.Marshal(patientmastercontract.ProxyAuthorityChangedData{AuthorityID: row.ID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, patientmastercontract.SubjectProxyAuthorityChanged, events.Event{Type: patientmastercontract.SubjectProxyAuthorityChanged, Version: 1, TenantID: p.TenantID, Data: data})
}
func proxyAuthorityError(c *gin.Context, err error) {
	if errors.Is(err, errProxyAuthorityConflict) {
		respond.Error(c, 409, "proxy_authority_conflict", "Consent authority state or input changed")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		respond.Error(c, 503, "proxy_authority_unavailable", "Consent authority unavailable; reconcile a submitted command before retrying")
		return
	}
	patientError(c, err)
}
func (h *patientHandlers) proposeProxyAuthority(c *gin.Context) {
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
		respond.NotFound(c, "proxy authority")
		return
	}
	var in proxyAuthorityProposal
	if !decodePatientInput(c, &in) {
		return
	}
	if !in.valid() {
		respond.BadRequest(c, errors.New("invalid proxy authority proposal"))
		return
	}
	in.ValidUntil = in.ValidUntil.UTC()
	encoded, err := json.Marshal(in)
	if err != nil {
		patientError(c, err)
		return
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row proxyAuthority
	created := false
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		err := readProxyAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false)
		if err == nil {
			if row.Fingerprint != fingerprint {
				return errProxyAuthorityConflict
			}
			return proxyAuthorityReadAudit(tx, p, row)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var review linkReview
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("request_id,patient_id,requester_subject,relationship,status,decision_id,registration_updated_at").Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, in.ReviewRequestID).Take(&review).Error; err != nil {
			return err
		}
		if review.Status != "approved" || review.Relationship != map[string]string{"parental": "child", "appointed_guardian": "ward"}[in.Kind] || review.RequesterSubject == p.Subject || review.DecisionID == nil || review.DecisionID.String() != in.ExpectedDecisionID || review.ReviewedRegistrationUpdatedAt == nil {
			return errProxyAuthorityConflict
		}
		var registration struct {
			UpdatedAt    time.Time
			DOB          time.Time
			DOBEstimated bool
		}
		if err := tx.Table("patients").Select("updated_at,dob,dob_estimated").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, review.PatientID).Take(&registration).Error; err != nil {
			return err
		}
		var now time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&now).Error; err != nil {
			return err
		}
		if !registration.UpdatedAt.Equal(*review.ReviewedRegistrationUpdatedAt) || !in.ValidUntil.After(now) {
			return errProxyAuthorityConflict
		}
		profile, _ := countryprofile.Lookup(in.CountryCode)
		var parentalEnd *time.Time
		if in.Kind == "parental" {
			end, err := proxyParentalEnd(registration.DOB, profile.Timezone)
			if err != nil {
				return err
			}
			if registration.DOBEstimated || !end.After(now) || in.ValidUntil.After(end) {
				return errProxyAuthorityConflict
			}
			parentalEnd = &end
		}
		result := tx.Exec(`INSERT INTO patient_proxy_authorities(id,tenant_id,hospital_id,patient_id,review_request_id,decision_id,subject,reviewer_subject,kind,country_code,time_zone,policy_reference,identity_age_reference,authority_evidence_reference,appointment_reference,identity_age_confirmed,authority_confirmed,registration_updated_at,fingerprint,valid_until,parental_end_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,id) DO NOTHING`, c.Param("authority"), p.TenantID, hospital, review.PatientID, review.RequestID, review.DecisionID, review.RequesterSubject, p.Subject, in.Kind, in.CountryCode, profile.Timezone, in.PolicyReference, in.IdentityAgeReference, in.AuthorityEvidenceReference, in.AppointmentReference, true, true, registration.UpdatedAt, fingerprint, in.ValidUntil, parentalEnd)
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
		if err := readProxyAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false); err != nil {
			return err
		}
		if row.Fingerprint != fingerprint {
			return errProxyAuthorityConflict
		}
		if created {
			if err := h.publishProxyAuthority(tx, p, row); err != nil {
				return err
			}
		}
		return proxyAuthorityReadAudit(tx, p, row)
	})
	if err != nil {
		proxyAuthorityError(c, err)
		return
	}
	if created {
		respond.Created(c, row)
		return
	}
	respond.OK(c, row)
}

func (r proxyAuthority) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *patientHandlers) getMyProxyAuthority(c *gin.Context)       { h.getProxyAuthority(c, false) }
func (h *patientHandlers) getReviewedProxyAuthority(c *gin.Context) { h.getProxyAuthority(c, true) }
func (h *patientHandlers) getProxyAuthority(c *gin.Context, staff bool) {
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
		respond.NotFound(c, "proxy authority")
		return
	}
	var row proxyAuthority
	read := func(tx *gorm.DB) error {
		if err := readProxyAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false); err != nil {
			return err
		}
		return proxyAuthorityReadAudit(tx, p, row)
	}
	var err error
	if staff {
		err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, read)
	} else {
		err = h.consentSubjectTransaction(c.Request.Context(), p, hospital, read)
	}
	if err != nil {
		proxyAuthorityError(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *patientHandlers) listMyProxyAuthorities(c *gin.Context, params pagination.Params) ([]proxyAuthority, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []proxyAuthority
	err := h.consentSubjectTransaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(proxyAuthorityColumns).Where("tenant_id=? AND hospital_id=? AND subject=?", p.TenantID, hospital, p.Subject).Find(&rows).Error; err != nil {
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
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read','patient_proxy_authorities',value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, string(encoded)).Error
	})
	if err != nil {
		proxyAuthorityError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *patientHandlers) acceptProxyAuthority(c *gin.Context) {
	h.changeProxyAuthority(c, true, false)
}
func (h *patientHandlers) revokeMyProxyAuthority(c *gin.Context) {
	h.changeProxyAuthority(c, false, false)
}
func (h *patientHandlers) revokeReviewedProxyAuthority(c *gin.Context) {
	h.changeProxyAuthority(c, false, true)
}
func (h *patientHandlers) changeProxyAuthority(c *gin.Context, accept, staff bool) {
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
		respond.NotFound(c, "proxy authority")
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
	var row proxyAuthority
	change := func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		if err := readProxyAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, true); err != nil {
			return err
		}
		if row.Status == target && row.Version == version+1 {
			return proxyAuthorityReadAudit(tx, p, row)
		}
		if row.Status == "revoked" || row.Version != version {
			return errProxyAuthorityConflict
		}
		var result *gorm.DB
		if accept {
			if row.Status != "proposed" {
				return errProxyAuthorityConflict
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
				return errProxyAuthorityConflict
			}
			if err := tx.Exec(`SELECT set_config('app.patient_proxy_authority',?,true)`, row.ID.String()).Error; err != nil {
				return err
			}
			var registration struct {
				UpdatedAt    time.Time
				DOB          time.Time
				DOBEstimated bool
			}
			columns := "updated_at"
			if row.Kind == "parental" {
				columns += ",dob,dob_estimated"
			}
			if err := tx.Table("patients").Select(columns).Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, row.PatientID).Take(&registration).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errProxyAuthorityConflict
				}
				return err
			}
			if !registration.UpdatedAt.Equal(row.RegistrationUpdatedAt) {
				return errProxyAuthorityConflict
			}
			if row.Kind == "parental" {
				profile, supported := countryprofile.Lookup(row.CountryCode)
				if !supported {
					return errProxyAuthorityConflict
				}
				end, err := proxyParentalEnd(registration.DOB, profile.Timezone)
				if err != nil {
					return err
				}
				if registration.DOBEstimated || row.ParentalEndAt == nil || !end.Equal(*row.ParentalEndAt) || !end.After(now) {
					return errProxyAuthorityConflict
				}
			}
			if err := patientAudit(tx, p, hospital, "read", []patient{{ID: row.PatientID}}); err != nil {
				return err
			}
			if err := reviewReadAudit(tx, p, []linkReview{review}); err != nil {
				return err
			}
			result = tx.Exec(`UPDATE patient_proxy_authorities SET status='accepted',version=version+1,accepted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND status='proposed' AND version=?`, p.TenantID, hospital, row.ID, version)
		} else {
			result = tx.Exec(`UPDATE patient_proxy_authorities SET status='revoked',version=version+1,revoked_at=clock_timestamp(),revoked_by=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND status<>'revoked' AND version=?`, p.Subject, p.TenantID, hospital, row.ID, version)
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errProxyAuthorityConflict
		}
		if err := readProxyAuthority(tx, p.TenantID, hospital, c.Param("authority"), &row, false); err != nil {
			return err
		}
		if err := h.publishProxyAuthority(tx, p, row); err != nil {
			return err
		}
		return proxyAuthorityReadAudit(tx, p, row)
	}
	var err error
	if staff {
		err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, change)
	} else {
		err = h.consentSubjectTransaction(c.Request.Context(), p, hospital, change)
	}
	if err != nil {
		proxyAuthorityError(c, err)
		return
	}
	respond.OK(c, row)
}
