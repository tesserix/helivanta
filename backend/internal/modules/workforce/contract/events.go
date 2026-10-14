// Package contract defines workforce's published event interface.
package contract

// SubjectStaffEnrollmentChanged carries no personal enrollment data.
const SubjectStaffEnrollmentChanged = "helivanta.in.workforce.enrollment_changed.v1"

// StaffEnrollmentChangedData identifies the committed resource version.
type StaffEnrollmentChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	Version    int64  `json:"version"`
}

const SubjectMasterChanged = "helivanta.in.workforce.master_changed.v1"

type MasterChangedData struct {
	HospitalID string `json:"hospital_id"`
	MasterID   string `json:"master_id"`
	RevisionID string `json:"revision_id"`
	Kind       string `json:"kind"`
	Version    int64  `json:"version"`
}

const SubjectShiftChanged = "helivanta.in.workforce.shift_changed.v1"

type ShiftChangedData struct {
	HospitalID string `json:"hospital_id"`
	ShiftID    string `json:"shift_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}
