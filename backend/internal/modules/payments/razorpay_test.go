package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRazorpayAdapter(t *testing.T) {
	var got struct {
		method, path, user, pass string
		body                     map[string]any
	}
	status := http.StatusOK
	answer := `{"id":"qr_A1","image_url":"https://rzp.io/i/x","status":"active"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.RequestURI()
		got.user, got.pass, _ = r.BasicAuth()
		got.body = nil
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	rp := newRazorpay(srv.URL)
	creds := credentials{KeyID: "rzp_test_k", KeySecret: "s3cret"}
	ctx := context.Background()
	closeBy := time.Unix(1900000000, 0)

	qr, img, err := rp.OpenQR(ctx, creds, "intent-1", 68000, closeBy)
	require.NoError(t, err)
	require.Equal(t, "qr_A1", qr)
	require.Equal(t, "https://rzp.io/i/x", img)
	require.Equal(t, "POST /v1/payments/qr_codes", got.method+" "+got.path)
	require.Equal(t, [2]string{"rzp_test_k", "s3cret"}, [2]string{got.user, got.pass})
	require.Equal(t, map[string]any{"type": "upi_qr", "usage": "single_use", "fixed_amount": true, "payment_amount": float64(68000),
		"close_by": float64(1900000000), "notes": map[string]any{"intent_id": "intent-1"}}, got.body)

	answer = `{"items":[{"id":"pay_1","status":"captured","amount":68000,"currency":"INR","created_at":1900000100,"vpa":"x@y","acquirer_data":{"rrn":"123"}}]}`
	pays, err := rp.QRPayments(ctx, creds, "qr_A1")
	require.NoError(t, err)
	require.Equal(t, "GET /v1/payments/qr_codes/qr_A1/payments?count=100", got.method+" "+got.path)
	require.Equal(t, []providerPayment{{ID: "pay_1", Status: "captured", AmountMinor: 68000, Currency: "INR", RRN: "123", CreatedAt: time.Unix(1900000100, 0).UTC()}}, pays)

	// A QR that already closed answers 400 to close: that is the wanted state.
	status, answer = http.StatusBadRequest, `{}`
	require.NoError(t, rp.CloseQR(ctx, creds, "qr_A1"))
	require.Equal(t, "POST /v1/payments/qr_codes/qr_A1/close", got.method+" "+got.path)
	status = http.StatusUnauthorized
	require.ErrorIs(t, rp.CloseQR(ctx, creds, "qr_A1"), errProviderRejected)
	require.ErrorIs(t, rp.Verify(ctx, creds), errProviderRejected)
	status = http.StatusTooManyRequests
	require.ErrorIs(t, rp.Verify(ctx, creds), errProviderUnavailable)
	status = http.StatusBadGateway
	_, _, err = rp.OpenQR(ctx, creds, "intent-1", 1, closeBy)
	require.ErrorIs(t, err, errProviderUnavailable)
	// An answer that is not an open QR is not taken as one.
	status, answer = http.StatusOK, `{"id":"qr_A1","image_url":"http://rzp.io/i/x","status":"active"}`
	_, _, err = rp.OpenQR(ctx, creds, "intent-1", 1, closeBy)
	require.ErrorIs(t, err, errProviderUnavailable)

	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer slow.Close()
	late := newRazorpay(slow.URL)
	late.client.Timeout = 50 * time.Millisecond
	err = late.Verify(ctx, creds)
	require.True(t, errors.Is(err, errProviderUnavailable), err)
}

func TestWebhookSignature(t *testing.T) {
	body := []byte(`{"event":"qr_code.credited"}`)
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	require.True(t, validWebhookSignature(body, sig, "whsec"))
	require.False(t, validWebhookSignature(body, sig, "other"))
	require.False(t, validWebhookSignature(append([]byte{}, append(body[:len(body)-1:len(body)-1], ' ')...), sig, "whsec"))
	require.False(t, validWebhookSignature(body, "", "whsec"))
	require.False(t, validWebhookSignature(body, sig, ""))
}

func TestPollSchedule(t *testing.T) {
	open := time.Unix(1000, 0)
	expires := open.Add(300 * time.Second)
	var offsets []float64
	for i := 0; i < 6; i++ {
		offsets = append(offsets, nextCheck(open, i, expires).Sub(open).Seconds())
	}
	require.Equal(t, []float64{5, 10, 20, 30, 60, 90}, offsets)
	require.Equal(t, expires, nextCheck(open, 50, expires), "never past expiry")
	require.Equal(t, open.Add(2*time.Minute), *lateCheckAfter(open, 2))
	require.Equal(t, open.Add(13*time.Minute), *lateCheckAfter(open, 1))
	require.Nil(t, lateCheckAfter(open, 0))
}
