package billing_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (w *bedWorld) orderDischarge(admission, patient, order string, at time.Time) error {
	return w.deliver("billing-discharge-ordered", medicorecontract.DischargeOrderedData{OrderID: order, AdmissionID: admission, PatientID: patient, HospitalID: w.hospital,
		Disposition: "home", OrderedAt: at.UTC().Format(time.RFC3339Nano)})
}

func (w *bedWorld) withdrawDischarge(admission, order string, at time.Time) error {
	return w.deliver("billing-discharge-withdrawn", medicorecontract.DischargeWithdrawnData{OrderID: order, AdmissionID: admission, HospitalID: w.hospital,
		WithdrawnAt: at.UTC().Format(time.RFC3339Nano)})
}

func (w *bedWorld) post(path string) (int, string) {
	res := do(w.r, "POST", w.root+path, "clerk", "")
	return res.Code, res.Body.String()
}

func (w *bedWorld) priced(t *testing.T) {
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2020-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code)
	w.census()
}

func TestAFinalBillWaitsForTheDischargeOrderAndEveryBedDayPriced(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	adm := "/admissions/" + admission
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-50*time.Hour)))
	w.charge(admission, patient, 2500)

	code, body := w.post(adm + "/final-bills")
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "discharge_not_ordered")
	interim := w.issue(admission)
	require.Equal(t, 1, interim.Number)

	order := uuid.NewString()
	require.NoError(t, w.orderDischarge(admission, patient, order, time.Now()))
	require.Error(t, w.orderDischarge(admission, patient, order, time.Now().Add(time.Hour)), "a replay that differs is refused")
	res := do(w.r, "GET", w.root+adm+"/account", "nurse", "")
	require.Contains(t, res.Body.String(), `"discharge_ordered":true`)
	code, body = w.post(adm + "/interim-bills")
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "discharge_ordered", "once discharge is ordered the next bill is final")

	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusConflict, code, "bed charging is not set up")
	require.Contains(t, body, "bed_days_not_priced")
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	w.census()
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusConflict, code, "days without a rate")
	require.Contains(t, body, "bed_days_not_priced")
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2020-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code)
	w.census()

	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)
	var final billBody
	require.NoError(t, json.Unmarshal([]byte(body), &final))
	require.Equal(t, 2, final.Number, "one series with the interim bills")
	require.Contains(t, body, `"kind":"final"`)
	require.Zero(t, final.UnpricedBedDays)
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "nothing_new")

	// A late charge reissues the final bill; the first stays as issued.
	w.charge(admission, patient, 400)
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)
	require.Contains(t, body, `"number":3`)
	res = do(w.r, "GET", w.root+adm+"/bills", "nurse", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var list struct {
		Data []struct {
			Number int    `json:"number"`
			Kind   string `json:"kind"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.Len(t, list.Data, 3)
	require.Equal(t, "final", list.Data[0].Kind)
	require.Equal(t, "interim", list.Data[2].Kind)
	require.Equal(t, http.StatusOK, do(w.r, "GET", w.root+adm+"/bills/2", "nurse", "").Code)
	require.Equal(t, http.StatusOK, do(w.r, "GET", w.root+adm+"/bills/2/lines", "nurse", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+adm+"/interim-bills/2", "nurse", "").Code, "the interim routes answer interim bills only")
	res = do(w.r, "GET", w.root+adm+"/interim-bills", "nurse", "")
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.root+adm+"/bills", "outsider", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "POST", w.root+adm+"/final-bills", "outsider", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "POST", w.root+adm+"/final-bills", "nurse", "").Code)
}

func TestAWithdrawnDischargeReturnsToInterimBillsInEitherEventOrder(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	adm := "/admissions/" + admission
	bedStay := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, bedStay, time.Now().Add(-time.Hour)))
	w.charge(admission, patient, 2500)

	// An interim bill that already holds everything is still followed by a
	// final bill: being final is what is new.
	first := w.issue(admission)
	require.Equal(t, 1, first.Number)
	early := uuid.NewString()
	require.NoError(t, w.orderDischarge(admission, patient, early, time.Now()))
	code, body := w.post(adm + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)
	require.NoError(t, w.withdrawDischarge(admission, early, time.Now()))

	// The withdrawal overtakes its order: the order never becomes active.
	order := uuid.NewString()
	now := time.Now()
	require.NoError(t, w.withdrawDischarge(admission, order, now.Add(time.Minute)))
	require.NoError(t, w.orderDischarge(admission, patient, order, now))
	require.NoError(t, w.orderDischarge(admission, patient, order, now), "a replay is idempotent")
	require.Error(t, w.withdrawDischarge(admission, order, now.Add(2*time.Minute)), "a replay that differs is refused")
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "discharge_not_ordered")

	second := uuid.NewString()
	require.NoError(t, w.orderDischarge(admission, patient, second, now))
	w.charge(admission, patient, 200)
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)
	require.NoError(t, w.withdrawDischarge(admission, second, now.Add(time.Minute)))
	code, body = w.post(adm + "/interim-bills")
	require.Equal(t, http.StatusConflict, code, "an interim bill after a final one still needs something new")
	require.Contains(t, body, "nothing_new")
	w.charge(admission, patient, 300)
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusConflict, code, "the patient stays: no final bill")
	require.Contains(t, body, "discharge_not_ordered")
	code, body = w.post(adm + "/interim-bills")
	require.Equal(t, http.StatusCreated, code, body)

	// Released without an order: the final bill is issued after departure.
	require.NoError(t, w.end(admission, bedStay, time.Now(), "release"))
	require.NoError(t, w.release(admission, patient, time.Now()))
	code, body = w.post(adm + "/interim-bills")
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "account_closed")
	code, body = w.post(adm + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)
}

func TestBillTablesHoldTheFinalBillRules(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.charge(admission, patient, 2500)
	asClerk := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-clerk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.billing_subject','user-clerk',true)`).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	insert := func(kind string) error {
		return asClerk(func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,kind,id,issued_by,unpriced_bed_days) VALUES (?,?,?,1,?,?,'user-clerk',0)`,
				testutil.TenantA, w.hospital, admission, kind, uuid.Must(uuid.NewV7())).Error
		})
	}
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	require.Error(t, insert("interim"), "no interim bill once discharge is ordered")
	require.Error(t, insert("final"), "no final bill without bed charging set up")
	w.priced(t)
	require.NoError(t, insert("final"))

	// A stay whose start is still on its way: the database alone refuses a final bill.
	require.NoError(t, w.end(admission, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now(), "transfer"))
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,kind,id,issued_by,unpriced_bed_days) VALUES (?,?,?,2,'final',?,'user-clerk',0)`,
			testutil.TenantA, w.hospital, admission, uuid.Must(uuid.NewV7())).Error
	}), "no final bill while bed days are still being counted")

	// Priced, but no discharge ordered: the database alone refuses a final bill.
	other := uuid.NewString()
	require.NoError(t, w.start(other, uuid.NewString(), stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_interim_bills (tenant_id,hospital_id,admission_id,number,kind,id,issued_by,unpriced_bed_days) VALUES (?,?,?,1,'final',?,'user-clerk',0)`,
			testutil.TenantA, w.hospital, other, uuid.Must(uuid.NewV7())).Error
	}), "no final bill before discharge is ordered")

	require.Error(t, asClerk(func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO billing_discharge_orders (tenant_id,order_id,hospital_id,admission_id,disposition,ordered_at) VALUES (?,?,?,?,'home',now())`,
			testutil.TenantA, uuid.NewString(), w.hospital, admission).Error
	}), "only billing's consumers record discharge orders")
	require.Error(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-discharge-ordered"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_discharge_orders SET ordered_at=now() WHERE admission_id=?`, admission).Error
	}), "an order is recorded once")
}

// A final bill never goes out while bed days are still being counted: setup
// the census has not applied, a stay whose start or end is still on its way,
// or a midnight inside the census grace.
func TestAFinalBillWaitsUntilEveryBedDayIsCounted(t *testing.T) {
	w := newBedWorld(t)
	pending := func(admission, why string) {
		t.Helper()
		code, body := w.post("/admissions/" + admission + "/final-bills")
		require.Equal(t, http.StatusConflict, code, why+": "+body)
		require.Contains(t, body, "bed_days_pending", why)
	}
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-50*time.Hour)))
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))

	// The timezone is saved but the census has not applied it: no day is recorded yet.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	pending(admission, "timezone not applied")
	w.census()
	code, body := w.post("/admissions/" + admission + "/final-bills")
	require.Contains(t, body, "bed_days_not_priced", "%d", code)
	// A rate saved but not applied: the days it would price are not priced yet.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-rates/general/2020-01-01", "admin", `{"daily_rate_minor":150000,"currency":"INR"}`).Code)
	pending(admission, "rate not applied")
	w.census()
	code, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)

	// No stay known yet: the account came from a pharmacy charge and the order.
	early := uuid.NewString()
	w.charge(early, patient, 2500)
	require.NoError(t, w.orderDischarge(early, patient, uuid.NewString(), time.Now()))
	pending(early, "no stay recorded")

	// An end whose start has not arrived.
	require.NoError(t, w.end(admission, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now(), "transfer"))
	pending(admission, "a stay without its start")
}

func TestAFinalBillAfterReleaseWaitsForTheLastStayToEnd(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	s := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, s, time.Now().Add(-time.Hour)))
	require.NoError(t, w.release(admission, patient, time.Now()))
	code, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusConflict, code, body)
	require.Contains(t, body, "bed_days_pending", "the stay's end is still on its way: no minimum day yet")
	require.NoError(t, w.end(admission, s, time.Now(), "release"))
	code, body = w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusCreated, code, body)
	require.Contains(t, body, `"source_module":"bed"`, "the minimum day is on it")
}

func TestAFinalBillInsideTheMidnightGraceWaits(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-50*time.Hour)))
	require.NoError(t, w.orderDischarge(admission, patient, uuid.NewString(), time.Now()))
	// As if a midnight passed a minute ago and the census has not counted it.
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE billing_bed_stays SET next_census_at=now()+interval '14 minutes' WHERE admission_id=?`, admission).Error
	}))
	code, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusConflict, code, body)
	require.Contains(t, body, "bed_days_pending")
}

func TestAFinalBillAfterReleaseWaitsForTheTimezoneToBeApplied(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	s := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(t, w.start(admission, patient, s, time.Now().Add(-time.Hour)))
	require.NoError(t, w.end(admission, s, time.Now(), "release"))
	require.NoError(t, w.release(admission, patient, time.Now()))
	// Every stay has ended, so only the unapplied timezone says the minimum day is still to come.
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.root+"/bed-charging", "admin", `{"timezone":"Asia/Kolkata"}`).Code)
	code, body := w.post("/admissions/" + admission + "/final-bills")
	require.Equal(t, http.StatusConflict, code, body)
	require.Contains(t, body, "bed_days_pending")
}
