package pharmacy

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

func (h *catalogueHandlers) postPrescriptionFulfilmentReview(c *gin.Context) {
	p, outlet, ok := stockCommandScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var input PrescriptionFulfilmentReviewInput
	if !decodeCatalogueWithin(c, &input, 16384) {
		return
	}
	if !validPrescriptionFulfilmentInput(input) {
		respond.Error(c, 400, "invalid_request", "Choose distinct prescription lines and positive whole-unit quantities.")
		return
	}
	var view PrescriptionFulfilmentReviewContext
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('lock_timeout','2s',true),set_config('statement_timeout','5s',true)").Error; err != nil {
			return err
		}
		var parent struct{ ID uuid.UUID }
		// Active sale constraints lock this same parent before consuming its allowance.
		if err := tx.Table("pharmacy_prescriptions").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND outlet_id=? AND id=?", p.TenantID, outlet, id).Take(&parent).Error; err != nil {
			return err
		}
		record, err := loadPrescription(tx, p.TenantID, outlet, id)
		if err != nil {
			return err
		}
		tenant, err := uuid.Parse(p.TenantID)
		if err != nil {
			return errPrescriptionReviewSource
		}
		source, err := prescriptionReviewContext(tenant, currentPharmacy(c.Request.Context()), p.Subject, record)
		if err != nil {
			return err
		}
		view, err = prescriptionFulfilmentReview(source, input)
		if err != nil {
			return err
		}
		return auditRegisterRead(c, tx, "prescription_fulfilment_review", p.TenantID, p.Subject, outlet, "fulfilment_review_read", "prescription_id", id.String())
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "prescription")
	case errors.Is(err, errFulfilmentInput):
		respond.Error(c, 400, "invalid_request", "Choose distinct prescription lines and positive whole-unit quantities.")
	case errors.Is(err, errFulfilmentSourceChanged):
		respond.Error(c, http.StatusConflict, "review_source_changed", "The prescription allowance changed. Refresh recorded details before proposing supply.")
	case errors.Is(err, errFulfilmentUnavailable):
		respond.Error(c, http.StatusConflict, "fulfilment_not_available", "The selected prescription lines no longer allow this supply. Refresh recorded details.")
	case err != nil:
		respond.Error(c, http.StatusServiceUnavailable, "fulfilment_review_unavailable", "The proposed fulfilment could not be checked. No clinical verification or stock reservation has been recorded.")
	default:
		respond.OK(c, view)
	}
}
