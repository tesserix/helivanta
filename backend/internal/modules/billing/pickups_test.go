package billing_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (w *bedWorld) pickup(token, shift, id, counts string) (int, string) {
	return w.put(token, "/cash-shifts/"+shift+"/pickups/"+id, `{"counts":`+counts+`}`)
}

func (w *bedWorld) as(subject string, fn func(tx *gorm.DB) error) error {
	return w.db.WithTenant(tenantdb.WithActor(w.ctx, subject), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject',?,true)`, subject).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

type pickedShift struct {
	Status         string          `json:"status"`
	ExpectedMinor  *int64          `json:"expected_minor"`
	DeclaredMinor  *int64          `json:"declared_minor"`
	VarianceMinor  *int64          `json:"variance_minor"`
	PickedUpMinor  int64           `json:"picked_up_minor"`
	DeclaredCounts json.RawMessage `json:"declared_counts"`
}

func (w *bedWorld) mine(t *testing.T, token string) pickedShift {
	t.Helper()
	res := do(w.r, "GET", w.root+"/cash-shifts/mine", token, "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var s pickedShift
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	return s
}

func TestAPickupLowersTheExpectedCash(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	receipt := v7()
	status, body := w.put("clerk", "/admissions/"+admission+"/receipts/"+receipt, `{"amount_minor":3000,"currency":"INR"}`)
	require.Equal(t, http.StatusCreated, status, body)

	first := v7()
	status, body = w.pickup("admin", shiftID, first, `{"note:2000":1}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"amount_minor":2000`)
	require.Contains(t, body, `"recorded_by":"user-admin"`)
	status, _ = w.pickup("admin", shiftID, first, `{"note:2000":1,"coin:50":0}`)
	require.Equal(t, http.StatusOK, status, "a retried pickup replays; a zero entry is no entry")
	status, _ = w.pickup("admin", shiftID, first, `{"note:1000":2}`)
	require.Equal(t, http.StatusConflict, status, "the same id with another count")
	status, _ = w.pickup("clerk", shiftID, v7(), `{"note:100":1}`)
	require.Equal(t, http.StatusForbidden, status, "a cashier does not record pickups")
	status, _ = w.pickup("admin", shiftID, v7(), `{}`)
	require.Equal(t, http.StatusBadRequest, status, "nothing counted")

	// 2000 is left: 1000 float + 3000 taken - 2000 picked up.
	status, body = w.pickup("admin", shiftID, v7(), `{"note:2000":1,"coin:50":1}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "drawer_short")
	require.NotContains(t, body, "2000", "the refusal tells no figure")
	mine := w.mine(t, "clerk")
	require.Equal(t, int64(2000), mine.PickedUpMinor, "the cashier sees what they handed over")
	require.Nil(t, mine.ExpectedMinor, "and nothing of what to expect")

	// The receipt's cash went to the safe: voiding it would take the drawer below zero.
	status, body = w.put("clerk", "/receipts/"+receipt+"/voids/"+v7(), `{"reason":"Wrong patient"}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "drawer_short")

	list := do(w.r, "GET", w.root+"/cash-shifts/"+shiftID+"/pickups", "admin", "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), first)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", w.root+"/cash-shifts/"+shiftID+"/pickups", "clerk", "").Code)

	status, body = w.pickup("admin", shiftID, v7(), `{"note:2000":1}`)
	require.Equal(t, http.StatusCreated, status, "the whole drawer may go: "+body)
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(0))
	require.Equal(t, http.StatusOK, status, body)
	var closed pickedShift
	require.NoError(t, json.Unmarshal([]byte(body), &closed))
	require.Equal(t, "closed", closed.Status, "the safe drops are no shortage")
	require.Equal(t, int64(0), *closed.ExpectedMinor)
	require.Equal(t, int64(4000), closed.PickedUpMinor)
	status, body = w.pickup("admin", shiftID, v7(), `{"note:100":1}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "shift_closed")
}

func TestAPickupIsNeverTheCashiersOwn(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk2", "IPD-2", 1000)

	status, body := w.pickup("clerk2", shiftID, v7(), `{"note:500":1}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "own_pickup")
	insert := func(subject, recordedBy string) error {
		return w.as(subject, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_pickups (tenant_id,hospital_id,id,shift_id,counter_id,kind,amount_minor,currency,counts,recorded_by)
 VALUES (?,?,?,?,?,'pickup',500,'INR','{"note:500":1}',?)`, testutil.TenantA, w.hospital, v7(), shiftID, counter, recordedBy).Error
		})
	}
	err := insert("user-clerk2", "user-clerk2")
	requireConstraint(t, err, "billing_cash_pickup_own", "the database refuses the cashier too")
	requireConstraint(t, insert("user-admin", "user-nurse"), "billing_cash_pickup_own", "recorded by the acting subject")
	require.NoError(t, insert("user-admin", "user-admin"))
}

func TestACorrectionPutsBackWhatWasOverstated(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-3", 0)
	status, body := w.pay("clerk", admission, 3000)
	require.Equal(t, http.StatusCreated, status, body)
	picked := v7()
	status, body = w.pickup("admin", shiftID, picked, `{"note:2000":1}`)
	require.Equal(t, http.StatusCreated, status, body)

	fix := v7()
	path := "/cash-pickups/" + picked + "/corrections/" + fix
	status, _ = w.put("admin", path, `{"amount_minor":500}`)
	require.Equal(t, http.StatusBadRequest, status, "a correction gives its reason")
	status, body = w.put("admin", path, `{"amount_minor":500,"reason":"Counted a 500 twice"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.Contains(t, body, `"kind":"correction"`)
	status, _ = w.put("admin", path, `{"amount_minor":500,"reason":"Counted a 500 twice"}`)
	require.Equal(t, http.StatusOK, status, "a retried correction replays")
	status, _ = w.put("admin", path, `{"amount_minor":400,"reason":"Counted a 500 twice"}`)
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, int64(1500), w.mine(t, "clerk").PickedUpMinor)

	status, body = w.put("admin", "/cash-pickups/"+picked+"/corrections/"+v7(), `{"amount_minor":1501,"reason":"Again"}`)
	require.Equal(t, http.StatusConflict, status, body)
	require.Contains(t, body, "over_pickup")
	status, body = w.put("admin", "/cash-pickups/"+picked+"/corrections/"+v7(), `{"amount_minor":1500,"reason":"Nothing went to the safe"}`)
	require.Equal(t, http.StatusCreated, status, "up to all it still counts: "+body)
	require.Equal(t, int64(0), w.mine(t, "clerk").PickedUpMinor)
	status, _ = w.put("admin", "/cash-pickups/"+fix+"/corrections/"+v7(), `{"amount_minor":1,"reason":"Of a correction"}`)
	require.Equal(t, http.StatusNotFound, status, "only a pickup is corrected")
	status, _ = w.put("admin", "/cash-pickups/"+v7()+"/corrections/"+v7(), `{"amount_minor":1,"reason":"Unknown"}`)
	require.Equal(t, http.StatusNotFound, status)

	// 0 + 3000 - 2000 + 500 + 1500.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(3000))
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"status":"closed"`)
	status, _ = w.put("admin", "/cash-pickups/"+picked+"/corrections/"+v7(), `{"amount_minor":1,"reason":"Late"}`)
	require.Equal(t, http.StatusConflict, status, "no correction once the shift is counted")
}

func TestTheCloseCountsByNote(t *testing.T) {
	w := newBedWorld(t)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-4", 1000)
	path := "/cash-shifts/" + shiftID + "/close"
	for _, bad := range []string{`{"declared_minor":1000}`, `{"counts":{"note:300":1}}`, `{"counts":{"note:500":-1}}`, `{"counts":{"note:500":1.5}}`,
		`{"counts":{"note:500":1000001}}`, `{"counts":[]}`, `{"counts":null}`} {
		status, body := w.put("clerk", path, bad)
		require.Equal(t, http.StatusBadRequest, status, bad+": "+body)
	}
	require.Equal(t, "open", w.mine(t, "clerk").Status)

	res := do(w.r, "GET", w.root+"/cash-denominations", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code)
	var list []struct {
		Key        string `json:"key"`
		ValueMinor int64  `json:"value_minor"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.Len(t, list, 16)
	require.Equal(t, "note:200000", list[0].Key, "₹2000 is still legal tender")
	require.Equal(t, "coin:50", list[15].Key)

	status, body := w.put("clerk", path, `{"counts":{"note:500":1,"coin:200":2,"coin:50":2,"note:10000":0}}`)
	require.Equal(t, http.StatusOK, status, body)
	var s pickedShift
	require.NoError(t, json.Unmarshal([]byte(body), &s))
	require.Equal(t, int64(1000), *s.DeclaredMinor)
	require.Equal(t, "closed", s.Status)
	require.JSONEq(t, `{"note:500":1,"coin:200":2,"coin:50":2}`, string(s.DeclaredCounts))
	status, _ = w.put("clerk", path, `{"counts":{"coin:50":2,"coin:200":2,"note:500":1}}`)
	require.Equal(t, http.StatusOK, status, "a retried close replays")
	status, _ = w.put("clerk", path, `{"counts":{"note:1000":1}}`)
	require.Equal(t, http.StatusConflict, status, "another count of the same total is another count")
}

func TestCountsAndPickupsHoldInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	total := func(counts string) *int64 {
		var v struct{ V *int64 }
		require.NoError(t, w.as("user-admin", func(tx *gorm.DB) error {
			return tx.Raw(`SELECT billing_cash_count_total('INR',?::jsonb) AS v`, counts).Scan(&v).Error
		}))
		return v.V
	}
	require.Equal(t, int64(250150), *total(`{"note:200000":1,"note:50000":1,"coin:50":3}`))
	require.Equal(t, int64(0), *total(`{}`))
	for _, bad := range []string{`{"note:300":1}`, `{"note:500":-1}`, `{"note:500":0.5}`, `{"note:500":"1"}`, `{"note:500":1000001}`, `[]`, `1`} {
		require.Nil(t, total(bad), bad)
	}

	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-6", 1000)
	insert := func(amount int64, counts string) error {
		return w.as("user-admin", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_pickups (tenant_id,hospital_id,id,shift_id,counter_id,kind,amount_minor,currency,counts,recorded_by)
 VALUES (?,?,?,?,?,'pickup',?,'INR',?::jsonb,'user-admin')`, testutil.TenantA, w.hospital, v7(), shiftID, counter, amount, counts).Error
		})
	}
	requireConstraint(t, insert(600, `{"note:500":1}`), "billing_cash_pickup_form", "the amount is the count's value")
	requireConstraint(t, insert(2000, `{"note:2000":1}`), "billing_cash_pickup_drawer", "never more than the drawer")
	picked := v7()
	require.NoError(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_cash_pickups (tenant_id,hospital_id,id,shift_id,counter_id,kind,amount_minor,currency,counts,recorded_by)
 VALUES (?,?,?,?,?,'pickup',600,'INR','{"note:500":1,"note:100":1}','user-admin')`, testutil.TenantA, w.hospital, picked, shiftID, counter).Error
	}))
	correct := func(id, of string, amount int64) error {
		return w.as("user-admin", func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_cash_pickups (tenant_id,hospital_id,id,shift_id,counter_id,kind,amount_minor,currency,corrects_id,reason,recorded_by)
 VALUES (?,?,?,?,?,'correction',?,'INR',?,'Miscounted','user-admin')`, testutil.TenantA, w.hospital, id, shiftID, counter, amount, of).Error
		})
	}
	fix := v7()
	require.NoError(t, correct(fix, picked, 100))
	requireConstraint(t, correct(v7(), fix, 1), "billing_cash_pickup_over", "only a pickup is corrected")
	require.Error(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_pickups SET amount_minor=100 WHERE shift_id=?`, shiftID).Error
	}), "a pickup is never edited")
	require.Error(t, w.as("user-admin", func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM billing_cash_pickups WHERE shift_id=?`, shiftID).Error
	}), "nor deleted")
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',expected_minor=500,declared_minor=500,closed_at=now() WHERE id=?`, shiftID).Error
	}), "billing_cash_shift_count", "a close carries its count")
	requireConstraint(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='pending_authorisation',expected_minor=500,declared_minor=600,declared_counts='{"note:500":1}',closed_at=now() WHERE id=?`, shiftID).Error
	}), "billing_cash_shift_count", "and its declared figure is the count's value")
	// 1000 float - 600 + 100: counted 1000, over by 500.
	require.NoError(t, w.as("user-clerk", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_cash_shifts SET status='pending_authorisation',expected_minor=500,declared_minor=1000,declared_counts='{"note:1000":1}',closed_at=now() WHERE id=?`, shiftID).Error
	}), "the expected cash nets the pickup less its correction")
	authorise := func(counts string) error {
		return w.as("user-admin", func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE billing_cash_shifts SET status='closed',declared_counts=?::jsonb,authorised_by='user-admin',authorisation_reason='Found',authorised_at=now() WHERE id=?`, counts, shiftID).Error
		})
	}
	requireConstraint(t, authorise(`{"note:500":2}`), "billing_cash_shift_own_variance", "authorising keeps the cashier's count")
	require.NoError(t, authorise(`{"note:1000":1}`))
}

// A pickup and a void at the counter each read the drawer the other left:
// only one of them fits.
func TestPickupsAndVoidsRaceForTheDrawer(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-7", 0)
	for range 5 {
		receipt := v7()
		status, body := w.put("clerk", "/admissions/"+admission+"/receipts/"+receipt, `{"amount_minor":1000,"currency":"INR"}`)
		require.Equal(t, http.StatusCreated, status, body)
		var codes [2]int
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			codes[0], _ = w.pickup("admin", shiftID, v7(), `{"note:1000":1}`)
		}()
		go func() {
			defer wg.Done()
			codes[1], _ = w.put("clerk", "/receipts/"+receipt+"/voids/"+v7(), `{"reason":"Race"}`)
		}()
		wg.Wait()
		picked, voided := codes[0] == http.StatusCreated, codes[1] == http.StatusOK
		require.True(t, picked != voided, "exactly one fits: %v", codes)
		require.Contains(t, []int{codes[0], codes[1]}, http.StatusConflict, "%v", codes)
	}
}
