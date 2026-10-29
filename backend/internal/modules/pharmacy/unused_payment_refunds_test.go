package pharmacy_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const unusedRefundBody = `{"reference":"TEST-RZP-REFUND-1","reason":"TEST customer cancelled before sale"}`

func unusedRefundPath(id string) string { return licencePath("/counter-payments/" + id + "/refund") }

func TestUnusedPaymentRefundRetainsPaidMoneyAndReplaysExactly(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-UPI-REFUND", "none")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	id := w.paidCounterPayment(t, 2500)
	first := do(w.r, "PUT", unusedRefundPath(id), "pharmacist", unusedRefundBody)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"amount_minor":2500`)
	require.Contains(t, first.Body.String(), `"recorded_by":"user-pharmacist"`)
	repeat := do(w.r, "PUT", unusedRefundPath(id), "pharmacist", unusedRefundBody)
	require.Equal(t, http.StatusOK, repeat.Code, repeat.Body.String())
	require.JSONEq(t, first.Body.String(), repeat.Body.String())
	read := do(w.r, "GET", unusedRefundPath(id), "pharmacist", "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.JSONEq(t, first.Body.String(), read.Body.String())
	changed := `{"reference":"TEST-OTHER","reason":"TEST customer cancelled before sale"}`
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", unusedRefundPath(id), "pharmacist", changed).Code)
	require.Contains(t, do(w.r, "GET", licencePath("/counter-payments/"+id), "pharmacist", "").Body.String(), `"status":"paid"`)
	require.NotContains(t, unusedPayments(t, w), id)
	var published int64
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Table("outbox_events").Where("subject=?", "helivanta.in.pharmacy.unused_payment_refund_recorded.v1").Count(&published).Error
	}))
	require.EqualValues(t, 1, published, "a replay emits no second refund event")
	code, _, out := w.sell(t, verifiedSale(product, 2, 2500, id))
	require.Equal(t, http.StatusConflict, code, out)
	available, _ := w.available(t, product)
	require.EqualValues(t, 30, available, "refunding unused money neither sells nor returns stock")
	require.Error(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM pharmacy_unused_payment_refunds WHERE payment_request_id=?`, id).Error
	}))
}

func TestUnusedPaymentRefundNeedsAnUnusedScopedPaidRequest(t *testing.T) {
	w := newSaleWorld(t, true)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", licencePath("/counter-payments/"+id), "pharmacist", `{"amount_minor":2500}`).Code)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", unusedRefundPath(id), "pharmacist", unusedRefundBody).Code)
	paid := w.paidCounterPayment(t, 2500)
	for _, token := range []string{"clerk", "foreign"} {
		result := do(w.r, "PUT", unusedRefundPath(paid), token, unusedRefundBody)
		require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, result.Code, result.Body.String())
	}
	require.Equal(t, http.StatusNotFound, do(w.r, "PUT", unusedRefundPath(uuid.Must(uuid.NewV7()).String()), "pharmacist", unusedRefundBody).Code)
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_unused_payment_refunds (tenant_id,outlet_id,payment_request_id,amount_minor,currency,reference,reason,recorded_by)
		 SELECT tenant_id,outlet_id,id,amount_minor-1,currency,'TEST-WRONG','TEST forged amount','user-editor' FROM pharmacy_payment_requests WHERE id=?`, paid).Error
	}), "only a whole unused paid payment", "the database rejects a forged partial refund independently of the handler")
	for _, body := range []string{`{"reference":" TEST ","reason":"TEST reason"}`, `{"reference":"TEST","reason":"bad"}`, `{"reference":"TEST","reason":"TEST\nreason"}`} {
		require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", unusedRefundPath(paid), "pharmacist", body).Code)
	}
	product := w.product(t, "TEST-UPI-USED", "none")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	code, _, out := w.sell(t, verifiedSale(product, 2, 2500, paid))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", unusedRefundPath(paid), "pharmacist", unusedRefundBody).Code)
}

func TestTwoTillsCannotSellAndRefundTheSamePayment(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-UPI-RACE", "none")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	id := w.paidCounterPayment(t, 2500)
	start := make(chan struct{})
	results := make(chan int, 2)
	var group sync.WaitGroup
	for _, command := range []struct{ path, body string }{
		{unusedRefundPath(id), unusedRefundBody},
		{salePath("/" + uuid.Must(uuid.NewV7()).String()), verifiedSale(product, 2, 2500, id)},
	} {
		group.Go(func() {
			<-start
			result := do(w.r, "PUT", command.path, "pharmacist", command.body)
			results <- result.Code
		})
	}
	close(start)
	group.Wait()
	close(results)
	statuses := []int{}
	for status := range results {
		statuses = append(statuses, status)
	}
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, statuses, fmt.Sprint(statuses))
	require.Empty(t, unusedPayments(t, w))
}
