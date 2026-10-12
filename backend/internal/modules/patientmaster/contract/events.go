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
	PatientID string `json:"patient_id"`
}
