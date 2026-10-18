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

// SubjectAttendancePeriodChanged reports a timesheet period finalised, recalculated or reopened; consumers re-read the timesheet through the API.
const SubjectAttendancePeriodChanged = "helivanta.in.workforce.attendance_period_changed.v1"

type AttendancePeriodChangedData struct {
	HospitalID  string `json:"hospital_id"`
	PeriodID    string `json:"period_id"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	TimesheetID string `json:"timesheet_id"`
}

// SubjectSalaryConfigChanged reports a salary head or structure revision; consumers re-read the configuration through the API.
const SubjectSalaryConfigChanged = "helivanta.in.workforce.salary_config_changed.v1"

type SalaryConfigChangedData struct {
	HospitalID string `json:"hospital_id"`
	ConfigID   string `json:"config_id"`
	RevisionID string `json:"revision_id"`
	Kind       string `json:"kind"`
	Version    int64  `json:"version"`
}

// SubjectStatutoryRuleChanged reports a statutory proposal or decision; payloads never carry rates, ceilings or citations.
const SubjectStatutoryRuleChanged = "helivanta.in.workforce.statutory_rule_changed.v1"

type StatutoryRuleChangedData struct {
	HospitalID string `json:"hospital_id"`
	RuleID     string `json:"rule_id"`
	RevisionID string `json:"revision_id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectSalaryAssignmentChanged reports a staff pay proposal or decision; payloads never carry structures or amounts.
const SubjectSalaryAssignmentChanged = "helivanta.in.workforce.salary_assignment_changed.v1"

type SalaryAssignmentChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	RevisionID string `json:"revision_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectTaxRegimeChanged reports a recorded or changed regime election; payloads never carry the regime.
const SubjectTaxRegimeChanged = "helivanta.in.workforce.tax_regime_changed.v1"

type TaxRegimeChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	TaxYear    string `json:"tax_year"`
	Version    int64  `json:"version"`
}

// SubjectStaffIdentifiersChanged reports a bank or tax identifier proposal or decision; payloads never carry values or masks.
const SubjectStaffIdentifiersChanged = "helivanta.in.workforce.staff_identifiers_changed.v1"

type StaffIdentifiersChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	RevisionID string `json:"revision_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectStaffIdentifierRevealed reports that someone viewed a full identifier; the reason stays in the audit log.
const SubjectStaffIdentifierRevealed = "helivanta.in.workforce.staff_identifier_revealed.v1"

type StaffIdentifierRevealedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	RevisionID string `json:"revision_id"`
	Field      string `json:"field"`
}

// SubjectTaxDeclarationChanged reports a tax declaration proposal or decision; payloads never carry amounts, parties or PANs.
const SubjectTaxDeclarationChanged = "helivanta.in.workforce.tax_declaration_changed.v1"

type TaxDeclarationChangedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	TaxYear    string `json:"tax_year"`
	RevisionID string `json:"revision_id"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
}

// SubjectTaxDeclarationPartyRevealed reports that someone viewed a landlord's or lender's PAN; the reason stays in the audit log.
const SubjectTaxDeclarationPartyRevealed = "helivanta.in.workforce.tax_declaration_party_revealed.v1"

type TaxDeclarationPartyRevealedData struct {
	HospitalID string `json:"hospital_id"`
	StaffID    string `json:"staff_id"`
	TaxYear    string `json:"tax_year"`
	RevisionID string `json:"revision_id"`
	Role       string `json:"role"`
	Position   int    `json:"position"`
}

// SubjectCredentialReviewChanged reports a credential review proposal or decision; payloads never carry numbers, issuers or dates.
const SubjectCredentialReviewChanged = "helivanta.in.workforce.credential_review_changed.v1" //nolint:gosec // G101: an event subject, not a secret

type CredentialReviewChangedData struct {
	HospitalID   string `json:"hospital_id"`
	StaffID      string `json:"staff_id"`
	CredentialID string `json:"credential_id"`
	ReviewID     string `json:"review_id"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
}

// SubjectCredentialValidityChanged reports that an approved review replaced a credential's reviewed validity.
const SubjectCredentialValidityChanged = "helivanta.in.workforce.credential_validity_changed.v1" //nolint:gosec // G101: an event subject, not a secret

type CredentialValidityChangedData struct {
	HospitalID   string `json:"hospital_id"`
	StaffID      string `json:"staff_id"`
	CredentialID string `json:"credential_id"`
	ReviewID     string `json:"review_id"`
	Version      int64  `json:"version"`
}
