package payments_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/payments" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/helivanta/pkg/transit"
)

var do = testutil.Do

// fakeRazorpay keeps QR codes and the payments made into them.
type fakeRazorpay struct {
	mu       sync.Mutex
	n        int
	opened   int
	fetches  int
	down     bool
	delay    time.Duration
	qrs      map[string]*fakeQR
	byIntent map[string]string
}

type fakeQR struct {
	amount   int64
	closed   bool
	payments []map[string]any
}

func (f *fakeRazorpay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	delay := f.delay
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, secret, _ := r.BasicAuth(); secret != "good-secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.down {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 3:
		_, _ = w.Write([]byte(`{"items":[]}`))
	case r.Method == http.MethodPost && len(parts) == 3:
		var in struct {
			Amount int64             `json:"payment_amount"`
			Notes  map[string]string `json:"notes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.n++
		f.opened++
		id := fmt.Sprintf("qr_T%d", f.n)
		f.qrs[id] = &fakeQR{amount: in.Amount}
		f.byIntent[in.Notes["intent_id"]] = id
		_, _ = fmt.Fprintf(w, `{"id":%q,"image_url":"https://rzp.io/i/%s","status":"active"}`, id, id)
	case len(parts) == 5 && parts[4] == "payments":
		f.fetches++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": f.qrs[parts[3]].payments})
	case len(parts) == 5 && parts[4] == "close":
		q := f.qrs[parts[3]]
		if q.closed {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q.closed = true
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeRazorpay) pay(qr string, amount int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id := fmt.Sprintf("pay_T%d", f.n)
	f.qrs[qr].payments = append(f.qrs[qr].payments, map[string]any{"id": id, "status": "captured", "amount": amount, "currency": "INR",
		"created_at": time.Now().Unix(), "vpa": "payer@bank", "acquirer_data": map[string]any{"rrn": "RRN" + id}})
	return id
}

func (f *fakeRazorpay) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opened, f.fetches
}

type world struct {
	r     *gin.Engine
	db    *tenantdb.DB
	ctx   context.Context
	fake  *fakeRazorpay
	deps  platform.Deps
	hook  *gin.Engine
	vault *transit.Client
}

func vaultClient(t *testing.T) *transit.Client {
	t.Helper()
	addr, _ := testinfra.StartOpenBao(t)
	user, password := testinfra.OpenBaoUser(t, time.Hour, "helivanta-api-transit")
	c, err := transit.New(addr, "transit", transit.UserpassLogin(user, password), nil)
	require.NoError(t, err)
	return c
}

func newWorld(t *testing.T, vault *transit.Client) *world {
	fake := &fakeRazorpay{qrs: map[string]*fakeQR{}, byIntent: map[string]string{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admA": testutil.TenantA, "admB": testutil.TenantB, "staffA": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admA": {payments.PermAccountManage}, "admB": {payments.PermAccountManage}, "staffA": {},
		},
		Modules:             []platform.Module{payments.New()},
		Transit:             vault,
		PaymentsProviderURL: srv.URL,
	})
	deps := platform.Deps{DB: db, Transit: vault, PaymentsProviderURL: srv.URL}
	hook := gin.New()
	hook.POST(payments.WebhookPath, payments.Webhook(deps, ratelimit.NewMemory(1000)))
	return &world{r: r, db: db, ctx: ctx, fake: fake, deps: deps, hook: hook, vault: vault}
}

func (w *world) configure(t *testing.T) (accountPath string) {
	res := do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_abc","key_secret":"good-secret","webhook_secret":"hook-secret"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var v struct {
		WebhookPath string `json:"webhook_path"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &v))
	return v.WebhookPath
}

// request publishes a pharmacy payment request the way pharmacy does.
func (w *world) request(t *testing.T, tenant string, amount int64) string {
	id := uuid.Must(uuid.NewV7()).String()
	w.publish(t, tenant, id, amount)
	return id
}

func (w *world) publish(t *testing.T, tenant, id string, amount int64) {
	data, _ := json.Marshal(pharmacycontract.PaymentRequestedData{RequestID: id, Purpose: pharmacycontract.PaymentPurposeCounterSale,
		AmountMinor: amount, Currency: "INR", ExpiresInSeconds: 300})
	require.NoError(t, w.db.WithTenant(w.ctx, tenant, func(tx *gorm.DB) error {
		return events.Enqueue(tx, pharmacycontract.SubjectPaymentRequested, events.Event{Type: "PaymentRequested", Version: 1, TenantID: tenant, Data: data})
	}))
}

// outcomes are the payments outcome events published for a request.
func (w *world) outcomes(t *testing.T, request string) []string {
	var subjects []string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT subject FROM outbox_events WHERE subject LIKE 'helivanta.in.payments.%' AND payload->'data'->>'request_id'=? ORDER BY created_at, id`, request).Scan(&subjects).Error
	}))
	for i, s := range subjects {
		subjects[i] = strings.TrimSuffix(strings.TrimPrefix(s, "helivanta.in.payments."), ".v1")
	}
	return subjects
}

func (w *world) waitFor(t *testing.T, request string, want ...string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Join(w.outcomes(t, request), ",") == strings.Join(want, ",") },
		20*time.Second, 100*time.Millisecond, "request %s: got %v, want %v", request, w.outcomes(t, request), want)
}

func (w *world) paidData(t *testing.T, request string) map[string]any {
	var raw string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject='helivanta.in.payments.intent_paid.v1' AND payload->'data'->>'request_id'=?`, request).Scan(&raw).Error
	}))
	var d map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &d))
	return d
}

func (w *world) qrFor(t *testing.T, request string) string {
	var intent string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id FROM payment_intents WHERE request_id=?`, request).Scan(&intent).Error
	}))
	w.fake.mu.Lock()
	defer w.fake.mu.Unlock()
	return w.fake.byIntent[intent]
}

// due makes a request's intent due now, optionally already expired.
func (w *world) due(t *testing.T, request string, expired bool) {
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "test"), testutil.TenantA, func(tx *gorm.DB) error {
		q := `UPDATE payment_intents SET next_check_at=now()-interval '1 second' WHERE request_id=?`
		if expired {
			q = `UPDATE payment_intents SET next_check_at=now()-interval '1 second', expires_at=now()-interval '1 second' WHERE request_id=?`
		}
		return tx.Exec(q, request).Error
	}))
	require.NoError(t, payments.ReconcileOnce(w.ctx, w.deps))
}

func TestAccountSetup(t *testing.T) {
	bare := newWorld(t, nil)
	res := do(bare.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_abc","key_secret":"good-secret","webhook_secret":"hook-secret"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code, "no vault, no stored secrets")

	w := newWorld(t, vaultClient(t))
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", "/v1/payments/razorpay-account", "staffA", "").Code)
	res = do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_abc","key_secret":"wrong-secret","webhook_secret":"hook-secret"}`)
	require.Equal(t, http.StatusUnprocessableEntity, res.Code)
	require.Contains(t, res.Body.String(), "credentials_rejected")
	require.JSONEq(t, `{"configured":false,"key_secret_set":false,"webhook_secret_set":false}`, do(w.r, "GET", "/v1/payments/razorpay-account", "admA", "").Body.String())
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"live","key_id":"rzp_test_abc","key_secret":"good-secret","webhook_secret":"hook-secret"}`).Code,
		"a test key is not a live account")
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_abc","key_secret":"good-secret"}`).Code,
		"the first save needs the webhook secret")

	w.configure(t)
	got := do(w.r, "GET", "/v1/payments/razorpay-account", "admA", "").Body.String()
	require.Contains(t, got, `"key_secret_set":true`)
	require.Contains(t, got, `"webhook_secret_set":true`)
	for _, secret := range []string{"good-secret", "hook-secret", "vault:v"} {
		require.NotContains(t, got, secret)
	}
	var sealed string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT key_secret_sealed FROM payment_accounts`).Scan(&sealed).Error
	}))
	require.True(t, strings.HasPrefix(sealed, "vault:v"), "stored sealed, not in the clear")
	require.Contains(t, do(w.r, "GET", "/v1/payments/razorpay-account", "admB", "").Body.String(), `"configured":false`, "another tenant sees nothing")

	// Changing only the webhook secret keeps the key secret, re-checked with Razorpay.
	res = do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_abc","webhook_secret":"hook-secret-2"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_other"}`).Code,
		"a new key id needs its own secret")
}

func TestIntentLifecycle(t *testing.T) {
	w := newWorld(t, vaultClient(t))

	// No account: refused, never served from some other account.
	none := w.request(t, testutil.TenantA, 100)
	w.waitFor(t, none, "intent_failed")
	require.Contains(t, fmt.Sprint(w.outcomesData(t, none)), "provider_not_configured")

	w.configure(t)

	// A request redelivered under a new event id still makes one QR.
	req := w.request(t, testutil.TenantA, 68000)
	w.waitFor(t, req, "intent_opened")
	w.publish(t, testutil.TenantA, req, 68000)
	time.Sleep(time.Second)
	w.waitFor(t, req, "intent_opened")
	opened, _ := w.fake.counts()
	require.Equal(t, 1, opened)

	// Paid: found by the poller from the provider API.
	pay := w.fake.pay(w.qrFor(t, req), 68000)
	w.due(t, req, false)
	w.waitFor(t, req, "intent_opened", "intent_paid")
	d := w.paidData(t, req)
	require.Equal(t, pay, d["provider_payment_id"])
	require.Equal(t, "RRN"+pay, d["rrn"])
	require.Equal(t, false, d["late"])
	require.NotContains(t, fmt.Sprint(d), "payer@bank", "no payer VPA leaves the provider answer")
	// A second captured payment on a paid intent is excess, not a second paid.
	w.fake.pay(w.qrFor(t, req), 68000)
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "test"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE payment_intents SET next_check_at=now()-interval '1 second' WHERE request_id=?`, req).Error
	}))
	require.NoError(t, payments.ReconcileOnce(w.ctx, w.deps))
	w.waitFor(t, req, "intent_opened", "intent_paid")

	// The wrong amount pays nothing.
	odd := w.request(t, testutil.TenantA, 5000)
	w.waitFor(t, odd, "intent_opened")
	w.fake.pay(w.qrFor(t, odd), 4999)
	w.due(t, odd, false)
	w.waitFor(t, odd, "intent_opened")
	var outcome string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT p.outcome FROM payment_provider_payments p JOIN payment_intents i ON i.id=p.intent_id WHERE i.request_id=?`, odd).Scan(&outcome).Error
	}))
	require.Equal(t, "mismatch", outcome)

	// Expired, then a payment that landed as it closed: paid, late.
	slow := w.request(t, testutil.TenantA, 7000)
	w.waitFor(t, slow, "intent_opened")
	w.due(t, slow, true)
	w.waitFor(t, slow, "intent_opened", "intent_expired")
	w.fake.pay(w.qrFor(t, slow), 7000)
	w.due(t, slow, false)
	w.waitFor(t, slow, "intent_opened", "intent_expired", "intent_paid")
	require.Equal(t, true, w.paidData(t, slow)["late"])

	// Cancel after the money landed: paid, not cancelled.
	late := w.request(t, testutil.TenantA, 9000)
	w.waitFor(t, late, "intent_opened")
	w.fake.pay(w.qrFor(t, late), 9000)
	cancel(t, w, late)
	w.waitFor(t, late, "intent_opened", "intent_paid")

	// Cancel with nothing paid: cancelled.
	gone := w.request(t, testutil.TenantA, 9100)
	w.waitFor(t, gone, "intent_opened")
	cancel(t, w, gone)
	w.waitFor(t, gone, "intent_opened", "intent_cancelled")

	// Provider down: failed at once, not left waiting.
	w.fake.mu.Lock()
	w.fake.down = true
	w.fake.mu.Unlock()
	down := w.request(t, testutil.TenantA, 100)
	w.waitFor(t, down, "intent_failed")
	require.Contains(t, fmt.Sprint(w.outcomesData(t, down)), "provider_unavailable")
}

func cancel(t *testing.T, w *world, request string) {
	data, _ := json.Marshal(pharmacycontract.PaymentCancelledData{RequestID: request, AmountMinor: 100, Currency: "INR"})
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, pharmacycontract.SubjectPaymentCancelled, events.Event{Type: "PaymentCancelled", Version: 1, TenantID: testutil.TenantA, Data: data})
	}))
}

func (w *world) outcomesData(t *testing.T, request string) []string {
	var rows []string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject LIKE 'helivanta.in.payments.%' AND payload->'data'->>'request_id'=?`, request).Scan(&rows).Error
	}))
	return rows
}

func signed(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func (w *world) deliver(path, eventID, secret, body string) int {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("X-Razorpay-Event-Id", eventID)
	req.Header.Set("X-Razorpay-Signature", signed(secret, body))
	rec := httptest.NewRecorder()
	w.hook.ServeHTTP(rec, req)
	return rec.Code
}

func TestWebhook(t *testing.T) {
	w := newWorld(t, vaultClient(t))
	path := w.configure(t)
	req := w.request(t, testutil.TenantA, 68000)
	w.waitFor(t, req, "intent_opened")
	qr := w.qrFor(t, req)
	body := fmt.Sprintf(`{"event":"qr_code.credited","payload":{"qr_code":{"entity":{"id":%q}}}}`, qr)

	require.Equal(t, http.StatusUnauthorized, w.deliver(path, "evt_1", "not-the-secret", body))
	require.Equal(t, http.StatusUnauthorized, w.deliver("/v1/payments/webhooks/razorpay/"+testutil.TenantA+"/"+uuid.NewString(), "evt_1", "hook-secret", body))
	other := strings.Replace(path, testutil.TenantA, testutil.TenantB, 1)
	require.Equal(t, http.StatusUnauthorized, w.deliver(other, "evt_1", "hook-secret", body), "another tenant's URL finds no account")
	require.Equal(t, http.StatusRequestEntityTooLarge, w.deliver(path, "evt_1", "hook-secret", `{"event":"x","pad":"`+strings.Repeat("a", 70000)+`"}`))

	// Nothing paid yet: the nudge checks and finds nothing; its replay is
	// acknowledged without a second provider read.
	require.Equal(t, http.StatusOK, w.deliver(path, "evt_0", "hook-secret", body))
	_, fetches := w.fake.counts()
	require.Equal(t, http.StatusOK, w.deliver(path, "evt_0", "hook-secret", body), "a replay is acknowledged")
	_, again := w.fake.counts()
	require.Equal(t, fetches, again, "a replayed delivery makes no second check")
	w.fake.pay(qr, 68000)
	require.Equal(t, http.StatusOK, w.deliver(path, "evt_1", "hook-secret", body))
	w.waitFor(t, req, "intent_opened", "intent_paid")
	// A verified event that names no QR of ours changes nothing.
	require.Equal(t, http.StatusOK, w.deliver(path, "evt_2", "hook-secret", `{"event":"qr_code.credited","payload":{"qr_code":{"entity":{"id":"qr_unknown"}}}}`))
}

func (w *world) intentRow(t *testing.T, request string) (status string, lateLeft int) {
	var row struct {
		Status         string
		LateChecksLeft int
	}
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT status, late_checks_left FROM payment_intents WHERE request_id=?`, request).Scan(&row).Error
	}))
	return row.Status, row.LateChecksLeft
}

// Reviewer findings I1-I3 and M1 (#1386 C-4a).
func TestIntentEdges(t *testing.T) {
	w := newWorld(t, vaultClient(t))
	path := w.configure(t)

	// A cancel that overtakes its request: cancelled at once, and no QR made.
	early := uuid.Must(uuid.NewV7()).String()
	cancel(t, w, early)
	w.waitFor(t, early, "intent_cancelled")
	w.publish(t, testutil.TenantA, early, 100)
	time.Sleep(time.Second)
	w.waitFor(t, early, "intent_cancelled")
	opened, _ := w.fake.counts()
	require.Zero(t, opened, "a cancelled request never opens a QR")

	// Keys cannot change while a QR is open on them.
	open := w.request(t, testutil.TenantA, 100)
	w.waitFor(t, open, "intent_opened")
	res := do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_new","key_secret":"good-secret"}`)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())

	// Razorpay's webhook for our own close does not spend a late check.
	w.due(t, open, true)
	w.waitFor(t, open, "intent_opened", "intent_expired")
	body := fmt.Sprintf(`{"event":"qr_code.closed","payload":{"qr_code":{"entity":{"id":%q}}}}`, w.qrFor(t, open))
	require.Equal(t, http.StatusOK, w.deliver(path, "evt_close", "hook-secret", body))
	status, left := w.intentRow(t, open)
	require.Equal(t, "expired", status)
	require.Equal(t, 2, left)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", "/v1/payments/razorpay-account", "admA", `{"mode":"test","key_id":"rzp_test_new","key_secret":"good-secret"}`).Code,
		"with nothing open the keys can change")

	// An intent whose QR was never made is failed at the deadline.
	stuck := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, w.db.WithTenant(tenantdb.WithActor(w.ctx, "test"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO payment_intents (id,tenant_id,requester,request_id,amount_minor,currency,status,account_id,key_id,expires_at,next_check_at,created_at)
			SELECT ?,?,'pharmacy',?,100,'INR','opening',id,key_id,now()+interval '5 minutes',now()-interval '1 second',now()-interval '1 minute' FROM payment_accounts`,
			uuid.Must(uuid.NewV7()), testutil.TenantA, stuck).Error
	}))
	require.NoError(t, payments.ReconcileOnce(w.ctx, w.deps))
	w.waitFor(t, stuck, "intent_failed")
}

// A slow provider must not take the shared connection pool with it.
func TestSlowProviderHoldsNoConnections(t *testing.T) {
	w := newWorld(t, vaultClient(t))
	path := w.configure(t)
	var bodies []string
	for i := 0; i < 8; i++ {
		req := w.request(t, testutil.TenantA, int64(100+i))
		w.waitFor(t, req, "intent_opened")
		bodies = append(bodies, fmt.Sprintf(`{"event":"qr_code.credited","payload":{"qr_code":{"entity":{"id":%q}}}}`, w.qrFor(t, req)))
	}
	w.fake.mu.Lock()
	w.fake.delay = 3 * time.Second
	w.fake.mu.Unlock()
	var wg sync.WaitGroup
	for i, body := range bodies {
		wg.Add(1)
		go func() { defer wg.Done(); w.deliver(path, fmt.Sprintf("evt_slow_%d", i), "hook-secret", body) }()
	}
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	require.Equal(t, http.StatusOK, do(w.r, "GET", "/v1/payments/razorpay-account", "admA", "").Code)
	require.Less(t, time.Since(start), time.Second, "other routes answer while eight checks wait on the provider")
	wg.Wait()
}
