package billing_test

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type collectionBody struct {
	Date     string `json:"date"`
	Timezone string `json:"timezone"`
	Final    bool   `json:"final"`
	Shifts   []struct {
		ID            string  `json:"shift_id"`
		CounterCode   string  `json:"counter_code"`
		Status        string  `json:"status"`
		Float         *int64  `json:"opening_float_minor"`
		Payments      *int64  `json:"payments_minor"`
		Deposits      *int64  `json:"deposits_minor"`
		Refunds       *int64  `json:"refunds_minor"`
		VoidedCount   *int64  `json:"voided_count"`
		VoidedRefunds *int64  `json:"voided_refunds_minor"`
		Voided        *int64  `json:"voided_minor"`
		Expected      *int64  `json:"expected_minor"`
		Declared      *int64  `json:"declared_minor"`
		Variance      *int64  `json:"variance_minor"`
		AuthorisedBy  *string `json:"authorised_by"`
		Authorisation *string `json:"authorisation_reason"`
	} `json:"shifts"`
	Totals struct {
		Payments       int64 `json:"payments_minor"`
		Deposits       int64 `json:"deposits_minor"`
		Refunds        int64 `json:"refunds_minor"`
		Net            int64 `json:"net_minor"`
		Shortage       int64 `json:"shortage_minor"`
		Excess         int64 `json:"excess_minor"`
		Counted        int   `json:"counted_shifts"`
		Open           int   `json:"open_shifts"`
		Pending        int   `json:"pending_shifts"`
		VoidedReceipts int64 `json:"voided_receipts"`
		Voided         int64 `json:"voided_minor"`
		VoidedRefunds  int64 `json:"voided_refunds_minor"`
	} `json:"totals"`
}

func (w *bedWorld) collection(t *testing.T, date string) collectionBody {
	t.Helper()
	res := do(w.r, "GET", w.root+"/collections/"+date, "admin", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var b collectionBody
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &b))
	return b
}

func TestTheDailyCollectionTotalsEachShiftByPurpose(t *testing.T) {
	w := newBedWorld(t)
	today := time.Now().In(ist).Format(time.DateOnly)
	// The clerk's shift: a ₹5,000 deposit, a ₹1,000 refund of what is unused,
	// a ₹200 payment that stands and a ₹50 one voided.
	admission, shiftID := w.refundable(t, 500000, 200000)
	status, body := w.refund(admission, 100000, "")
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.refund(admission, 50000, "")
	require.Equal(t, http.StatusCreated, status, body)
	var refunded struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &refunded))
	status, body = w.put("clerk", "/receipts/"+refunded.ID+"/voids/"+v7(), `{"reason":"Patient stayed"}`)
	require.Equal(t, http.StatusOK, status, body)
	paying, patient := uuid.NewString(), uuid.NewString()
	w.charge(paying, patient, 30000)
	status, body = w.pay("clerk", paying, 20000)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.pay("clerk", paying, 5000)
	require.Equal(t, http.StatusCreated, status, body)
	var voided struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &voided))
	status, body = w.put("clerk", "/receipts/"+voided.ID+"/voids/"+v7(), `{"reason":"Keyed twice"}`)
	require.Equal(t, http.StatusOK, status, body)
	// Expected 5000 - 1000 + 200 = 4200; counted ₹1 short.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":419900}`)
	require.Equal(t, http.StatusOK, status, body)

	// clerk2's shift is still open, with cash in it.
	_, openID := w.counterWithShift(t, "clerk2", "OPD-1", 1000)
	other := uuid.NewString()
	w.charge(other, uuid.NewString(), 900)
	status, body = w.pay("clerk2", other, 500)
	require.Equal(t, http.StatusCreated, status, body)

	if time.Now().In(ist).Format(time.DateOnly) != today {
		t.Skip("the setup ran across midnight in the hospital; its shifts are on two days")
	}
	day := w.collection(t, today)
	require.Equal(t, today, day.Date)
	require.Equal(t, "Asia/Kolkata", day.Timezone)
	require.False(t, day.Final, "today, with a shift open and one waiting")
	require.Len(t, day.Shifts, 2)
	counted, open := day.Shifts[0], day.Shifts[1]
	require.Equal(t, shiftID, counted.ID)
	require.Equal(t, "pending_authorisation", counted.Status)
	require.EqualValues(t, 0, *counted.Float)
	require.EqualValues(t, 20000, *counted.Payments, "the voided payment does not stand")
	require.EqualValues(t, 500000, *counted.Deposits)
	require.EqualValues(t, 100000, *counted.Refunds)
	require.EqualValues(t, 2, *counted.VoidedCount)
	require.EqualValues(t, 5000, *counted.Voided, "voided takings only")
	require.EqualValues(t, 50000, *counted.VoidedRefunds, "a voided refund is money that never left, apart")
	require.EqualValues(t, 420000, *counted.Expected)
	require.EqualValues(t, 419900, *counted.Declared)
	require.EqualValues(t, -100, *counted.Variance)

	require.Equal(t, openID, open.ID)
	require.Equal(t, "open", open.Status)
	for name, v := range map[string]*int64{"float": open.Float, "payments": open.Payments, "deposits": open.Deposits, "refunds": open.Refunds,
		"voided": open.Voided, "voided_count": open.VoidedCount, "voided_refunds": open.VoidedRefunds, "expected": open.Expected, "declared": open.Declared, "variance": open.Variance} {
		require.Nil(t, v, "an open shift is blind: %s", name)
	}
	tot := day.Totals
	require.EqualValues(t, 20000, tot.Payments, "only counted shifts are totalled")
	require.EqualValues(t, 500000, tot.Deposits)
	require.EqualValues(t, 100000, tot.Refunds)
	require.EqualValues(t, 420000, tot.Net, "a refund is paid out of the day's cash")
	require.EqualValues(t, 100, tot.Shortage)
	require.EqualValues(t, 0, tot.Excess)
	require.Equal(t, 1, tot.Counted)
	require.Equal(t, 1, tot.Open)
	require.Equal(t, 1, tot.Pending)
	require.EqualValues(t, 2, tot.VoidedReceipts)
	require.EqualValues(t, 5000, tot.Voided)
	require.EqualValues(t, 50000, tot.VoidedRefunds)

	// Authorised with a reason a spreadsheet would run as a formula.
	status, body = w.put("admin", "/cash-shifts/"+shiftID+"/authorisation", `{"reason":"=HYPERLINK(\"x\")"}`)
	require.Equal(t, http.StatusOK, status, body)
	day = w.collection(t, today)
	require.Equal(t, "closed", day.Shifts[0].Status)
	require.Equal(t, "user-admin", *day.Shifts[0].AuthorisedBy)
	require.Equal(t, 0, day.Totals.Pending)

	res := do(w.r, "GET", w.root+"/collections/"+today+"/export.csv", "admin", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, "text/csv; charset=utf-8", res.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="billing-collection-`+w.hospital+`-`+today+`.csv"`, res.Header().Get("Content-Disposition"))
	require.Equal(t, "2", res.Header().Get("X-Export-Row-Count"))
	require.Len(t, res.Header().Get("X-Export-Sha256"), 64)
	rows, err := csv.NewReader(strings.NewReader(res.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 4, "header, two shifts, total")
	header := map[string]int{}
	for i, name := range rows[0] {
		header[name] = i
	}
	cell := func(row int, name string) string {
		i, ok := header[name]
		require.True(t, ok, name)
		return rows[row][i]
	}
	require.Equal(t, "closed", cell(1, "status"))
	require.Equal(t, "200.00", cell(1, "payments"))
	require.Equal(t, "5000.00", cell(1, "deposits"))
	require.Equal(t, "1000.00", cell(1, "refunds"))
	require.Equal(t, "4200.00", cell(1, "net"))
	require.Equal(t, "-1.00", cell(1, "variance"))
	require.Equal(t, "1.00", cell(1, "shortage"))
	require.Equal(t, "0.00", cell(1, "excess"))
	require.Equal(t, `'=HYPERLINK("x")`, cell(1, "authorisation_reason"), "text cells cannot run as formulas")
	require.Equal(t, "open", cell(2, "status"))
	for _, name := range []string{"opening_float", "payments", "deposits", "refunds", "net", "voided_receipts", "voided_amount", "voided_refunds", "expected", "declared", "variance", "shortage", "excess"} {
		require.Empty(t, cell(2, name), "an open shift is blind in the CSV too: %s", name)
	}
	require.Equal(t, "TOTAL", cell(3, "counter_code"))
	require.Equal(t, "4200.00", cell(3, "net"))
	require.Equal(t, "1.00", cell(3, "shortage"))
	require.Equal(t, "2", cell(3, "voided_receipts"))
	require.Equal(t, "50.00", cell(3, "voided_amount"))
	require.Equal(t, "500.00", cell(3, "voided_refunds"))
	require.Equal(t, "500.00", cell(1, "voided_refunds"))
	require.NotContains(t, res.Body.String(), admission, "no admission identifiers")
	require.NotContains(t, res.Body.String(), paying)

	// Refusals.
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", w.root+"/collections/"+today, "clerk", "").Code, "cashiers do not read the day")
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", w.root+"/collections/"+today+"/export.csv", "clerk", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/collections/"+today, "outsider", "").Code, "no billing grant here")
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/collections/"+today+"/export.csv", "outsider", "").Code)
	// Another tenant's administrator, even granted the same hospital id in their tenant, sees none of it.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/access/user-adminB", "adminB", "").Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "adminB", `{"timezone":"Asia/Kolkata"}`).Code)
	var tenantB collectionBody
	res = do(w.r, "GET", w.root+"/collections/"+today, "adminB", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &tenantB))
	require.Empty(t, tenantB.Shifts, "tenant A's shifts never reach tenant B")
	require.Zero(t, tenantB.Totals.Net)
	for _, bad := range []string{"2026-02-30", "2026-1-05", "today", "0000-01-01"} {
		require.Equal(t, http.StatusBadRequest, do(w.r, "GET", w.root+"/collections/"+bad, "admin", "").Code, bad)
	}
	tomorrow := time.Now().In(ist).AddDate(0, 0, 1).Format(time.DateOnly)
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", w.root+"/collections/"+tomorrow, "admin", "").Code, "a day not yet begun")
	yesterday := w.collection(t, time.Now().In(ist).AddDate(0, 0, -1).Format(time.DateOnly))
	require.True(t, yesterday.Final, "a past day with nothing open is final")
	require.Empty(t, yesterday.Shifts)
}

func TestTheCollectionDayIsTheHospitalsLocalDay(t *testing.T) {
	w := newBedWorld(t)
	// One clock reading picks a zone whose local date is not the UTC date and
	// whose midnight is at least an hour away: UTC-12 (14:00 to 22:59 the day
	// before) until 10:59 UTC, UTC+14 (01:00 to 13:59 the day after) from 11:00.
	now := time.Now().UTC()
	today := now.Format(time.DateOnly)
	res := do(w.r, "GET", w.root+"/collections/"+today, "admin", "")
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "timezone_unset", "never a default zone")

	zone := "Etc/GMT-14"
	if now.Hour() <= 10 {
		zone = "Etc/GMT+12"
	}
	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"`+zone+`"}`).Code)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-Z", 700)
	status, body := w.put("clerk", "/cash-shifts/"+shiftID+"/close", `{"declared_minor":700}`)
	require.Equal(t, http.StatusOK, status, body)

	local := now.In(loc).Format(time.DateOnly)
	require.Equal(t, local, time.Now().In(loc).Format(time.DateOnly), "the test ran within an hour")
	require.NotEqual(t, local, today)
	day := w.collection(t, local)
	require.Len(t, day.Shifts, 1, "the shift is on the hospital's date")
	require.Equal(t, shiftID, day.Shifts[0].ID)
	require.False(t, day.Final, "today is not final while more shifts can open")
	require.Equal(t, 1, day.Totals.Counted)

	res = do(w.r, "GET", w.root+"/collections/"+today, "admin", "")
	if today > local {
		require.Equal(t, http.StatusBadRequest, res.Code, "the UTC date is the hospital's tomorrow")
	} else {
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		require.NotContains(t, res.Body.String(), shiftID, "the UTC date is the hospital's yesterday")
	}
}
