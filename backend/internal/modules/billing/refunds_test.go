package billing_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// refundable sets up an admission with a deposit held beyond what it owes and
// a final bill, and the clerk's open shift with the deposit in the drawer.
func (w *bedWorld) refundable(t *testing.T, deposit, owed int64) (admission, shift string) {
	t.Helper()
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	_, shift = w.counterWithShift(t, "clerk", "IPD-R"+strings.ToUpper(admission[:4]), 0)
	status, body := w.deposit("clerk", admission, deposit)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, owed)
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.apply("clerk", admission)
	require.Equal(t, http.StatusCreated, status, body)
	return admission, shift
}

func (w *bedWorld) refund(admission string, amount int64, approval string) (int, string) {
	body := fmt.Sprintf(`{"amount_minor":%d,"currency":"INR"}`, amount)
	if approval != "" {
		body = fmt.Sprintf(`{"amount_minor":%d,"currency":"INR","approval_id":%q}`, amount, approval)
	}
	return w.put("clerk", "/admissions/"+admission+"/refunds/"+v7(), body)
}

func TestARefundPaysBackWhatIsHeldAndNetsTheDrawer(t *testing.T) {
	w := newBedWorld(t)
	admission, shiftID := w.refundable(t, 500000, 200000)
	require.Equal(t, inrTotals{OutstandingMinor: 200000, AppliedMinor: 200000, DepositHeldMinor: 300000}, w.inr(admission))

	status, body := w.refund(admission, 300001, "")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_held")
	status, body = w.refund(admission, 100000, "")
	require.Equal(t, http.StatusCreated, status, "₹1,000 is within the default ₹10,000 threshold: "+body)
	require.Contains(t, body, `"purpose":"refund"`)
	require.Contains(t, body, `"number":2`, "refunds share the counter's numbering")
	totals := w.inr(admission)
	require.EqualValues(t, 200000, totals.DepositHeldMinor)
	status, body = w.refund(admission, 200001, "")
	require.Contains(t, body, "over_held", "what was refunded is no longer held: %d", status)
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/account", "nurse", "")
	require.Contains(t, res.Body.String(), `"refunded_minor":100000`)

	// The drawer holds 5000 less 1000 refunded: the close is counted against 4000.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":400000}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"expected_minor":400000`)
	require.Contains(t, body, `"status":"closed"`)
}

func TestARefundAboveTheThresholdNeedsSomeoneElsesApproval(t *testing.T) {
	w := newBedWorld(t)
	admission, _ := w.refundable(t, 10000000, 1000000)

	// The threshold: default ₹10,000; an administrator sets ₹20,000.
	res := do(w.r, "GET", w.root+"/refund-policy", "nurse", "")
	require.Contains(t, res.Body.String(), `"threshold_minor":1000000`)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/refund-policies/"+v7(), "clerk", `{"threshold_minor":2000000}`).Code)
	policy := v7()
	status, body := w.put("admin", "/refund-policies/"+policy, `{"threshold_minor":2000000}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, _ = w.put("admin", "/refund-policies/"+policy, `{"threshold_minor":2000000}`)
	require.Equal(t, http.StatusOK, status, "a retry replays")
	res = do(w.r, "GET", w.root+"/refund-policy", "nurse", "")
	require.Contains(t, res.Body.String(), `"threshold_minor":2000000`)

	status, body = w.refund(admission, 2000001, "")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "approval_required")

	// The clerk2 supervisor approves ₹25,000; the clerk pays exactly that, once.
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/admissions/"+admission+"/refund-approvals/"+v7(), "clerk", `{"amount_minor":2500000,"reason":"x"}`).Code)
	status, body = w.put("clerk2", "/admissions/"+admission+"/refund-approvals/"+v7(), `{"amount_minor":9000001,"reason":"Too much"}`)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_held")
	approval := v7()
	status, body = w.put("clerk2", "/admissions/"+admission+"/refund-approvals/"+approval, `{"amount_minor":2500000,"reason":"Discharged early"}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.refund(admission, 3000000, approval)
	require.Contains(t, body, "approval_required", "an approval is for its amount only: %d", status)
	status, body = w.refund(admission, 2500000, approval)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.refund(admission, 2500000, approval)
	require.Contains(t, body, "approval_required", "an approval is used once: %d", status)
	res = do(w.r, "GET", w.root+"/admissions/"+admission+"/refund-approvals", "nurse", "")
	require.Contains(t, res.Body.String(), `"used_by":"`)
	// Voided, the refund's cash is back in the drawer and held again, but its
	// approval stays used: paying it again needs a new approval.
	var paidRefund struct {
		ID string `json:"id"`
	}
	res = do(w.r, "GET", w.root+"/admissions/"+admission+"/receipts", "nurse", "")
	var list struct {
		Data []struct {
			ID      string `json:"id"`
			Purpose string `json:"purpose"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	for _, r := range list.Data {
		if r.Purpose == "refund" {
			paidRefund.ID = r.ID
		}
	}
	require.NotEmpty(t, paidRefund.ID)
	status, body = w.put("clerk", "/receipts/"+paidRefund.ID+"/voids/"+v7(), `{"reason":"Paid the wrong relative"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = w.refund(admission, 2500000, approval)
	require.Contains(t, body, "approval_required", "a voided refund's approval is not reused: %d", status)

	// The cashier's own approval does not count.
	own := v7()
	status, body = w.put("clerk", "/admissions/"+admission+"/refund-approvals/"+own, `{"amount_minor":2100000,"reason":"Mine"}`)
	require.Equal(t, http.StatusForbidden, status, body)
}

func TestARefundWaitsForTheFinalBillAndTheDrawer(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	// The deposit was taken at another counter: this drawer holds only its float.
	w.counterWithShift(t, "clerk2", "IPD-RX", 0)
	status, body := w.deposit("clerk2", admission, 500000)
	require.Equal(t, http.StatusCreated, status, body)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-RY", 10000)
	raw := func(amount int64) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency)
 VALUES (?,?,?,?,?,1,'user-clerk',?,'refund',?,'INR')`, testutil.TenantA, w.hospital, v7(), shiftID, counter, admission, amount).Error
		})
	}
	require.Error(t, raw(1000), "no refund before a final bill, by the database alone")
	status, body = w.refund(admission, 1000, "")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "no_final_bill")
	w.charge(admission, patient, 100)
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	require.Error(t, raw(10001), "never more than the drawer, by the database alone")
	status, body = w.refund(admission, 10001, "")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "drawer_short", "the float is 10000")
	status, body = w.refund(admission, 10000, "")
	require.Equal(t, http.StatusCreated, status, body)
}

func TestConcurrentRefundsNeverExceedWhatIsHeld(t *testing.T) {
	w := newBedWorld(t)
	admission, _ := w.refundable(t, 500000, 200000)
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = w.refund(admission, 200000, "")
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			created++
		}
	}
	require.Equal(t, 1, created, "₹2,000 of ₹3,000 held fits once")
}

func TestRefundRulesHoldInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	admission, shiftID := w.refundable(t, 5000000, 1000000)
	as := func(subject string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, subject).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	var counter string
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT counter_id FROM billing_cash_shifts WHERE id=?`, shiftID).Scan(&counter).Error
	}))
	require.NotEmpty(t, counter, "the cashier sees their own shift")
	var noApproval *uuid.UUID
	insertRefund := func(amount int64, approval any) error {
		return as("user-clerk", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency,approval_id)
 VALUES (?,?,?,?,?,2,'user-clerk',?,'refund',?,'INR',?)`, testutil.TenantA, w.hospital, v7(), shiftID, counter, admission, amount, approval).Error
		})
	}
	// With the threshold out of the way, only what is held refuses it.
	status, body := w.put("admin", "/refund-policies/"+v7(), `{"threshold_minor":1000000000}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Error(t, insertRefund(4000001, noApproval), "never more than held")
	status, body = w.put("admin", "/refund-policies/"+v7(), `{"threshold_minor":1000000}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Error(t, insertRefund(1000001, noApproval), "above the threshold, no approval")
	own := v7()
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_refund_approvals (tenant_id,hospital_id,id,admission_id,amount_minor,currency,reason,approved_by) VALUES (?,?,?,?,1500000,'INR','Mine','user-clerk')`,
			testutil.TenantA, w.hospital, own, admission).Error
	}))
	require.Error(t, insertRefund(1500000, own), "never against the cashier's own approval")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_refund_approvals (tenant_id,hospital_id,id,admission_id,amount_minor,currency,reason,approved_by) VALUES (?,?,?,?,1500000,'INR','Forged','user-clerk2')`,
			testutil.TenantA, w.hospital, v7(), admission).Error
	}), "an approval names its own approver")
	require.NoError(t, insertRefund(1000000, noApproval), "at the threshold, no approval needed")
	// An approval is used once, by the database alone.
	other := v7()
	require.NoError(t, as("user-clerk2", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_refund_approvals (tenant_id,hospital_id,id,admission_id,amount_minor,currency,reason,approved_by) VALUES (?,?,?,?,1100000,'INR','Ok','user-clerk2')`,
			testutil.TenantA, w.hospital, other, admission).Error
	}))
	use := func(number int) error {
		return as("user-clerk", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency,approval_id)
 VALUES (?,?,?,?,?,?,'user-clerk',?,'refund',1100000,'INR',?)`, testutil.TenantA, w.hospital, v7(), shiftID, counter, number, admission, other).Error
		})
	}
	require.NoError(t, use(3))
	require.Error(t, use(4), "an approval is used once")
	// The deposit can no longer be voided below what was applied or refunded.
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason='x',voided_at=now() WHERE admission_id=? AND purpose='deposit'`, v7(), admission).Error
	}))
	var expected struct{ V int64 }
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_shift_expected(?,?) AS v`, testutil.TenantA, shiftID).Scan(&expected).Error
	}))
	require.EqualValues(t, 2900000, expected.V, "the drawer nets the refunds")
}

// Only the surplus is refunded: a deposit still needed for the bill is not
// unused, applied or not.
func TestARefundIsCappedAtTheSurplusOverTheBill(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.counterWithShift(t, "clerk", "IPD-RS", 0)
	status, body := w.deposit("clerk", admission, 400000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, 300000)
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	// Nothing applied yet: 4000 held, 3000 due, so 1000 is the surplus.
	status, body = w.refund(admission, 100001, "")
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_held", "the deposit still covers the bill")
	var counter, shiftID string
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT counter_id,shift_id FROM billing_cash_receipts WHERE admission_id=?`, admission).Row().Scan(&counter, &shiftID)
	}))
	require.Error(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency)
 VALUES (?,?,?,?,?,2,'user-clerk',?,'refund',100001,'INR')`, testutil.TenantA, w.hospital, v7(), shiftID, counter, admission).Error
	}), "the database caps at the surplus too")
	status, body = w.refund(admission, 100000, "")
	require.Equal(t, http.StatusCreated, status, body)
}

// A void never leaves the drawer below zero once cash has gone out.
func TestAVoidCannotTakeTheDrawerBelowZero(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	// The deposit was taken at another counter.
	refunded, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(refunded, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.counterWithShift(t, "clerk2", "IPD-V1", 0)
	status, body := w.deposit("clerk2", refunded, 500000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(refunded, patient, 100000)
	require.NoError(t, w.orderDischarge(refunded, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + refunded + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)

	// This drawer: float 0, a 3000 payment from another patient, then 3000 refunded.
	w.counterWithShift(t, "clerk", "IPD-V2", 0)
	payer, other := uuid.NewString(), uuid.NewString()
	w.charge(payer, other, 300000)
	status, body = w.pay("clerk", payer, 300000)
	require.Equal(t, http.StatusCreated, status, body)
	var paid struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &paid))
	status, body = w.refund(refunded, 300000, "")
	require.Equal(t, http.StatusCreated, status, body)

	status, body = w.put("clerk", "/receipts/"+paid.ID+"/voids/"+v7(), `{"reason":"Wrong patient"}`)
	require.Equal(t, http.StatusConflict, status, "the payment's cash went out as the refund: "+body)
	require.Contains(t, body, "drawer_short")
	require.Error(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason='x',voided_at=now() WHERE id=?`, v7(), paid.ID).Error
	}), "the database refuses it alone")
}
