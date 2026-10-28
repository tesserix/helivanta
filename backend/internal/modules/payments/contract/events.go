// Package contract is payments's published event interface. Data only:
// consts, types and vars, importing nothing beyond time and uuid. See
// docs/standards/backend.md and internal/archtest/contract_test.go.
package contract

import "time"

// Intent outcomes (#1386 C-4a). Each names the requester and its request
// id, so a requester consumes only what it asked for.
const (
	SubjectIntentOpened    = "helivanta.in.payments.intent_opened.v1"
	SubjectIntentFailed    = "helivanta.in.payments.intent_failed.v1"
	SubjectIntentPaid      = "helivanta.in.payments.intent_paid.v1"
	SubjectIntentExpired   = "helivanta.in.payments.intent_expired.v1"
	SubjectIntentCancelled = "helivanta.in.payments.intent_cancelled.v1"
)

// RequesterPharmacy is the requester value for PharmaConnect counter
// payment requests.
const RequesterPharmacy = "pharmacy"

// Failure reasons carried by SubjectIntentFailed.
const (
	ReasonProviderNotConfigured = "provider_not_configured"
	ReasonProviderUnavailable   = "provider_unavailable"
	ReasonProviderRejected      = "provider_rejected"
)

// IntentRef identifies an intent and the request it answers.
type IntentRef struct {
	Requester string `json:"requester"`
	RequestID string `json:"request_id"`
	IntentID  string `json:"intent_id"`
}

// IntentOpenedData carries the QR the payer scans.
type IntentOpenedData struct {
	IntentRef
	QRImageURL string    `json:"qr_image_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// IntentFailedData says why no QR exists.
type IntentFailedData struct {
	IntentRef
	Reason string `json:"reason"`
}

// IntentPaidData is a payment fetched from the provider and matched to the
// intent's amount and currency. It carries no payer identity. Late is true
// when it landed after the intent was reported expired or cancelled.
type IntentPaidData struct {
	IntentRef
	AmountMinor       int64     `json:"amount_minor"`
	Currency          string    `json:"currency"`
	ProviderPaymentID string    `json:"provider_payment_id"`
	RRN               string    `json:"rrn"`
	PaidAt            time.Time `json:"paid_at"`
	Late              bool      `json:"late"`
}

// IntentExpiredData reports an intent whose QR closed unpaid at expiry.
type IntentExpiredData struct {
	IntentRef
}

// IntentCancelledData reports an intent closed unpaid on the requester's
// cancel.
type IntentCancelledData struct {
	IntentRef
}
