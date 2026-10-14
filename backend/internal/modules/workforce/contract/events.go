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
