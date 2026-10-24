// Package contract is patientmaster's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectPatientRegistered is published when a patient is registered.
const SubjectPatientRegistered = "helivanta.in.patientmaster.registered.v1"

// PatientRegisteredData is the v1 payload.
//
// It carries the id and NOTHING identifying — no name, date of birth,
// mobile or address. #835 was PHI leaving the RLS boundary in event
// payloads; a consumer that needs demographics calls the API under its
// own permission, where authorization is enforced per request. Adding a
// demographic field here would put clinical data in every replica's
// outbox and in the JetStream retention window.
type PatientRegisteredData struct {
	PatientID  string `json:"patient_id"`
	HospitalID string `json:"hospital_id"`
}

// SubjectRegistrationReviewed publishes an identity decision, never clinical authority.
const SubjectRegistrationReviewed = "helivanta.in.patientmaster.registration_reviewed.v1"

// RegistrationReviewedData omits evidence, demographics and reviewer identity.
type RegistrationReviewedData struct {
	RequestID  string `json:"request_id"`
	AccountID  string `json:"account_id"`
	HospitalID string `json:"hospital_id"`
	PatientID  string `json:"patient_id"`
	DecisionID string `json:"decision_id"`
	Outcome    string `json:"outcome"`
	ReviewedAt string `json:"reviewed_at"`
}
