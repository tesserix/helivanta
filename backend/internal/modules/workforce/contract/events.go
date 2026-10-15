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

const SubjectRosterConfigChanged = "helivanta.in.workforce.roster_config_changed.v1"

type RosterConfigChangedData struct {
	HospitalID string `json:"hospital_id"`
	ConfigID   string `json:"config_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}

const SubjectDocumentScanRequested = "helivanta.in.workforce.document_scan_requested.v1"

type DocumentScanRequestedData struct {
	HospitalID  string `json:"hospital_id"`
	StaffID     string `json:"staff_id"`
	FileID      string `json:"file_id"`
	ScanAttempt int64  `json:"scan_attempt"`
}

const SubjectDocumentChanged = "helivanta.in.workforce.document_changed.v1"

type DocumentChangedData struct {
	HospitalID  string `json:"hospital_id"`
	StaffID     string `json:"staff_id"`
	FileID      string `json:"file_id"`
	ScanAttempt int64  `json:"scan_attempt"`
	Status      string `json:"status"`
}

type DocumentScanInput struct {
	TenantID    string `json:"tenant_id"`
	HospitalID  string `json:"hospital_id"`
	StaffID     string `json:"staff_id"`
	FileID      string `json:"file_id"`
	ScanAttempt int64  `json:"scan_attempt"`
}

const SubjectRosterSettingsChanged = "helivanta.in.workforce.roster_settings_changed.v1"

type RosterSettingsChangedData struct {
	HospitalID string `json:"hospital_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}

const SubjectStaffRosterAssignmentChanged = "helivanta.in.workforce.staff_roster_assignment_changed.v1"

type StaffRosterAssignmentChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}
