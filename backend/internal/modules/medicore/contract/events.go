// Package contract is medicore's published event interface: the subject
// constants and payload types other modules may depend on.
//
// It is the ONE part of a module other modules may import (see
// docs/standards/backend.md and internal/archtest/contract_test.go).
// That exception exists so a publisher and its consumers share one
// definition instead of three copies — before this, a renamed field
// left consumers unmarshalling into a struct whose json tags no longer
// matched, which encoding/json reports as no error at all and a zero
// value, writing an empty patient name into a real clinical record
// (#827).
//
// Data only: consts, types and vars. No funcs, no methods, and no
// imports beyond time and uuid. A method here would be behaviour
// crossing the module boundary, which is what the isolation rule exists
// to stop; an import of medicore itself would re-open that boundary
// transitively, because this package is importable by everyone.
package contract

// SubjectVisitCreated is published when a visit opens. Pharmacy and lab
// consume it to open their pending work.
const SubjectVisitCreated = "helivanta.in.medicore.visit_created.v1"

// VisitCreatedData is the v1 payload of SubjectVisitCreated.
//
// The json tags are the wire contract. Renaming a field here is a
// breaking change to every consumer, and is meant to break their build —
// that is the whole point of this package existing.
type VisitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
	Department  string `json:"department"`

	// PatientID references the patientmaster record. Added ALONGSIDE
	// PatientName rather than replacing it (spec D7): renaming or
	// removing a field here breaks every consumer's build on purpose, so
	// pharmacy and lab adopt this on their own schedules and PatientName
	// retires in a later version once nothing reads it.
	PatientID string `json:"patient_id"`
}
