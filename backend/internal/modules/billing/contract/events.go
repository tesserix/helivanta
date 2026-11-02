// Package contract is billing's published event interface. Data only:
// consts and types. See docs/standards/backend.md and
// internal/archtest/contract_test.go.
package contract

import "time"

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

// Shift events (#198 slice 7), written by database triggers in the same
// transaction as the change, so no path changes a drawer unannounced.
// Amounts are paise; reasons and note counts are never published.

// SubjectShiftOpened: a shift opened, by its cashier or from a handover.
const SubjectShiftOpened = "helivanta.in.billing.shift_opened.v1"

// SubjectShiftVarianceRecorded: a shift's count differed from its expected
// cash; it waits for a supervisor.
const SubjectShiftVarianceRecorded = "helivanta.in.billing.shift_variance_recorded.v1"

// SubjectShiftClosed: a shift closed for good, its count matched or its
// variance authorised.
const SubjectShiftClosed = "helivanta.in.billing.shift_closed.v1"

// SubjectCashPickupRecorded: cash taken from an open drawer to the safe, or
// a pickup corrected.
const SubjectCashPickupRecorded = "helivanta.in.billing.cash_pickup_recorded.v1"

// SubjectShiftHandedOver: a handover completed, accepted by its taker or
// settled by a supervisor.
const SubjectShiftHandedOver = "helivanta.in.billing.shift_handed_over.v1"

// ShiftOpenedData names the handover a shift opened from, if any.
type ShiftOpenedData struct {
	ShiftID           string    `json:"shift_id"`
	HospitalID        string    `json:"hospital_id"`
	CounterID         string    `json:"counter_id"`
	Cashier           string    `json:"cashier"`
	Currency          string    `json:"currency"`
	OpeningFloatMinor int64     `json:"opening_float_minor"`
	HandoverID        *string   `json:"handover_id"`
	OpenedAt          time.Time `json:"opened_at"`
}

// ShiftVarianceRecordedData is the count against the expected cash.
type ShiftVarianceRecordedData struct {
	ShiftID       string    `json:"shift_id"`
	HospitalID    string    `json:"hospital_id"`
	CounterID     string    `json:"counter_id"`
	Cashier       string    `json:"cashier"`
	Currency      string    `json:"currency"`
	ExpectedMinor int64     `json:"expected_minor"`
	DeclaredMinor int64     `json:"declared_minor"`
	VarianceMinor int64     `json:"variance_minor"`
	HandedOver    bool      `json:"handed_over"`
	CountedAt     time.Time `json:"counted_at"`
}

// ShiftClosedData is the shift's final figures; who authorised a variance,
// null when the count matched.
type ShiftClosedData struct {
	ShiftID           string     `json:"shift_id"`
	HospitalID        string     `json:"hospital_id"`
	CounterID         string     `json:"counter_id"`
	Cashier           string     `json:"cashier"`
	Currency          string     `json:"currency"`
	OpeningFloatMinor int64      `json:"opening_float_minor"`
	ExpectedMinor     int64      `json:"expected_minor"`
	DeclaredMinor     int64      `json:"declared_minor"`
	VarianceMinor     int64      `json:"variance_minor"`
	HandedOver        bool       `json:"handed_over"`
	CountedAt         time.Time  `json:"counted_at"`
	AuthorisedBy      *string    `json:"authorised_by"`
	AuthorisedAt      *time.Time `json:"authorised_at"`
}

// Pickup kinds: cash taken out, or an overstated pickup put back.
const (
	PickupKindPickup     = "pickup"
	PickupKindCorrection = "correction"
)

// CashPickupRecordedData names the pickup a correction puts back into.
type CashPickupRecordedData struct {
	PickupID    string    `json:"pickup_id"`
	ShiftID     string    `json:"shift_id"`
	HospitalID  string    `json:"hospital_id"`
	CounterID   string    `json:"counter_id"`
	Kind        string    `json:"kind"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	CorrectsID  *string   `json:"corrects_id"`
	RecordedBy  string    `json:"recorded_by"`
	RecordedAt  time.Time `json:"recorded_at"`
}

// ShiftHandedOverData pairs the closing and opening shifts; settled_by is
// the supervisor who settled a disputed count, null when it was accepted.
type ShiftHandedOverData struct {
	HandoverID   string    `json:"handover_id"`
	HospitalID   string    `json:"hospital_id"`
	CounterID    string    `json:"counter_id"`
	FromShiftID  string    `json:"from_shift_id"`
	ToShiftID    string    `json:"to_shift_id"`
	FromCashier  string    `json:"from_cashier"`
	ToCashier    string    `json:"to_cashier"`
	Currency     string    `json:"currency"`
	AgreedMinor  int64     `json:"agreed_minor"`
	SettledBy    *string   `json:"settled_by"`
	HandedOverAt time.Time `json:"handed_over_at"`
}
