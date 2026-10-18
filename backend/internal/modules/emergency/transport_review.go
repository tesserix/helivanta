package emergency

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ClinicalReviewerInput is an explicit, expiring manual verification under a reviewed policy.
type ClinicalReviewerInput struct {
	Version           int64     `json:"version" binding:"min=0"`
	Enabled           *bool     `json:"enabled" binding:"required"`
	EvidenceReference string    `json:"evidence_reference" binding:"required,max=120"`
	ExpiresAt         time.Time `json:"expires_at" binding:"required"`
}
type clinicalReviewer struct {
	ID      uuid.UUID `json:"id"`
	Subject string    `json:"subject"`
	ClinicalReviewerInput
	VerifiedBy      string    `json:"verified_by"`
	CurrentlyActive bool      `json:"currently_active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (clinicalReviewer) TableName() string { return "emergency_clinical_reviewers" }

const reviewerColumns = "id,subject,version,enabled,evidence_reference,expires_at,verified_by,created_at,updated_at,(enabled AND expires_at>statement_timestamp()) AS currently_active"

func clinicalBaseLock(tx *gorm.DB, p authn.Principal, b uuid.UUID, strength string) error {
	return tx.Select("id").Clauses(clause.Locking{Strength: strength}).Where("tenant_id=? AND id=?", p.TenantID, b).First(&base{}).Error
}
func currentClinicalReviewer(tx *gorm.DB, p authn.Principal, b uuid.UUID, subject string) (clinicalReviewer, error) {
	var out clinicalReviewer
	err := tx.Select(reviewerColumns).Where("tenant_id=? AND base_id=? AND subject=?", p.TenantID, b, subject).Order("version DESC").First(&out).Error
	return out, err
}
func (h *handlers) putClinicalReviewer(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	subject := c.Param("subject")
	var in ClinicalReviewerInput
	if !decode(c, &in) {
		return
	}
	if !cleanText(subject) || len(subject) > 200 || !policyShape.MatchString(in.EvidenceReference) {
		respond.Error(c, 400, "invalid_request", "invalid clinical verification fields")
		return
	}
	if *in.Enabled && subject == p.Subject {
		respond.Error(c, 400, "invalid_request", "independent verification is required to enable clinical authority")
		return
	}
	in.ExpiresAt = in.ExpiresAt.Truncate(time.Microsecond)
	var out clinicalReviewer
	err := h.transaction(c.Request.Context(), p, b, true, func(tx *gorm.DB) error {
		if err := clinicalBaseLock(tx, p, b, "UPDATE"); err != nil {
			return err
		}
		current, err := currentClinicalReviewer(tx, p, b, subject)
		if err == nil {
			if current.Version == in.Version+1 && current.VerifiedBy == p.Subject && *current.Enabled == *in.Enabled && current.EvidenceReference == in.EvidenceReference && current.ExpiresAt.Equal(in.ExpiresAt) {
				out = current
				return nil
			}
			if current.Version != in.Version {
				return errCaseConflict
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else if in.Version != 0 {
			return errCaseConflict
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_clinical_reviewers(tenant_id,base_id,id,subject,version,enabled,evidence_reference,expires_at,verified_by) VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, b, id, subject, in.Version+1, *in.Enabled, in.EvidenceReference, in.ExpiresAt, p.Subject).Error; err != nil {
			return err
		}
		if err := audit(tx, p, b, id, "clinical.verification_recorded", &subject); err != nil {
			return err
		}
		data, err := json.Marshal(emergencycontract.ClinicalAccessChangedData{BaseID: b.String(), GrantID: id.String(), Version: in.Version + 1})
		if err != nil {
			return err
		}
		if err := transportEvent(tx, p, emergencycontract.SubjectClinicalAccessChanged, data); err != nil {
			return err
		}
		out, err = currentClinicalReviewer(tx, p, b, subject)
		return err
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) getClinicalReviewer(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	subject := c.Param("subject")
	admin := subject != ""
	if !admin {
		subject = p.Subject
	}
	if !cleanText(subject) || len(subject) > 200 {
		respond.NotFound(c, "resource")
		return
	}
	var out clinicalReviewer
	err := h.transaction(c.Request.Context(), p, b, admin, func(tx *gorm.DB) error {
		if !admin {
			if err := requireAccess(tx, p, b, "read"); err != nil {
				return err
			}
		}
		var err error
		out, err = currentClinicalReviewer(tx, p, b, subject)
		if err != nil {
			return err
		}
		return audit(tx, p, b, out.ID, "clinical.verification_viewed", &subject)
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}

// TransportAssessmentInput records a clinician's requirement, not a diagnosis or dispatch.
type TransportAssessmentInput struct {
	CaseVersion     int64     `json:"case_version" binding:"min=1"`
	RequiredClass   string    `json:"required_class" binding:"required,oneof=patient_transport bls als"`
	PolicyReference string    `json:"policy_reference" binding:"required,max=120"`
	ValidUntil      time.Time `json:"valid_until" binding:"required"`
}
type transportAssessment struct {
	ID              uuid.UUID `json:"id"`
	CaseID          uuid.UUID `json:"case_id"`
	Sequence        int64     `json:"sequence"`
	ReviewerGrantID uuid.UUID `json:"reviewer_grant_id"`
	ReviewerSubject string    `json:"reviewer_subject"`
	TransportAssessmentInput
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (transportAssessment) TableName() string                 { return "emergency_transport_assessments" }
func (a transportAssessment) PageKey() (time.Time, uuid.UUID) { return a.CreatedAt, a.ID }

const assessmentColumns = "id,case_id,case_version,sequence,reviewer_grant_id,reviewer_subject,required_class,policy_reference,valid_until,created_at,updated_at"

func transportEvent(tx *gorm.DB, p authn.Principal, subject string, data json.RawMessage) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return events.Enqueue(tx, subject, events.Event{ID: id.String(), Type: subject, Version: 1, TenantID: p.TenantID, OccurredAt: time.Now().UTC(), Data: data})
}
func (h *handlers) putTransportAssessment(c *gin.Context) {
	p, b, ok := scope(c)
	if !ok {
		return
	}
	caseID, ok := caseID(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "assessment")
	if !ok {
		return
	}
	var in TransportAssessmentInput
	if !decode(c, &in) {
		return
	}
	if !policyShape.MatchString(in.PolicyReference) {
		respond.Error(c, 400, "invalid_request", "approved transport policy reference required")
		return
	}
	in.ValidUntil = in.ValidUntil.Truncate(time.Microsecond)
	var out transportAssessment
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := requireAccess(tx, p, b, "read"); err != nil {
			return err
		}
		if err := clinicalBaseLock(tx, p, b, "SHARE"); err != nil {
			return err
		}
		var grant struct{ ID *uuid.UUID }
		if err := tx.Raw(`SELECT emergency_current_reviewer(?::uuid,?::uuid) AS id`, p.TenantID, b).Scan(&grant).Error; err != nil {
			return err
		}
		if grant.ID == nil {
			return gorm.ErrRecordNotFound
		}
		var saved caseRow
		if err := tx.Select("id,version,status,requested_service").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, caseID).First(&saved).Error; err != nil {
			return err
		}
		if saved.Version != in.CaseVersion || saved.Status == "closed" || saved.Status == "completed" || saved.RequestedService == "neonatal" || saved.RequestedService == "mortuary" || saved.RequestedService == "home_visit" {
			return errCaseConflict
		}
		err := tx.Select(assessmentColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
		if err == nil {
			if out.CaseID != caseID || out.CaseVersion != in.CaseVersion || out.ReviewerSubject != p.Subject || out.ReviewerGrantID != *grant.ID || out.RequiredClass != in.RequiredClass || out.PolicyReference != in.PolicyReference || !out.ValidUntil.Equal(in.ValidUntil) {
				return errCaseConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var sequence int64
		if err := tx.Raw(`SELECT COALESCE(MAX(sequence),0)+1 FROM emergency_transport_assessments WHERE tenant_id=? AND base_id=? AND case_id=?`, p.TenantID, b, caseID).Scan(&sequence).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO emergency_transport_assessments(tenant_id,base_id,id,case_id,case_version,sequence,reviewer_grant_id,reviewer_subject,required_class,policy_reference,valid_until) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, b, id, caseID, in.CaseVersion, sequence, *grant.ID, p.Subject, in.RequiredClass, in.PolicyReference, in.ValidUntil).Error; err != nil {
			return err
		}
		if err := audit(tx, p, b, caseID, "case.transport_review_recorded", nil); err != nil {
			return err
		}
		data, err := json.Marshal(emergencycontract.CaseAssessedData{BaseID: b.String(), CaseID: caseID.String(), AssessmentID: id.String(), Sequence: sequence})
		if err != nil {
			return err
		}
		if err := transportEvent(tx, p, emergencycontract.SubjectCaseAssessed, data); err != nil {
			return err
		}
		return tx.Select(assessmentColumns).Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&out).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	respond.OK(c, out)
}
func (h *handlers) listTransportAssessments(c *gin.Context, params pagination.Params) ([]transportAssessment, error) {
	p, b, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := caseID(c)
	if !ok {
		return nil, nil
	}
	var rows []transportAssessment
	err := h.transaction(c.Request.Context(), p, b, false, func(tx *gorm.DB) error {
		if err := tx.Select("id").Where("tenant_id=? AND base_id=? AND id=?", p.TenantID, b, id).First(&caseRow{}).Error; err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Select(assessmentColumns).Where("tenant_id=? AND base_id=? AND case_id=?", p.TenantID, b, id).Find(&rows).Error
	})
	if err != nil {
		respondError(c, err)
		return nil, nil
	}
	return rows, nil
}
