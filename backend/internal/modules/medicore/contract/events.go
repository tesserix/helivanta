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

// SubjectEncounterOpened is published when an OPD encounter opens at a
// hospital (#80 E6). It carries references only: no name, and not the
// consult mode or visit type, which are clinical context a consumer reads
// through an authorised query if it needs them.
const SubjectEncounterOpened = "helivanta.in.medicore.encounter_opened.v1"

// EncounterOpenedData is the v1 payload of SubjectEncounterOpened.
type EncounterOpenedData struct {
	EncounterID string `json:"encounter_id"`
	HospitalID  string `json:"hospital_id"`
	PatientID   string `json:"patient_id"`
}

// SubjectEncounterStatusChanged is published for every encounter status
// transition, in the transaction that records it.
const SubjectEncounterStatusChanged = "helivanta.in.medicore.encounter_status_changed.v1"

// EncounterStatusChangedData is the v1 payload of SubjectEncounterStatusChanged.
type EncounterStatusChangedData struct {
	EncounterID string `json:"encounter_id"`
	HospitalID  string `json:"hospital_id"`
	FromStatus  string `json:"from_status"`
	ToStatus    string `json:"to_status"`
	Version     int    `json:"version"`
}

// SubjectAllergyChanged is published when an allergy entry is recorded or
// corrected, or "no known allergy" is recorded (#1373 A11). References
// only: no substance, reaction or severity; consumers re-read through an
// authorised query.
const SubjectAllergyChanged = "helivanta.in.medicore.allergy_changed.v1"

// AllergyChangedData is the v1 payload of SubjectAllergyChanged.
type AllergyChangedData struct {
	PatientID  string `json:"patient_id"`
	HospitalID string `json:"hospital_id"`
	RecordID   string `json:"record_id"`
	Version    int    `json:"version"`
	// Kind is "entry" or "no_known_allergy".
	Kind string `json:"kind"`
}

// SubjectWeightRecorded is published when a weight is recorded or marked
// entered in error (#1373 A11). No value travels.
const SubjectWeightRecorded = "helivanta.in.medicore.weight_recorded.v1"

// WeightRecordedData is the v1 payload of SubjectWeightRecorded.
type WeightRecordedData struct {
	PatientID      string `json:"patient_id"`
	HospitalID     string `json:"hospital_id"`
	ObservationID  string `json:"observation_id"`
	EnteredInError bool   `json:"entered_in_error"`
}

// SubjectBedOccupancyStarted is published when a patient's admission takes a
// bed, by admission or by transfer in (#106). References and the start
// instant only: billing resolves "who was in this bed at time t" from these.
const SubjectBedOccupancyStarted = "helivanta.in.medicore.bed_occupancy_started.v1"

// BedOccupancyStartedData is the v1 payload of SubjectBedOccupancyStarted.
type BedOccupancyStartedData struct {
	OccupancyID string `json:"occupancy_id"`
	AdmissionID string `json:"admission_id"`
	PatientID   string `json:"patient_id"`
	HospitalID  string `json:"hospital_id"`
	WardID      string `json:"ward_id"`
	RoomID      string `json:"room_id"`
	BedID       string `json:"bed_id"`
	// The codes ward staff say aloud, so a consumer can resolve "bed A-1"
	// without reading MediCore's tables. Location labels, not patient data.
	WardCode string `json:"ward_code"`
	RoomCode string `json:"room_code"`
	BedCode  string `json:"bed_code"`
	// The bed's class, fixed on the bed; billing prices the stay by it.
	BedClass  string `json:"bed_class"`
	StartedAt string `json:"started_at"`
}

// BedClasses are the classes a bed may have; billing prices a stay by its
// bed's class (#197).
var BedClasses = map[string]bool{"general": true, "semi_private": true, "private": true, "icu": true, "hdu": true, "isolation": true, "neonatal": true, "nicu": true, "day_care": true}

// SubjectBedOccupancyEnded is published when a stay in a bed ends, by
// transfer out or release.
const SubjectBedOccupancyEnded = "helivanta.in.medicore.bed_occupancy_ended.v1"

// BedOccupancyEndedData is the v1 payload of SubjectBedOccupancyEnded.
type BedOccupancyEndedData struct {
	OccupancyID string `json:"occupancy_id"`
	AdmissionID string `json:"admission_id"`
	HospitalID  string `json:"hospital_id"`
	BedID       string `json:"bed_id"`
	EndedAt     string `json:"ended_at"`
	End         string `json:"end"`
}

// SubjectAdmissionReleased is published when an admission leaves its bed for
// good. The reason records the departure; it is not clinical discharge (#96).
const SubjectAdmissionReleased = "helivanta.in.medicore.admission_released.v1"

// AdmissionReleasedData is the v1 payload of SubjectAdmissionReleased.
type AdmissionReleasedData struct {
	AdmissionID string `json:"admission_id"`
	PatientID   string `json:"patient_id"`
	HospitalID  string `json:"hospital_id"`
	ReleasedAt  string `json:"released_at"`
	Reason      string `json:"reason"`
}

// SubjectDischargeOrdered is published when a doctor orders an admitted
// patient's discharge (#96): clinical readiness, before the release, which is
// the departure. Billing starts the final bill from it.
const SubjectDischargeOrdered = "helivanta.in.medicore.discharge_ordered.v1"

// DischargeOrderedData is the v1 payload of SubjectDischargeOrdered.
type DischargeOrderedData struct {
	OrderID     string `json:"order_id"`
	AdmissionID string `json:"admission_id"`
	PatientID   string `json:"patient_id"`
	HospitalID  string `json:"hospital_id"`
	// home or transfer_out; the doctor's note stays in MediCore.
	Disposition string `json:"disposition"`
	OrderedAt   string `json:"ordered_at"`
}

// SubjectDischargeWithdrawn is published when a doctor withdraws a discharge
// order before the patient leaves.
const SubjectDischargeWithdrawn = "helivanta.in.medicore.discharge_withdrawn.v1"

// DischargeWithdrawnData is the v1 payload of SubjectDischargeWithdrawn.
type DischargeWithdrawnData struct {
	OrderID     string `json:"order_id"`
	AdmissionID string `json:"admission_id"`
	HospitalID  string `json:"hospital_id"`
	WithdrawnAt string `json:"withdrawn_at"`
}
