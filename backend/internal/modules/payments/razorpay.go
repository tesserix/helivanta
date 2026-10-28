package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// errProviderRejected is a 4xx from the provider: the request or the
	// credentials are wrong, and retrying the same call will not help.
	errProviderRejected = errors.New("payments: provider rejected the request")
	// errProviderUnavailable is everything else: 5xx, transport errors,
	// timeouts and answers we cannot read.
	errProviderUnavailable = errors.New("payments: provider unavailable")
)

// rejectedStatus is the 4xx a provider refused with; it is errProviderRejected.
type rejectedStatus int

func (s rejectedStatus) Error() string {
	return fmt.Sprintf("payments: provider rejected the request (%d)", int(s))
}
func (s rejectedStatus) Is(target error) bool { return target == errProviderRejected }

// credentials are one account's unsealed keys, held only for one call.
type credentials struct {
	KeyID, KeySecret string
}

// providerPayment is a payment the provider API reported for a QR. It has
// no payer field by design: payments keeps no payer identity.
type providerPayment struct {
	ID          string
	Status      string
	AmountMinor int64
	Currency    string
	RRN         string
	CreatedAt   time.Time
}

// provider is the one seam between the orchestration code and a payment
// provider; nothing outside razorpay.go names Razorpay's API.
type provider interface {
	OpenQR(ctx context.Context, c credentials, intentID string, amountMinor int64, closeBy time.Time) (qrID, imageURL string, err error)
	CloseQR(ctx context.Context, c credentials, qrID string) error
	QRPayments(ctx context.Context, c credentials, qrID string) ([]providerPayment, error)
	Verify(ctx context.Context, c credentials) error
}

// razorpay calls the Razorpay REST API with an account's Basic auth.
type razorpay struct {
	base   string
	client *http.Client
}

// newRazorpay pins one attempt per call with a 10 s ceiling: a counter
// waiting on a QR must hear "unavailable" quickly, not after retries.
func newRazorpay(base string) *razorpay {
	return &razorpay{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 10 * time.Second}}
}

func (r *razorpay) OpenQR(ctx context.Context, c credentials, intentID string, amountMinor int64, closeBy time.Time) (string, string, error) {
	body := map[string]any{
		"type": "upi_qr", "usage": "single_use", "fixed_amount": true,
		"payment_amount": amountMinor, "close_by": closeBy.Unix(),
		"notes": map[string]string{"intent_id": intentID},
	}
	var out struct {
		ID       string `json:"id"`
		ImageURL string `json:"image_url"`
		Status   string `json:"status"`
	}
	if err := r.call(ctx, c, http.MethodPost, "/v1/payments/qr_codes", body, &out); err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(out.ID, "qr_") || !strings.HasPrefix(out.ImageURL, "https://") || out.Status != "active" {
		return "", "", fmt.Errorf("%w: unexpected QR answer", errProviderUnavailable)
	}
	return out.ID, out.ImageURL, nil
}

func (r *razorpay) CloseQR(ctx context.Context, c credentials, qrID string) error {
	err := r.call(ctx, c, http.MethodPost, "/v1/payments/qr_codes/"+url.PathEscape(qrID)+"/close", nil, nil)
	// A QR that already closed itself (paid, or past close_by) answers 400;
	// closed is the state we wanted. Any other refusal (401: keys revoked)
	// stays an error, so the caller does not decide on an unclosed QR.
	var status rejectedStatus
	if errors.As(err, &status) && status == http.StatusBadRequest {
		return nil
	}
	return err
}

func (r *razorpay) QRPayments(ctx context.Context, c credentials, qrID string) ([]providerPayment, error) {
	var out struct {
		Items []struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			Amount       int64  `json:"amount"`
			Currency     string `json:"currency"`
			CreatedAt    int64  `json:"created_at"`
			AcquirerData struct {
				RRN string `json:"rrn"`
			} `json:"acquirer_data"`
		} `json:"items"`
	}
	// A single-use QR takes one payment; 100 is Razorpay's page ceiling and
	// bounds the duplicates we could ever see.
	if err := r.call(ctx, c, http.MethodGet, "/v1/payments/qr_codes/"+url.PathEscape(qrID)+"/payments?count=100", nil, &out); err != nil {
		return nil, err
	}
	payments := make([]providerPayment, 0, len(out.Items))
	for _, p := range out.Items {
		payments = append(payments, providerPayment{ID: p.ID, Status: p.Status, AmountMinor: p.Amount, Currency: p.Currency,
			RRN: p.AcquirerData.RRN, CreatedAt: time.Unix(p.CreatedAt, 0).UTC()})
	}
	return payments, nil
}

// Verify proves the keys work with the cheapest authenticated read.
func (r *razorpay) Verify(ctx context.Context, c credentials) error {
	return r.call(ctx, c, http.MethodGet, "/v1/payments/qr_codes?count=1", nil, nil)
}

func (r *razorpay) call(ctx context.Context, c credentials, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.KeyID, c.KeySecret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %w", errProviderUnavailable, err)
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
	case resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests:
		return rejectedStatus(resp.StatusCode)
	default:
		return fmt.Errorf("%w: %d", errProviderUnavailable, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %w", errProviderUnavailable, err)
	}
	return nil
}

// validWebhookSignature checks X-Razorpay-Signature: hex HMAC-SHA256 of the
// raw body under the account's webhook secret, compared in constant time.
func validWebhookSignature(body []byte, signature, secret string) bool {
	got, err := hex.DecodeString(signature)
	if err != nil || len(got) != sha256.Size || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
