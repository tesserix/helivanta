package billing_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type deskRow struct {
	AdmissionID      string `json:"admission_id"`
	Status           string `json:"status"`
	DischargeOrdered bool   `json:"discharge_ordered"`
	BalanceMinor     int64  `json:"balance_minor"`
	HeldMinor        int64  `json:"deposit_held_minor"`
	RefundableMinor  int64  `json:"refundable_minor"`
	HasFinalBill     bool   `json:"has_final_bill"`
}

func (w *bedWorld) desk(view string) []string {
	path := w.root + "/accounts"
	if view != "" {
		path += "?view=" + view
	}
	res := do(w.r, "GET", path, "nurse", "")
	require.Equal(w.t, http.StatusOK, res.Code, res.Body.String())
	var page struct{ Data []deskRow }
	require.NoError(w.t, json.Unmarshal(res.Body.Bytes(), &page))
	out := []string{}
	for _, r := range page.Data {
		out = append(out, r.AdmissionID)
	}
	return out
}

func TestTheBillingDeskListsAccountsByWhatTheyNeed(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	w.counterWithShift(t, "clerk", "IPD-K1", 0)

	// Staying: owes 1000, nothing ordered.
	staying, p1 := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(staying, p1, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.charge(staying, p1, 100000)

	// Going: discharge ordered, owes 2000.
	going, p2 := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(going, p2, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.charge(going, p2, 200000)
	require.NoError(t, w.orderDischarge(going, p2, uuid.NewString(), time.Now()))
	status, body := w.deposit("clerk", going, 900000)
	require.Equal(t, http.StatusCreated, status, body)

	// Gone: released with a deposit beyond the bill, final bill issued.
	gone, p3 := uuid.NewString(), uuid.NewString()
	goneStay := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(gone, p3, goneStay, time.Now().Add(-time.Hour)))
	status, body = w.deposit("clerk", gone, 500000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(gone, p3, 100000)
	require.NoError(t, w.end(gone, goneStay, time.Now(), "release"))
	require.NoError(t, w.release(gone, p3, time.Now()))
	status, body = w.post("/admissions/" + gone + "/final-bills")
	require.Equal(t, http.StatusCreated, status, body)

	require.ElementsMatch(t, []string{staying, going, gone}, w.desk(""))
	require.Equal(t, []string{staying}, w.desk("open"))
	require.Equal(t, []string{going}, w.desk("discharge_ordered"))
	require.Equal(t, []string{gone}, w.desk("released"))
	require.ElementsMatch(t, []string{staying, going, gone}, w.desk("to_collect"), "a held deposit pays nothing until applied")
	require.Equal(t, []string{gone}, w.desk("to_refund"), "a deposit is refundable only after a final bill")
	res := do(w.r, "GET", w.root+"/accounts?view=to_refund", "nurse", "")
	var before struct{ Data []deskRow }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &before))
	require.EqualValues(t, 250000, before.Data[0].RefundableMinor, "held 5000 less what the bill still needs, 2500")
	require.EqualValues(t, 500000, before.Data[0].HeldMinor)

	// Applied, the gone patient owes nothing and has 2500 to refund.
	status, body = w.apply("clerk", gone)
	require.Equal(t, http.StatusCreated, status, body)
	require.ElementsMatch(t, []string{staying, going}, w.desk("to_collect"))
	res = do(w.r, "GET", w.root+"/accounts?view=to_refund", "nurse", "")
	var page struct{ Data []deskRow }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, deskRow{AdmissionID: gone, Status: "closed", BalanceMinor: 0, HeldMinor: 250000, RefundableMinor: 250000, HasFinalBill: true}, page.Data[0])

	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", w.root+"/accounts?view=everyone", "nurse", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/accounts", "outsider", "").Code, "no grant at the hospital")
}
