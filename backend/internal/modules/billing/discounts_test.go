package billing_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type discountBody struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	AmountMinor int64   `json:"amount_minor"`
	NetMinor    int64   `json:"net_minor"`
	ReasonCode  string  `json:"reason_code"`
	DecidedBy   *string `json:"decided_by"`
}

// reason sets a reason: desk and approval limits in basis points and paise.
func (w *bedWorld) reason(t *testing.T, code string, deskBp int, deskMax int64, approvalBp int, approvalMax int64) string {
	t.Helper()
	id := v7()
	status, body := w.put("admin", "/discount-reasons/"+id, fmt.Sprintf(`{"code":%q,"name":"Reason %s","desk_percent_bp":%d,"desk_max_minor":%d,"approval_percent_bp":%d,"approval_max_minor":%d}`,
		code, code, deskBp, deskMax, approvalBp, approvalMax))
	require.Equal(t, http.StatusCreated, status, body)
	return id
}

func (w *bedWorld) discount(token, admission, id, reason string, amount int64) (int, discountBody, string) {
	res := do(w.r, "PUT", w.root+"/admissions/"+admission+"/discounts/"+id, token, fmt.Sprintf(`{"reason_id":%q,"amount_minor":%d,"justification":"Staff dependant"}`, reason, amount))
	var b discountBody
	_ = json.Unmarshal(res.Body.Bytes(), &b)
	return res.Code, b, res.Body.String()
}

func (w *bedWorld) decide(token, id string, approve bool) (int, discountBody, string) {
	res := do(w.r, "PUT", w.root+"/discounts/"+id+"/decision", token, fmt.Sprintf(`{"approve":%t,"note":"Checked the staff card"}`, approve))
	var b discountBody
	_ = json.Unmarshal(res.Body.Bytes(), &b)
	return res.Code, b, res.Body.String()
}

func errorCode(body string) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &e)
	return e.Error
}

// threeDays admits to a general bed (₹1,500) for two midnights and then to a
// private one (₹1,000.01) for one: room charges of 150000, 150000, 100001.
func (w *bedWorld) threeDays(t *testing.T) string {
	t.Helper()
	w.priced(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/private/2020-01-01", "admin", `{"daily_rate_minor":100001,"currency":"INR"}`).Code)
	today, err := time.ParseInLocation(time.DateOnly, time.Now().In(ist).Format(time.DateOnly), ist)
	require.NoError(t, err)
	admission, patient := uuid.NewString(), uuid.NewString()
	general := stay{uuid.NewString(), uuid.NewString(), "general"}
	private := stay{uuid.NewString(), uuid.NewString(), "private"}
	require.NoError(t, w.start(admission, patient, general, today.AddDate(0, 0, -3).Add(10*time.Hour)))
	require.NoError(t, w.end(admission, general, today.AddDate(0, 0, -1).Add(10*time.Hour), "transfer"))
	require.NoError(t, w.start(admission, patient, private, today.AddDate(0, 0, -1).Add(10*time.Hour)))
	w.census()
	if time.Now().In(ist).Sub(today) < 20*time.Minute {
		t.Skip("today's midnight is not yet counted")
	}
	return admission
}

// lines are the discount lines and withdrawals of an admission, by amount.
func (w *bedWorld) discountLines(t *testing.T, admission, kind string) []int64 {
	t.Helper()
	var amounts []int64
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT amount_minor FROM billing_account_entries WHERE admission_id=? AND source_module='discount' AND kind=?`, admission, kind).Scan(&amounts).Error
	}))
	sort.Slice(amounts, func(i, j int) bool { return amounts[i] < amounts[j] })
	return amounts
}

func (w *bedWorld) source(admission, module string) int64 {
	a, _ := w.account(admission)
	for _, s := range a.Sources {
		if s.SourceModule == module {
			return s.OutstandingMinor
		}
	}
	return 0
}

func TestADiscountWithinTheDeskLimitIsSplitExactlyAndBeyondItWaitsForSomeoneElse(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t)
	require.Equal(t, int64(400001), w.inr(admission).BalanceMinor)
	require.Equal(t, int64(400001), w.inr(admission).DiscountableMinor)
	// Desk: 25% and ₹1,000; approval: 50% and ₹2,000.
	staff := w.reason(t, "staff", 2500, 100000, 5000, 200000)

	id := v7()
	status, d, body := w.discount("clerk", admission, id, staff, 100000)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "applied", d.Status)
	require.Equal(t, int64(100000), d.NetMinor)
	// 100000 × each charge ÷ 400001: 37499.9, 37499.9, 25000.2; the two
	// leftover paise go to the two largest remainders.
	require.Equal(t, []int64{25000, 37500, 37500}, w.discountLines(t, admission, "reversal"))
	require.Equal(t, int64(-100000), w.source(admission, "discount"))
	require.Equal(t, int64(300001), w.inr(admission).BalanceMinor)
	require.Equal(t, int64(300001), w.inr(admission).DiscountableMinor)

	status, _, _ = w.discount("clerk", admission, id, staff, 100000)
	require.Equal(t, http.StatusOK, status, "the same request again replays")
	status, _, _ = w.discount("clerk", admission, id, staff, 99999)
	require.Equal(t, http.StatusConflict, status, "the id is the first request's")
	require.Equal(t, []int64{25000, 37500, 37500}, w.discountLines(t, admission, "reversal"), "nothing posted twice")

	// The desk's ₹1,000 is spent for this reason: the next paisa waits.
	pending := v7()
	status, d, body = w.discount("clerk2", admission, pending, staff, 50000)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "pending", d.Status)
	require.Equal(t, int64(0), d.NetMinor)
	require.Equal(t, int64(300001), w.inr(admission).BalanceMinor, "a pending discount takes nothing off")

	res := do(w.r, "GET", w.root+"/pending-discounts", "admin", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), pending)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", w.root+"/pending-discounts", "clerk", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.root+"/discounts/"+pending+"/decision", "clerk", `{"approve":true,"note":"x"}`).Code)

	status, _, body = w.decide("clerk2", pending, true)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "own_request", errorCode(body))

	status, d, body = w.decide("admin", pending, true)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "applied", d.Status)
	require.Equal(t, int64(250001), w.inr(admission).BalanceMinor)
	status, _, _ = w.decide("admin", pending, true)
	require.Equal(t, http.StatusOK, status, "the same decision again replays")
	status, _, body = w.decide("admin", pending, false)
	require.Equal(t, "already_decided", errorCode(body), body)
	require.Equal(t, http.StatusConflict, status)

	// ₹1,500 given; the approval limit is ₹2,000, or 50% of 400001.
	status, _, body = w.discount("clerk", admission, v7(), staff, 50001)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "discount_over_limit", errorCode(body))

	// Another reason has its own limits, but not more than the charges carry.
	charity := w.reason(t, "charity", 10000, 1000000000, 10000, 1000000000)
	status, _, body = w.discount("clerk", admission, v7(), charity, 250002)
	require.Equal(t, "nothing_to_discount", errorCode(body), body)
	require.Equal(t, http.StatusConflict, status)
	status, d, body = w.discount("clerk", admission, v7(), charity, 250001)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "applied", d.Status)
	require.Equal(t, int64(0), w.inr(admission).BalanceMinor, "fully conceded")
}

func TestAnApprovalIsCheckedAsThingsStandWhenItIsGiven(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t)
	staff := w.reason(t, "staff", 0, 0, 10000, 1000000000)
	pending := v7()
	status, d, body := w.discount("clerk", admission, pending, staff, 300000)
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, "pending", d.Status)

	// Cash then takes most of the balance.
	w.counterWithShift(t, "clerk", "IPD-1", 0)
	status, body = w.pay("clerk", admission, 200000)
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = w.decide("admin", pending, true)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "discount_over_balance", errorCode(body))

	status, d, body = w.decide("admin", pending, false)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "rejected", d.Status)
	require.Equal(t, int64(200001), w.inr(admission).BalanceMinor)
}

func TestReasonsAreFixedRetiredOnceAndScopedToTheHospital(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t)
	id := w.reason(t, "goodwill", 500, 5000, 1000, 10000)
	status, _ := w.put("admin", "/discount-reasons/"+id, `{"code":"goodwill","name":"Reason goodwill","desk_percent_bp":500,"desk_max_minor":5000,"approval_percent_bp":1000,"approval_max_minor":10000}`)
	require.Equal(t, http.StatusOK, status)
	status, _ = w.put("admin", "/discount-reasons/"+id, `{"code":"goodwill","name":"Reason goodwill","desk_percent_bp":600,"desk_max_minor":5000,"approval_percent_bp":1000,"approval_max_minor":10000}`)
	require.Equal(t, http.StatusConflict, status)
	status, body := w.put("admin", "/discount-reasons/"+v7(), `{"code":"goodwill","name":"Again","desk_percent_bp":0,"desk_max_minor":0,"approval_percent_bp":0,"approval_max_minor":0}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "code_in_use", errorCode(body))
	status, _ = w.put("admin", "/discount-reasons/"+v7(), `{"code":"desk-over","name":"Bad","desk_percent_bp":2000,"desk_max_minor":0,"approval_percent_bp":1000,"approval_max_minor":0}`)
	require.Equal(t, http.StatusBadRequest, status, "the desk's limit is never above the approval's")
	status, _ = w.put("clerk", "/discount-reasons/"+v7(), `{"code":"mine","name":"Mine","desk_percent_bp":0,"desk_max_minor":0,"approval_percent_bp":0,"approval_max_minor":0}`)
	require.Equal(t, http.StatusForbidden, status)

	require.Equal(t, http.StatusOK, do(w.r, "POST", w.root+"/discount-reasons/"+id+"/retire", "admin", "").Code)
	require.Equal(t, http.StatusOK, do(w.r, "POST", w.root+"/discount-reasons/"+id+"/retire", "admin", "").Code, "retired again answers it")
	status, _, body = w.discount("clerk", admission, v7(), id, 100)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "reason_retired", errorCode(body))
	w.reason(t, "goodwill", 500, 5000, 1000, 10000)

	status, _, _ = w.discount("clerk", admission, v7(), v7(), 100)
	require.Equal(t, http.StatusNotFound, status, "an unknown reason")
	status, _, _ = w.discount("clerk", uuid.NewString(), v7(), id, 100)
	require.Equal(t, http.StatusNotFound, status, "an unknown account")
	status, _, _ = w.discount("outsider", admission, v7(), id, 100)
	require.Equal(t, http.StatusNotFound, status, "no billing grant")
	res := do(w.r, "GET", w.root+"/discount-reasons", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"retired_at":"`)
}

// A discounted day the census later voids: its discount is withdrawn, then the
// day is reversed in full, so the day nets to zero and both stay on record.
func TestADiscountedDayThatIsVoidedIsWithdrawnAndReversedInFull(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	day := time.Now().In(ist).AddDate(0, 0, -2).Format(time.DateOnly)
	at, err := time.ParseInLocation(time.DateTime, day+" 10:00:00", ist)
	require.NoError(t, err)
	admission, patient := uuid.NewString(), uuid.NewString()
	first := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, first, at))
	require.NoError(t, w.end(admission, first, at.Add(time.Minute), "transfer"))
	require.NoError(t, w.release(admission, patient, at.Add(2*time.Minute)))
	w.census()
	staff := w.reason(t, "staff", 10000, 1000000000, 10000, 1000000000)
	status, _, body := w.discount("clerk", admission, v7(), staff, 150000)
	require.Equal(t, http.StatusCreated, status, body, "the whole day")
	require.Equal(t, int64(0), w.inr(admission).BalanceMinor)

	// A stay that began earlier arrives late: the minimum day moves to it.
	earlier := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, earlier, at.Add(-time.Minute)))
	require.NoError(t, w.end(admission, earlier, at, "transfer"))
	w.census()

	require.Equal(t, []int64{150000}, w.discountLines(t, admission, "charge"), "withdrawn in full")
	require.Equal(t, int64(0), w.source(admission, "discount"))
	require.Equal(t, int64(150000), w.source(admission, "bed"), "the new day stands, undiscounted")
	res := do(w.r, "GET", w.root+"/admissions/"+admission+"/discounts", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"net_minor":0`)
}

func TestTaxableAndInvoicedRoomChargesAreNotDiscounted(t *testing.T) {
	w, day := gstWorld(t, true)
	staff := w.reason(t, "staff", 10000, 1000000000, 10000, 1000000000)
	taxable, _ := w.dayStay(t, day, "private")
	exempt, _ := w.dayStay(t, day, "general")
	w.census()
	sameDay(t, day)

	status, _, body := w.discount("clerk", taxable, v7(), staff, 100)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "nothing_to_discount", errorCode(body))

	status, _, body = w.discount("clerk", exempt, v7(), staff, 100000)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.post("/admissions/" + exempt + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, inv, body := w.invoice(exempt, v7(), "Asha Rao")
	require.Equal(t, http.StatusCreated, status, body)
	require.Equal(t, int64(400000), inv.ValueMinor, "the value after the discount")
	require.Equal(t, int64(400000), inv.ExemptMinor)
	res := do(w.r, "GET", w.root+"/admissions/"+exempt+"/tax-invoices/"+inv.ID+"/lines", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"discount_minor":100000`)

	status, _, body = w.discount("clerk", exempt, v7(), staff, 100)
	require.Equal(t, http.StatusConflict, status, body)
	require.Equal(t, "nothing_to_discount", errorCode(body), "an invoiced charge needs a credit note")
}

// Two desk requests at once, each within the desk's limit alone: the second
// sees the first and waits for approval.
func TestConcurrentRequestsCountEachOtherAgainstTheLimit(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t)
	staff := w.reason(t, "staff", 10000, 100000, 10000, 1000000000)
	var wg sync.WaitGroup
	statuses := make([]string, 2)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, d, _ := w.discount("clerk", admission, v7(), staff, 60000)
			statuses[i] = d.Status
		}()
	}
	wg.Wait()
	sort.Strings(statuses)
	require.Equal(t, []string{"applied", "pending"}, statuses)
	require.Equal(t, int64(340001), w.inr(admission).BalanceMinor)
}

func TestDiscountRulesHoldInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t)
	staff := w.reason(t, "staff", 0, 0, 10000, 1000000000)
	pending := v7()
	status, _, body := w.discount("clerk", admission, pending, staff, 1000)
	require.Equal(t, http.StatusCreated, status, body)
	as := func(subject string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, subject).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	exec := func(subject, sql string, args ...any) error {
		return as(subject, func(tx *gorm.DB) error { return tx.Exec(sql, args...).Error })
	}
	var day string
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT source_reference FROM billing_account_entries WHERE admission_id=? AND source_module='bed' LIMIT 1`, admission).Scan(&day).Error
	}))
	line := `INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at,discount_id)
 VALUES (?,?,billing_uuid_v7(),?,'reversal','discount',gen_random_uuid(),'Discount',?,1,'INR',now(),?)`
	require.Error(t, exec("user-clerk", line, testutil.TenantA, w.hospital, admission, day, pending), "a line only as its discount is applied")
	require.Error(t, exec("user-clerk", `INSERT INTO billing_discounts (tenant_id,hospital_id,admission_id,id,reason_id,amount_minor,currency,justification,status,requested_by,requested_at)
 VALUES (?,?,?,?,?,1,'INR','x','applied','user-clerk2',now())`, testutil.TenantA, w.hospital, admission, v7(), staff), "asked only as oneself")
	require.Error(t, exec("user-clerk", `UPDATE billing_discounts SET status='applied',decided_by='user-clerk',decision_note='mine' WHERE id=?`, pending), "never one's own")
	require.Error(t, exec("user-admin", `UPDATE billing_discounts SET status='applied',decided_by='user-clerk2',decision_note='as another' WHERE id=?`, pending), "decided as oneself")
	require.Error(t, exec("user-admin", `UPDATE billing_discounts SET amount_minor=2,status='applied',decided_by='user-admin',decision_note='more' WHERE id=?`, pending), "the request is fixed")
	require.NoError(t, exec("user-admin", `UPDATE billing_discounts SET status='applied',decided_by='user-admin',decision_note='ok' WHERE id=?`, pending))
	require.Error(t, exec("user-admin", `UPDATE billing_discounts SET status='rejected',decided_by='user-admin',decision_note='no' WHERE id=?`, pending), "decided once")
	require.Error(t, exec("user-clerk", line, testutil.TenantA, w.hospital, admission, day, pending), "nor after it was applied")
	require.Error(t, exec("user-admin", `UPDATE billing_discount_reasons SET name='Other' WHERE id=?`, staff), "a reason is fixed")
	require.Error(t, exec("user-admin", `UPDATE billing_discount_reasons SET retired_by='user-clerk' WHERE id=?`, staff), "retired as oneself")
	require.NoError(t, exec("user-admin", `UPDATE billing_discount_reasons SET retired_by='user-admin' WHERE id=?`, staff))
	require.Error(t, exec("user-admin", `UPDATE billing_discount_reasons SET retired_by='user-admin' WHERE id=?`, staff), "retired once")
	require.Error(t, exec("user-admin", `DELETE FROM billing_discounts WHERE id=?`, pending))
	require.Error(t, exec("system:billing-bed-census", `INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at,discount_id)
 SELECT tenant_id,hospital_id,billing_uuid_v7(),admission_id,'charge','discount',source_reference,'Withdrawn',source_reference,amount_minor,currency,now(),discount_id
 FROM billing_account_entries WHERE admission_id=? AND source_module='discount' LIMIT 1`, admission), "a standing day's discount is not withdrawn")
	require.Error(t, exec("user-clerk", `INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at)
 VALUES (?,?,billing_uuid_v7(),?,'reversal','bed',gen_random_uuid(),'Bed day',?,1,'INR',now())`, testutil.TenantA, w.hospital, admission, day), "the desk writes discount lines only")
	// A room charge's reversals count its standing discount: 150000 less the
	// applied 1000 paise leaves 149000 to reverse, not the whole day.
	var charge struct {
		Ref    string
		Amount int64
	}
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT x.charge_reference AS ref,e.amount_minor AS amount FROM billing_account_entries x JOIN billing_account_entries e
 ON e.tenant_id=x.tenant_id AND e.source_module='bed' AND e.kind='charge' AND e.source_reference=x.charge_reference
 WHERE x.admission_id=? AND x.source_module='discount' ORDER BY x.amount_minor DESC LIMIT 1`, admission).Scan(&charge).Error
	}))
	reverse := func(amount int64) error {
		return exec("system:billing-bed-census", `INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at)
 VALUES (?,?,billing_uuid_v7(),?,'reversal','bed',gen_random_uuid(),'Bed day',?,?,'INR',now())`, testutil.TenantA, w.hospital, admission, charge.Ref, amount)
	}
	var off int64
	require.NoError(t, as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT billing_discount_on(?,?)`, testutil.TenantA, charge.Ref).Scan(&off).Error
	}))
	require.Positive(t, off)
	require.Error(t, reverse(charge.Amount-off+1), "never past what the discount leaves")
	require.NoError(t, reverse(charge.Amount-off))
	var version int
	require.NoError(t, as("user-admin", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT get_byte(uuid_send(billing_uuid_v7()),6)>>4`).Scan(&version).Error
	}))
	require.Equal(t, 7, version)
}

// Each limit binds on its own: the amount where the percentage would allow
// more, and the percentage where the amount would.
func TestEachLimitBindsOnItsOwn(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t) // a base of 400001
	cases := []struct {
		code            string
		deskBp          int
		deskMax         int64
		approvalBp      int
		approvalMax     int64
		amount          int64
		status, code409 string
	}{
		{"desk-amount", 10000, 500, 10000, 1000000000, 501, "pending", ""},
		{"desk-amount-in", 10000, 500, 10000, 1000000000, 500, "applied", ""},
		{"desk-percent", 1, 1000000000, 10000, 1000000000, 41, "pending", ""}, // 0.01% of 400001 is 40.0001
		{"desk-percent-in", 1, 1000000000, 10000, 1000000000, 40, "applied", ""},
		{"approval-amount", 0, 0, 10000, 1000, 1001, "", "discount_over_limit"},
		{"approval-percent", 0, 0, 1, 1000000000, 41, "", "discount_over_limit"},
		{"approval-percent-in", 0, 0, 1, 1000000000, 40, "pending", ""},
	}
	for _, c := range cases {
		reason := w.reason(t, c.code, c.deskBp, c.deskMax, c.approvalBp, c.approvalMax)
		status, d, body := w.discount("clerk", admission, v7(), reason, c.amount)
		if c.code409 != "" {
			require.Equal(t, http.StatusConflict, status, c.code+": "+body)
			require.Equal(t, c.code409, errorCode(body), c.code)
			continue
		}
		require.Equal(t, http.StatusCreated, status, c.code+": "+body)
		require.Equal(t, c.status, d.Status, c.code)
	}
}

// The database's own lock: a discount still being written is counted by the
// next one against the reason's limit, however it is written.
func TestADiscountBeingWrittenHoldsTheLimit(t *testing.T) {
	w := newBedWorld(t)
	admission := w.threeDays(t)
	staff := w.reason(t, "staff", 10000, 100000, 10000, 1000000000)
	insert := func(tx *gorm.DB, subject, id string) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, subject).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO billing_discounts (tenant_id,hospital_id,admission_id,id,reason_id,amount_minor,currency,justification,status,requested_by,requested_at)
 VALUES (?,?,?,?,?,60000,'INR','Staff','pending',?,now())`, testutil.TenantA, w.hospital, admission, id, staff, subject).Error
	}
	written, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := insert(tx, "user-clerk", v7()); err != nil {
				return err
			}
			close(written)
			<-release
			return nil
		})
	}()
	<-written
	second := v7()
	answer := make(chan error, 1)
	go func() {
		answer <- w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk2"), testutil.TenantA, func(tx *gorm.DB) error {
			return insert(tx, "user-clerk2", second)
		})
	}()
	select {
	case err := <-answer:
		t.Fatalf("the second discount did not wait for the first: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, <-answer)
	var status string
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT status FROM billing_discounts WHERE id=?`, second).Scan(&status).Error
	}))
	require.Equal(t, "pending", status, "120000 is beyond the desk's 100000")
}
