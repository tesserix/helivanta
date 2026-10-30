package medicore_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #96 slice 1: the doctor orders discharge; the ward releases the bed.

func (w bedWorld) admitted(t *testing.T) (admission, bed string) {
	t.Helper()
	code := "D" + strings.ToUpper(newID()[30:])
	_, beds := w.room(t, code, code+"-1")
	admission = newID()
	status, body := w.admit("desk", admission, registered(t, w.db, w.ctx, testutil.TenantA, hospitalA), beds[0], 1)
	require.Equal(t, http.StatusCreated, status, body)
	return admission, beds[0]
}

func (w bedWorld) order(token, admission, order, body string) (int, string) {
	res := do(w.r, "PUT", w.url("/admissions/"+admission+"/discharge-orders/"+order), token, body)
	return res.Code, res.Body.String()
}

func (w bedWorld) withdraw(token, admission, order, withdrawal, reason string) (int, string) {
	res := do(w.r, "PUT", w.url("/admissions/"+admission+"/discharge-orders/"+order+"/withdrawals/"+withdrawal), token, fmt.Sprintf(`{"reason":%q}`, reason))
	return res.Code, res.Body.String()
}

type boardDischarge struct {
	ID                   string  `json:"id"`
	DischargeOrderID     *string `json:"discharge_order_id"`
	DischargeDisposition *string `json:"discharge_disposition"`
}

func (w bedWorld) boardDischarge(t *testing.T, token, bed string) *boardDischarge {
	t.Helper()
	res := do(w.r, "GET", w.url("/beds?limit=200"), token, "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page struct{ Data []boardDischarge }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	for _, b := range page.Data {
		if b.ID == bed {
			return &b
		}
	}
	return nil
}

func TestADoctorOrdersAndWithdrawsDischarge(t *testing.T) {
	w := newBedWorld(t)
	admission, bed := w.admitted(t)
	order := newID()
	home := `{"disposition":"home","note":"  Review in OPD in a week  "}`

	code, _ := w.order("nurse", admission, order, home)
	require.Equal(t, http.StatusForbidden, code, "the ward releases; a doctor orders")
	code, _ = w.order("doctorOut", admission, order, home)
	require.Equal(t, http.StatusNotFound, code, "no grant at the hospital")
	for _, bad := range []string{`{"disposition":"died"}`, `{"disposition":"home","note":"   "}`, `{}`} {
		code, _ = w.order("doctor", admission, order, bad)
		require.Equal(t, http.StatusBadRequest, code, bad)
	}

	code, body := w.order("doctor", admission, order, home)
	require.Equal(t, http.StatusCreated, code, body)
	require.Contains(t, body, `"note":"Review in OPD in a week"`)
	require.Contains(t, body, `"ordered_by":"user-doctor"`)
	code, _ = w.order("doctor", admission, order, home)
	require.Equal(t, http.StatusOK, code, "a retry replays")
	code, body = w.order("doctor", admission, order, `{"disposition":"transfer_out","note":"Review in OPD in a week"}`)
	require.Equal(t, http.StatusConflict, code, "the same id with another disposition")
	require.NotContains(t, body, "already_ordered")
	code, body = w.order("doctor2", admission, newID(), `{"disposition":"transfer_out"}`)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "already_ordered")

	b := w.boardDischarge(t, "reader", bed)
	require.NotNil(t, b.DischargeOrderID, "the ward board shows the order")
	require.Equal(t, order, *b.DischargeOrderID)
	require.Equal(t, "home", *b.DischargeDisposition)
	res := do(w.r, "GET", w.url("/admissions/"+admission), "reader", "")
	require.Contains(t, res.Body.String(), `"disposition":"home"`)
	require.EqualValues(t, 1, outboxCount(t, w.db, w.ctx, "helivanta.in.medicore.discharge_ordered.v1"))

	withdrawal := newID()
	code, _ = w.withdraw("doctor", admission, order, withdrawal, " ")
	require.Equal(t, http.StatusBadRequest, code, "a withdrawal needs a reason")
	code, body = w.withdraw("doctor", admission, order, withdrawal, "Fever overnight")
	require.Equal(t, http.StatusOK, code, body)
	require.Contains(t, body, `"withdrawal_reason":"Fever overnight"`)
	code, _ = w.withdraw("doctor", admission, order, withdrawal, "Fever overnight")
	require.Equal(t, http.StatusOK, code, "a retried withdrawal replays")
	code, _ = w.withdraw("doctor", admission, order, newID(), "Fever overnight")
	require.Equal(t, http.StatusConflict, code, "withdrawn once")
	code, _ = w.withdraw("doctor", admission, newID(), newID(), "Fever overnight")
	require.Equal(t, http.StatusNotFound, code)
	require.Nil(t, w.boardDischarge(t, "reader", bed).DischargeOrderID, "a withdrawn order is off the board")
	require.EqualValues(t, 1, outboxCount(t, w.db, w.ctx, "helivanta.in.medicore.discharge_withdrawn.v1"))

	// A new order may follow; the release completes it and keeps it on the admission.
	second := newID()
	code, body = w.order("doctor2", admission, second, `{"disposition":"transfer_out"}`)
	require.Equal(t, http.StatusCreated, code, body)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.url("/admissions/"+admission+"/release"), "nurse", `{"reason":"transferred_out"}`).Code)
	res = do(w.r, "GET", w.url("/admissions/"+admission), "reader", "")
	require.Contains(t, res.Body.String(), `"id":"`+second+`"`)
	code, body = w.withdraw("doctor2", admission, second, newID(), "Too late")
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "not_admitted")
	code, body = w.order("doctor", admission, newID(), home)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, body, "not_admitted")
	code, _ = w.order("doctor2", admission, second, `{"disposition":"transfer_out"}`)
	require.Equal(t, http.StatusOK, code, "a replay after release is still answered from the record")
	code, _ = w.order("doctor", newID(), newID(), home)
	require.Equal(t, http.StatusNotFound, code)
}

func TestConcurrentDischargeOrdersLeaveOne(t *testing.T) {
	w := newBedWorld(t)
	admission, _ := w.admitted(t)
	codes := make([]int, 6)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = w.order("doctor", admission, newID(), `{"disposition":"home"}`)
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
}

func TestDischargeOrdersRefuseReleasedAdmissionsAndOtherWriters(t *testing.T) {
	w := newBedWorld(t)
	admission, _ := w.admitted(t)
	order := newID()
	code, body := w.order("doctor", admission, order, `{"disposition":"home"}`)
	require.Equal(t, http.StatusCreated, code, body)

	as := func(subject string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.medicore_subject',?,true)`, subject).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	require.Error(t, as("user-doctor", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_discharge_orders SET disposition='transfer_out' WHERE id=?`, order).Error
	}), "an order never changes")
	require.Error(t, as("user-doctor", func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM medicore_discharge_orders WHERE id=?`, order).Error
	}))
	var seen int64
	require.NoError(t, as("user-doctorOut", func(tx *gorm.DB) error {
		return tx.Table("medicore_discharge_orders").Where("id=?", order).Count(&seen).Error
	}))
	require.Zero(t, seen, "no grant, no order")

	// Withdrawn once: a withdrawn order cannot be withdrawn again or revived.
	code, body = w.withdraw("doctor", admission, order, newID(), "Not ready")
	require.Equal(t, http.StatusOK, code, body)
	require.Error(t, as("user-doctor", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_discharge_orders SET withdrawal_reason='Changed' WHERE id=?`, order).Error
	}), "a withdrawal is fixed")
	second := newID()
	code, body = w.order("doctor", admission, second, `{"disposition":"home"}`)
	require.Equal(t, http.StatusCreated, code, body)

	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.url("/admissions/"+admission+"/release"), "nurse", `{"reason":"discharged"}`).Code)
	require.Error(t, as("user-doctor", func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_discharge_orders SET withdrawal_id=?,withdrawal_reason='x',withdrawn_by='user-doctor',withdrawn_at=now() WHERE id=?`, newID(), second).Error
	}), "no withdrawal once the patient has left")
	// Another admission, released with no order: the guard alone refuses one now.
	left, _ := w.admitted(t)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.url("/admissions/"+left+"/release"), "nurse", `{"reason":"discharged"}`).Code)
	require.Error(t, as("user-doctor", func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO medicore_discharge_orders (tenant_id,hospital_id,id,admission_id,disposition,ordered_by) VALUES (?,?,?,?,'home','user-doctor')`,
			testutil.TenantA, hospitalA, newID(), left).Error
	}), "no order for a released admission")
}
