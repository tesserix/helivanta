package billing_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
)

func (w *bedWorld) switchMode(token, counter, mode string, accepted bool) (int, string) {
	return w.put(token, "/counters/"+counter+"/modes/"+mode, fmt.Sprintf(`{"accepted":%t}`, accepted))
}

func (w *bedWorld) mineBody(t *testing.T, token string) string {
	t.Helper()
	res := do(w.r, "GET", w.root+"/cash-shifts/mine", token, "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	return res.Body.String()
}

func TestACounterTakesTheModesItIsSetUpFor(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, _ := w.counterWithShift(t, "clerk", "IPD-1", 0)
	card := `{"amount_minor":1000,"currency":"INR","mode":"card","reference":"412345678901","approval_code":"a1b2c3"}`

	status, body := w.put("clerk", "/admissions/"+admission+"/receipts/"+v7(), card)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "mode_not_accepted", "a new counter takes no card")

	status, body = w.switchMode("admin", counter, "card", true)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"modes":["card"]`)
	status, _ = w.switchMode("admin", counter, "card", true)
	require.Equal(t, http.StatusOK, status, "the state it is in replays")
	status, _ = w.switchMode("clerk", counter, "card", false)
	require.Equal(t, http.StatusForbidden, status, "a cashier does not switch modes")
	status, _ = w.switchMode("admin", counter, "cash", false)
	require.Equal(t, http.StatusBadRequest, status, "cash is not switched")
	status, _ = w.switchMode("admin", v7(), "card", true)
	require.Equal(t, http.StatusNotFound, status)
	status, body = w.switchMode("admin", counter, "wallet", true)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"modes":["card","wallet"]`)
	require.Contains(t, w.mineBody(t, "clerk"), `"counter_modes":["card","wallet"]`, "the cashier's shift says what its counter takes")
	list := do(w.r, "GET", w.root+"/counters", "clerk", "")
	require.Contains(t, list.Body.String(), `"modes":["card","wallet"]`)

	status, body = w.put("clerk", "/admissions/"+admission+"/receipts/"+v7(), card)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"approval_code":"A1B2C3"`, "stored upper case")

	status, _ = w.switchMode("admin", counter, "card", false)
	require.Equal(t, http.StatusOK, status)
	status, body = w.put("clerk", "/admissions/"+admission+"/receipts/"+v7(), strings.Replace(card, "412345678901", "412345678902", 1))
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "mode_not_accepted", "switched off takes nothing more")

	var rows int64
	require.NoError(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Table("billing_counter_modes").Where("counter_id=?", counter).Count(&rows).Error
	}))
	require.Equal(t, int64(3), rows, "a row per change, none for a replay")
}

func TestCardNetbankingAndWalletStayOutOfTheDrawer(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	for _, m := range []string{"card", "netbanking", "wallet"} {
		status, body := w.switchMode("admin", counter, m, true)
		require.Equal(t, http.StatusOK, status, body)
	}
	receipts := "/admissions/" + admission + "/receipts/"

	bad := map[string]string{
		"card without an approval code":   `{"mode":"card","reference":"412345678901"}`,
		"card with a short approval code": `{"mode":"card","reference":"412345678901","approval_code":"12345"}`,
		"netbanking without a reference":  `{"mode":"netbanking"}`,
		"netbanking with an approval":     `{"mode":"netbanking","reference":"HDFCR52026110212345","approval_code":"123456"}`,
		"wallet without its name":         `{"mode":"wallet","reference":"T2611021234"}`,
		"a reference too short":           `{"mode":"netbanking","reference":"AB1"}`,
		"a reference with a space":        `{"mode":"netbanking","reference":"AB 1234"}`,
		"cash with a reference":           `{"mode":"cash","reference":"412345678901"}`,
		"an unknown mode":                 `{"mode":"cheque","reference":"412345678901"}`,
		"a card number as the RRN":        `{"mode":"card","reference":"4111111111111111","approval_code":"123456"}`,
		"a masked card number":            `{"mode":"card","reference":"XXXXXXXX1111","approval_code":"123456"}`,
		"a card reference not an RRN":     `{"mode":"card","reference":"41234567890","approval_code":"123456"}`,
		"a card number as a UTR":          `{"mode":"netbanking","reference":"4111111111111111"}`,
		"a masked card number as a UTR":   `{"mode":"netbanking","reference":"XXXXXXXXXXXX1111"}`,
	}
	for why, fields := range bad {
		status, body := w.put("clerk", receipts+v7(), `{"amount_minor":100,"currency":"INR",`+fields[1:])
		require.Equal(t, http.StatusBadRequest, status, why+": "+body)
	}

	cardID := v7()
	card := `{"amount_minor":1500,"currency":"INR","mode":"card","reference":"412345678901","approval_code":"123456"}`
	status, body := w.put("clerk", receipts+cardID, card)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"mode":"card"`)
	status, _ = w.put("clerk", receipts+cardID, card)
	require.Equal(t, http.StatusOK, status, "a retried receipt replays")
	status, _ = w.put("clerk", receipts+cardID, strings.Replace(card, "412345678901", "412345678909", 1))
	require.Equal(t, http.StatusConflict, status, "the same id with another reference")
	status, body = w.put("clerk", receipts+v7(), `{"amount_minor":500,"currency":"INR","mode":"netbanking","reference":"hdfcr52026110212345"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"reference":"HDFCR52026110212345"`)
	status, body = w.put("clerk", "/admissions/"+admission+"/deposits/"+v7(), `{"amount_minor":2000,"currency":"INR","mode":"wallet","reference":"T2611021234","wallet":" PhonePe "}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"wallet":"phonepe"`)

	// Another admission, the same slip: refused while the first stands.
	other := uuid.NewString()
	w.charge(other, uuid.NewString(), 5000)
	status, body = w.put("clerk", "/admissions/"+other+"/receipts/"+v7(), card)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "duplicate_reference")
	status, body = w.put("clerk", "/admissions/"+admission+"/deposits/"+v7(), `{"amount_minor":100,"currency":"INR","mode":"wallet","reference":"T2611021234","wallet":"phone pe"}`)
	require.Equal(t, http.StatusConflict, status, "the same wallet transaction under another spelling: "+body)
	require.Contains(t, body, "duplicate_reference")
	status, body = w.put("clerk", "/admissions/"+other+"/receipts/"+v7(), `{"amount_minor":100,"currency":"INR","mode":"netbanking","reference":"T2611021234"}`)
	require.Equal(t, http.StatusConflict, status, "or under another mode: "+body)
	status, body = w.put("clerk", "/admissions/"+other+"/receipts/"+v7(), strings.Replace(card, "123456", "654321", 1))
	require.Equal(t, http.StatusCreated, status, "an RRN with another approval code is another payment: "+body)
	status, body = w.put("clerk", "/admissions/"+admission+"/refunds/"+v7(), `{"amount_minor":100,"currency":"INR","mode":"card","reference":"412345678901","approval_code":"123456"}`)
	require.Equal(t, http.StatusBadRequest, status, "a refund is cash: "+body)

	// The drawer holds the float and nothing else: a pickup of it empties it.
	status, body = w.pickup("admin", shiftID, v7(), `{"note:1000":1}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.pickup("admin", shiftID, v7(), `{"note:100":1}`)
	require.Equal(t, http.StatusConflict, status, "card money is never in the drawer: "+body)
	// Voiding the card payment takes nothing out of an empty drawer.
	status, body = w.put("clerk", "/receipts/"+cardID+"/voids/"+v7(), `{"reason":"Wrong patient"}`)
	require.Equal(t, http.StatusOK, status, body)
	status, body = w.put("clerk", "/admissions/"+other+"/receipts/"+v7(), card)
	require.Equal(t, http.StatusCreated, status, "a voided receipt frees its reference: "+body)

	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(0))
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`, "expected cash is the float less the pickup")
	require.Contains(t, body, `"expected_minor":0`)
}

func TestAnAppliedCardDepositIsNotVoided(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	counter, _ := w.counterWithShift(t, "clerk", "IPD-1", 0)
	status, body := w.switchMode("admin", counter, "card", true)
	require.Equal(t, http.StatusOK, status, body)
	deposit := v7()
	status, body = w.put("clerk", "/admissions/"+admission+"/deposits/"+deposit, `{"amount_minor":300000,"currency":"INR","mode":"card","reference":"412345678901","approval_code":"123456"}`)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, 200000)
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	status, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.apply("clerk", admission)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.put("clerk", "/receipts/"+deposit+"/voids/"+v7(), `{"reason":"Wrong patient"}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "deposit_applied")
}

func TestModesHoldInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 0)
	insert := func(mode, reference, approval, wallet any) error {
		return w.as("user-clerk", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency,mode,reference,approval_code,wallet)
 VALUES (?,?,?,?,?,(SELECT COALESCE(max(number),0)+1 FROM billing_cash_receipts WHERE counter_id=?),'user-clerk',?,'payment',100,'INR',?,?,?,?)`,
				testutil.TenantA, w.hospital, v7(), shiftID, counter, counter, admission, mode, reference, approval, wallet).Error
		})
	}
	requireConstraint(t, insert("card", "412345678901", "123456", nil), "billing_cash_receipt_mode", "a counter that takes no card")
	setMode := func(subject, by string, accepted bool) error {
		return w.as(subject, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_counter_modes (tenant_id,hospital_id,id,counter_id,mode,accepted,set_by) VALUES (?,?,?,?,'card',?,?)`,
				testutil.TenantA, w.hospital, v7(), counter, accepted, by).Error
		})
	}
	require.Error(t, setMode("user-admin", "user-nurse", true), "set by the acting subject")
	requireConstraint(t, setMode("user-admin", "user-admin", false), "billing_counter_mode_unchanged", "only a change is recorded")
	require.NoError(t, setMode("user-admin", "user-admin", true))
	require.Error(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_counter_modes SET accepted=false WHERE counter_id=?`, counter).Error
	}), "a mode change is never edited")

	requireConstraint(t, insert("card", "412345678901", nil, nil), "billing_cash_receipt_reference", "a card carries its approval code")
	requireConstraint(t, insert("card", "412345678901", "123456", "paytm"), "billing_cash_receipt_reference", "and no wallet")
	requireConstraint(t, insert("cash", "412345678901", nil, nil), "billing_cash_receipt_reference", "cash carries no reference")
	requireConstraint(t, insert("card", "412345678901", "abcdef", nil), "billing_cash_receipts_approval_code_check", "upper case only")
	requireConstraint(t, insert("card", "41234567890", "123456", nil), "billing_cash_receipt_rrn", "a card's reference is its 12-digit RRN")
	require.NoError(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_counter_modes (tenant_id,hospital_id,id,counter_id,mode,accepted,set_by) VALUES (?,?,?,?,'netbanking',true,'user-admin')`,
			testutil.TenantA, w.hospital, v7(), counter).Error
	}))
	requireConstraint(t, insert("netbanking", "4111111111111111", nil, nil), "billing_cash_receipt_no_card_number", "no card number in any mode")
	requireConstraint(t, insert("netbanking", "XXXXXXXXXXXX1111", nil, nil), "billing_cash_receipt_no_card_number", "nor a masked one")
	require.NoError(t, insert("card", "412345678901", "123456", nil))
	requireConstraint(t, insert("card", "412345678901", "123456", nil), "billing_cash_receipt_reference_once", "one standing receipt per slip")

	var id string
	require.NoError(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id::text FROM billing_cash_receipts WHERE reference='412345678901'`).Scan(&id).Error
	}))
	require.Error(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET reference='412345678999',void_id=?,void_reason='Wrong patient',voided_at=clock_timestamp() WHERE id=?`, v7(), id).Error
	}), "a void keeps the reference")
	// Nothing in the drawer, yet the card receipt is voided.
	require.NoError(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_receipts SET void_id=?,void_reason='Wrong patient',voided_at=clock_timestamp() WHERE id=?`, v7(), id).Error
	}))
	require.NoError(t, insert("card", "412345678901", "123456", nil), "a voided receipt frees its reference")
}

func TestTheDailyCollectionShowsEachMode(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	today := time.Now().In(ist).Format(time.DateOnly)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.charge(admission, patient, 50000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 0)
	for _, m := range []string{"card", "netbanking", "wallet"} {
		status, body := w.switchMode("admin", counter, m, true)
		require.Equal(t, http.StatusOK, status, body)
	}
	take := func(path string, amount int64, fields string) {
		status, body := w.put("clerk", "/admissions/"+admission+path+v7(), fmt.Sprintf(`{"amount_minor":%d,"currency":"INR",%s}`, amount, fields))
		require.Equal(t, http.StatusCreated, status, body)
	}
	take("/receipts/", 100, `"mode":"card","reference":"100000000001","approval_code":"AAAAAA"`)
	take("/deposits/", 200, `"mode":"card","reference":"100000000002","approval_code":"AAAAAA"`)
	take("/receipts/", 300, `"mode":"netbanking","reference":"R1003"`)
	take("/deposits/", 400, `"mode":"netbanking","reference":"R1004"`)
	take("/receipts/", 500, `"mode":"wallet","reference":"R1005","wallet":"paytm"`)
	take("/deposits/", 600, `"mode":"wallet","reference":"R1006","wallet":"paytm"`)
	take("/receipts/", 700, `"mode":"cash"`)

	open := do(w.r, "GET", w.root+"/collections/"+today, "admin", "")
	require.Equal(t, http.StatusOK, open.Code, open.Body.String())
	require.Contains(t, open.Body.String(), `"card_payments_minor":null`, "an open shift is blind")

	status, body := w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(700))
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`, "only the cash is expected")
	type modes struct {
		CardPayments       int64 `json:"card_payments_minor"`
		CardDeposits       int64 `json:"card_deposits_minor"`
		NetbankingPayments int64 `json:"netbanking_payments_minor"`
		NetbankingDeposits int64 `json:"netbanking_deposits_minor"`
		WalletPayments     int64 `json:"wallet_payments_minor"`
		WalletDeposits     int64 `json:"wallet_deposits_minor"`
		Payments           int64 `json:"payments_minor"`
		Net                int64 `json:"net_minor"`
	}
	var day struct {
		Shifts []modes `json:"shifts"`
		Totals modes   `json:"totals"`
	}
	res := do(w.r, "GET", w.root+"/collections/"+today, "admin", "")
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &day))
	want := modes{CardPayments: 100, CardDeposits: 200, NetbankingPayments: 300, NetbankingDeposits: 400, WalletPayments: 500, WalletDeposits: 600, Payments: 700}
	require.Equal(t, want, day.Shifts[0], "cash columns are cash")
	want.Net = 700
	require.Equal(t, want, day.Totals, "net stays cash")

	csvRes := do(w.r, "GET", w.root+"/collections/"+today+"/export.csv", "admin", "")
	require.Equal(t, http.StatusOK, csvRes.Code, csvRes.Body.String())
	rows, err := csv.NewReader(strings.NewReader(csvRes.Body.String())).ReadAll()
	require.NoError(t, err)
	header := rows[0]
	require.Equal(t, []string{"card_payments", "card_deposits", "netbanking_payments", "netbanking_deposits", "wallet_payments", "wallet_deposits"}, header[len(header)-6:])
	for _, row := range rows[1:] {
		require.Equal(t, []string{"1.00", "2.00", "3.00", "4.00", "5.00", "6.00"}, row[len(row)-6:], row[3])
	}
}
