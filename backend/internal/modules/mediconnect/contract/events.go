// Package contract defines MediConnect's published account events.
package contract

// SubjectAccountCreated carries only an opaque internal account reference.
const SubjectAccountCreated = "helivanta.in.mediconnect.account_created.v1"

// AccountCreatedData identifies the account created in the envelope's tenant.
type AccountCreatedData struct {
	AccountID string `json:"account_id"`
}

const (
	SubjectReviewRequested = "helivanta.in.mediconnect.registration_review_requested.v1"
	SubjectReviewWithdrawn = "helivanta.in.mediconnect.registration_review_withdrawn.v1"
)

// RegistrationReviewData carries an account's scoped registration claim, not clinical authority.
type RegistrationReviewData struct {
	RequestID        string `json:"request_id"`
	AccountID        string `json:"account_id"`
	RequesterSubject string `json:"requester_subject"`
	HospitalID       string `json:"hospital_id"`
	PatientID        string `json:"patient_id"`
	Relationship     string `json:"relationship"`
}
