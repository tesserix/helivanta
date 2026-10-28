package pharmacy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
)

// #1386 C-4b: a sale paid by a UPI payment the system saw settle.

func (w saleWorld) paidCounterPayment(t *testing.T, amount int64) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", licencePath("/counter-payments/"+id), "pharmacist", fmt.Sprintf(`{"amount_minor":%d}`, amount))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	data, _ := json.Marshal(paymentscontract.IntentPaidData{IntentRef: paymentscontract.IntentRef{Requester: paymentscontract.RequesterPharmacy, RequestID: id, IntentID: uuid.NewString()},
		AmountMinor: amount, Currency: "INR", ProviderPaymentID: "pay_" + id[:8], RRN: "1", PaidAt: time.Now()})
	require.NoError(t, w.db.WithTenant(context.Background(), testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, paymentscontract.SubjectIntentPaid, events.Event{Type: "IntentPaid", Version: 1, TenantID: testutil.TenantA, Data: data})
	}))
	require.Eventually(t, func() bool {
		return json.Valid(do(w.r, "GET", licencePath("/counter-payments/"+id), "pharmacist", "").Body.Bytes()) &&
			statusOf(w, id) == "paid"
	}, 15*time.Second, 100*time.Millisecond)
	return id
}

func statusOf(w saleWorld, id string) string {
	var p struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(do(w.r, "GET", licencePath("/counter-payments/"+id), "pharmacist", "").Body.Bytes(), &p)
	return p.Status
}

func verifiedSale(product string, units, amount int, request string) string {
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":%d}],"tenders":[{"kind":"upi_verified","amount_minor":%d,"payment_request_id":%q}]}`, product, units, amount, request)
}

func unusedPayments(t *testing.T, w saleWorld) []string {
	t.Helper()
	res := do(w.r, "GET", licencePath("/counter-payments"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	ids := []string{}
	for _, d := range page.Data {
		ids = append(ids, d.ID)
	}
	return ids
}

func TestAVerifiedUPISaleNeedsItsOwnPaidPayment(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)

	// Not paid yet: nothing posts.
	waiting := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", licencePath("/counter-payments/"+waiting), "pharmacist", `{"amount_minor":2500}`).Code)
	code, _, out := w.sell(t, verifiedSale(p, 2, 2500, waiting))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "not a paid payment")

	// A request id that is not this outlet's payment request at all.
	code, _, out = w.sell(t, verifiedSale(p, 2, 2500, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, http.StatusConflict, code, out)

	// A shape the tender cannot have.
	code, _, _ = w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"upi_verified","amount_minor":2500,"reference":"X","payment_request_id":%q}]}`, p, waiting))
	require.Equal(t, http.StatusBadRequest, code)

	// Paid, but for another amount.
	short := w.paidCounterPayment(t, 2400)
	code, _, out = w.sell(t, verifiedSale(p, 2, 2500, short))
	require.Equal(t, http.StatusConflict, code, out)

	paid := w.paidCounterPayment(t, 2500)
	require.ElementsMatch(t, []string{short, paid}, unusedPayments(t, w), "paid money not on a sale stays findable")
	code, sale, out := w.sell(t, verifiedSale(p, 2, 2500, paid))
	require.Equal(t, http.StatusCreated, code, out)
	require.Contains(t, out, `"payment_request_id":"`+paid+`"`)
	require.Equal(t, []string{short}, unusedPayments(t, w))

	// One payment pays one sale.
	code, _, out = w.sell(t, verifiedSale(p, 2, 2500, paid))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "already on another sale")

	// The daily report keeps verified UPI apart from typed references.
	_, days, _ := dailyReport(t, w, "pharmacist", "?from=2026-11-01&to=2026-11-01")
	require.Len(t, days, 1)
	require.EqualValues(t, sale.TotalMinor, days[0].UPIVerifiedMinor)
	require.Zero(t, days[0].UPIMinor)
}
