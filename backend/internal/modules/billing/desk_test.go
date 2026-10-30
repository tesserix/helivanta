package billing_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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

// A position is what the records say, whoever writes it; and an account from
// before positions were kept is filled in by the billing worker.
func TestPositionsAreComputedAndFilledInForOlderAccounts(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 100000)
	asConsumer := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "system:billing-bed-census"), testutil.TenantA, fn)
	}
	var balance int64
	require.NoError(t, asConsumer(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE billing_account_positions SET balance_minor=0,refundable_minor=999 WHERE admission_id=?`, admission).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT balance_minor FROM billing_account_positions WHERE admission_id=?`, admission).Scan(&balance).Error
	}))
	require.EqualValues(t, 100000, balance, "a supplied figure is recomputed")
	require.Equal(t, []string{admission}, w.desk("to_collect"))

	// The account loses its position, as an account from before 0008 has none.
	// The owner, under the tenant's row security as billing's census, removes it.
	require.NoError(t, w.db.WithAdmin(w.ctx, func(tx *gorm.DB) error {
		for _, q := range []string{
			`ALTER TABLE billing_account_positions DISABLE TRIGGER billing_account_positions_kept`,
			`SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true)`,
			`SELECT set_config('app.actor_subject','system:billing-bed-census',true)`,
		} {
			if err := tx.Exec(q).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`DELETE FROM billing_account_positions WHERE admission_id=?`, admission).Error; err != nil {
			return err
		}
		return tx.Exec(`ALTER TABLE billing_account_positions ENABLE TRIGGER billing_account_positions_kept`).Error
	}))
	require.Empty(t, w.desk("to_collect"))
	w.census()
	require.Equal(t, []string{admission}, w.desk("to_collect"), "the worker fills it in")

	// Each view reads its own partial index, so a page never computes the
	// hospital's whole history.
	var plan []string
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-nurse"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.billing_subject','user-nurse',true)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SET LOCAL enable_seqscan=off`).Error; err != nil {
			return err
		}
		return tx.Raw(`EXPLAIN SELECT admission_id FROM billing_account_positions WHERE tenant_id=? AND hospital_id=? AND refundable_minor > 0 ORDER BY account_created_at DESC,admission_id DESC LIMIT 26`,
			testutil.TenantA, w.hospital).Scan(&plan).Error
	}))
	require.Contains(t, strings.Join(plan, "\n"), "billing_positions_refund")
}

// Two writers changing one account at once: the position counts both.
func TestConcurrentWritesLeaveTheLatestPosition(t *testing.T) {
	w := newBedWorld(t)
	w.priced(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, w.start(admission, patient, stay{uuid.NewString(), uuid.NewString(), "general"}, time.Now().Add(-time.Hour)))
	w.counterWithShift(t, "clerk", "IPD-K9", 0)
	w.charge(admission, patient, 1000000)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); w.charge(admission, patient, 1000) }()
		go func() { defer wg.Done(); w.pay("clerk", admission, 1000) }()
	}
	wg.Wait()
	totals := w.inr(admission)
	res := do(w.r, "GET", w.root+"/accounts?view=to_collect", "nurse", "")
	var page struct{ Data []deskRow }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, totals.BalanceMinor, page.Data[0].BalanceMinor, "the stored position matches the account")
}

// A charge from a consumer after a payment: the consumer sees the payment,
// so the position it leaves still counts it.
func TestAConsumerWriteKeepsThePaymentInThePosition(t *testing.T) {
	w := newBedWorld(t)
	w.counterWithShift(t, "clerk", "IPD-K8", 0)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 500000)
	status, body := w.pay("clerk", admission, 200000)
	require.Equal(t, http.StatusCreated, status, body)
	w.charge(admission, patient, 100000)
	res := do(w.r, "GET", w.root+"/accounts?view=to_collect", "nurse", "")
	var page struct{ Data []deskRow }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.EqualValues(t, 400000, page.Data[0].BalanceMinor)
}
