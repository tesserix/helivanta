// Package workforce owns hospital-local staff directories and explicit access.
package workforce

import (
	_ "embed"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermHospitalManage authz.Permission = "workforce.hospital.manage"
const PermEnrollmentManage authz.Permission = "workforce.enrollment.manage"
const PermDocumentManage authz.Permission = "workforce.documents.manage"
const PermLoginLinkManage authz.Permission = "workforce.login_links.manage"
const PermLoginLinkReview authz.Permission = "workforce.login_links.review"
const LoginLinkChanged = workforcecontract.SubjectLoginLinkChanged

//go:embed login_links.sql
var loginLinksSchema string

//go:embed scoped_leave.sql
var scopedLeaveSchema string

// PermCredentialReview decides credential validity reviews; a proposer can never decide their own.
const PermCredentialReview authz.Permission = "workforce.credentials.review"
const PermBenefitsView authz.Permission = "workforce.benefits.view"
const PermBenefitsManage authz.Permission = "workforce.benefits.manage"
const PermBenefitsApprove authz.Permission = "workforce.benefits.approve"

// PermBenefitsExport generates coverage files; no other benefits permission implies it.
const PermBenefitsExport authz.Permission = "workforce.benefits.export"
const PermRosterManage authz.Permission = "workforce.roster.manage"

// PermRosterCorrect reopens a frozen roster; it is separate so publication and correction can be held by different people.
const PermRosterCorrect authz.Permission = "workforce.roster.correct"
const RosterConfigChanged = workforcecontract.SubjectRosterConfigChanged
const DocumentScanRequested = workforcecontract.SubjectDocumentScanRequested
const DocumentChanged = workforcecontract.SubjectDocumentChanged
const ShiftChanged = workforcecontract.SubjectShiftChanged
const RosterSettingsChanged = workforcecontract.SubjectRosterSettingsChanged
const StaffRosterAssignmentChanged = workforcecontract.SubjectStaffRosterAssignmentChanged
const RosterChanged = workforcecontract.SubjectRosterChanged

// PermLeaveConfigure governs leave types, holiday calendars and structures; balances and approvals use their own permissions.
const PermLeaveConfigure authz.Permission = "workforce.leave.configure"
const LeaveConfigChanged = workforcecontract.SubjectLeaveConfigChanged
const SalaryConfigChanged = workforcecontract.SubjectSalaryConfigChanged
const StatutoryRuleChanged = workforcecontract.SubjectStatutoryRuleChanged
const SalaryAssignmentChanged = workforcecontract.SubjectSalaryAssignmentChanged
const TaxRegimeChanged = workforcecontract.SubjectTaxRegimeChanged
const StaffIdentifiersChanged = workforcecontract.SubjectStaffIdentifiersChanged
const StaffIdentifierRevealed = workforcecontract.SubjectStaffIdentifierRevealed
const TaxDeclarationChanged = workforcecontract.SubjectTaxDeclarationChanged
const TaxDeclarationPartyRevealed = workforcecontract.SubjectTaxDeclarationPartyRevealed
const CredentialReviewChanged = workforcecontract.SubjectCredentialReviewChanged
const CredentialValidityChanged = workforcecontract.SubjectCredentialValidityChanged
const CredentialReminderPolicyChanged = workforcecontract.SubjectCredentialReminderPolicyChanged
const CredentialReminderChanged = workforcecontract.SubjectCredentialReminderChanged
const BenefitPlanChanged = workforcecontract.SubjectBenefitPlanChanged
const DependentChanged = workforcecontract.SubjectDependentChanged
const BenefitEnrollmentChanged = workforcecontract.SubjectBenefitEnrollmentChanged

// PermLeaveAllocate assigns structures and posts or closes leave years; PermLeaveCorrect is held apart for reasoned balance corrections.
const PermLeaveAllocate authz.Permission = "workforce.leave.allocate"
const PermLeaveCorrect authz.Permission = "workforce.leave.correct"
const StaffLeaveAssignmentChanged = workforcecontract.SubjectStaffLeaveAssignmentChanged
const LeaveLedgerChanged = workforcecontract.SubjectLeaveLedgerChanged

// PermLeaveRequest files and withdraws requests for staff; PermLeaveApprove decides and revokes them, never on the holder's own filing.
const PermLeaveRequest authz.Permission = "workforce.leave.request"
const PermLeaveApprove authz.Permission = "workforce.leave.approve"
const LeaveRequestChanged = workforcecontract.SubjectLeaveRequestChanged

// PermDutyRequest files and cancels duty exchanges; PermDutyApprove decides and revokes them, never on the holder's own filing.
const PermDutyRequest authz.Permission = "workforce.duty.request"
const PermDutyApprove authz.Permission = "workforce.duty.approve"
const DutyExchangeChanged = workforcecontract.SubjectDutyExchangeChanged

// PermOvertimeConfigure sets the hospital overtime limits; PermOvertimeRequest files and withdraws requests; PermOvertimeApprove decides and revokes them, never on the holder's own filing.
const PermOvertimeConfigure authz.Permission = "workforce.overtime.configure"
const PermOvertimeRequest authz.Permission = "workforce.overtime.request"
const PermOvertimeApprove authz.Permission = "workforce.overtime.approve"
const OvertimeSettingsChanged = workforcecontract.SubjectOvertimeSettingsChanged
const OvertimeChanged = workforcecontract.SubjectOvertimeChanged

// PermAttendanceConfigure sets the hospital attendance policy; PermAttendanceRecord records and voids manual punches and reads their reasons.
const PermAttendanceConfigure authz.Permission = "workforce.attendance.configure"
const PermAttendanceRecord authz.Permission = "workforce.attendance.record"
const AttendanceSettingsChanged = workforcecontract.SubjectAttendanceSettingsChanged
const AttendancePunchChanged = workforcecontract.SubjectAttendancePunchChanged

// PermAttendanceCorrect files and cancels punch corrections; PermAttendanceApprove decides them, never on the holder's own filing.
const PermAttendanceCorrect authz.Permission = "workforce.attendance.correct"
const PermAttendanceApprove authz.Permission = "workforce.attendance.approve"
const AttendanceCorrectionChanged = workforcecontract.SubjectAttendanceCorrectionChanged

// PermAttendanceFinalise finalises, recalculates and reopens timesheet periods.
const PermAttendanceFinalise authz.Permission = "workforce.attendance.finalise"
const AttendancePeriodChanged = workforcecontract.SubjectAttendancePeriodChanged

// PermPayrollConfigure writes salary heads and structures; PermPayrollView reads them and runs previews, since pay configuration is commercially sensitive.
const PermPayrollConfigure authz.Permission = "workforce.payroll.configure"
const PermPayrollView authz.Permission = "workforce.payroll.view"

// PermPayrollApprove decides statutory proposals and is never satisfied by the proposal's own author.
const PermPayrollApprove authz.Permission = "workforce.payroll.approve"

// PermPayrollIdentifiersReveal shows a full bank account, PAN or UAN; masked values only need PermPayrollView.
const PermPayrollIdentifiersReveal authz.Permission = "workforce.payroll.identifiers.reveal"

const PermMasterManage authz.Permission = "workforce.master.manage"
const EnrollmentChanged = workforcecontract.SubjectStaffEnrollmentChanged
const MasterChanged = workforcecontract.SubjectMasterChanged

//go:embed schema.sql
var schema string

//go:embed enrollment.sql
var enrollmentSchema string

//go:embed staff_code.sql
var staffCodeSchema string

//go:embed masters.sql
var mastersSchema string

//go:embed shifts.sql
var shiftsSchema string

//go:embed roster_config.sql
var rosterConfigSchema string

//go:embed roster_policy.sql
var rosterPolicySchema string

//go:embed documents.sql
var documentsSchema string

//go:embed document_worker.sql
var documentWorkerSchema string

//go:embed worker_outbox_scope.sql
var workerOutboxScopeSchema string

//go:embed rosters.sql
var rostersSchema string

//go:embed leave_setup.sql
var leaveSetupSchema string

//go:embed leave_ledger.sql
var leaveLedgerSchema string

//go:embed leave_requests.sql
var leaveRequestsSchema string

//go:embed leave_reports.sql
var leaveReportsSchema string

//go:embed duty_exchanges.sql
var dutyExchangesSchema string

//go:embed overtime.sql
var overtimeSchema string

//go:embed attendance.sql
var attendanceSchema string

//go:embed attendance_corrections.sql
var attendanceCorrectionsSchema string

//go:embed attendance_timesheets.sql
var attendanceTimesheetsSchema string

//go:embed salary_config.sql
var salaryConfigSchema string

//go:embed statutory_rules.sql
var statutoryRulesSchema string

//go:embed statutory_pt_lwf.sql
var statutoryPtLwfSchema string

//go:embed income_tax.sql
var incomeTaxSchema string

//go:embed salary_assignment.sql
var salaryAssignmentSchema string

//go:embed staff_identifiers.sql
var staffIdentifiersSchema string

//go:embed statutory_pt_periods.sql
var ptPeriodsSchema string

//go:embed income_tax_claims.sql
var incomeTaxClaimsSchema string

//go:embed tax_declarations.sql
var taxDeclarationsSchema string

//go:embed tax_declaration_evidence.sql
var taxDeclarationEvidenceSchema string

//go:embed credential_validity.sql
var credentialValiditySchema string

//go:embed credential_reminders.sql
var credentialRemindersSchema string

//go:embed roster_credential_clashes.sql
var rosterCredentialClashesSchema string

//go:embed benefits.sql
var benefitsSchema string

//go:embed benefit_evidence.sql
var benefitEvidenceSchema string

//go:embed qualification_masters.sql
var qualificationMastersSchema string

//go:embed document_recovery.sql
var documentRecoverySchema string

//go:embed document_classification.sql
var documentClassificationSchema string

//go:embed contact_masters.sql
var contactMastersSchema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "workforce" }

//go:embed supervision.sql
var supervisionSchema string

//go:embed profile_masters.sql
var profileMastersSchema string

//go:embed identity_source.sql
var identitySourceSchema string

//go:embed identity_cases.sql
var identityCasesSchema string

//go:embed employment_periods.sql
var employmentPeriodsSchema string

//go:embed employment_evidence.sql
var employmentEvidenceSchema string

func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_workforce", SQL: schema}, {ID: "0002_workforce_enrollment", SQL: enrollmentSchema}, {ID: "0003_workforce_staff_code_casefold", SQL: staffCodeSchema}, {ID: "0004_workforce_hr_masters", SQL: mastersSchema}, {ID: "0005_workforce_shifts", SQL: shiftsSchema}, {ID: "0006_workforce_roster_config", SQL: rosterConfigSchema}, {ID: "0007_workforce_roster_policy", SQL: rosterPolicySchema}, {ID: "0007_workforce_documents", SQL: documentsSchema}, {ID: "0008_workforce_document_worker", SQL: documentWorkerSchema}, {ID: "0009_workforce_rosters", SQL: rostersSchema}, {ID: "0010_workforce_leave_setup", SQL: leaveSetupSchema}, {ID: "0011_workforce_leave_ledger", SQL: leaveLedgerSchema}, {ID: "0012_workforce_leave_requests", SQL: leaveRequestsSchema}, {ID: "0013_workforce_leave_reports", SQL: leaveReportsSchema}, {ID: "0014_workforce_duty_exchanges", SQL: dutyExchangesSchema}, {ID: "0015_workforce_overtime", SQL: overtimeSchema}, {ID: "0016_workforce_attendance", SQL: attendanceSchema}, {ID: "0017_workforce_attendance_corrections", SQL: attendanceCorrectionsSchema}, {ID: "0018_workforce_worker_outbox_scope", SQL: workerOutboxScopeSchema}, {ID: "0019_workforce_attendance_timesheets", SQL: attendanceTimesheetsSchema}, {ID: "0020_workforce_salary_config", SQL: salaryConfigSchema}, {ID: "0021_workforce_statutory_rules", SQL: statutoryRulesSchema}, {ID: "0022_workforce_statutory_pt_lwf", SQL: statutoryPtLwfSchema}, {ID: "0023_workforce_income_tax", SQL: incomeTaxSchema}, {ID: "0024_workforce_salary_assignment", SQL: salaryAssignmentSchema}, {ID: "0025_workforce_staff_identifiers", SQL: staffIdentifiersSchema}, {ID: "0026_workforce_pt_periods", SQL: ptPeriodsSchema}, {ID: "0027_workforce_income_tax_claims", SQL: incomeTaxClaimsSchema}, {ID: "0028_workforce_tax_declarations", SQL: taxDeclarationsSchema}, {ID: "0029_workforce_tax_declaration_evidence", SQL: taxDeclarationEvidenceSchema}, {ID: "0030_workforce_credential_validity", SQL: credentialValiditySchema}, {ID: "0031_workforce_credential_reminders", SQL: credentialRemindersSchema}, {ID: "0032_workforce_roster_credential_clashes", SQL: rosterCredentialClashesSchema}, {ID: "0033_workforce_benefits", SQL: benefitsSchema}, {ID: "0034_workforce_benefit_evidence", SQL: benefitEvidenceSchema}, {ID: "0035_workforce_login_links", SQL: loginLinksSchema}, {ID: "0036_workforce_scoped_leave", SQL: scopedLeaveSchema}, {ID: "0037_workforce_leave_review_discovery_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "workforce_login_links_review_discovery", Table: "workforce_login_links", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "subject"}, {Name: "status"}, {Name: "created_at", Descending: true}, {Name: "id", Descending: true}}}}}, {ID: "0038_workforce_leave_review_queue_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "workforce_leave_requests_review_queue", Table: "workforce_leave_requests", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "hospital_id"}, {Name: "status"}, {Name: "created_at", Descending: true}, {Name: "id", Descending: true}}}}}, {ID: "0039_workforce_supervision", SQL: supervisionSchema}, {ID: "0040_workforce_profile_masters", SQL: profileMastersSchema}, {ID: "0041_workforce_profile_master_indexes", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "workforce_profile_language_ref_idx", Table: "workforce_enrollment_profile", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "hospital_id"}, {Name: "preferred_language_revision_id"}}}, {Name: "workforce_profile_exit_reason_ref_idx", Table: "workforce_enrollment_profile", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "hospital_id"}, {Name: "exit_reason_revision_id"}}}, {Name: "workforce_profile_contract_type_ref_idx", Table: "workforce_enrollment_profile", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "hospital_id"}, {Name: "contract_type_revision_id"}}}}}, {ID: "0042_workforce_qualification_masters", SQL: qualificationMastersSchema}, {ID: "0043_workforce_qualification_master_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "workforce_qualification_master_ref_idx", Table: "workforce_enrollment_qualifications", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "hospital_id"}, {Name: "qualification_revision_id"}}}}}, {ID: "0044_workforce_contact_masters", SQL: contactMastersSchema}, {ID: "0045_workforce_contact_master_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "workforce_contact_master_ref_idx", Table: "workforce_enrollment_emergency_contacts", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "hospital_id"}, {Name: "relationship_revision_id"}}}}}, {ID: "0046_workforce_document_classification", SQL: documentClassificationSchema}, {ID: "0047_workforce_document_recovery", SQL: documentRecoverySchema}, {ID: "0048_workforce_identity_source", SQL: identitySourceSchema}, {ID: "0049_workforce_identity_cases", SQL: identityCasesSchema}, {ID: "0050_workforce_employment_period_drafts", SQL: employmentPeriodsSchema}, {ID: "0051_workforce_employment_evidence", SQL: employmentEvidenceSchema}}
}
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermIdentityChecksManage}, {Permission: PermIdentityChecksReview}, {Permission: PermLoginLinkManage}, {Permission: PermLoginLinkReview}, {Permission: PermHospitalManage}, {Permission: PermEnrollmentManage}, {Permission: PermDocumentManage}, {Permission: PermCredentialReview}, {Permission: PermMasterManage}, {Permission: PermRosterManage}, {Permission: PermRosterCorrect}, {Permission: PermLeaveConfigure}, {Permission: PermLeaveAllocate}, {Permission: PermLeaveCorrect}, {Permission: PermLeaveRequest}, {Permission: PermLeaveApprove}, {Permission: PermDutyRequest}, {Permission: PermDutyApprove}, {Permission: PermOvertimeConfigure}, {Permission: PermOvertimeRequest}, {Permission: PermOvertimeApprove}, {Permission: PermAttendanceConfigure}, {Permission: PermAttendanceRecord}, {Permission: PermAttendanceCorrect}, {Permission: PermAttendanceApprove}, {Permission: PermAttendanceFinalise}, {Permission: PermPayrollConfigure}, {Permission: PermPayrollView}, {Permission: PermPayrollApprove}, {Permission: PermPayrollIdentifiersReveal}, {Permission: PermBenefitsView}, {Permission: PermBenefitsManage}, {Permission: PermBenefitsApprove}, {Permission: PermBenefitsExport}}
}
func (*Module) Publishes() []string {
	return []string{workforcecontract.SubjectIdentityCaseChanged, workforcecontract.SubjectDocumentClassificationChanged, workforcecontract.SubjectAssignmentScopeVerified, LoginLinkChanged, EnrollmentChanged, MasterChanged, ShiftChanged, RosterConfigChanged, RosterSettingsChanged, StaffRosterAssignmentChanged, DocumentScanRequested, DocumentChanged, RosterChanged, LeaveConfigChanged, StaffLeaveAssignmentChanged, LeaveLedgerChanged, LeaveRequestChanged, DutyExchangeChanged, OvertimeSettingsChanged, OvertimeChanged, AttendanceSettingsChanged, AttendancePunchChanged, AttendanceCorrectionChanged, AttendancePeriodChanged, SalaryConfigChanged, StatutoryRuleChanged, SalaryAssignmentChanged, TaxRegimeChanged, StaffIdentifiersChanged, StaffIdentifierRevealed, TaxDeclarationChanged, TaxDeclarationPartyRevealed, CredentialReviewChanged, CredentialValidityChanged, CredentialReminderPolicyChanged, CredentialReminderChanged, BenefitPlanChanged, DependentChanged, BenefitEnrollmentChanged}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(deps platform.Deps) []events.Consumer {
	if deps.Workflows == nil {
		return []events.Consumer{assignmentScopeConsumer(deps.Bus)}
	}
	return []events.Consumer{assignmentScopeConsumer(deps.Bus), DocumentScanConsumer(deps.Workflows), CredentialReminderConsumer(deps.Workflows)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{scopeAuthority: deps.ScopedAuthorizer, db: deps.DB, bus: deps.Bus, documents: deps.Documents, transit: deps.Transit, roles: deps.Roles}
	platform.FilteredListRoute(r.Group("/workforce"), "/leave-review-scopes", authz.Public, 20, h.leaveReviewScopeCandidates, h.authorizedLeaveReviewScopes)
	g := r.Group("/workforce/hospitals")
	// The owner guards the union of current identity manager/reviewer assignments.
	g.GET("/:hospital/identity-staff/:id", authz.Public, h.getIdentitySource)
	g.PUT("/:hospital/identity-staff/:id/checks/:check", authz.Public, h.putIdentityCaseDraft)
	g.GET("/:hospital/identity-staff/:id/checks/:check", authz.Public, h.getIdentityCase)
	platform.ListRoute(g, "/:hospital/identity-staff/:id/checks", authz.Public, h.listIdentityCases)
	platform.ListRoute(g, "/:hospital/identity-staff/:id/checks/:check/history", authz.Public, h.identityCaseHistory)
	platform.ScopedHospitalListRoute(g, "/:hospital/leave-review-requests", PermLeaveApprove, "hospital", h.listLeaveReviewRequests)
	g.GETScopedHospital("/:hospital/staff/:id/leave-review-requests/:request", PermLeaveApprove, "hospital", h.getLeaveReviewRequest)
	g.PUT("/:hospital/staff/:id/login-links/:link", PermLoginLinkManage, h.proposeLoginLink)
	g.POST("/:hospital/staff/:id/login-links/:link/decision", authz.Public, h.decideLoginLink)
	platform.ListRoute(g, "/:hospital/staff/:id/login-links", authz.Public, h.listLoginLinks)
	platform.ListRoute(g, "/:hospital/staff/:id/login-links/:link/history", authz.Public, h.loginLinkHistory)
	g.GET("/:hospital/insights", authz.Public, h.insights)
	platform.ListRoute(g, "", authz.Public, h.listHospitals)
	g.PUT("/:hospital", PermHospitalManage, h.putHospital)
	g.PUT("/:hospital/access/:subject", PermHospitalManage, h.putAccess)
	g.DELETE("/:hospital/access/:subject", PermHospitalManage, h.deleteAccess)
	// Public still requires authentication and current tenant membership.
	// These handlers additionally check hospital grants inside the transaction.
	g.PUT("/:hospital/staff/:id", authz.Public, h.putStaff)
	g.GET("/:hospital/staff/:id", authz.Public, h.getStaff)
	g.GET("/:hospital/staff/:id/enrollment", PermEnrollmentManage, h.getEnrollment)
	g.GET("/:hospital/staff/:id/employment-source", PermEnrollmentManage, h.getEmploymentPeriodSource)
	evidencePath := "/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence"
	g.PUT(evidencePath+"/:binding", PermEnrollmentManage, h.attachEmploymentEvidence)
	g.POST(evidencePath+"/:binding/withdraw", PermEnrollmentManage, h.withdrawEmploymentEvidence)
	g.GET(evidencePath+"/:binding", PermEnrollmentManage, h.getEmploymentEvidence)
	g.GET(evidencePath+"/:binding/content", PermEnrollmentManage, h.downloadEmploymentEvidence)
	platform.ListRoute(g, evidencePath, PermEnrollmentManage, h.listEmploymentEvidence)
	platform.ListRoute(g, evidencePath+"/:binding/history", PermEnrollmentManage, h.employmentEvidenceHistory)

	g.PUT("/:hospital/staff/:id/employment-periods/:period", PermEnrollmentManage, h.putEmploymentPeriodDraft)
	g.GET("/:hospital/staff/:id/employment-periods/:period", PermEnrollmentManage, h.getEmploymentPeriodDraft)
	platform.ListRoute(g, "/:hospital/staff/:id/employment-periods", PermEnrollmentManage, h.listEmploymentPeriodDrafts)
	platform.ListRoute(g, "/:hospital/staff/:id/employment-periods/:period/history", PermEnrollmentManage, h.employmentPeriodHistory)
	g.GET("/:hospital/staff/:id/document-records", PermDocumentManage, h.documentRecords)
	g.PUT("/:hospital/staff/:id/enrollment", PermEnrollmentManage, h.putEnrollment)
	g.GET("/:hospital/staff/:id/documents/:file/classification", PermDocumentManage, h.getDocumentClassification)
	g.PUT("/:hospital/staff/:id/documents/:file/classification", PermDocumentManage, h.putDocumentClassification)
	platform.ListRoute(g, "/:hospital/staff/:id/documents/:file/classification/history", PermDocumentManage, h.documentClassificationHistory)
	g.PUT("/:hospital/staff/:id/documents/:file", PermDocumentManage, h.putDocument)
	g.PUT("/:hospital/staff/:id/documents/:file/content", PermDocumentManage, h.uploadDocument)
	g.GET("/:hospital/staff/:id/documents/:file/content", PermDocumentManage, h.downloadDocument)
	g.PUT("/:hospital/staff/:id/documents/:file/revocation", PermDocumentManage, h.revokeDocument)
	g.PUT("/:hospital/staff/:id/documents/:file/retries/:attempt", PermDocumentManage, h.retryDocument)
	platform.ListRoute(g, "/:hospital/staff/:id/documents", PermDocumentManage, h.listDocuments)
	g.GET("/:hospital/staff/:id/credentials", PermEnrollmentManage, h.listStaffCredentials)
	platform.ListRoute(g, "/:hospital/staff/:id/credentials/:credential/reviews", PermEnrollmentManage, h.credentialReviewHistory)
	g.PUT("/:hospital/staff/:id/credentials/:credential/reviews/:review", PermEnrollmentManage, h.putCredentialReview)
	g.POST("/:hospital/staff/:id/credentials/:credential/reviews/:review/decision", PermCredentialReview, h.decideCredentialReview)
	g.POST("/:hospital/staff/:id/credentials/:credential/reviews/:review/withdraw", PermEnrollmentManage, h.withdrawCredentialReview)
	platform.ListRoute(g, "/:hospital/hr/credentials", PermEnrollmentManage, h.credentialRegister)
	platform.ListRoute(g, "/:hospital/hr/benefits/plans", PermBenefitsView, h.listBenefitPlans)
	g.PUT("/:hospital/hr/benefits/plans/:plan", PermBenefitsManage, h.putBenefitPlan)
	platform.ListRoute(g, "/:hospital/hr/benefits/plans/:plan/history", PermBenefitsView, h.benefitPlanHistory)
	g.GET("/:hospital/hr/benefits/coverage.csv", PermBenefitsExport, h.exportBenefitCoverage)
	platform.ListRoute(g, "/:hospital/staff/:id/dependents", PermBenefitsView, h.listDependents)
	g.PUT("/:hospital/staff/:id/dependents/:dependent", PermBenefitsManage, h.putDependent)
	platform.ListRoute(g, "/:hospital/staff/:id/dependents/:dependent/history", PermBenefitsView, h.dependentHistory)
	platform.ListRoute(g, "/:hospital/staff/:id/benefit-enrollments", PermBenefitsView, h.listBenefitEnrollments)
	platform.ListRoute(g, "/:hospital/staff/:id/benefit-enrollments/:enrollment/history", PermBenefitsView, h.benefitEnrollmentHistory)
	g.PUT("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal", PermBenefitsManage, h.putBenefitProposal)
	g.POST("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/decision", PermBenefitsApprove, h.decideBenefitProposal)
	g.POST("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/withdraw", PermBenefitsManage, h.withdrawBenefitProposal)
	g.GET("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/evidence", PermBenefitsView, h.listBenefitEvidence)
	g.PUT("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/evidence/:file", PermBenefitsManage, h.putBenefitEvidence)
	g.PUT("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/evidence/:file/content", PermBenefitsManage, h.uploadBenefitEvidence)
	g.PUT("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/evidence/:file/retries/:attempt", PermBenefitsManage, h.retryBenefitEvidence)
	g.GET("/:hospital/staff/:id/benefit-enrollments/:enrollment/proposals/:proposal/evidence/:file/content", PermBenefitsView, h.downloadBenefitEvidence)
	g.GET("/:hospital/hr/credential-reminder-policy", PermEnrollmentManage, h.getReminderPolicy)
	g.PUT("/:hospital/hr/credential-reminder-policy", PermEnrollmentManage, h.putReminderPolicy)
	platform.ListRoute(g, "/:hospital/hr/credential-reminders", PermEnrollmentManage, h.listCredentialReminders)
	g.POST("/:hospital/hr/credential-reminders/:reminder/acknowledgement", PermEnrollmentManage, h.acknowledgeCredentialReminder)
	g.PUT("/:hospital/hr/roster-configs/:id", PermRosterManage, h.putRosterConfig)
	platform.ListRoute(g, "/:hospital/hr/roster-configs", authz.Public, h.listRosterConfigs)
	platform.ListRoute(g, "/:hospital/hr/roster-configs/:id/history", authz.Public, h.rosterConfigHistory)
	g.PUT("/:hospital/hr/leave-configs/:id", PermLeaveConfigure, h.putLeaveConfig)
	platform.ListRoute(g, "/:hospital/hr/leave-configs", authz.Public, h.listLeaveConfigs)
	platform.ListRoute(g, "/:hospital/hr/leave-configs/:id/history", authz.Public, h.leaveConfigHistory)
	g.GET("/:hospital/hr/leave-settings", authz.Public, h.getLeaveSettings)
	g.PUT("/:hospital/hr/leave-settings", PermLeaveConfigure, h.putLeaveSettings)
	platform.ListRoute(g, "/:hospital/hr/leave-assignments", authz.Public, h.listStaffLeaveAssignments)
	g.PUT("/:hospital/staff/:id/leave-assignment", PermLeaveAllocate, h.putStaffLeaveAssignment)
	platform.ListRoute(g, "/:hospital/staff/:id/leave-assignment/history", authz.Public, h.staffLeaveAssignmentHistory)
	platform.ListRoute(g, "/:hospital/hr/leave-years", authz.Public, h.listLeaveYears)
	g.PUT("/:hospital/hr/leave-years/:year", PermLeaveAllocate, h.putLeaveYear)
	g.POST("/:hospital/hr/leave-years/:year/postings", PermLeaveAllocate, h.postLeaveMonth)
	g.POST("/:hospital/hr/leave-years/:year/close", PermLeaveAllocate, h.closeLeaveYear)
	platform.ListRoute(g, "/:hospital/hr/leave-years/:year/runs", authz.Public, h.listLeaveRuns)
	platform.ListRoute(g, "/:hospital/hr/leave-balances", authz.Public, h.listLeaveBalances)
	g.GET("/:hospital/staff/:id/leave-balances", authz.Public, h.getStaffLeaveBalances)
	platform.ListRoute(g, "/:hospital/staff/:id/leave-ledger", authz.Public, h.staffLeaveLedger)
	g.PUT("/:hospital/staff/:id/leave-corrections/:entry", PermLeaveCorrect, h.putLeaveCorrection)
	platform.ListRoute(g, "/:hospital/hr/leave-requests", authz.Public, h.listLeaveRequests)
	g.PUT("/:hospital/staff/:id/leave-requests/:request", PermLeaveRequest, h.putLeaveRequest)
	g.GET("/:hospital/staff/:id/leave-requests/:request", authz.Public, h.getLeaveRequest)
	platform.ListRoute(g, "/:hospital/staff/:id/leave-requests/:request/history", authz.Public, h.leaveRequestHistory)
	g.POSTScopedHospital("/:hospital/staff/:id/leave-requests/:request/decision", PermLeaveApprove, "hospital", h.decideLeaveRequest)
	g.POST("/:hospital/staff/:id/leave-requests/:request/cancellation", PermLeaveRequest, h.cancelLeaveRequest)
	g.POST("/:hospital/staff/:id/leave-requests/:request/revocation", PermLeaveApprove, h.revokeLeaveRequest)
	platform.ListRoute(g, "/:hospital/hr/leave-reports/monthly", authz.Public, h.leaveMonthlyReport)
	platform.ListRoute(g, "/:hospital/hr/leave-reports/status", authz.Public, h.leaveStatusReport)
	platform.ListRoute(g, "/:hospital/hr/duty-exchanges", authz.Public, h.listDutyExchanges)
	g.PUT("/:hospital/hr/duty-exchanges/:exchange", PermDutyRequest, h.putDutyExchange)
	g.GET("/:hospital/hr/duty-exchanges/:exchange", authz.Public, h.getDutyExchange)
	platform.ListRoute(g, "/:hospital/hr/duty-exchanges/:exchange/history", authz.Public, h.dutyExchangeHistory)
	g.POST("/:hospital/hr/duty-exchanges/:exchange/decision", PermDutyApprove, h.decideDutyExchange)
	g.POST("/:hospital/hr/duty-exchanges/:exchange/cancellation", PermDutyRequest, h.cancelDutyExchange)
	g.POST("/:hospital/hr/duty-exchanges/:exchange/revocation", PermDutyApprove, h.revokeDutyExchange)
	g.GET("/:hospital/hr/overtime-settings", authz.Public, h.getOvertimeSettings)
	g.PUT("/:hospital/hr/overtime-settings", PermOvertimeConfigure, h.putOvertimeSettings)
	platform.ListRoute(g, "/:hospital/hr/overtime-settings/history", authz.Public, h.overtimeSettingsHistory)
	platform.ListRoute(g, "/:hospital/hr/overtime-summary", authz.Public, h.overtimeSummary)
	platform.ListRoute(g, "/:hospital/hr/overtime", authz.Public, h.listOvertime)
	g.PUT("/:hospital/hr/overtime/:request", PermOvertimeRequest, h.putOvertime)
	g.GET("/:hospital/hr/overtime/:request", authz.Public, h.getOvertime)
	platform.ListRoute(g, "/:hospital/hr/overtime/:request/history", authz.Public, h.overtimeHistory)
	g.POST("/:hospital/hr/overtime/:request/decision", PermOvertimeApprove, h.decideOvertime)
	g.POST("/:hospital/hr/overtime/:request/cancellation", PermOvertimeRequest, h.cancelOvertime)
	g.POST("/:hospital/hr/overtime/:request/revocation", PermOvertimeApprove, h.revokeOvertime)
	g.GET("/:hospital/hr/attendance-settings", authz.Public, h.getAttendanceSettings)
	g.PUT("/:hospital/hr/attendance-settings", PermAttendanceConfigure, h.putAttendanceSettings)
	platform.ListRoute(g, "/:hospital/hr/attendance-settings/history", authz.Public, h.attendanceSettingsHistory)
	platform.ListRoute(g, "/:hospital/hr/attendance/punches", authz.Public, h.listPunches)
	g.PUT("/:hospital/hr/attendance/punches/:punch", PermAttendanceRecord, h.putPunch)
	g.GET("/:hospital/hr/attendance/punches/:punch", authz.Public, h.getPunch)
	g.POST("/:hospital/hr/attendance/punches/:punch/void", PermAttendanceRecord, h.voidPunch)
	platform.ListRoute(g, "/:hospital/hr/attendance", authz.Public, h.listAttendance)
	g.GET("/:hospital/hr/attendance-totals", authz.Public, h.attendanceTotals)
	g.GET("/:hospital/staff/:id/attendance", authz.Public, h.staffAttendance)
	platform.ListRoute(g, "/:hospital/hr/attendance/corrections", authz.Public, h.listCorrections)
	g.PUT("/:hospital/hr/attendance/corrections/:correction", PermAttendanceCorrect, h.putCorrection)
	g.GET("/:hospital/hr/attendance/corrections/:correction", authz.Public, h.getCorrection)
	platform.ListRoute(g, "/:hospital/hr/attendance/corrections/:correction/history", authz.Public, h.correctionHistory)
	platform.ListRoute(g, "/:hospital/hr/payroll/salary-heads", PermPayrollView, h.listSalaryHeads)
	g.PUT("/:hospital/hr/payroll/salary-heads/:id", PermPayrollConfigure, h.putSalaryHead)
	platform.ListRoute(g, "/:hospital/hr/payroll/salary-heads/:id/history", PermPayrollView, h.salaryHeadHistory)
	platform.ListRoute(g, "/:hospital/hr/payroll/salary-structures", PermPayrollView, h.listSalaryStructures)
	g.PUT("/:hospital/hr/payroll/salary-structures/:id", PermPayrollConfigure, h.putSalaryStructure)
	platform.ListRoute(g, "/:hospital/hr/payroll/salary-structures/:id/history", PermPayrollView, h.salaryStructureHistory)
	g.POST("/:hospital/hr/payroll/salary-structures/:id/preview", PermPayrollView, h.previewSalaryStructure)
	platform.ListRoute(g, "/:hospital/hr/payroll/statutory-rules", PermPayrollView, h.listStatutoryRules)
	g.PUT("/:hospital/hr/payroll/statutory-rules/:id", PermPayrollConfigure, h.putStatutoryRule)
	g.POST("/:hospital/hr/payroll/statutory-rules/:id/decision", PermPayrollApprove, h.decideStatutoryRule)
	g.POST("/:hospital/hr/payroll/statutory-rules/:id/withdraw", PermPayrollConfigure, h.withdrawStatutoryRule)
	platform.ListRoute(g, "/:hospital/hr/payroll/statutory-rules/:id/history", PermPayrollView, h.statutoryRuleHistory)
	g.POST("/:hospital/hr/payroll/statutory-preview", PermPayrollView, h.previewStatutory)
	g.POST("/:hospital/hr/payroll/income-tax-preview", PermPayrollView, h.previewIncomeTax)
	platform.ListRoute(g, "/:hospital/hr/payroll/salary-assignments", PermPayrollView, h.listSalaryAssignments)
	g.GET("/:hospital/hr/payroll/staff/:staff/salary-assignment", PermPayrollView, h.getSalaryAssignment)
	g.PUT("/:hospital/hr/payroll/staff/:staff/salary-assignment", PermPayrollConfigure, h.putSalaryAssignment)
	g.POST("/:hospital/hr/payroll/staff/:staff/salary-assignment/decision", PermPayrollApprove, h.decideSalaryAssignment)
	g.POST("/:hospital/hr/payroll/staff/:staff/salary-assignment/withdraw", PermPayrollConfigure, h.withdrawSalaryAssignment)
	g.GET("/:hospital/hr/payroll/staff/:staff/identifiers", PermPayrollView, h.getStaffIdentifiers)
	g.PUT("/:hospital/hr/payroll/staff/:staff/identifiers", PermPayrollConfigure, h.putStaffIdentifiers)
	g.POST("/:hospital/hr/payroll/staff/:staff/identifiers/decision", PermPayrollApprove, h.decideStaffIdentifiers)
	g.POST("/:hospital/hr/payroll/staff/:staff/identifiers/withdraw", PermPayrollConfigure, h.withdrawStaffIdentifiers)
	platform.ListRoute(g, "/:hospital/hr/payroll/staff/:staff/identifiers/history", PermPayrollView, h.staffIdentifierHistory)
	g.POST("/:hospital/hr/payroll/staff/:staff/identifiers/reveal", PermPayrollIdentifiersReveal, h.revealStaffIdentifier)
	platform.ListRoute(g, "/:hospital/hr/payroll/staff/:staff/salary-assignment/history", PermPayrollView, h.salaryAssignmentHistory)
	g.POST("/:hospital/hr/payroll/staff/:staff/salary-preview", PermPayrollView, h.previewStaffSalary)
	platform.ListRoute(g, "/:hospital/hr/payroll/staff/:staff/tax-regimes", PermPayrollView, h.listTaxRegimes)
	g.PUT("/:hospital/hr/payroll/staff/:staff/tax-regimes/:year", PermPayrollConfigure, h.putTaxRegime)
	platform.ListRoute(g, "/:hospital/hr/payroll/staff/:staff/tax-regimes/:year/history", PermPayrollView, h.taxRegimeHistory)
	g.GET("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year", PermPayrollView, h.getTaxDeclaration)
	g.PUT("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year", PermPayrollConfigure, h.putTaxDeclaration)
	g.POST("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/decision", PermPayrollApprove, h.decideTaxDeclaration)
	g.POST("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/withdraw", PermPayrollConfigure, h.withdrawTaxDeclaration)
	platform.ListRoute(g, "/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/history", PermPayrollView, h.taxDeclarationHistory)
	g.POST("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/reveal", PermPayrollIdentifiersReveal, h.revealTaxDeclarationParty)
	g.GET("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/evidence", PermPayrollView, h.listTaxEvidence)
	g.PUT("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/evidence/:file", PermPayrollConfigure, h.putTaxEvidence)
	g.PUT("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/evidence/:file/content", PermPayrollConfigure, h.uploadTaxEvidence)
	g.PUT("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/evidence/:file/retries/:attempt", PermPayrollConfigure, h.retryTaxEvidence)
	g.GET("/:hospital/hr/payroll/staff/:staff/tax-declarations/:year/evidence/:file/content", PermPayrollView, h.downloadTaxEvidence)
	g.POST("/:hospital/hr/attendance/corrections/:correction/decision", PermAttendanceApprove, h.decideCorrection)
	g.POST("/:hospital/hr/attendance/corrections/:correction/cancellation", PermAttendanceCorrect, h.cancelCorrection)
	platform.ListRoute(g, "/:hospital/hr/attendance/periods", authz.Public, h.listPeriods)
	g.PUT("/:hospital/hr/attendance/periods/:period", PermAttendanceFinalise, h.putPeriod)
	g.GET("/:hospital/hr/attendance/periods/:period", authz.Public, h.getPeriod)
	g.POST("/:hospital/hr/attendance/periods/:period/finalisation", PermAttendanceFinalise, h.finalisePeriod)
	g.POST("/:hospital/hr/attendance/periods/:period/reopening", PermAttendanceFinalise, h.reopenPeriod)
	platform.ListRoute(g, "/:hospital/hr/attendance/periods/:period/history", authz.Public, h.periodHistory)
	platform.ListRoute(g, "/:hospital/hr/attendance/periods/:period/timesheets", authz.Public, h.listTimesheets)
	platform.ListRoute(g, "/:hospital/hr/attendance/periods/:period/timesheets/:timesheet/people", authz.Public, h.listTimesheetPeople)
	platform.ListRoute(g, "/:hospital/hr/attendance/periods/:period/timesheets/:timesheet/days", authz.Public, h.listTimesheetDays)
	platform.ListRoute(g, "/:hospital/hr/attendance/punch-report", authz.Public, h.punchReport)
	g.PUT("/:hospital/hr/shifts/:id", PermRosterManage, h.putShift)
	platform.ListRoute(g, "/:hospital/hr/shifts", authz.Public, h.listShifts)
	platform.ListRoute(g, "/:hospital/hr/shifts/:id/history", authz.Public, h.shiftHistory)
	g.GET("/:hospital/hr/roster-settings", authz.Public, h.getRosterSettings)
	g.PUT("/:hospital/hr/roster-settings", PermRosterManage, h.putRosterSettings)
	platform.ListRoute(g, "/:hospital/hr/roster-settings/history", authz.Public, h.rosterSettingsHistory)
	platform.ListRoute(g, "/:hospital/hr/roster-assignments", authz.Public, h.listStaffRosterAssignments)
	g.PUT("/:hospital/staff/:id/roster-assignment", PermRosterManage, h.putStaffRosterAssignment)
	platform.ListRoute(g, "/:hospital/staff/:id/roster-assignment/history", authz.Public, h.staffRosterAssignmentHistory)
	platform.ListRoute(g, "/:hospital/hr/rosters", authz.Public, h.listRosters)
	g.PUT("/:hospital/hr/rosters/:id", PermRosterManage, h.putRoster)
	g.GET("/:hospital/hr/rosters/:id", authz.Public, h.getRoster)
	platform.ListRoute(g, "/:hospital/hr/rosters/:id/allocations", authz.Public, h.rosterAllocations)
	platform.ListRoute(g, "/:hospital/hr/rosters/:id/revisions", authz.Public, h.rosterRevisions)
	g.POST("/:hospital/hr/rosters/:id/changes", PermRosterManage, h.changeRoster)
	g.POST("/:hospital/hr/rosters/:id/populate", PermRosterManage, h.populateRoster)
	g.POST("/:hospital/hr/rosters/:id/freeze", PermRosterManage, h.transitionRoster(rosterTransition{from: "draft", to: "frozen", checkClashes: true}, "frozen"))
	platform.ListRoute(g, "/:hospital/hr/rosters/:id/credential-clashes", PermRosterManage, h.rosterCredentialClashes)
	g.POST("/:hospital/hr/rosters/:id/discard", PermRosterManage, h.transitionRoster(rosterTransition{from: "draft", to: "discarded", needsReason: true, clearsRoster: true}, "discarded"))
	g.POST("/:hospital/hr/rosters/:id/unfreeze", PermRosterCorrect, h.transitionRoster(rosterTransition{from: "frozen", to: "draft", needsReason: true}, "unfrozen"))
	g.PUT("/:hospital/hr/masters/:id", PermMasterManage, h.putMaster)
	platform.ListRoute(g, "/:hospital/hr/masters", authz.Public, h.listMasters)
	platform.ListRoute(g, "/:hospital/hr/masters/:id/history", authz.Public, h.masterHistory)
	platform.ListRoute(g, "/:hospital/staff", authz.Public, h.listStaff)
}
