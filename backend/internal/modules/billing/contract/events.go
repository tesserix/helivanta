// Package contract is billing's published event interface. Data only:
// consts and types. See docs/standards/backend.md and
// internal/archtest/contract_test.go.
package contract

// SubjectPaymentRequested asks payments to collect UPI money on an
// admission account at the billing desk (#198, #211). Payments answers with
// its intent events for requester "billing".
const SubjectPaymentRequested = "helivanta.in.billing.payment_requested.v1"

// SubjectPaymentCancelled asks payments to close a request's QR and settle
// it; the answer is intent_paid or intent_cancelled, never silence.
const SubjectPaymentCancelled = "helivanta.in.billing.payment_cancelled.v1"

// Purposes: a payment against the balance due, or a deposit held for the
// patient.
const (
	PaymentPurposeAdmissionPayment = "admission_payment"
	PaymentPurposeAdmissionDeposit = "admission_deposit"
)

// PaymentRequestedData carries the amount and its purpose, and nothing
// about the patient, the admission or the hospital.
type PaymentRequestedData struct {
	RequestID        string `json:"request_id"`
	Purpose          string `json:"purpose"`
	AmountMinor      int64  `json:"amount_minor"`
	Currency         string `json:"currency"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

// PaymentCancelledData names the request to close, with its amount so a
// cancel that overtakes its request can stand in for it.
type PaymentCancelledData struct {
	RequestID   string `json:"request_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}
