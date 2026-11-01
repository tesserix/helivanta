package billing_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func (w *bedWorld) offer(token, shift, id, to, counts string) (int, string) {
	return w.put(token, "/cash-shifts/"+shift+"/handovers/"+id, `{"to":"`+to+`","counts":`+counts+`}`)
}

func (w *bedWorld) accept(token, id, shift, counts string) (int, string) {
	return w.put(token, "/cash-handovers/"+id+"/acceptance", `{"shift":"`+shift+`","counts":`+counts+`}`)
}

type handoverShift struct {
	Status             string  `json:"status"`
	OpeningFloatMinor  int64   `json:"opening_float_minor"`
	ExpectedMinor      *int64  `json:"expected_minor"`
	VarianceMinor      *int64  `json:"variance_minor"`
	HandoverID         *string `json:"handover_id"`
	HandedOver         bool    `json:"handed_over"`
	LiveHandoverID     *string `json:"live_handover_id"`
	LiveHandoverTo     *string `json:"live_handover_to"`
	LiveHandoverStatus *string `json:"live_handover_status"`
}

func (w *bedWorld) myHandoverShift(t *testing.T, token string) handoverShift {
	t.Helper()
	res := do(w.r, "GET", w.root+"/cash-shifts/mine", token, "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var s handoverShift
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	return s
}

// shiftByID reads any shift from the supervisor's list.
func (w *bedWorld) shiftByID(t *testing.T, id string) handoverShift {
	t.Helper()
	res := do(w.r, "GET", w.root+"/cash-shifts?limit=100", "admin", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page struct {
		Items []struct {
			ID string `json:"id"`
			handoverShift
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	for _, s := range page.Items {
		if s.ID == id {
			return s.handoverShift
		}
	}
	t.Fatalf("shift %s not listed", id)
	return handoverShift{}
}

func TestAHandoverMovesTheDrawer(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	status, body := w.pay("clerk", admission, 3000)
	require.Equal(t, http.StatusCreated, status, body)

	id := v7()
	status, body = w.offer("clerk", shiftID, id, "user-clerk2", `{"note:2000":2}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"status":"offered"`)
	require.NotContains(t, body, "note:", "the offer carries no count")
	require.NotContains(t, body, "4000", "and nothing of the expected cash")
	status, _ = w.offer("clerk", shiftID, id, "user-clerk2", `{"note:2000":2,"coin:50":0}`)
	require.Equal(t, http.StatusOK, status, "a retried offer replays")
	status, _ = w.offer("clerk", shiftID, id, "user-clerk2", `{"note:2000":1}`)
	require.Equal(t, http.StatusConflict, status, "the same id with another count")
	status, _ = w.offer("clerk", shiftID, v7(), "user-clerk2", `{"note:2000":2}`)
	require.Equal(t, http.StatusConflict, status, "one handover at a time")

	// The drawer is between cashiers: nothing goes in or out.
	status, body = w.pay("clerk", admission, 500)
	require.Equal(t, http.StatusConflict, status, body)
	status, body = w.pickup("admin", shiftID, v7(), `{"note:500":1}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "shift_closed")
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(4000))
	require.Equal(t, http.StatusConflict, status, body)
	mine := w.myHandoverShift(t, "clerk")
	require.Equal(t, "handing_over", mine.Status)
	require.Equal(t, id, *mine.LiveHandoverID)
	require.Equal(t, "user-clerk2", *mine.LiveHandoverTo)
	require.Nil(t, mine.ExpectedMinor)
	status, body = w.put("admin", "/bed-charging", `{"timezone":"Asia/Kolkata"}`)
	require.Equal(t, http.StatusOK, status, body)
	today := time.Now().In(ist).Format(time.DateOnly)
	if day := w.collection(t, today); day.Date == today {
		require.Len(t, day.Shifts, 1)
		require.Equal(t, "handing_over", day.Shifts[0].Status)
		require.Nil(t, day.Shifts[0].Float, "a drawer being handed over is as blind as an open one")
		require.Nil(t, day.Shifts[0].Payments)
		require.Equal(t, 1, day.Totals.Open)
		require.False(t, day.Final)
	}

	incoming := do(w.r, "GET", w.root+"/cash-handovers/incoming", "clerk2", "")
	require.Equal(t, http.StatusOK, incoming.Code, incoming.Body.String())
	require.Contains(t, incoming.Body.String(), id)
	require.NotContains(t, incoming.Body.String(), "note:", "the incoming cashier counts blind")
	require.NotContains(t, do(w.r, "GET", w.root+"/cash-handovers/incoming", "admin", "").Body.String(), id, "only the named cashier's offers")

	next := v7()
	status, _ = w.accept("clerk", id, next, `{"note:2000":2}`)
	require.Equal(t, http.StatusNotFound, status, "a handover is accepted only by its taker")
	status, body = w.accept("clerk2", id, next, `{"note:2000":2}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"accepted"`)
	status, _ = w.accept("clerk2", id, next, `{"note:2000":2}`)
	require.Equal(t, http.StatusOK, status, "a retried acceptance replays")
	status, _ = w.accept("clerk2", id, v7(), `{"note:2000":2}`)
	require.Equal(t, http.StatusConflict, status, "and is not a second acceptance")

	out := w.shiftByID(t, shiftID)
	require.Equal(t, "closed", out.Status, "the count was the expected cash")
	require.True(t, out.HandedOver)
	require.Equal(t, int64(4000), *out.ExpectedMinor)
	in := w.myHandoverShift(t, "clerk2")
	require.Equal(t, "open", in.Status)
	require.Equal(t, int64(4000), in.OpeningFloatMinor, "the agreed count is the float")
	require.Equal(t, id, *in.HandoverID)
	status, body = w.pay("clerk2", admission, 1000)
	require.Equal(t, http.StatusCreated, status, "the counter takes cash again, in the new shift: "+body)
	require.Contains(t, body, `"cashier":"user-clerk2"`)
}

func TestAHandoverThatDisagreesWaitsForASupervisor(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	status, body := w.pay("clerk", admission, 3000)
	require.Equal(t, http.StatusCreated, status, body)
	id := v7()
	status, body = w.offer("clerk", shiftID, id, "user-clerk2", `{"note:2000":2}`)
	require.Equal(t, http.StatusCreated, status, body)

	next := v7()
	status, body = w.accept("clerk2", id, next, `{"note:2000":1,"note:1000":2}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"disputed"`, "the same total in other notes is a miscount")
	require.NotContains(t, body, "note:", "and the answer tells neither count")
	status, body = w.accept("clerk2", id, next, `{"note:2000":2}`)
	require.Equal(t, http.StatusConflict, status, "one count, no probing: "+body)
	require.Contains(t, body, "handover_decided")
	status, body = w.put("clerk", "/cash-handovers/"+id+"/withdrawal", "")
	require.Equal(t, http.StatusConflict, status, "a dispute is not withdrawn: "+body)
	require.Equal(t, "disputed", *w.myHandoverShift(t, "clerk").LiveHandoverStatus)

	disputed := do(w.r, "GET", w.root+"/cash-handovers/disputed", "admin", "")
	require.Equal(t, http.StatusOK, disputed.Code, disputed.Body.String())
	require.Contains(t, disputed.Body.String(), id)
	require.NotContains(t, disputed.Body.String(), "note:", "the supervisor counts blind too")
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", w.root+"/cash-handovers/disputed", "clerk", "").Code)

	settle := `{"counts":{"note:2000":1,"note:1000":1},"reason":"Recounted at the counter with both cashiers"}`
	status, body = w.put("clerk2", "/cash-handovers/"+id+"/settlement", settle)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "own_handover", "neither cashier settles their own dispute")
	status, _ = w.put("clerk", "/cash-handovers/"+id+"/settlement", settle)
	require.Equal(t, http.StatusForbidden, status)
	status, body = w.put("admin", "/cash-handovers/"+id+"/settlement", settle)
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"settled"`)
	require.Contains(t, body, `"agreed_minor":3000`)
	status, _ = w.put("admin", "/cash-handovers/"+id+"/settlement", settle)
	require.Equal(t, http.StatusOK, status, "a retried settlement replays")
	status, _ = w.put("admin", "/cash-handovers/"+id+"/settlement", `{"counts":{"note:2000":1,"note:1000":1},"reason":"Another reason"}`)
	require.Equal(t, http.StatusConflict, status)

	out := w.shiftByID(t, shiftID)
	require.Equal(t, "pending_authorisation", out.Status, "the supervisor's count is short of the expected cash")
	require.Equal(t, int64(-1000), *out.VarianceMinor)
	require.True(t, out.HandedOver)
	in := w.myHandoverShift(t, "clerk2")
	require.Equal(t, "open", in.Status, "the counter is not held by the waiting variance")
	require.Equal(t, int64(3000), in.OpeningFloatMinor)
	status, body = w.put("admin", "/cash-shifts/"+shiftID+"/authorisation", `{"reason":"Shortage written off after recount"}`)
	require.Equal(t, http.StatusOK, status, body)
}

func TestAHandoverIsWithdrawnOnlyWhileOffered(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)

	status, _ := w.offer("clerk", shiftID, v7(), "user-clerk", `{"note:1000":1}`)
	require.Equal(t, http.StatusBadRequest, status, "not to yourself")
	status, _ = w.offer("clerk", shiftID, v7(), "user-clerk2", `{"note:200000":5001}`)
	require.Equal(t, http.StatusBadRequest, status, "more than a float may hold")
	status, _ = w.offer("clerk", shiftID, v7(), "user-clerk2", `{"note:7":1}`)
	require.Equal(t, http.StatusBadRequest, status, "a note that does not exist")
	status, _ = w.offer("clerk2", shiftID, v7(), "user-admin", `{"note:1000":1}`)
	require.Equal(t, http.StatusNotFound, status, "only the shift's own cashier")

	id := v7()
	status, body := w.offer("clerk", shiftID, id, "user-clerk2", `{"note:1000":1}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, _ = w.put("clerk2", "/cash-handovers/"+id+"/withdrawal", "")
	require.Equal(t, http.StatusNotFound, status, "only the giver withdraws")
	status, body = w.put("clerk", "/cash-handovers/"+id+"/withdrawal", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"withdrawn"`)
	status, _ = w.put("clerk", "/cash-handovers/"+id+"/withdrawal", "")
	require.Equal(t, http.StatusOK, status, "a retried withdrawal replays")
	require.Equal(t, "open", w.myHandoverShift(t, "clerk").Status)
	status, body = w.pay("clerk", admission, 500)
	require.Equal(t, http.StatusCreated, status, "the drawer is back in use: "+body)
	status, body = w.accept("clerk2", id, v7(), `{"note:1000":1}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "handover_decided")

	// The incoming cashier already runs a drawer: the acceptance changes nothing.
	again := v7()
	status, body = w.offer("clerk", shiftID, again, "user-clerk2", `{"note:1000":1,"note:500":1}`)
	require.Equal(t, http.StatusCreated, status, "a new offer after a withdrawal: "+body)
	w.counterWithShift(t, "clerk2", "IPD-2", 0)
	status, body = w.accept("clerk2", again, v7(), `{"note:1000":2}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "shift_open")
	require.Equal(t, "offered", *w.myHandoverShift(t, "clerk").LiveHandoverStatus, "not even a dispute")
}

func TestAHandoverWaitsForUPI(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 500)
	status, _, body := w.askUpi("clerk", admission, v7(), "deposit", 7000)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.offer("clerk", shiftID, v7(), "user-clerk2", `{"note:500":1}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "upi_waiting")
	require.Equal(t, "open", w.myHandoverShift(t, "clerk").Status)
}

func TestHandoversHoldInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	insert := func(tx *gorm.DB, id, shift, from string) error {
		return tx.Exec(`INSERT INTO billing_cash_handovers (tenant_id,hospital_id,id,from_shift_id,counter_id,from_cashier,to_cashier,currency,outgoing_counts)
 SELECT tenant_id,hospital_id,?,id,counter_id,?,'user-admin',currency,'{"note:1000":1}' FROM billing_cash_shifts WHERE id=?`, id, from, shift).Error
	}

	requireConstraint(t, w.as("user-clerk2", func(tx *gorm.DB) error { return insert(tx, v7(), shiftID, "user-clerk") }),
		"billing_cash_handover_offer", "only the shift's cashier offers it")
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='handing_over' WHERE id=?`, shiftID).Error
	}), "billing_cash_shift_handover", "no handover without an offer")

	id := v7()
	status, body := w.offer("clerk", shiftID, id, "user-admin", `{"note:1000":1}`)
	require.Equal(t, http.StatusCreated, status, body)
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET status='accepted',incoming_counts=outgoing_counts,to_shift_id=?,agreed_counts=outgoing_counts,agreed_minor=1000,decided_at=now() WHERE id=?`, v7(), id).Error
	}), "billing_cash_handover_actor", "the giver does not accept their own offer")
	requireConstraint(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET status='accepted',incoming_counts='{"note:500":2}',to_shift_id=?,agreed_counts=outgoing_counts,agreed_minor=1000,decided_at=now() WHERE id=?`, v7(), id).Error
	}), "billing_cash_handover_form", "accepted means the same notes")
	requireConstraint(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET outgoing_counts='{"note:500":2}' WHERE id=?`, id).Error
	}), "billing_cash_handover_actor", "an offer keeps its count")
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',expected_minor=1000,declared_minor=1000,declared_counts='{"note:1000":1}',closed_at=now(),handed_over=true WHERE id=?`, shiftID).Error
	}), "billing_cash_shift_handover", "a shift being handed over closes only through its handover")
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='open' WHERE id=?`, shiftID).Error
	}), "billing_cash_shift_handover", "nor reopens while the offer stands")
	requireConstraint(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_cash_shifts (tenant_id,hospital_id,id,counter_id,cashier,currency,opening_float_minor,handover_id)
 SELECT tenant_id,hospital_id,?,counter_id,'user-admin',currency,1000,? FROM billing_cash_shifts WHERE id=?`, v7(), id, shiftID).Error
	}), "billing_cash_shift_from_handover", "no shift opens from an offer not yet accepted")

	requireConstraint(t, w.as("user-clerk2", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET status='withdrawn',decided_at=now() WHERE id=?`, id).Error
	}), "billing_cash_handover_actor", "only the giver withdraws")
	next := v7()
	accept := func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET status='accepted',incoming_counts=outgoing_counts,to_shift_id=?,agreed_counts=outgoing_counts,agreed_minor=1000,decided_at=now() WHERE id=?`, next, id).Error
	}
	closeWith := func(tx *gorm.DB, counts string, minor int) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',expected_minor=1000,declared_minor=?,declared_counts=?::jsonb,closed_at=now(),handed_over=true WHERE id=?`, minor, counts, shiftID).Error
	}
	openWith := func(tx *gorm.DB, float int) error {
		return tx.Exec(`INSERT INTO billing_cash_shifts (tenant_id,hospital_id,id,counter_id,cashier,currency,opening_float_minor,handover_id)
 SELECT tenant_id,hospital_id,?,counter_id,'user-admin',currency,?,? FROM billing_cash_shifts WHERE id=?`, next, float, id, shiftID).Error
	}
	requireConstraint(t, w.as("user-admin", func(tx *gorm.DB) error {
		if err := accept(tx); err != nil {
			return err
		}
		return closeWith(tx, `{"note:500":2}`, 1000)
	}), "billing_cash_shift_handover", "the shift closes on the agreed notes, not the same sum in others")
	requireConstraint(t, w.as("user-admin", func(tx *gorm.DB) error {
		if err := accept(tx); err != nil {
			return err
		}
		if err := closeWith(tx, `{"note:1000":1}`, 1000); err != nil {
			return err
		}
		return openWith(tx, 999)
	}), "billing_cash_shift_from_handover", "the next shift's float is the agreed count")
	requireConstraint(t, w.as("user-admin", func(tx *gorm.DB) error {
		if err := accept(tx); err != nil {
			return err
		}
		if err := closeWith(tx, `{"note:1000":1}`, 1000); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-clerk',true)`).Error; err != nil {
			return err
		}
		return openWith(tx, 1000)
	}), "billing_cash_shift_from_handover", "only who resolved the handover opens the next shift")
	rolledBack := errors.New("rolled back")
	require.ErrorIs(t, w.as("user-admin", func(tx *gorm.DB) error {
		if err := accept(tx); err != nil {
			return err
		}
		if err := closeWith(tx, `{"note:1000":1}`, 1000); err != nil {
			return err
		}
		if err := openWith(tx, 1000); err != nil {
			return err
		}
		return rolledBack
	}), rolledBack, "the same steps with the agreed count hold")

	// Disputed, then settled by one of its own cashiers.
	status, body = w.accept("admin", id, v7(), `{"note:500":2}`)
	require.Equal(t, http.StatusOK, status, body)
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET status='settled',agreed_counts=outgoing_counts,agreed_minor=1000,settled_by='user-clerk',settlement_reason='Mine',settled_at=now() WHERE id=?`, id).Error
	}), "billing_cash_handover_form", "a dispute is settled by neither cashier")
	requireConstraint(t, w.as("user-clerk2", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_handovers SET status='settled',agreed_counts=outgoing_counts,agreed_minor=1000,settled_by='user-clerk2',settlement_reason='Recount',settled_at=now(),to_shift_id=? WHERE id=?`, v7(), id).Error
	}), "billing_cash_handover_actor", "and keeps the incoming count and shift")
}

func TestAcceptanceRacesWithdrawal(t *testing.T) {
	for range 5 {
		w := newBedWorld(t)
		_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
		id := v7()
		status, body := w.offer("clerk", shiftID, id, "user-clerk2", `{"note:1000":1}`)
		require.Equal(t, http.StatusCreated, status, body)
		var wg sync.WaitGroup
		var accepted, withdrawn int
		wg.Add(2)
		go func() {
			defer wg.Done()
			accepted, _ = w.accept("clerk2", id, v7(), `{"note:1000":1}`)
		}()
		go func() {
			defer wg.Done()
			withdrawn, _ = w.put("clerk", "/cash-handovers/"+id+"/withdrawal", "")
		}()
		wg.Wait()
		require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, []int{accepted, withdrawn}, "exactly one wins")
		out := w.shiftByID(t, shiftID)
		if accepted == http.StatusOK {
			require.Equal(t, "closed", out.Status)
			require.Equal(t, "open", w.myHandoverShift(t, "clerk2").Status)
		} else {
			require.Equal(t, "open", out.Status)
			require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+"/cash-shifts/mine", "clerk2", "").Code)
		}
	}
}
