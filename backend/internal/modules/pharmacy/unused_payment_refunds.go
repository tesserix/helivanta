package pharmacy

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
)

type unusedPaymentRefundInput = UnusedPaymentRefundFields

type unusedPaymentRefund struct {
	PaymentRequestID uuid.UUID `json:"payment_request_id"`
	AmountMinor      int64     `json:"amount_minor"`
	Currency         string    `json:"currency"`
	Reference        string    `json:"reference"`
	Reason           string    `json:"reason"`
	RecordedBy       string    `json:"recorded_by"`
	RecordedAt       time.Time `json:"recorded_at"`
}

const unusedRefundColumns = "payment_request_id,amount_minor,currency,reference,reason,recorded_by,recorded_at"

var errUnusedPaymentRefund = errors.New("payment is not an unused paid payment, or a different refund is already recorded")

func (h *catalogueHandlers) getUnusedPaymentRefund(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var rows []unusedPaymentRefund
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		if err := tx.Table("pharmacy_unused_payment_refunds").Select(unusedRefundColumns).Where("tenant_id=? AND outlet_id=? AND payment_request_id=?", p.TenantID, outlet, id).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','pharmacy_unused_payment_refunds',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		stockError(c, err)
		return
	}
	if len(rows) == 0 {
		respond.NotFound(c, "unused payment refund")
		return
	}
	respond.OK(c, rows[0])
}

// putUnusedPaymentRefund records a completed external refund. It never calls a
// provider or changes the owner's captured-payment outcome.
func (h *catalogueHandlers) putUnusedPaymentRefund(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in unusedPaymentRefundInput
	if !decodeCatalogue(c, &in) {
		return
	}
	if !prescriptionText(in.Reference, 64) || !prescriptionText(in.Reason, 200) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "give a refund reference and reason without surrounding whitespace or control characters")
		return
	}
	var out unusedPaymentRefund
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		// Serialize with sale-tender insertion before checking availability or replay.
		var payment []paymentRequest
		if err := tx.Raw(`SELECT `+paymentRequestColumns+` FROM pharmacy_payment_requests WHERE tenant_id=? AND outlet_id=? AND id=? FOR UPDATE`, p.TenantID, outlet, id).Scan(&payment).Error; err != nil {
			return err
		}
		if len(payment) == 0 {
			return gorm.ErrRecordNotFound
		}
		var kept []unusedPaymentRefund
		if err := tx.Table("pharmacy_unused_payment_refunds").Select(unusedRefundColumns).Where("tenant_id=? AND outlet_id=? AND payment_request_id=?", p.TenantID, outlet, id).Find(&kept).Error; err != nil {
			return err
		}
		if len(kept) > 0 {
			if kept[0].Reference != in.Reference || kept[0].Reason != in.Reason {
				return errUnusedPaymentRefund
			}
			out = kept[0]
			return nil
		}
		var used bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pharmacy_sale_tenders WHERE tenant_id=? AND payment_request_id=?)`, p.TenantID, id).Scan(&used).Error; err != nil {
			return err
		}
		if payment[0].Status != "paid" || used {
			return errUnusedPaymentRefund
		}
		if err := tx.Raw(`INSERT INTO pharmacy_unused_payment_refunds (tenant_id,outlet_id,payment_request_id,amount_minor,currency,reference,reason,recorded_by) VALUES (?,?,?,?,?,?,?,?) RETURNING `+unusedRefundColumns, p.TenantID, outlet, id, payment[0].AmountMinor, payment[0].Currency, in.Reference, in.Reason, p.Subject).Scan(&out).Error; err != nil {
			return err
		}
		created = true
		data, err := json.Marshal(pharmacycontract.UnusedPaymentRefundRecordedData{OutletID: outlet.String(), PaymentRequestID: id.String(), AmountMinor: out.AmountMinor, Currency: out.Currency})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectUnusedPaymentRefundRecorded, events.Event{Type: "UnusedPaymentRefundRecorded", Version: 1, TenantID: p.TenantID, Data: data})
	})
	switch {
	case errors.Is(err, errUnusedPaymentRefund):
		respond.Error(c, http.StatusConflict, "payment_not_unused", errUnusedPaymentRefund.Error())
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}
