package mediconnect

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
)

type reviewSubmission struct {
	RequestID       uuid.UUID  `json:"request_id"`
	AccountID       uuid.UUID  `json:"account_id"`
	Subject         string     `json:"-"`
	HospitalID      uuid.UUID  `json:"hospital_id"`
	PatientID       uuid.UUID  `json:"patient_id"`
	Relationship    string     `json:"relationship"`
	Status          string     `json:"status"`
	DecisionID      *uuid.UUID `json:"decision_id"`
	DecisionOutcome *string    `json:"decision_outcome"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	WithdrawnAt     *time.Time `json:"withdrawn_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (reviewSubmission) TableName() string { return "mediconnect_review_submissions" }

const reviewColumns = "request_id,account_id,subject,hospital_id,patient_id,relationship,status,decision_id,decision_outcome,reviewed_at,withdrawn_at,created_at,updated_at"

func readReview(tx *gorm.DB, p authn.Principal, id uuid.UUID, row *reviewSubmission) error {
	return tx.Select(reviewColumns).Where("tenant_id=? AND subject=? AND request_id=?", p.TenantID, p.Subject, id).Take(row).Error
}
func auditReviewRead(tx *gorm.DB, p authn.Principal, id uuid.UUID) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES (?,?,'read','mediconnect_review_submissions',?)`, p.TenantID, p.Subject, id).Error
}
func (h *accountHandlers) publishReview(tx *gorm.DB, tenant, subject string, row reviewSubmission) error {
	data, err := json.Marshal(mediconnectcontract.RegistrationReviewData{RequestID: row.RequestID.String(), AccountID: row.AccountID.String(), RequesterSubject: row.Subject, HospitalID: row.HospitalID.String(), PatientID: row.PatientID.String(), Relationship: row.Relationship})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: data})
}
func (h *accountHandlers) submitReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := linkID(c)
	if !ok {
		return
	}
	var input struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !decode(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion > 9007199254740991 {
		respond.Error(c, 400, "invalid_request", "Provide an expected request version")
		return
	}
	var row reviewSubmission
	created := false
	err := h.linkTransaction(c, p, func(tx *gorm.DB, owner account) error {
		var request linkRequest
		if err := readLink(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, id, &request); err != nil {
			return err
		}
		if request.Status != "pending_verification" || request.Version != input.ExpectedVersion {
			return errLinkConflict
		}
		result := tx.Exec(`INSERT INTO mediconnect_review_submissions(tenant_id,request_id,account_id,subject,hospital_id,patient_id,relationship) VALUES (?,?,?,?,?,?,?) ON CONFLICT (tenant_id,request_id) DO NOTHING`, p.TenantID, id, owner.ID, p.Subject, request.HospitalID, request.PatientID, request.Relationship)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if err := readReview(tx, p, id, &row); err != nil {
			return err
		}
		if !created {
			return auditReviewRead(tx, p, id)
		}
		return h.publishReview(tx, p.TenantID, mediconnectcontract.SubjectReviewRequested, row)
	})
	if err != nil {
		linkError(c, err)
		return
	}
	if created {
		respond.Accepted(c, row)
	} else {
		respond.OK(c, row)
	}
}
func (h *accountHandlers) getReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, ok := linkID(c)
	if !ok {
		return
	}
	var row reviewSubmission
	err := h.linkTransaction(c, p, func(tx *gorm.DB, _ account) error {
		if err := readReview(tx, p, id, &row); err != nil {
			return err
		}
		return auditReviewRead(tx, p, id)
	})
	if err != nil {
		linkError(c, err)
		return
	}
	respond.OK(c, row)
}
func (h *accountHandlers) withdrawReview(tx *gorm.DB, p authn.Principal, request linkRequest) error {
	var row reviewSubmission
	if err := readReview(tx.Clauses(clause.Locking{Strength: "UPDATE"}), p, request.ID, &row); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if err := tx.Exec(`UPDATE mediconnect_review_submissions SET status='withdrawn',withdrawn_at=?,updated_at=clock_timestamp() WHERE tenant_id=? AND subject=? AND request_id=?`, request.WithdrawnAt, p.TenantID, p.Subject, request.ID).Error; err != nil {
		return err
	}
	return h.publishReview(tx, p.TenantID, mediconnectcontract.SubjectReviewWithdrawn, row)
}
