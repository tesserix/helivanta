package mediconnect

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func reviewResultConsumer(bus *events.Bus) events.Consumer {
	return events.Consumer{Name: "mediconnect-registration-reviewed", Subject: patientmastercontract.SubjectRegistrationReviewed, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var data patientmastercontract.RegistrationReviewedData
		if evt.Type != patientmastercontract.SubjectRegistrationReviewed || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &data) != nil {
			return errors.New("invalid review result")
		}
		for _, value := range []string{evt.TenantID, data.RequestID, data.AccountID, data.HospitalID, data.PatientID, data.DecisionID} {
			id, err := uuid.Parse(value)
			if err != nil || id == uuid.Nil || id.String() != value {
				return errors.New("invalid review result reference")
			}
		}
		for _, value := range []string{data.RequestID, data.AccountID, data.DecisionID} {
			id, _ := uuid.Parse(value)
			if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
				return errors.New("invalid review result reference")
			}
		}
		at, err := time.Parse(time.RFC3339Nano, data.ReviewedAt)
		if err != nil || at.IsZero() || (data.Outcome != "approved" && data.Outcome != "rejected") {
			return errors.New("invalid review result decision")
		}
		if err := tx.Exec(`SELECT set_config('app.mediconnect_review_request',?,true)`, data.RequestID).Error; err != nil {
			return errors.New("review result scope unavailable")
		}
		// Match withdrawal's lock order: original request, then review projection.
		var request linkRequest
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(linkColumns).Where("tenant_id=? AND id=? AND account_id=? AND hospital_id=? AND patient_id=?", evt.TenantID, data.RequestID, data.AccountID, data.HospitalID, data.PatientID).Take(&request).Error; err != nil {
			return errors.New("review source request unavailable")
		}
		var row reviewSubmission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(reviewColumns).Where("tenant_id=? AND request_id=? AND account_id=? AND hospital_id=? AND patient_id=?", evt.TenantID, data.RequestID, data.AccountID, data.HospitalID, data.PatientID).Take(&row).Error; err != nil {
			return errors.New("review result receipt unavailable")
		}
		if request.Status == "withdrawn" && row.Status != "withdrawn" {
			h := accountHandlers{bus: bus}
			p := authn.Principal{TenantID: evt.TenantID, Subject: row.Subject}
			if err := h.withdrawReview(tx, p, request); err != nil {
				return errors.New("review withdrawal reconciliation unavailable")
			}
			row.Status = "withdrawn"
		}
		if row.DecisionID != nil {
			if row.DecisionID.String() != data.DecisionID || *row.DecisionOutcome != data.Outcome || !row.ReviewedAt.Equal(at) {
				return errors.New("review result conflicts")
			}
			return nil
		}
		status := "identity_verified"
		if data.Outcome == "rejected" {
			status = "rejected"
		}
		if row.Status == "withdrawn" {
			status = "withdrawn"
		}
		if err := tx.Exec(`UPDATE mediconnect_review_submissions SET status=?,decision_id=?,decision_outcome=?,reviewed_at=?,updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=? AND account_id=? AND hospital_id=? AND patient_id=?`, status, data.DecisionID, data.Outcome, at, evt.TenantID, data.RequestID, data.AccountID, data.HospitalID, data.PatientID).Error; err != nil {
			return errors.New("review result update unavailable")
		}
		return nil
	}}
}
