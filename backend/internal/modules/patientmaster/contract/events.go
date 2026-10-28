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

// SubjectSharingRecipientChanged signals an immutable reviewed recipient revision without identity evidence.
const SubjectSharingRecipientChanged = "helivanta.in.patientmaster.sharing_recipient_changed.v1"

// SharingRecipientChangedData carries only the opaque binding identifier and revision.
type SharingRecipientChangedData struct {
	RecipientID string `json:"recipient_id"`
	Version     int64  `json:"version"`
}

// SubjectClinicalConsentChanged invalidates projections without carrying clinical scope or actor data.
const SubjectClinicalConsentChanged = "helivanta.in.patientmaster.clinical_consent_changed.v1"

// ClinicalConsentChangedData carries only declaration identity and monotonic revision.
type ClinicalConsentChangedData struct {
	ConsentID string `json:"consent_id"`
	Version   int64  `json:"version"`
}

// SubjectProxyAuthorityChanged records a guardian review revision, not consent or disclosure.
const SubjectProxyAuthorityChanged = "helivanta.in.patientmaster.proxy_authority_changed.v1"

// ProxyAuthorityChangedData carries only an opaque authority identifier and revision.
type ProxyAuthorityChangedData struct {
	AuthorityID string `json:"authority_id"`
	Version     int64  `json:"version"`
}

// SubjectPharmacyAffiliationDecided records a hospital's accept, decline or
// revoke of a pharmacy affiliation request.
const SubjectPharmacyAffiliationDecided = "helivanta.in.patientmaster.pharmacy_affiliation_decided.v1"

// PharmacyAffiliationDecidedData omits the reviewer and reason; both stay
// in patientmaster's decision history.
type PharmacyAffiliationDecidedData struct {
	RequestID     string `json:"request_id"`
	AffiliationID string `json:"affiliation_id"`
	HospitalID    string `json:"hospital_id"`
	DecisionID    string `json:"decision_id"`
	Outcome       string `json:"outcome"`
	DecidedAt     string `json:"decided_at"`
}
