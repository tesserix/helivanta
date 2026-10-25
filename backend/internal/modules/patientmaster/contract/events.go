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

// SubjectConsentAuthorityChanged records a reviewed authority revision, not disclosure.
const SubjectConsentAuthorityChanged = "helivanta.in.patientmaster.consent_authority_changed.v1"

// ConsentAuthorityChangedData carries only the opaque receipt and monotonic revision.
type ConsentAuthorityChangedData struct {
	AuthorityID string `json:"authority_id"`
	Version     int64  `json:"version"`
}

// SubjectSharingNoticeChanged records a hospital notice revision without its content.
const SubjectSharingNoticeChanged = "helivanta.in.patientmaster.sharing_notice_changed.v1"

// SharingNoticeChangedData contains only the opaque notice and monotonic revision.
type SharingNoticeChangedData struct {
	NoticeID string `json:"notice_id"`
	Version  int64  `json:"version"`
}
