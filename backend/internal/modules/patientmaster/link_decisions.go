package patientmaster

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errReviewConflict = errors.New("review state conflicts")

type reviewDecisionInput struct {
	AccountID                     string     `json:"account_id"`
	Outcome                       string     `json:"outcome"`
	Reason                        string     `json:"reason"`
	EvidenceReference             string     `json:"evidence_reference"`
	ExpectedRegistrationUpdatedAt *time.Time `json:"expected_registration_updated_at"`
}

func (in reviewDecisionInput) valid() bool {
	if !validReviewUUID(in.AccountID, true) || !validReviewUUID(in.EvidenceReference, true) {
		return false
	}
	if in.Outcome == "approved" {
		return in.Reason == "identity_confirmed" && in.ExpectedRegistrationUpdatedAt != nil && !in.ExpectedRegistrationUpdatedAt.IsZero()
	}
	return in.Outcome == "rejected" && in.ExpectedRegistrationUpdatedAt == nil && (in.Reason == "identity_mismatch" || in.Reason == "registration_missing" || in.Reason == "insufficient_evidence")
}
func (h *patientHandlers) decideLinkReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("request"), true) || !validReviewUUID(c.Param("decision"), true) {
		respond.NotFound(c, "review")
		return
	}
	var input reviewDecisionInput
	if !decodePatientInput(c, &input) {
		return
	}
	if !input.valid() {
		respond.BadRequest(c, errors.New("invalid review decision"))
		return
	}
	if input.ExpectedRegistrationUpdatedAt != nil {
		v := input.ExpectedRegistrationUpdatedAt.UTC()
		input.ExpectedRegistrationUpdatedAt = &v
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		patientError(c, err)
		return
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row linkReview
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		// Serialize decision identifiers across different requests before locking a row.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, p.TenantID+":patient-review:"+c.Param("decision")).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(linkReviewColumns).Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, c.Param("request")).Take(&row).Error; err != nil {
			return err
		}
		if row.AccountID.String() != input.AccountID || row.RequesterSubject == p.Subject {
			return errReviewConflict
		}
		if row.DecisionID != nil {
			if row.DecisionID.String() != c.Param("decision") || row.DecisionFingerprint == nil || *row.DecisionFingerprint != fingerprint {
				return errReviewConflict
			}
			return reviewReadAudit(tx, p, []linkReview{row})
		}
		if row.Status != "pending" {
			return errReviewConflict
		}
		var used int64
		if err := tx.Table("patient_link_reviews").Where("tenant_id=? AND decision_id=?", p.TenantID, c.Param("decision")).Count(&used).Error; err != nil {
			return err
		}
		if used != 0 {
			return errReviewConflict
		}
		var confirmed *uuid.UUID
		if input.Outcome == "approved" {
			var registration struct{ UpdatedAt time.Time }
			if err := tx.Table("patients").Select("updated_at").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, row.PatientID).Take(&registration).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errReviewConflict
				}
				return err
			}
			if !registration.UpdatedAt.Equal(*input.ExpectedRegistrationUpdatedAt) {
				return errReviewConflict
			}
			confirmed = &row.PatientID
		}
		if err := tx.Exec(`UPDATE patient_link_reviews SET status=?,decision_id=?,decision_outcome=?,decision_reason=?,decision_fingerprint=?,evidence_reference=?,reviewer_subject=?,reviewed_at=clock_timestamp(),registration_updated_at=?,confirmed_patient_id=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND request_id=?`, input.Outcome, c.Param("decision"), input.Outcome, input.Reason, fingerprint, input.EvidenceReference, p.Subject, input.ExpectedRegistrationUpdatedAt, confirmed, p.TenantID, hospital, row.RequestID).Error; err != nil {
			return err
		}
		if err := tx.Select(linkReviewColumns).Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, row.RequestID).Take(&row).Error; err != nil {
			return err
		}
		data, err := json.Marshal(patientmastercontract.RegistrationReviewedData{RequestID: row.RequestID.String(), AccountID: row.AccountID.String(), HospitalID: row.HospitalID.String(), PatientID: row.PatientID.String(), DecisionID: row.DecisionID.String(), Outcome: *row.DecisionOutcome, ReviewedAt: row.ReviewedAt.UTC().Format(time.RFC3339Nano)})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, patientmastercontract.SubjectRegistrationReviewed, events.Event{Type: patientmastercontract.SubjectRegistrationReviewed, Version: 1, TenantID: p.TenantID, Data: data})
	})
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "23505" {
		err = errReviewConflict
	}
	if errors.Is(err, errReviewConflict) {
		respond.Error(c, 409, "review_conflict", "Review state or decision input changed")
		return
	}
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, row)
}
