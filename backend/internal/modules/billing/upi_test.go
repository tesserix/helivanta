package billing_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type upiBody struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	Purpose       string  `json:"purpose"`
	QRImageURL    *string `json:"qr_image_url"`
	RRN           *string `json:"rrn"`
	Late          bool    `json:"late"`
	ReceiptNumber *int    `json:"receipt_number"`
}

func (w *bedWorld) askUpi(token, admission, id, purpose string, amount int64) (int, upiBody, string) {
	status, body := w.put(token, "/admissions/"+admission+"/upi-requests/"+id, fmt.Sprintf(`{"purpose":%q,"amount_minor":%d}`, purpose, amount))
	var u upiBody
	_ = json.Unmarshal([]byte(body), &u)
	return status, u, body
}

func (w *bedWorld) upi(id string) upiBody {
	res := do(w.r, "GET", w.root+"/upi-requests/"+id, "clerk", "")
	require.Equal(w.t, http.StatusOK, res.Code, res.Body.String())
	var u upiBody
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &u))
	return u
}

func ref(id string) paymentscontract.IntentRef {
	return paymentscontract.IntentRef{Requester: paymentscontract.RequesterBilling, RequestID: id, IntentID: uuid.NewString()}
}

func (w *bedWorld) opened(id string) {
	require.NoError(w.t, w.deliver("billing-upi-opened", paymentscontract.IntentOpenedData{IntentRef: ref(id), QRImageURL: "https://rzp.io/i/qr_1", ExpiresAt: time.Now().Add(5 * time.Minute)}))
}

func (w *bedWorld) paid(r paymentscontract.IntentRef, amount int64, late bool) {
	require.NoError(w.t, w.deliver("billing-upi-paid", paymentscontract.IntentPaidData{IntentRef: r, AmountMinor: amount, Currency: "INR",
		ProviderPaymentID: "pay_" + r.RequestID[:8], RRN: "RRN" + r.RequestID[:8], PaidAt: time.Now(), Late: late}))
}

func (w *bedWorld) receiptsOf(admission string) []struct {
	Number  int     `json:"number"`
	Mode    string  `json:"mode"`
	Purpose string  `json:"purpose"`
	ShiftID string  `json:"shift_id"`
	RRN     *string `json:"rrn"`
	Late    bool    `json:"late"`
} {
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/receipts", "nurse", "")
	require.Equal(w.t, http.StatusOK, res.Code, res.Body.String())
	var page struct {
		Data []struct {
			Number  int     `json:"number"`
			Mode    string  `json:"mode"`
			Purpose string  `json:"purpose"`
			ShiftID string  `json:"shift_id"`
			RRN     *string `json:"rrn"`
			Late    bool    `json:"late"`
		} `json:"data"`
	}
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &page))
	return page.Data
}

func (w *bedWorld) outboxFor(t *testing.T, subject, request string) []string {
	var rows []string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=?`, subject, request).Scan(&rows).Error
	}))
	return rows
}

// A UPI payment is asked in the cashier's shift, holds its amount against the
// balance while it waits, and becomes a numbered receipt once payments says
// it was paid, outside the drawer.
func TestAUpiPaymentBecomesAReceiptOutsideTheDrawer(t *testing.T) {
	w := newBedWorld(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)

	id := v7()
	status, _, body := w.askUpi("clerk", admission, id, "payment", 3000)
	require.Equal(t, http.StatusConflict, status, "no shift open: "+body)
	require.Contains(t, body, "no_open_shift")
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 0)

	status, u, body := w.askUpi("clerk", admission, id, "payment", 3000)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "requested", u.Status)
	sent := w.outboxFor(t, "helivanta.in.billing.payment_requested.v1", id)
	require.Len(t, sent, 1)
	require.Contains(t, sent[0], `"purpose": "admission_payment"`)
	require.Contains(t, sent[0], `"expires_in_seconds": 300`)
	require.NotContains(t, sent[0], admission, "no admission on the wire")
	require.NotContains(t, sent[0], w.hospital, "no hospital on the wire")
	status, _, _ = w.askUpi("clerk", admission, id, "payment", 3000)
	require.Equal(t, http.StatusOK, status, "a retried ask replays")
	require.Len(t, w.outboxFor(t, "helivanta.in.billing.payment_requested.v1", id), 1, "and asks payments once")
	status, _, _ = w.askUpi("clerk", admission, id, "payment", 2999)
	require.Equal(t, http.StatusConflict, status, "the same id for another amount")
	status, _, _ = w.askUpi("clerk", admission, v7(), "card", 100)
	require.Equal(t, http.StatusBadRequest, status)

	// The waiting 3000 is held: neither UPI nor cash can take it again.
	status, _, body = w.askUpi("clerk", admission, v7(), "payment", 2001)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_balance")
	status, body = w.pay("clerk", admission, 2001)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_balance", "cash cannot pay what a QR is waiting for")
	status, body = w.pay("clerk", admission, 2000)
	require.Equal(t, http.StatusCreated, status, body)
	acct := do(w.r, "GET", w.root+"/admissions/"+admission+"/account", "nurse", "").Body.String()
	require.Contains(t, acct, `"upi_waiting_minor":3000`)
	require.Contains(t, acct, `"balance_minor":3000`, "waiting is not paid")

	w.opened(id)
	u = w.upi(id)
	require.Equal(t, "awaiting_payment", u.Status)
	require.Equal(t, "https://rzp.io/i/qr_1", *u.QRImageURL)
	status, _ = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(2000))
	require.Equal(t, http.StatusConflict, status, "a shift does not close while its QR waits")

	// Paid: one receipt, numbered next on the counter, redelivery or not.
	r := ref(id)
	w.paid(r, 3000, false)
	w.paid(r, 3000, false)
	u = w.upi(id)
	require.Equal(t, "paid", u.Status)
	require.False(t, u.Late)
	require.Equal(t, 2, *u.ReceiptNumber)
	got := w.receiptsOf(admission)
	require.Len(t, got, 2, "one payment, one receipt")
	require.Equal(t, "upi", got[0].Mode)
	require.Equal(t, "payment", got[0].Purpose)
	require.Equal(t, 2, got[0].Number)
	require.Equal(t, shiftID, got[0].ShiftID)
	require.Equal(t, "RRN"+id[:8], *got[0].RRN)
	require.Equal(t, "cash", got[1].Mode)
	acct = do(w.r, "GET", w.root+"/admissions/"+admission+"/account", "nurse", "").Body.String()
	require.Contains(t, acct, `"paid_minor":5000`)
	require.Contains(t, acct, `"upi_waiting_minor":0`)
	require.Contains(t, acct, `"balance_minor":0`)

	status, body = w.put("clerk", "/receipts/"+id+"/voids/"+v7(), `{"reason":"Changed mind"}`)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "upi_receipt", "the money is at the bank, whatever the drawer holds")

	// The drawer holds the cash, not the UPI.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(2000))
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`)
	require.Contains(t, body, `"expected_minor":2000`)

	day := do(w.r, "GET", w.root+"/collections/"+time.Now().In(ist).Format(time.DateOnly), "admin", "").Body.String()
	require.Contains(t, day, `"payments_minor":2000,"deposits_minor":0,"refunds_minor":0,"net_minor":2000`, "cash totals stay cash")
	require.Contains(t, day, `"upi_payments_minor":3000,"upi_deposits_minor":0,"picked_up_minor":0,"card_payments_minor":0`, "UPI beside it")
	res := do(w.r, "GET", w.root+"/collections/"+time.Now().In(ist).Format(time.DateOnly)+"/export.csv", "admin", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), "authorisation_reason,upi_payments,upi_deposits,picked_up,card_payments,")
	require.Regexp(t, `\n[^\n]*,IPD-1,[^\n]*,,,30\.00,0\.00,0\.00,0\.00,0\.00,0\.00,0\.00,0\.00,0\.00\r\n`, res.Body.String(), "the shift carries its UPI")
	require.Regexp(t, `\n[^\n]*,TOTAL,[^\n]*,,,30\.00,0\.00,0\.00,0\.00,0\.00,0\.00,0\.00,0\.00,0\.00\r\n`, res.Body.String(), "and so does the TOTAL")
}

// A cancel waits for payments' answer; money that lands after the QR closed
// is still recorded, in the shift that asked, even once it has closed.
func TestLateUpiMoneyIsRecordedInTheShiftThatAsked(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 500)

	id := v7()
	status, _, body := w.askUpi("clerk", admission, id, "deposit", 7000)
	require.Equal(t, http.StatusCreated, status, "a deposit has no cap: "+body)
	w.opened(id)
	require.Equal(t, http.StatusNotFound, do(w.r, "POST", w.root+"/upi-requests/"+id+"/cancel", "clerk2", "").Code, "only the cashier who asked")
	res := do(w.r, "POST", w.root+"/upi-requests/"+id+"/cancel", "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"status":"cancelling"`)
	require.Len(t, w.outboxFor(t, "helivanta.in.billing.payment_cancelled.v1", id), 1)
	require.Equal(t, http.StatusOK, do(w.r, "POST", w.root+"/upi-requests/"+id+"/cancel", "clerk", "").Code, "a retried cancel replays")
	require.Len(t, w.outboxFor(t, "helivanta.in.billing.payment_cancelled.v1", id), 1, "and asks once")
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(500))
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "upi_waiting", "cancelling still waits for the answer")

	require.NoError(t, w.deliver("billing-upi-cancelled", paymentscontract.IntentCancelledData{IntentRef: ref(id)}))
	require.Equal(t, "cancelled", w.upi(id).Status)
	require.Equal(t, http.StatusOK, do(w.r, "POST", w.root+"/upi-requests/"+id+"/cancel", "clerk", "").Code, "cancelling what was cancelled replays")
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(500))
	require.Equal(t, http.StatusOK, status, body)

	// Another requester's outcome, and the wrong amount, pay nothing.
	other := ref(id)
	other.Requester = paymentscontract.RequesterPharmacy
	w.paid(other, 7000, false)
	w.paid(ref(id), 6999, false)
	require.Equal(t, "cancelled", w.upi(id).Status)

	w.paid(ref(id), 7000, false)
	u := w.upi(id)
	require.Equal(t, "paid", u.Status)
	require.True(t, u.Late, "told cancelled, then paid")
	got := w.receiptsOf(admission)
	require.Len(t, got, 1)
	require.Equal(t, "deposit", got[0].Purpose)
	require.Equal(t, shiftID, got[0].ShiftID, "in the shift that asked, closed or not")
	require.True(t, got[0].Late)
	require.Equal(t, int64(7000), w.inr(admission).DepositHeldMinor)
	res = do(w.r, "GET", w.root+"/cash-shifts", "admin", "")
	require.Contains(t, res.Body.String(), `"expected_minor":500`, "the closed drawer is unchanged")

	// A paid request never moves again.
	require.NoError(t, w.deliver("billing-upi-expired", paymentscontract.IntentExpiredData{IntentRef: ref(id)}))
	require.Equal(t, "paid", w.upi(id).Status)
	res = do(w.r, "POST", w.root+"/upi-requests/"+id+"/cancel", "clerk", "")
	require.Equal(t, http.StatusConflict, res.Code)
	require.Contains(t, res.Body.String(), "upi_not_open")
}

func TestUpiRulesHoldInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	w.counterWithShift(t, "clerk", "IPD-1", 0)
	id := v7()
	status, _, body := w.askUpi("clerk", admission, id, "payment", 1000)
	require.Equal(t, http.StatusCreated, status, body)

	as := func(actor string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, actor), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, actor).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}

	// Only an unanswered request younger than 15 minutes is waiting.
	var pending []bool
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_upi_pending(s, now() - (m || ' minutes')::interval) FROM (VALUES ('requested',14),('requested',16),('cancelling',1),('expired',1),('paid',1)) v(s,m)`).Scan(&pending).Error
	}))
	require.Equal(t, []bool{true, false, true, false, false}, pending)

	// The cashier cannot write a UPI receipt, nor move a request but to cancel it.
	err := as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency,mode,upi_request_id)
			SELECT tenant_id,hospital_id,id,shift_id,counter_id,1,cashier,admission_id,purpose,amount_minor,currency,'upi',id FROM billing_upi_requests WHERE id=?`, id).Error
	})
	requireConstraint(t, err, "billing_cash_receipt_upi_request", "a UPI receipt is the paid request's, by its consumer")
	err = as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_upi_requests SET status='paid',provider_payment_id='pay_x',paid_at=now() WHERE id=?`, id).Error
	})
	requireConstraint(t, err, "billing_upi_request_fixed", "only payments' answer pays")
	err = as("user-clerk2", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_upi_requests SET status='cancelling' WHERE id=?`, id).Error
	})
	requireConstraint(t, err, "billing_upi_request_fixed", "only the cashier who asked cancels")

	// A consumer moves along the lifecycle only, and never changes the ask.
	err = as("system:billing-upi-opened", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_upi_requests SET status='requested' WHERE id=?`, id).Error
	})
	require.NoError(t, err, "no move is no move")
	err = as("system:billing-upi-opened", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_upi_requests SET status='cancelling' WHERE id=?`, id).Error
	})
	requireConstraint(t, err, "billing_upi_request_fixed", "cancelling is the cashier's move")
	err = as("system:billing-upi-paid", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_upi_requests SET amount_minor=2 WHERE id=?`, id).Error
	})
	requireConstraint(t, err, "billing_upi_request_fixed", "the amount asked never changes")

	// A request id is never a receipt's, nor a receipt's id a request's.
	status, _ = w.put("clerk", "/admissions/"+admission+"/receipts/"+id, `{"amount_minor":100,"currency":"INR"}`)
	require.Equal(t, http.StatusConflict, status, "a cash receipt cannot take a request's id")
	cash := v7()
	status, body = w.put("clerk", "/admissions/"+admission+"/receipts/"+cash, `{"amount_minor":100,"currency":"INR"}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, _, _ = w.askUpi("clerk", admission, cash, "payment", 100)
	require.Equal(t, http.StatusConflict, status, "a request cannot take a receipt's id")
}

// Deposits applied while a UPI payment waits cover only what the QR does not,
// and the database refuses a UPI receipt's void on its own.
func TestAWaitingUpiPaymentBoundsDepositApplications(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.counterWithShift(t, "clerk", "IPD-1", 0)
	status, body := w.deposit("clerk", admission, 4000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, 5000)
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)

	id := v7()
	status, _, body = w.askUpi("clerk", admission, id, "payment", 3000)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.apply("clerk", admission)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"amount_minor":2000`, "the waiting 3000 is not covered twice")

	asClerk := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	err := asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_deposit_applications (tenant_id,hospital_id,id,admission_id,bill_number,amount_minor,currency,applied_by)
			SELECT tenant_id,hospital_id,?,admission_id,number,1,'INR','user-clerk' FROM billing_interim_bills WHERE admission_id=? AND kind='final'`, v7(), admission).Error
	})
	requireConstraint(t, err, "billing_deposit_application_bound", "the database holds the same bound")

	w.paid(ref(id), 3000, false)
	require.Equal(t, int64(0), w.inr(admission).BalanceMinor)
	err = asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason='x',voided_at=now() WHERE id=?`, v7(), id).Error
	})
	requireConstraint(t, err, "billing_cash_receipt_upi_void", "a UPI receipt is not voided")
	err = w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-upi-paid"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_upi_requests SET rrn='other' WHERE id=?`, id).Error
	})
	requireConstraint(t, err, "billing_upi_request_fixed", "a paid request never changes, even by a consumer")
}

// A paid event and a cash receipt on the same counter and admission take the
// admission lock first, as every charge and receipt does, so neither is
// aborted as a deadlock.
func TestAPaidEventAndCashAtTheSameCounterDoNotDeadlock(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, _ := w.counterWithShift(t, "clerk", "IPD-1", 0)
	id := v7()
	status, _, body := w.askUpi("clerk", admission, id, "payment", 1000)
	require.Equal(t, http.StatusCreated, status, body)

	// The cash path's order: the admission lock, then the counter.
	locked, release := make(chan struct{}), make(chan struct{})
	cashDone := make(chan error, 1)
	go func() {
		cashDone <- w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?::text||':billing-bed-days:'||?::text, 0))`, testutil.TenantA, admission).Error; err != nil {
				return err
			}
			close(locked)
			<-release
			var got []string
			if err := tx.Raw(`SELECT id::text FROM billing_counters WHERE tenant_id=? AND id=? FOR UPDATE`, testutil.TenantA, counter).Scan(&got).Error; err != nil {
				return err
			}
			if len(got) != 1 {
				return fmt.Errorf("the counter was not locked: %v", got)
			}
			return nil
		})
	}()
	<-locked
	paidDone := make(chan error, 1)
	go func() {
		paidDone <- w.deliver("billing-upi-paid", paymentscontract.IntentPaidData{IntentRef: ref(id), AmountMinor: 1000, Currency: "INR",
			ProviderPaymentID: "pay_x", RRN: "RRN1", PaidAt: time.Now()})
	}()
	time.Sleep(300 * time.Millisecond) // let the paid event reach its locks
	close(release)
	require.NoError(t, <-cashDone, "the cash side is not aborted")
	require.NoError(t, <-paidDone, "nor the paid event")
	require.Equal(t, "paid", w.upi(id).Status)
}
