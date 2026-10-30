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

func v7() string { return uuid.Must(uuid.NewV7()).String() }

func (w *bedWorld) put(token, path, body string) (int, string) {
	res := do(w.r, "PUT", w.root+path, token, body)
	return res.Code, res.Body.String()
}

// counterWithShift sets up a counter and opens the token's shift on it.
func (w *bedWorld) counterWithShift(t *testing.T, token, code string, float int64) (counter, shift string) {
	t.Helper()
	counter, shift = v7(), v7()
	status, body := w.put("admin", "/counters/"+counter, fmt.Sprintf(`{"code":%q,"name":"Desk %s"}`, code, code))
	require.Equal(t, http.StatusOK, status, body)
	status, body = w.put(token, "/counters/"+counter+"/shifts/"+shift, fmt.Sprintf(`{"opening_float_minor":%d}`, float))
	require.Equal(t, http.StatusCreated, status, body)
	return counter, shift
}

func (w *bedWorld) pay(token, admission string, amount int64) (int, string) {
	return w.put(token, "/admissions/"+admission+"/receipts/"+v7(), fmt.Sprintf(`{"amount_minor":%d,"currency":"INR"}`, amount))
}

type shiftBody struct {
	Status        string  `json:"status"`
	ExpectedMinor *int64  `json:"expected_minor"`
	DeclaredMinor *int64  `json:"declared_minor"`
	VarianceMinor *int64  `json:"variance_minor"`
	AuthorisedBy  *string `json:"authorised_by"`
}

func TestCashIsTakenInAShiftAndTheShiftClosesBlind(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)

	status, _ := w.pay("clerk", admission, 1000)
	require.Equal(t, http.StatusConflict, status, "no shift open")
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/counters/"+v7(), "clerk", `{"code":"C1","name":"Desk"}`).Code, "only administrators set up counters")
	status, _ = w.put("admin", "/counters/"+v7(), `{"code":"bad code","name":"Desk"}`)
	require.Equal(t, http.StatusBadRequest, status)

	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 2000)
	status, body := w.put("clerk", "/counters/"+counter+"/shifts/"+shiftID, `{"opening_float_minor":2000}`)
	require.Equal(t, http.StatusOK, status, "a retried open replays: "+body)
	status, body = w.put("clerk2", "/counters/"+counter+"/shifts/"+v7(), `{"opening_float_minor":0}`)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "shift_open", "one open shift per counter")
	other := v7()
	status, body = w.put("admin", "/counters/"+other, `{"code":"IPD-9","name":"Desk 9"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = w.put("clerk", "/counters/"+other+"/shifts/"+v7(), `{"opening_float_minor":0}`)
	require.Contains(t, body, "shift_open", "one open shift per cashier, on any counter: %d", status)

	// Receipts: numbered on the counter, never more than the balance.
	receipt := v7()
	status, body = w.put("clerk", "/admissions/"+admission+"/receipts/"+receipt, `{"amount_minor":3000,"currency":"INR"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"number":1`)
	require.Contains(t, body, `"counter_code":"IPD-1"`)
	status, _ = w.put("clerk", "/admissions/"+admission+"/receipts/"+receipt, `{"amount_minor":3000,"currency":"INR"}`)
	require.Equal(t, http.StatusOK, status, "a retried receipt replays")
	status, _ = w.put("clerk", "/admissions/"+admission+"/receipts/"+receipt, `{"amount_minor":2999,"currency":"INR"}`)
	require.Equal(t, http.StatusConflict, status, "the same id with another amount")
	status, body = w.pay("clerk", admission, 2001)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "over_balance", "an advance is a deposit")
	status, body = w.pay("clerk", admission, 1500)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"number":2`)
	second := struct {
		ID string `json:"id"`
	}{}
	require.NoError(t, json.Unmarshal([]byte(body), &second))
	status, _ = w.put("clerk", "/admissions/"+admission+"/receipts/"+v7(), `{"amount_minor":100,"currency":"USD"}`)
	require.Equal(t, http.StatusBadRequest, status)

	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/account", "nurse", "")
	require.Contains(t, res.Body.String(), `"paid_minor":4500`)
	require.Contains(t, res.Body.String(), `"balance_minor":500`)

	// Voided with a reason, once, by its cashier, in the open shift.
	void := v7()
	status, _ = w.put("clerk2", "/receipts/"+second.ID+"/voids/"+void, `{"reason":"Wrong patient"}`)
	require.Equal(t, http.StatusNotFound, status, "another cashier's receipt")
	status, body = w.put("clerk", "/receipts/"+second.ID+"/voids/"+void, `{"reason":"Wrong patient"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"void_reason":"Wrong patient"`)
	status, _ = w.put("clerk", "/receipts/"+second.ID+"/voids/"+v7(), `{"reason":"Again"}`)
	require.Equal(t, http.StatusConflict, status, "voided once")
	res = do(w.r, "GET", w.root+"/admissions/"+admission+"/receipts", "nurse", "")
	require.Contains(t, res.Body.String(), `"voided_at":"`, "a voided receipt stays on the list")

	// The open shift is blind, to its cashier and the supervisor.
	res = do(w.r, "GET", w.root+"/cash-shifts/mine", "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"expected_minor":null`)
	res = do(w.r, "GET", w.root+"/cash-shifts", "admin", "")
	require.Contains(t, res.Body.String(), `"expected_minor":null`)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", w.root+"/cash-shifts", "clerk", "").Code)

	// A short count waits for someone else's authorisation.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":4900}`)
	require.Equal(t, http.StatusOK, status, body)
	var closed shiftBody
	require.NoError(t, json.Unmarshal([]byte(body), &closed))
	require.Equal(t, "pending_authorisation", closed.Status)
	require.EqualValues(t, 5000, *closed.ExpectedMinor, "float 2000 + 3000 standing")
	require.EqualValues(t, -100, *closed.VarianceMinor)
	status, _ = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":4900}`)
	require.Equal(t, http.StatusOK, status, "a retried close replays")
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":5000}`)
	require.Contains(t, body, "shift_closed", "a recount is not a second close: %d", status)
	status, body = w.pay("clerk", admission, 100)
	require.Contains(t, body, "no_open_shift", "%d", status)
	status, body = w.put("clerk", "/receipts/"+second.ID+"/voids/"+v7(), `{"reason":"Late"}`)
	require.Equal(t, http.StatusConflict, status, body)
	res = do(w.r, "GET", w.root+"/cash-shifts/mine", "clerk", "")
	require.Contains(t, res.Body.String(), `"variance_minor":-100`, "once closed, the cashier sees the variance")

	res = do(w.r, "GET", w.root+"/cash-shifts?status=pending_authorisation", "admin", "")
	require.Contains(t, res.Body.String(), shiftID, "a waiting variance is found by its status")
	res = do(w.r, "GET", w.root+"/cash-shifts?status=closed", "admin", "")
	require.NotContains(t, res.Body.String(), shiftID)
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", w.root+"/cash-shifts?status=lost", "admin", "").Code)
	status, body = w.put("clerk2", "/cash-shifts/"+shiftID+"/authorisation", `{"reason":"Counted short by one note"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`)
	require.Contains(t, body, `"authorised_by":"user-clerk2"`)
	status, _ = w.put("clerk2", "/cash-shifts/"+shiftID+"/authorisation", `{"reason":"Counted short by one note"}`)
	require.Equal(t, http.StatusOK, status, "a retried authorisation replays")

	// A matching count closes at once; the counter takes the next shift.
	_, next := v7(), v7()
	status, body = w.put("clerk", "/counters/"+counter+"/shifts/"+next, `{"opening_float_minor":0}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.pay("clerk", admission, 500)
	require.Contains(t, body, `"number":3`, "numbering carries on across shifts: %d", status)
	status, body = w.put("clerk", "/cash-shifts/"+next+"/close", `{"declared_minor":500}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`)
	require.Contains(t, body, `"authorised_by":null`)

	for _, path := range []string{"/counters", "/admissions/" + admission + "/receipts"} {
		require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+path, "outsider", "").Code, path)
	}
	status, _ = w.pay("outsider", admission, 1)
	require.Equal(t, http.StatusNotFound, status)
}

func TestACashierCannotAuthoriseTheirOwnVariance(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk2", "IPD-2", 0)
	status, body := w.pay("clerk2", admission, 1000)
	require.Equal(t, http.StatusCreated, status, body)
	status, _ = w.put("clerk2", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":900}`)
	require.Equal(t, http.StatusOK, status)
	status, body = w.put("clerk2", "/cash-shifts/"+shiftID+"/authorisation", `{"reason":"Mine"}`)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "own_variance")
	status, body = w.put("admin", "/cash-shifts/"+shiftID+"/authorisation", `{"reason":"Checked the drawer"}`)
	require.Equal(t, http.StatusOK, status, body)
}

func TestConcurrentReceiptsNeverExceedTheBalance(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 1000)
	w.counterWithShift(t, "clerk", "IPD-3", 0)
	w.counterWithShift(t, "clerk2", "IPD-4", 0)
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := []string{"clerk", "clerk2"}[i%2]
			codes[i], _ = w.pay(token, admission, 600)
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
	require.Equal(t, 1, created, "600 fits a 1000 balance once")
}

func TestCashTablesHoldTheirRules(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-5", 1000)
	status, body := w.pay("clerk", admission, 1000)
	require.Equal(t, http.StatusCreated, status, body)

	as := func(subject string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, subject).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	insertReceipt := func(subject string, number int, amount int64) error {
		return as(subject, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,amount_minor,currency) VALUES (?,?,?,?,?,?,'user-clerk',?,?,'INR')`,
				testutil.TenantA, w.hospital, v7(), shiftID, counter, number, admission, amount).Error
		})
	}
	require.Error(t, insertReceipt("user-clerk", 3, 10), "numbers have no gaps")
	require.Error(t, insertReceipt("user-clerk2", 2, 10), "only the shift's cashier takes cash in it, even naming them")
	require.Error(t, insertReceipt("user-clerk", 2, 4001), "never more than the balance")
	require.NoError(t, insertReceipt("user-clerk", 2, 4000))
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET amount_minor=1 WHERE admission_id=?`, admission).Error
	}), "a receipt is never edited")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET opening_float_minor=0 WHERE id=?`, shiftID).Error
	}), "a float never changes")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',expected_minor=1000,declared_minor=1000,closed_at=now() WHERE id=?`, shiftID).Error
	}), "a close is measured against the cash actually taken")
	require.Error(t, as("user-clerk2", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',expected_minor=6000,declared_minor=6000,closed_at=now() WHERE id=?`, shiftID).Error
	}), "only its cashier closes a shift")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_counters SET name='Other' WHERE id=?`, counter).Error
	}), "a counter is fixed once set up")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM billing_cash_receipts WHERE admission_id=?`, admission).Error
	}))
	// Closed short, the cashier can neither authorise it nor void a receipt.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":1}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',authorised_by='user-clerk',authorisation_reason='Mine',authorised_at=now() WHERE id=?`, shiftID).Error
	}), "a cashier never authorises their own variance")
	require.Error(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason='Late',voided_at=now() WHERE admission_id=? AND number=1`, v7(), admission).Error
	}), "no void once the shift has closed")
	var seen int64
	require.NoError(t, as("user-outsider", func(tx *gorm.DB) error {
		return tx.Table("billing_cash_receipts").Where("admission_id=?", admission).Count(&seen).Error
	}))
	require.Zero(t, seen, "no grant, no receipts")

}

// The database's own lock: a receipt still being written holds the balance,
// so a second one waits for it and then finds no room, whoever writes it.
func TestAReceiptBeingWrittenHoldsTheBalance(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 1000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-6", 0)
	w.counterWithShift(t, "clerk2", "IPD-7", 0)

	written, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,amount_minor,currency) VALUES (?,?,?,?,?,1,'user-clerk',?,600,'INR')`,
				testutil.TenantA, w.hospital, v7(), shiftID, counter, admission).Error; err != nil {
				return err
			}
			close(written)
			<-release
			return nil
		})
	}()
	<-written
	answer := make(chan string, 1)
	go func() {
		_, body := w.pay("clerk2", admission, 600)
		answer <- body
	}()
	select {
	case body := <-answer:
		t.Fatalf("the second receipt did not wait for the first: %s", body)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
	require.Contains(t, <-answer, "over_balance")
}

// A close waits for a receipt still being written and counts it; a void
// that arrives while a close holds the shift is refused once it has closed.
func TestACloseCountsAReceiptStillBeingWrittenAndRefusesALateVoid(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-8", 1000)
	status, body := w.pay("clerk", admission, 700)
	require.Equal(t, http.StatusCreated, status, body)
	var first struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &first))

	written, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,amount_minor,currency) VALUES (?,?,?,?,?,2,'user-clerk',?,300,'INR')`,
				testutil.TenantA, w.hospital, v7(), shiftID, counter, admission).Error; err != nil {
				return err
			}
			close(written)
			<-release
			return nil
		})
	}()
	<-written
	closed := make(chan string, 1)
	go func() {
		_, body := w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":2000}`)
		closed <- body
	}()
	select {
	case body := <-closed:
		t.Fatalf("the close did not wait for the receipt being written: %s", body)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
	body = <-closed
	require.Contains(t, body, `"expected_minor":2000`, "float 1000 + 700 + the receipt written meanwhile")
	require.Contains(t, body, `"status":"closed"`)

	status, body = w.put("clerk", "/receipts/"+first.ID+"/voids/"+v7(), `{"reason":"Too late"}`)
	require.Equal(t, http.StatusConflict, status)
	require.Contains(t, body, "shift_closed")
}

func TestAVoidWaitingOnACloseIsRefused(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-10", 0)
	status, body := w.pay("clerk", admission, 700)
	require.Equal(t, http.StatusCreated, status, body)
	var r struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &r))

	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		// A close in progress: the shift is held while its expected cash is taken.
		done <- w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`SELECT 1 FROM billing_cash_shifts WHERE id=? FOR UPDATE`, shiftID).Error; err != nil {
				return err
			}
			close(locked)
			<-release
			return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',expected_minor=700,declared_minor=700,closed_at=clock_timestamp() WHERE id=?`, shiftID).Error
		})
	}()
	<-locked
	voided := make(chan string, 1)
	go func() {
		_, body := w.put("clerk", "/receipts/"+r.ID+"/voids/"+v7(), `{"reason":"Wrong patient"}`)
		voided <- body
	}()
	time.Sleep(300 * time.Millisecond)
	close(release)
	require.NoError(t, <-done)
	require.Contains(t, <-voided, "shift_closed", "the void waited for the close and found the shift closed")
}
