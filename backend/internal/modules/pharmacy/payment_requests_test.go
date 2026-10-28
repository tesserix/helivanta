package pharmacy_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	paymentscontract "github.com/tesserix/helivanta/internal/modules/payments/contract"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

// #1386 C-4a: counter payment requests and the outcomes payments reports.

type counterPayment struct {
	Status            string  `json:"status"`
	AmountMinor       int64   `json:"amount_minor"`
	Currency          string  `json:"currency"`
	QRImageURL        *string `json:"qr_image_url"`
	FailureReason     *string `json:"failure_reason"`
	ProviderPaymentID *string `json:"provider_payment_id"`
	Late              bool    `json:"late"`
}

func TestCounterPayments(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"editor": testutil.TenantA, "reader": testutil.TenantA, "foreign": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"editor": salePerms, "reader": salePerms, "foreign": salePerms,
		}, Modules: []platform.Module{pharmacy.New()},
	})
	seedPharmacyOutlet(t, r)
	grantCatalogue(t, r, "user-editor", "editor")
	grantCatalogue(t, r, "user-reader", "reader")
	path := func(id string) string { return licencePath("/counter-payments/" + id) }
	get := func(id string) counterPayment {
		res := do(r, "GET", path(id), "editor", "")
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		var p counterPayment
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &p))
		return p
	}
	report := func(subject string, data any) {
		raw, _ := json.Marshal(data)
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: testutil.TenantA, Data: raw})
		}))
	}
	await := func(id, status string) {
		t.Helper()
		require.Eventually(t, func() bool { return get(id).Status == status }, 15*time.Second, 100*time.Millisecond, "want %s, have %s", status, get(id).Status)
	}
	requested := func(id string) int {
		var n int
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject='helivanta.in.pharmacy.payment_requested.v1' AND payload->'data'->>'request_id'=?`, id).Scan(&n).Error
		}))
		return n
	}
	ref := func(id string) paymentscontract.IntentRef {
		return paymentscontract.IntentRef{Requester: paymentscontract.RequesterPharmacy, RequestID: id, IntentID: uuid.NewString()}
	}

	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusNotFound, do(r, "PUT", path(id), "reader", `{"amount_minor":68000}`).Code, "a reader cannot take payment")
	require.Equal(t, http.StatusNotFound, do(r, "PUT", path(id), "foreign", `{"amount_minor":68000}`).Code)
	require.Equal(t, http.StatusCreated, do(r, "PUT", path(id), "editor", `{"amount_minor":68000}`).Code)
	require.Equal(t, http.StatusOK, do(r, "PUT", path(id), "editor", `{"amount_minor":68000}`).Code, "a retried click finds its request")
	require.Equal(t, http.StatusConflict, do(r, "PUT", path(id), "editor", `{"amount_minor":1}`).Code)
	require.Equal(t, 1, requested(id), "one request event, however often it is clicked")
	require.Equal(t, counterPayment{Status: "requested", AmountMinor: 68000, Currency: "INR"}, get(id))
	require.Equal(t, http.StatusNotFound, do(r, "GET", path(id), "reader", "").Code)

	qr := "https://rzp.io/i/qr1"
	report(paymentscontract.SubjectIntentOpened, paymentscontract.IntentOpenedData{IntentRef: ref(id), QRImageURL: qr, ExpiresAt: time.Now().Add(5 * time.Minute)})
	await(id, "awaiting_payment")
	require.Equal(t, qr, *get(id).QRImageURL)
	paid := paymentscontract.IntentPaidData{IntentRef: ref(id), AmountMinor: 68000, Currency: "INR", ProviderPaymentID: "pay_1", RRN: "1", PaidAt: time.Now()}
	report(paymentscontract.SubjectIntentPaid, paid)
	await(id, "paid")
	// A late expiry, a redelivered paid or a stray failure never undo paid.
	report(paymentscontract.SubjectIntentExpired, paymentscontract.IntentExpiredData{IntentRef: ref(id)})
	report(paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: ref(id), Reason: paymentscontract.ReasonProviderUnavailable})
	paid.ProviderPaymentID = "pay_2"
	report(paymentscontract.SubjectIntentPaid, paid)
	time.Sleep(2 * time.Second)
	got := get(id)
	require.Equal(t, "paid", got.Status)
	require.Equal(t, "pay_1", *got.ProviderPaymentID)
	require.False(t, got.Late)
	require.Equal(t, http.StatusConflict, do(r, "POST", path(id)+"/cancel", "editor", "").Code)

	// Expired, then the money lands: paid, marked late.
	slow := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", path(slow), "editor", `{"amount_minor":500}`).Code)
	report(paymentscontract.SubjectIntentExpired, paymentscontract.IntentExpiredData{IntentRef: ref(slow)})
	await(slow, "expired")
	report(paymentscontract.SubjectIntentPaid, paymentscontract.IntentPaidData{IntentRef: ref(slow), AmountMinor: 500, Currency: "INR", ProviderPaymentID: "pay_3", PaidAt: time.Now()})
	await(slow, "paid")
	require.True(t, get(slow).Late)

	// A paid report for another amount is not this request's payment.
	odd := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", path(odd), "editor", `{"amount_minor":700}`).Code)
	report(paymentscontract.SubjectIntentPaid, paymentscontract.IntentPaidData{IntentRef: ref(odd), AmountMinor: 699, Currency: "INR", ProviderPaymentID: "pay_4", PaidAt: time.Now()})
	report(paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: ref(odd), Reason: paymentscontract.ReasonProviderNotConfigured})
	await(odd, "failed")
	require.Equal(t, "provider_not_configured", *get(odd).FailureReason)

	// Cancel waits in cancelling for payments' answer.
	gone := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", path(gone), "editor", `{"amount_minor":900}`).Code)
	require.Equal(t, http.StatusOK, do(r, "POST", path(gone)+"/cancel", "editor", "").Code)
	require.Equal(t, "cancelling", get(gone).Status)
	require.Equal(t, http.StatusConflict, do(r, "POST", path(gone)+"/cancel", "editor", "").Code)
	report(paymentscontract.SubjectIntentCancelled, paymentscontract.IntentCancelledData{IntentRef: ref(gone)})
	await(gone, "cancelled")

	// Another requester's outcome is not the pharmacy's.
	other := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", path(other), "editor", `{"amount_minor":100}`).Code)
	foreign := ref(other)
	foreign.Requester = "billing"
	report(paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: foreign, Reason: paymentscontract.ReasonProviderRejected})
	report(paymentscontract.SubjectIntentFailed, paymentscontract.IntentFailedData{IntentRef: ref(odd), Reason: paymentscontract.ReasonProviderRejected})
	time.Sleep(2 * time.Second)
	require.Equal(t, "requested", get(other).Status)
}
