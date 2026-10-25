package patientmaster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type linkReview struct {
	DecisionID                    *uuid.UUID `json:"decision_id"`
	DecisionOutcome               *string    `json:"decision_outcome"`
	DecisionReason                *string    `json:"decision_reason"`
	DecisionFingerprint           *string    `json:"-"`
	EvidenceReference             *uuid.UUID `json:"evidence_reference"`
	ReviewerSubject               *string    `json:"reviewer_subject"`
	ReviewedAt                    *time.Time `json:"reviewed_at"`
	ReviewedRegistrationUpdatedAt *time.Time `json:"reviewed_registration_updated_at" gorm:"column:registration_updated_at"`

	RequestID             uuid.UUID  `json:"request_id"`
	AccountID             uuid.UUID  `json:"account_id"`
	RequesterSubject      string     `json:"-"`
	HospitalID            uuid.UUID  `json:"hospital_id"`
	PatientID             uuid.UUID  `json:"patient_id"`
	Relationship          string     `json:"relationship"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	RegistrationUpdatedAt *time.Time `json:"registration_updated_at,omitempty" gorm:"-"`
}

func (linkReview) TableName() string                 { return "patient_link_reviews" }
func (r linkReview) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.RequestID }

const linkReviewColumns = "request_id,account_id,requester_subject,hospital_id,patient_id,relationship,status,created_at,updated_at,decision_id,decision_outcome,decision_reason,decision_fingerprint,evidence_reference,reviewer_subject,reviewed_at,registration_updated_at"

func validReviewUUID(value string, v7 bool) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value && (!v7 || (id.Version() == 7 && id.Variant() == uuid.RFC4122))
}
func reviewConsumer(subject, name string) events.Consumer {
	return events.Consumer{Name: name, Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var data mediconnectcontract.RegistrationReviewData
		if evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || !validReviewUUID(evt.TenantID, false) || json.Unmarshal(evt.Data, &data) != nil || !validReviewUUID(data.RequestID, true) || !validReviewUUID(data.AccountID, true) || !validReviewUUID(data.HospitalID, false) || !validReviewUUID(data.PatientID, false) || strings.TrimSpace(data.RequesterSubject) != data.RequesterSubject || len(data.RequesterSubject) < 1 || len(data.RequesterSubject) > 512 {
			return errors.New("invalid registration review event")
		}
		switch data.Relationship {
		case "self", "child", "parent", "spouse", "sibling", "ward", "other":
		default:
			return errors.New("invalid review relationship")
		}
		if err := tx.Exec(`SELECT set_config('app.patient_review_hospital',?,true),set_config('app.patient_review_request',?,true)`, data.HospitalID, data.RequestID).Error; err != nil {
			return errors.New("review scope unavailable")
		}
		status := "pending"
		if subject == mediconnectcontract.SubjectReviewWithdrawn {
			status = "withdrawn"
		}
		if err := tx.Exec(`INSERT INTO patient_link_reviews(tenant_id,request_id,account_id,requester_subject,hospital_id,patient_id,relationship,status) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,request_id) DO NOTHING`, evt.TenantID, data.RequestID, data.AccountID, data.RequesterSubject, data.HospitalID, data.PatientID, data.Relationship, status).Error; err != nil {
			return errors.New("review ingestion unavailable")
		}
		var row linkReview
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(linkReviewColumns).Where("tenant_id=? AND request_id=?", evt.TenantID, data.RequestID).Take(&row).Error; err != nil {
			return errors.New("review receipt unavailable")
		}
		if row.AccountID.String() != data.AccountID || row.RequesterSubject != data.RequesterSubject || row.HospitalID.String() != data.HospitalID || row.PatientID.String() != data.PatientID || row.Relationship != data.Relationship {
			return errors.New("review claim mismatch")
		}
		if status == "withdrawn" && row.Status != "withdrawn" {
			if err := tx.Exec(`UPDATE patient_link_reviews SET status='withdrawn',updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=?`, evt.TenantID, data.RequestID).Error; err != nil {
				return errors.New("review withdrawal unavailable")
			}
		}
		return nil
	}}
}
func reviewReadAudit(tx *gorm.DB, p authn.Principal, rows []linkReview) error {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.RequestID
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read','patient_link_reviews',value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, string(encoded)).Error
}
func (h *patientHandlers) listLinkReviews(c *gin.Context, params pagination.Params) ([]linkReview, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []linkReview
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(linkReviewColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		return reviewReadAudit(tx, p, rows)
	})
	if err != nil {
		patientError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *patientHandlers) getLinkReview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("request"), true) {
		respond.NotFound(c, "review")
		return
	}
	var row linkReview
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Select(linkReviewColumns).Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, c.Param("request")).Take(&row).Error; err != nil {
			return err
		}
		var registration struct{ UpdatedAt *time.Time }
		if err := tx.Raw(`SELECT updated_at FROM patients WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, row.PatientID).Scan(&registration).Error; err != nil {
			return err
		}
		row.RegistrationUpdatedAt = registration.UpdatedAt
		if registration.UpdatedAt != nil {
			if err := patientAudit(tx, p, hospital, "read", []patient{{ID: row.PatientID}}); err != nil {
				return err
			}
		}
		return reviewReadAudit(tx, p, []linkReview{row})
	})
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, row)
}
