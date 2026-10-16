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

const SubjectRosterChanged = "helivanta.in.workforce.roster_changed.v1"

type RosterChangedData struct {
	HospitalID string `json:"hospital_id"`
	RosterID   string `json:"roster_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}

const SubjectLeaveConfigChanged = "helivanta.in.workforce.leave_config_changed.v1"

type LeaveConfigChangedData struct {
	HospitalID string `json:"hospital_id"`
	ConfigID   string `json:"config_id"`
	RevisionID string `json:"revision_id"`
	Kind       string `json:"kind"`
	Version    int64  `json:"version"`
}

const SubjectStaffLeaveAssignmentChanged = "helivanta.in.workforce.staff_leave_assignment_changed.v1"

type StaffLeaveAssignmentChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}

// SubjectLeaveLedgerChanged reports a posting run, year close, correction or request decision; consumers re-read balances through the API.
const SubjectLeaveLedgerChanged = "helivanta.in.workforce.leave_ledger_changed.v1"

type LeaveLedgerChangedData struct {
	HospitalID  string `json:"hospital_id"`
	LeaveYear   int    `json:"leave_year"`
	Source      string `json:"source"`
	ReferenceID string `json:"reference_id"`
	Entries     int    `json:"entries"`
}

// SubjectLeaveRequestChanged reports a leave request filing or transition; consumers re-read it through the API.
const SubjectLeaveRequestChanged = "helivanta.in.workforce.leave_request_changed.v1"

type LeaveRequestChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectDutyExchangeChanged reports a duty exchange filing or transition; consumers re-read it through the API.
const SubjectDutyExchangeChanged = "helivanta.in.workforce.duty_exchange_changed.v1"

type DutyExchangeChangedData struct {
	HospitalID string `json:"hospital_id"`
	ExchangeID string `json:"exchange_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectOvertimeSettingsChanged reports a new overtime policy revision for a hospital.
const SubjectOvertimeSettingsChanged = "helivanta.in.workforce.overtime_settings_changed.v1"

type OvertimeSettingsChangedData struct {
	HospitalID string `json:"hospital_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}

// SubjectOvertimeChanged reports an overtime filing or transition; payroll re-reads payable minutes through the API.
const SubjectOvertimeChanged = "helivanta.in.workforce.overtime_changed.v1"

type OvertimeChangedData struct {
	HospitalID string `json:"hospital_id"`
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectAttendanceSettingsChanged reports a new attendance policy revision for a hospital.
const SubjectAttendanceSettingsChanged = "helivanta.in.workforce.attendance_settings_changed.v1"

type AttendanceSettingsChangedData struct {
	HospitalID string `json:"hospital_id"`
	RevisionID string `json:"revision_id"`
	Version    int64  `json:"version"`
}

// SubjectAttendancePunchChanged reports a punch recorded or voided; consumers re-read derived attendance through the API.
const SubjectAttendancePunchChanged = "helivanta.in.workforce.attendance_punch_changed.v1"

type AttendancePunchChangedData struct {
	HospitalID string `json:"hospital_id"`
	PunchID    string `json:"punch_id"`
	Action     string `json:"action"`
}

// SubjectAttendanceCorrectionChanged reports a correction request filed or moved to a new status.
const SubjectAttendanceCorrectionChanged = "helivanta.in.workforce.attendance_correction_changed.v1"

type AttendanceCorrectionChangedData struct {
	HospitalID   string `json:"hospital_id"`
	CorrectionID string `json:"correction_id"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
}
