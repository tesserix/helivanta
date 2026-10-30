package billing_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (w *bedWorld) deposit(token, admission string, amount int64) (int, string) {
	return w.put(token, "/admissions/"+admission+"/deposits/"+v7(), fmt.Sprintf(`{"amount_minor":%d,"currency":"INR"}`, amount))
}

func (w *bedWorld) apply(token, admission string) (int, string) {
	return w.put(token, "/admissions/"+admission+"/deposit-applications/"+v7(), "")
}

type inrTotals struct {
	OutstandingMinor int64 `json:"outstanding_minor"`
	PaidMinor        int64 `json:"paid_minor"`
	DepositHeldMinor int64 `json:"deposit_held_minor"`
	AppliedMinor     int64 `json:"applied_minor"`
	BalanceMinor     int64 `json:"balance_minor"`
}

func (w *bedWorld) inr(admission string) inrTotals {
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/account", "nurse", "")
	require.Equal(w.t, http.StatusOK, res.Code, res.Body.String())
	var a struct {
		Totals []struct {
			Currency string `json:"currency"`
			inrTotals
		} `json:"totals"`
	}
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &a))
	for _, t := range a.Totals {
		if t.Currency == "INR" {
			return t.inrTotals
		}
	}
	return inrTotals{}
}

// A stay: an advance at admission, charges, a final bill, the deposit applied
// and the rest paid.
func TestADepositIsHeldThenAppliedAgainstTheFinalBill(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	bedStay := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, bedStay, time.Now().Add(-time.Hour)))
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-D1", 0)

	// An advance beyond anything owed yet is a deposit, not a payment.
	status, body := w.pay("clerk", admission, 2000)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_balance")
	status, body = w.deposit("clerk", admission, 2000)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"purpose":"deposit"`)
	require.Contains(t, body, `"number":1`, "deposits share the counter's numbering")
	w.charge(admission, patient, 5000)
	require.Equal(t, inrTotals{OutstandingMinor: 5000, DepositHeldMinor: 2000, BalanceMinor: 5000}, w.inr(admission), "a held deposit is not a payment")

	status, body = w.apply("clerk", admission)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "no_final_bill")

	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"paid_minor":0`)
	require.Contains(t, body, `"deposit_held_minor":2000`, "the final bill shows what is held")

	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/admissions/"+admission+"/deposit-applications/"+v7(), "nurse", "").Code)
	application := v7()
	status, body = w.put("clerk", "/admissions/"+admission+"/deposit-applications/"+application, "")
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"amount_minor":2000`)
	status, _ = w.put("clerk", "/admissions/"+admission+"/deposit-applications/"+application, "")
	require.Equal(t, http.StatusOK, status, "a retried application replays")
	status, body = w.put("clerk2", "/admissions/"+admission+"/deposit-applications/"+application, "")
	require.Equal(t, http.StatusConflict, status, "the same id from someone else: "+body)
	require.Contains(t, body, `"error":"conflict"`)
	status, body = w.put("clerk", "/admissions/"+uuid.NewString()+"/deposit-applications/"+application, "")
	require.Equal(t, http.StatusConflict, status, "the same id for another admission: "+body)
	status, body = w.apply("clerk", admission)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "nothing_to_apply")
	require.Equal(t, inrTotals{OutstandingMinor: 5000, AppliedMinor: 2000, BalanceMinor: 3000}, w.inr(admission))

	// The deposit cannot be voided now that it is applied.
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/receipts", "nurse", "")
	var receipts struct {
		Data []struct {
			ID      string `json:"id"`
			Purpose string `json:"purpose"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &receipts))
	require.Len(t, receipts.Data, 1)
	status, body = w.put("clerk", "/receipts/"+receipts.Data[0].ID+"/voids/"+v7(), `{"reason":"Mistake"}`)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "deposit_applied")

	// A payment is capped at what is left after the deposit.
	status, body = w.pay("clerk", admission, 3001)
	require.Contains(t, body, "over_balance", "%d", status)
	status, body = w.pay("clerk", admission, 3000)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, inrTotals{OutstandingMinor: 5000, PaidMinor: 3000, AppliedMinor: 2000}, w.inr(admission))
	res = do(w.r, "GET", w.root+"/admissions/"+admission+"/deposit-applications", "nurse", "")
	require.Contains(t, res.Body.String(), `"bill_number":1`)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/admissions/"+admission+"/deposit-applications", "outsider", "").Code)

	// Released, the account takes no deposit.
	require.NoError(t, w.end(admission, bedStay, time.Now(), "release"))
	require.NoError(t, w.release(admission, patient, time.Now()))
	status, body = w.deposit("clerk", admission, 100)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "account_closed")
	// Deposits count in the drawer: float 0 + deposit 2000 + payment 3000.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":5000}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`)
}

func TestApplyingTwiceAtOnceNeverExceedsWhatIsHeld(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.counterWithShift(t, "clerk", "IPD-D2", 0)
	status, body := w.deposit("clerk", admission, 1000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, 5000)
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = w.apply("clerk", admission)
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		require.Contains(t, []int{http.StatusCreated, http.StatusConflict}, c)
		if c == http.StatusCreated {
			created++
		}
	}
	require.Equal(t, 1, created)
	require.EqualValues(t, 1000, w.inr(admission).AppliedMinor)
}

func TestDepositTablesHoldTheirRules(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.counterWithShift(t, "clerk", "IPD-D3", 0)
	status, body := w.deposit("clerk", admission, 1000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, 5000)
	as := func(subject string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, subject).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	insertApplication := func(number int, amount int64) error {
		return as("user-clerk", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_deposit_applications (tenant_id,hospital_id,id,admission_id,bill_number,amount_minor,currency,applied_by) VALUES (?,?,?,?,?,?,'INR','user-clerk')`,
				testutil.TenantA, w.hospital, v7(), admission, number, amount).Error
		})
	}
	// An interim bill is not a final bill.
	interim := w.issue(admission)
	require.Error(t, insertApplication(interim.Number, 500), "only against a final bill")
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	require.Error(t, insertApplication(2, 1001), "never more than is held")
	require.NoError(t, insertApplication(2, 600))
	require.Error(t, insertApplication(2, 401), "the 400 left is all that is held")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_deposit_applications SET amount_minor=1 WHERE admission_id=?`, admission).Error
	}), "an application is never edited")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason='x',voided_at=now() WHERE admission_id=? AND purpose='deposit'`, v7(), admission).Error
	}), "an applied deposit is not voided, by the database alone")
	// A bill's money figures are the database's, whatever a writer supplies.
	var paid struct{ Paid, Held int64 }
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT paid_minor AS paid, deposit_held_minor AS held FROM billing_interim_bills WHERE admission_id=? AND number=2`, admission).Scan(&paid).Error
	}))
	require.EqualValues(t, 0, paid.Paid)
	require.EqualValues(t, 1000, paid.Held)
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency)
 SELECT tenant_id,hospital_id,?,shift_id,counter_id,2,'user-clerk',admission_id,'payment',4401,'INR' FROM billing_cash_receipts WHERE admission_id=?`, v7(), admission).Error
	}), "a payment is capped at the balance net of what was applied")

	// A released account: the database alone refuses a deposit.
	gone, goner := uuid.NewString(), uuid.NewString()
	w.charge(gone, goner, 500)
	require.NoError(t, w.release(gone, goner, time.Now()))
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency)
 SELECT tenant_id,hospital_id,?,shift_id,counter_id,2,'user-clerk',?,'deposit',100,'INR' FROM billing_cash_receipts WHERE admission_id=?`, v7(), gone, admission).Error
	}), "no deposit on a released account")
}
