package practitioner

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

type evidenceRetry struct {
	ID              uuid.UUID `json:"id"`
	FileID          uuid.UUID `json:"file_id"`
	ExpectedAttempt int64     `json:"expected_attempt"`
	ScanAttempt     int64     `json:"scan_attempt"`
	ReviewReference uuid.UUID `json:"review_reference"`
	PreviousFailure string    `json:"previous_failure"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

func (r *evidenceRetry) AfterFind(*gorm.DB) error { r.CreatedAt = r.CreatedAt.UTC(); return nil }
func retryQuery(tx *gorm.DB, p authn.Principal, parent evidenceParent, file, id uuid.UUID) *gorm.DB {
	return tx.Table("practitioner_evidence_retries").Select("id,file_id,expected_attempt,scan_attempt,review_reference,previous_failure,status,created_at").Where("tenant_id=? AND practitioner_id=? AND registration_id=? AND revision_id=? AND file_id=? AND id=?", p.TenantID, parent.practitioner, parent.registration, parent.revision, file, id)
}
func (h *handlers) retryEvidence(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, parent, ok := evidenceScope(c)
	if !ok {
		return
	}
	file, ok := resourceID(c, "file")
	if !ok {
		return
	}
	id, ok := resourceID(c, "retry")
	if !ok {
		return
	}
	var in *struct {
		ExpectedAttempt int64  `json:"expected_attempt"`
		ReviewReference string `json:"review_reference"`
	}
	if !decode(c, &in) {
		return
	}
	if in == nil || in.ExpectedAttempt < 1 || in.ExpectedAttempt >= 9007199254740991 {
		respond.Error(c, 400, "invalid_request", "Expected a bounded current scan attempt and review reference")
		return
	}
	reference, valid := canonicalStewardReference(in.ReviewReference)
	if !valid {
		respond.Error(c, 400, "invalid_request", "Expected an opaque review reference")
		return
	}
	if !h.reauthorize(c, PermEvidenceManage) {
		return
	}
	var receipt evidenceRetry
	created := false
	err := h.transaction(c.Request.Context(), p, false, func(tx *gorm.DB) error {
		if err := evidenceAccess(tx, true); err != nil {
			return err
		}
		var row registrationEvidence
		if err := lockedEvidence(tx, p, parent, file, &row); err != nil {
			return err
		}
		err := retryQuery(tx, p, parent, file, id).Take(&receipt).Error
		if err == nil {
			if receipt.ExpectedAttempt != in.ExpectedAttempt || receipt.ReviewReference != reference {
				return errIntentConflict
			}
			return tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','practitioner_evidence_retries',?)", p.TenantID, p.Subject, id).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if row.Status != "scan_error" || row.ScanAttempt != in.ExpectedAttempt {
			return errIntentConflict
		}
		if h.files == nil {
			return errEvidenceUnavailable
		}
		if err := tx.Exec("INSERT INTO practitioner_evidence_retries(tenant_id,practitioner_id,registration_id,revision_id,file_id,id,expected_attempt,scan_attempt,review_reference,created_by) VALUES(?,?,?,?,?,?,?,?,?,?)", p.TenantID, parent.practitioner, parent.registration, parent.revision, file, id, in.ExpectedAttempt, in.ExpectedAttempt+1, reference, p.Subject).Error; err != nil {
			return err
		}
		data, err := json.Marshal(practitionercontract.EvidenceScanRequestedData{PractitionerID: parent.practitioner.String(), RegistrationID: parent.registration.String(), RevisionID: parent.revision.String(), FileID: file.String(), ScanAttempt: in.ExpectedAttempt + 1})
		if err != nil {
			return err
		}
		if err := events.Enqueue(tx, EvidenceScanRequested, events.Event{Type: EvidenceScanRequested, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		if err := retryQuery(tx, p, parent, file, id).Take(&receipt).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		failure(c, err)
		return
	}
	if created {
		respond.Created(c, receipt)
	} else {
		respond.OK(c, receipt)
	}
}
