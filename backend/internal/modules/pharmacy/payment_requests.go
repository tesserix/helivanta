package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/countryprofile"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// counterPaymentSeconds is how long a counter QR stays payable: long
// enough to open a UPI app and pay, short enough that the queue moves on
// to another tender when it does not.
const counterPaymentSeconds = 300

func paymentRequestRoutes(s *platform.Router, h *catalogueHandlers) {
	// Paid counter payments not yet on a sale (#1386 C-4b): money taken
	// whose sale did not post stays findable, so it can still be used.
	platform.ListRoute(s, "/counter-payments", PermSaleCreate, h.listUnusedCounterPayments)
	s.PUT("/counter-payments/:id", PermSaleCreate, h.putCounterPayment)
	s.GET("/counter-payments/:id", PermSaleCreate, h.getCounterPayment)
	s.POST("/counter-payments/:id/cancel", PermSaleCreate, h.cancelCounterPayment)
	s.PUT("/counter-payments/:id/refund", PermSaleReturn, h.putUnusedPaymentRefund)
	s.GET("/counter-payments/:id/refund", PermSaleReturn, h.getUnusedPaymentRefund)
}

type paymentRequest struct {
	ID                uuid.UUID  `json:"id"`
	Purpose           string     `json:"purpose"`
	AmountMinor       int64      `json:"amount_minor"`
	Currency          string     `json:"currency"`
	Status            string     `json:"status"`
	QRImageURL        *string    `json:"qr_image_url"`
	ExpiresAt         *time.Time `json:"expires_at"`
	FailureReason     *string    `json:"failure_reason"`
	ProviderPaymentID *string    `json:"provider_payment_id"`
	RRN               *string    `json:"rrn"`
	PaidAt            *time.Time `json:"paid_at"`
	Late              bool       `json:"late"`
	CreatedAt         time.Time  `json:"created_at"`
}

// PageKey is the keyset position of a request, newest first.
func (r paymentRequest) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *catalogueHandlers) listUnusedCounterPayments(c *gin.Context, params pagination.Params) ([]paymentRequest, error) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return nil, nil
	}
	var rows []paymentRequest
	err := h.transaction(c.Request.Context(), p, outlet, false, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx.Table("pharmacy_payment_requests r"), params).Select(paymentRequestColumns).
			Where("r.tenant_id=? AND r.outlet_id=? AND r.status='paid' AND NOT EXISTS (SELECT 1 FROM pharmacy_unused_payment_refunds f WHERE f.tenant_id=r.tenant_id AND f.outlet_id=r.outlet_id AND f.payment_request_id=r.id) AND NOT EXISTS (SELECT 1 FROM pharmacy_sale_tenders t WHERE t.tenant_id=r.tenant_id AND t.payment_request_id=r.id)", p.TenantID, outlet).
			Find(&rows).Error
	})
	if err != nil {
		stockError(c, err)
	}
	return rows, nil
}

const paymentRequestColumns = "id,purpose,amount_minor,currency,status,qr_image_url,expires_at,failure_reason,provider_payment_id,rrn,paid_at,late,created_at"

func loadPaymentRequest(tx *gorm.DB, outlet, id uuid.UUID) (paymentRequest, error) {
	var rows []paymentRequest
	if err := tx.Raw(`SELECT `+paymentRequestColumns+` FROM pharmacy_payment_requests WHERE outlet_id=? AND id=?`, outlet, id).Scan(&rows).Error; err != nil {
		return paymentRequest{}, err
	}
	if len(rows) == 0 {
		return paymentRequest{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

type counterPaymentInput struct {
	AmountMinor int64 `json:"amount_minor" binding:"required,min=1,max=1000000000000"`
}

var errPaymentRequestDiffers = errors.New("pharmacy: payment request exists with a different amount")

// putCounterPayment asks payments for a UPI QR. The id is the client's, so
// a retried click finds the request it already made instead of a second.
func (h *catalogueHandlers) putCounterPayment(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var in counterPaymentInput
	if !decodeCatalogue(c, &in) {
		return
	}
	var out paymentRequest
	created := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var o pharmacyOutlet
		if err := tx.Select(outletColumns).Where("tenant_id=? AND id=?", p.TenantID, outlet).First(&o).Error; err != nil {
			return err
		}
		country, ok := countryprofile.Lookup(o.CountryCode)
		if !ok {
			return errNotSellable{"this outlet's country has no sale rules"}
		}
		res := tx.Exec(`INSERT INTO pharmacy_payment_requests (id,tenant_id,outlet_id,purpose,amount_minor,currency,actor_subject)
			VALUES (?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, id, p.TenantID, outlet, pharmacycontract.PaymentPurposeCounterSale, in.AmountMinor, country.Currency, p.Subject)
		if res.Error != nil {
			return res.Error
		}
		created = res.RowsAffected == 1
		var err error
		if out, err = loadPaymentRequest(tx, outlet, id); err != nil {
			return err
		}
		if !created {
			if out.AmountMinor != in.AmountMinor {
				return errPaymentRequestDiffers
			}
			return nil
		}
		data, err := json.Marshal(pharmacycontract.PaymentRequestedData{RequestID: id.String(), Purpose: pharmacycontract.PaymentPurposeCounterSale,
			AmountMinor: in.AmountMinor, Currency: country.Currency, ExpiresInSeconds: counterPaymentSeconds})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectPaymentRequested, events.Event{Type: "PaymentRequested", Version: 1, TenantID: p.TenantID, Data: data})
	})
	var refusal errNotSellable
	switch {
	case errors.Is(err, errPaymentRequestDiffers):
		respond.Conflict(c, "this payment request already exists with a different amount")
	case errors.As(err, &refusal):
		respond.Error(c, http.StatusConflict, "not_sellable", refusal.reason)
	case err != nil:
		stockError(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

func (h *catalogueHandlers) getCounterPayment(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var out paymentRequest
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		var err error
		out, err = loadPaymentRequest(tx, outlet, id)
		return err
	})
	if err != nil {
		stockError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}

// cancelCounterPayment asks payments to close the QR. The request waits in
// cancelling for the answer, which is cancelled, or paid if the money
// landed first: the cashier must not take another tender until then.
func (h *catalogueHandlers) cancelCounterPayment(c *gin.Context) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return
	}
	id, ok := catalogueID(c)
	if !ok {
		return
	}
	var out paymentRequest
	moved := false
	err := h.transaction(c.Request.Context(), p, outlet, true, func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE pharmacy_payment_requests SET status='cancelling' WHERE outlet_id=? AND id=? AND status IN ('requested','awaiting_payment')`, outlet, id)
		if res.Error != nil {
			return res.Error
		}
		moved = res.RowsAffected == 1
		var err error
		if out, err = loadPaymentRequest(tx, outlet, id); err != nil || !moved {
			return err
		}
		data, err := json.Marshal(pharmacycontract.PaymentCancelledData{RequestID: id.String(), AmountMinor: out.AmountMinor, Currency: out.Currency})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, pharmacycontract.SubjectPaymentCancelled, events.Event{Type: "PaymentCancelled", Version: 1, TenantID: p.TenantID, Data: data})
	})
	switch {
	case err != nil:
		stockError(c, err)
	case !moved:
		respond.Conflict(c, "this payment request is "+out.Status+" and can no longer be cancelled")
	default:
		respond.OK(c, out)
	}
}

// paymentOutcomeConsumers move a request on what payments reported. Every
// update is guarded by the statuses it may leave, so a redelivered or
// reordered event changes nothing it should not, and paid is never undone.
func paymentOutcomeConsumers() []events.Consumer {
	outcome := func(name, subject string, apply func(tx *gorm.DB, raw json.RawMessage, ref paymentscontract.IntentRef) error) events.Consumer {
		return events.Consumer{Name: name, Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var ref paymentscontract.IntentRef
			if err := json.Unmarshal(evt.Data, &ref); err != nil {
				return err
			}
			if ref.Requester != paymentscontract.RequesterPharmacy {
				return nil
			}
			id, err := uuid.Parse(ref.RequestID)
			if err != nil {
				return err
			}
			if err := tx.Exec(`SELECT set_config('app.pharmacy_payment_outcome',?,true)`, id.String()).Error; err != nil {
				return err
			}
			return apply(tx, evt.Data, ref)
		}}
	}
	return []events.Consumer{
		outcome("pharmacy-payment-opened", paymentscontract.SubjectIntentOpened, func(tx *gorm.DB, raw json.RawMessage, ref paymentscontract.IntentRef) error {
			var d paymentscontract.IntentOpenedData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			return tx.Exec(`UPDATE pharmacy_payment_requests SET intent_id=?, qr_image_url=?, expires_at=?,
				status=CASE WHEN status='requested' THEN 'awaiting_payment' ELSE status END
				WHERE id=? AND status IN ('requested','cancelling')`, ref.IntentID, d.QRImageURL, d.ExpiresAt, ref.RequestID).Error
		}),
		outcome("pharmacy-payment-failed", paymentscontract.SubjectIntentFailed, func(tx *gorm.DB, raw json.RawMessage, ref paymentscontract.IntentRef) error {
			var d paymentscontract.IntentFailedData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			return tx.Exec(`UPDATE pharmacy_payment_requests SET intent_id=?, status='failed', failure_reason=?
				WHERE id=? AND status IN ('requested','cancelling')`, ref.IntentID, d.Reason, ref.RequestID).Error
		}),
		outcome("pharmacy-payment-paid", paymentscontract.SubjectIntentPaid, func(tx *gorm.DB, raw json.RawMessage, ref paymentscontract.IntentRef) error {
			var d paymentscontract.IntentPaidData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			// Paid wins over every other state: the money is real, so a
			// request already told expired or cancelled becomes paid and is
			// marked late rather than the payment going unrecorded.
			return tx.Exec(`UPDATE pharmacy_payment_requests SET intent_id=?, status='paid', provider_payment_id=?, rrn=?, paid_at=?,
				late = ? OR status IN ('expired','cancelled','failed'), failure_reason=NULL
				WHERE id=? AND status <> 'paid' AND amount_minor=? AND currency=?`,
				ref.IntentID, d.ProviderPaymentID, d.RRN, d.PaidAt, d.Late, ref.RequestID, d.AmountMinor, d.Currency).Error
		}),
		outcome("pharmacy-payment-expired", paymentscontract.SubjectIntentExpired, func(tx *gorm.DB, _ json.RawMessage, ref paymentscontract.IntentRef) error {
			return tx.Exec(`UPDATE pharmacy_payment_requests SET status='expired' WHERE id=? AND status IN ('requested','awaiting_payment','cancelling')`, ref.RequestID).Error
		}),
		outcome("pharmacy-payment-cancelled", paymentscontract.SubjectIntentCancelled, func(tx *gorm.DB, _ json.RawMessage, ref paymentscontract.IntentRef) error {
			return tx.Exec(`UPDATE pharmacy_payment_requests SET status='cancelled' WHERE id=? AND status IN ('requested','awaiting_payment','cancelling')`, ref.RequestID).Error
		}),
	}
}
