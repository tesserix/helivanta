package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	billingcontract "github.com/tesserix/helivanta/internal/modules/billing/contract"
	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// UPI at the billing desk (#198 slice 3, #211): the cashier asks payments for
// a QR on an admission's account; payments answers by events, and a paid
// answer becomes the request's receipt in the database.

// upiSeconds is how long a desk QR stays payable, as at the pharmacy counter.
const upiSeconds = 300

var (
	errNoUpiRequest = errors.New("UPI request not found")
	errUpiNotOpen   = errors.New("this UPI request can no longer be cancelled")
)

type upiRequest struct {
	ID                uuid.UUID  `json:"id"`
	HospitalID        uuid.UUID  `json:"hospital_id"`
	ShiftID           uuid.UUID  `json:"shift_id"`
	CounterID         uuid.UUID  `json:"counter_id"`
	CounterCode       string     `json:"counter_code"`
	Cashier           string     `json:"cashier"`
	AdmissionID       uuid.UUID  `json:"admission_id"`
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
	// The receipt a paid request made, numbered on its counter.
	ReceiptNumber *int `json:"receipt_number"`
}

func (u upiRequest) PageKey() (time.Time, uuid.UUID) { return u.CreatedAt, u.ID }

const upiSelect = `SELECT u.id,u.hospital_id,u.shift_id,u.counter_id,c.code AS counter_code,u.cashier,u.admission_id,u.purpose,u.amount_minor,u.currency,
 u.status,u.qr_image_url,u.expires_at,u.failure_reason,u.provider_payment_id,u.rrn,u.paid_at,u.late,u.created_at,r.number AS receipt_number
 FROM billing_upi_requests u JOIN billing_counters c ON c.tenant_id=u.tenant_id AND c.id=u.counter_id
 LEFT JOIN billing_cash_receipts r ON r.tenant_id=u.tenant_id AND r.upi_request_id=u.id`

func loadUpiRequest(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (upiRequest, error) {
	var rows []upiRequest
	if err := tx.Raw(upiSelect+` WHERE u.tenant_id=? AND u.hospital_id=? AND u.id=?`, tenant, hospital, id).Scan(&rows).Error; err != nil {
		return upiRequest{}, err
	}
	if len(rows) == 0 {
		return upiRequest{}, errNoUpiRequest
	}
	return rows[0], nil
}

func upiFailure(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errNoUpiRequest):
		respond.NotFound(c, "UPI request")
	case errors.Is(err, errUpiNotOpen), errors.As(err, &pg) && pg.ConstraintName == "billing_upi_request_fixed":
		respond.Error(c, http.StatusConflict, "upi_not_open", errUpiNotOpen.Error())
	default:
		cashFailure(c, err)
	}
}

var upiPurposes = map[string]string{
	"payment": billingcontract.PaymentPurposeAdmissionPayment,
	"deposit": billingcontract.PaymentPurposeAdmissionDeposit,
}

// putUpiRequest asks payments for a QR in the caller's open shift. The id is
// the client's, so a retried click finds the request it already made.
func (h *handlers) putUpiRequest(c *gin.Context) {
	admission, err := uuid.Parse(c.Param("admission"))
	id, ok := v7(c, "request")
	var in struct {
		Purpose     string `json:"purpose"`
		AmountMinor int64  `json:"amount_minor"`
	}
	if bindErr := c.ShouldBindJSON(&in); err != nil || !ok || bindErr != nil || upiPurposes[in.Purpose] == "" || in.AmountMinor < 1 || in.AmountMinor > 1_000_000_000 {
		respond.BadRequest(c, errors.New("purpose payment or deposit, and an amount of 1 to 1000000000 minor units, are required"))
		return
	}
	var out upiRequest
	created := false
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		if err := lockAdmission(tx, p.TenantID, admission); err != nil {
			return err
		}
		held, err := loadUpiRequest(tx, p.TenantID, hospital, id)
		if err == nil {
			if held.AdmissionID != admission || held.Purpose != in.Purpose || held.AmountMinor != in.AmountMinor || held.Cashier != p.Subject {
				return errCashConflict
			}
			out = held
			return nil
		}
		if !errors.Is(err, errNoUpiRequest) {
			return err
		}
		var status []string
		if err := tx.Raw(`SELECT status FROM billing_admission_accounts WHERE tenant_id=? AND hospital_id=? AND admission_id=? FOR SHARE`, p.TenantID, hospital, admission).Scan(&status).Error; err != nil {
			return err
		}
		if len(status) == 0 {
			return errNoAccount
		}
		if in.Purpose == "deposit" && status[0] != "open" {
			return errAccountClosed
		}
		var open []struct {
			ID        uuid.UUID
			CounterID uuid.UUID
		}
		if err := tx.Raw(`SELECT id,counter_id FROM billing_cash_shifts WHERE tenant_id=? AND hospital_id=? AND cashier=? AND status='open' FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&open).Error; err != nil {
			return err
		}
		if len(open) == 0 {
			return errNoShift
		}
		if in.Purpose == "payment" {
			due, err := balanceDue(tx, p.TenantID, admission, shiftCurrency)
			if err != nil {
				return err
			}
			if in.AmountMinor > due {
				return errOverBalance
			}
		}
		if err := tx.Exec(`INSERT INTO billing_upi_requests (tenant_id,hospital_id,id,shift_id,counter_id,cashier,admission_id,purpose,amount_minor,currency) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, id, open[0].ID, open[0].CounterID, p.Subject, admission, in.Purpose, in.AmountMinor, shiftCurrency).Error; err != nil {
			return err
		}
		data, err := json.Marshal(billingcontract.PaymentRequestedData{RequestID: id.String(), Purpose: upiPurposes[in.Purpose],
			AmountMinor: in.AmountMinor, Currency: shiftCurrency, ExpiresInSeconds: upiSeconds})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, billingcontract.SubjectPaymentRequested, events.Event{Type: "PaymentRequested", Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		created = true
		out, err = loadUpiRequest(tx, p.TenantID, hospital, id)
		return err
	})
	switch {
	case err != nil:
		upiFailure(c, err)
	case created:
		respond.Created(c, out)
	default:
		respond.OK(c, out)
	}
}

func (h *handlers) upiRequest(c *gin.Context) {
	id, ok := v7(c, "request")
	if !ok {
		respond.NotFound(c, "UPI request")
		return
	}
	var out upiRequest
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var err error
		out, err = loadUpiRequest(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		upiFailure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, out)
}

// cancelUpiRequest asks payments to close the QR. The request waits in
// cancelling for the answer: cancelled, or paid if the money landed first.
// Only the cashier who asked cancels; anyone else gets the same 404 as none.
func (h *handlers) cancelUpiRequest(c *gin.Context) {
	id, ok := v7(c, "request")
	if !ok {
		respond.NotFound(c, "UPI request")
		return
	}
	var out upiRequest
	err := h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		u, err := loadUpiRequest(tx, p.TenantID, hospital, id)
		if err != nil {
			return err
		}
		if u.Cashier != p.Subject {
			return errNoUpiRequest
		}
		res := tx.Exec(`UPDATE billing_upi_requests SET status='cancelling' WHERE tenant_id=? AND id=? AND status IN ('requested','awaiting_payment')`, p.TenantID, id)
		if res.Error != nil {
			return res.Error
		}
		if out, err = loadUpiRequest(tx, p.TenantID, hospital, id); err != nil {
			return err
		}
		if res.RowsAffected == 0 {
			// A retried cancel finds it cancelling or settled: answered from the record.
			if out.Status == "cancelling" || out.Status == "cancelled" {
				return nil
			}
			return errUpiNotOpen
		}
		data, err := json.Marshal(billingcontract.PaymentCancelledData{RequestID: id.String(), AmountMinor: out.AmountMinor, Currency: out.Currency})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, billingcontract.SubjectPaymentCancelled, events.Event{Type: "PaymentCancelled", Version: 1, TenantID: p.TenantID, Data: data})
	})
	if err != nil {
		upiFailure(c, err)
		return
	}
	respond.OK(c, out)
}

// upiRequests lists an admission's UPI requests, newest first, so a desk
// that reloads finds a QR still waiting.
func (h *handlers) upiRequests(c *gin.Context, params pagination.Params) ([]upiRequest, error) {
	admission, err := uuid.Parse(c.Param("admission"))
	if err != nil {
		respond.NotFound(c, "account")
		return nil, nil
	}
	var rows []upiRequest
	err = h.inHospital(c, func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) error {
		var known int64
		if err := tx.Table("billing_admission_accounts").Where("tenant_id=? AND hospital_id=? AND admission_id=?", p.TenantID, hospital, admission).Count(&known).Error; err != nil {
			return err
		}
		if known == 0 {
			return errNoAccount
		}
		q := tx.Table("(?) AS requests", tx.Raw(upiSelect+` WHERE u.tenant_id=? AND u.hospital_id=? AND u.admission_id=?`, p.TenantID, hospital, admission))
		return tenantdb.ApplyOrderedKeyset(q, params, tenantdb.Keyset{Column: "created_at", Direction: pagination.NewestFirst}).Find(&rows).Error
	})
	if err != nil {
		failure(c, err)
		return nil, nil
	}
	return rows, nil
}

// upiOutcomeConsumers move a request on what payments reported. Each update
// is guarded by the statuses it may leave, so a redelivered or reordered
// event changes nothing it should not; the database holds the same edges.
// Paid wins over every other status, and its receipt is written by the
// transition's trigger, once.
func upiOutcomeConsumers() []events.Consumer {
	outcome := func(name, subject string, apply func(tx *gorm.DB, tenant string, raw json.RawMessage, ref paymentscontract.IntentRef, id uuid.UUID) error) events.Consumer {
		return events.Consumer{Name: name, Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var ref paymentscontract.IntentRef
			if evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &ref) != nil {
				return errors.New("invalid payment outcome event")
			}
			if ref.Requester != paymentscontract.RequesterBilling {
				return nil
			}
			id, err := uuid.Parse(ref.RequestID)
			if err != nil {
				return errors.New("invalid payment outcome event")
			}
			return apply(tx, evt.TenantID, evt.Data, ref, id)
		}}
	}
	return []events.Consumer{
		outcome("billing-upi-opened", paymentscontract.SubjectIntentOpened, func(tx *gorm.DB, _ string, raw json.RawMessage, ref paymentscontract.IntentRef, id uuid.UUID) error {
			var d paymentscontract.IntentOpenedData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			return tx.Exec(`UPDATE billing_upi_requests SET intent_id=?, qr_image_url=?, expires_at=?,
				status=CASE WHEN status='requested' THEN 'awaiting_payment' ELSE status END
				WHERE id=? AND status IN ('requested','cancelling')`, ref.IntentID, d.QRImageURL, d.ExpiresAt, id).Error
		}),
		outcome("billing-upi-failed", paymentscontract.SubjectIntentFailed, func(tx *gorm.DB, _ string, raw json.RawMessage, ref paymentscontract.IntentRef, id uuid.UUID) error {
			var d paymentscontract.IntentFailedData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			return tx.Exec(`UPDATE billing_upi_requests SET intent_id=?, status='failed', failure_reason=?
				WHERE id=? AND status IN ('requested','cancelling')`, ref.IntentID, d.Reason, id).Error
		}),
		outcome("billing-upi-paid", paymentscontract.SubjectIntentPaid, func(tx *gorm.DB, tenant string, raw json.RawMessage, ref paymentscontract.IntentRef, id uuid.UUID) error {
			var d paymentscontract.IntentPaidData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			// The admission lock first, as every charge and cash receipt takes
			// it before the counter: the receipt this writes takes both, and the
			// opposite order deadlocks with a cashier at the same counter.
			var admission []uuid.UUID
			if err := tx.Raw(`SELECT admission_id FROM billing_upi_requests WHERE id=?`, id).Scan(&admission).Error; err != nil {
				return err
			}
			if len(admission) == 0 {
				return nil
			}
			if err := lockAdmission(tx, tenant, admission[0]); err != nil {
				return err
			}
			// The money is real: a request already told expired, cancelled or
			// failed becomes paid, marked late, rather than the payment going
			// unrecorded. Another amount or currency pays nothing.
			return tx.Exec(`UPDATE billing_upi_requests SET intent_id=?, status='paid', provider_payment_id=?, rrn=?, paid_at=?,
				late = ? OR status IN ('expired','cancelled','failed'), failure_reason=NULL
				WHERE id=? AND status <> 'paid' AND amount_minor=? AND currency=?`,
				ref.IntentID, d.ProviderPaymentID, d.RRN, d.PaidAt, d.Late, id, d.AmountMinor, d.Currency).Error
		}),
		outcome("billing-upi-expired", paymentscontract.SubjectIntentExpired, func(tx *gorm.DB, _ string, _ json.RawMessage, _ paymentscontract.IntentRef, id uuid.UUID) error {
			return tx.Exec(`UPDATE billing_upi_requests SET status='expired' WHERE id=? AND status IN ('requested','awaiting_payment','cancelling')`, id).Error
		}),
		outcome("billing-upi-cancelled", paymentscontract.SubjectIntentCancelled, func(tx *gorm.DB, _ string, _ json.RawMessage, _ paymentscontract.IntentRef, id uuid.UUID) error {
			return tx.Exec(`UPDATE billing_upi_requests SET status='cancelled' WHERE id=? AND status IN ('requested','awaiting_payment','cancelling')`, id).Error
		}),
	}
}
