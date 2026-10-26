package archtest

import (
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// eventPayloadPHIAllowlist is the positive allowlist from
// docs/superpowers/specs/2026-08-14-event-transport-isolation-design.md
// (D4): every payload field, across every contract package, that is
// known to carry clinical or patient-identifying data, keyed
// "<module>.<Type>.<Field>" with the reason it has to be on the wire.
//
// This is deliberately a positive allowlist, not a name-pattern
// denylist. A denylist guesses at names ("Patient*", "*Name"), which
// fails on exactly the field the product needs (this one) and misses
// anything it didn't think to guess — "notes", "reason_for_visit", a
// DOB stored as a timestamp. Every field this test finds is presumed
// PHI-shaped until a human puts it in one of the two maps below; adding
// an entry is therefore a diff a reviewer sees and must justify, not a
// pattern match that can silently start admitting a new field.
var eventPayloadPHIAllowlist = map[string]string{
	"mediconnect.ClaimCheckRequest.Relationship":          "exact relationship bound to patient/subject in workload-authenticated private owner HTTP only; never broadcast or logged",
	"mediconnect.ClaimCheckRequest.Subject":               "exact owner-subject selection in workload-authenticated internal HTTP only; never published as an event",
	"mediconnect.ClaimCheckRequest.PatientID":             "exact patient selection in workload-authenticated internal HTTP only; never published as an event",
	"mediconnect.RegistrationReviewData.RequesterSubject": "opaque authenticated requester identity is required solely to reject registrar self-review across owners; no contact or identity evidence; restricted same-tenant consumer",
	"mediconnect.RegistrationReviewData.Relationship":     "unverified allowlisted relationship claim is necessary for hospital evidence review and immutable request lineage; never proxy authority or consent; no free text",

	"medicore.VisitCreatedData.PatientName": "pharmacy and lab display it on their work queues; modules may not import modules, so there is no cross-module read path to fetch it instead",
}

// eventPayloadReviewedNonPHI is the second half of the same decision:
// fields a human has confirmed do NOT carry PHI or identifying data.
// It exists so the classifier does not have
// to guess at "enum-shaped" or "operational" strings by type or name
// pattern — Go's type system cannot tell RoleKey from PatientName, both
// are plain `string` fields, so the distinction is recorded here
// instead of inferred.
var eventPayloadReviewedNonPHI = map[string]string{
	"practitioner.EvidenceScanInput.ScanAttempt":            "bounded internal processing attempt counter; no private attributes",
	"practitioner.EvidenceScanRequestedData.ScanAttempt":    "bounded internal processing attempt counter; no private attributes",
	"practitioner.EvidenceChangedData.ScanAttempt":          "bounded internal processing attempt counter; no private attributes",
	"practitioner.ReviewRecordedData.PractitionerID":        "opaque owner review scope reference; source finding and evidence stay private",
	"practitioner.ReviewRecordedData.RegistrationID":        "opaque owner review scope reference; source finding and evidence stay private",
	"practitioner.ReviewRecordedData.RevisionID":            "opaque owner review scope reference; source finding and evidence stay private",
	"practitioner.ReviewRecordedData.ReviewID":              "opaque owner review scope reference; source finding and evidence stay private",
	"practitioner.EvidenceScanInput.TenantID":               "internal tenant boundary for private professional-evidence processing; no tenant attributes",
	"practitioner.EvidenceScanInput.PractitionerID":         "internal professional registry reference; no patient, professional attributes or national identifier",
	"practitioner.EvidenceScanInput.RegistrationID":         "internal reported-claim reference; council, number and dates remain private",
	"practitioner.EvidenceScanInput.RevisionID":             "immutable internal claim revision reference; no claim content",
	"practitioner.EvidenceScanInput.FileID":                 "internal upload intent reference; no filename, object path or bytes",
	"practitioner.EvidenceChangedData.PractitionerID":       "internal professional registry reference for scoped metadata refresh; no patient or professional attributes",
	"practitioner.EvidenceChangedData.RegistrationID":       "internal reported-claim reference; no council, number or dates",
	"practitioner.EvidenceChangedData.RevisionID":           "immutable internal claim revision reference; no claim content or verification outcome",
	"practitioner.EvidenceChangedData.FileID":               "internal upload intent reference; no filename, object path, checksum or bytes",
	"practitioner.EvidenceChangedData.Status":               "closed file transport/scanning lifecycle enum; confers no professional verification or clinical privilege",
	"practitioner.EvidenceScanRequestedData.PractitionerID": "internal professional registry reference for scoped credential scanning; no patient or professional attributes or national identifier",
	"practitioner.EvidenceScanRequestedData.RegistrationID": "internal reported-claim row reference; council, registration number and dates remain private",
	"practitioner.EvidenceScanRequestedData.RevisionID":     "immutable internal claim revision reference; no claim content or verification outcome",
	"practitioner.EvidenceScanRequestedData.FileID":         "immutable upload intent reference; no filename, storage key, checksum or bytes",
	"patientmaster.ProxyAuthorityChangedData.AuthorityID":   "opaque tenant-local guardian-review receipt; no patient, subject, relationship or evidence; resolution requires owner authorization",
	"patientmaster.ProxyAuthorityChangedData.Version":       "monotonic registry revision for duplicate/replay ordering; no identity or clinical content",
	"patientmaster.SharingRecipientChangedData.RecipientID": "opaque reviewed recipient binding; no identity or evidence; scoped owner lookup required",
	"patientmaster.SharingRecipientChangedData.Version":     "monotonic binding revision for invalidation only",
	"patientmaster.ClinicalConsentChangedData.ConsentID":    "opaque declaration identifier; no patient, recipient, purpose or content; scoped owner lookup required",
	"patientmaster.ClinicalConsentChangedData.Version":      "monotonic retained declaration state revision for invalidation only",
	"mediconnect.ClaimCheckRequest.TenantID":                "opaque tenant selector for private owner lookup; requires audience-bound owner identity and exact tuple RLS",
	"mediconnect.ClaimCheckRequest.RequestID":               "opaque known claim identifier for a private exact lookup, not an account search or event",
	"mediconnect.ClaimCheckRequest.HospitalID":              "opaque scope identifier in private owner lookup; does not confer hospital authority",
	"mediconnect.ClaimCheckResponse.Current":                "predicate on a known intake claim, not identity verification, clinical consent or disclosure authority",
	"patientmaster.SharingNoticeChangedData.NoticeID":       "opaque hospital notice identifier; no content, evidence, actor or patient; owner access remains scoped",
	"patientmaster.SharingNoticeChangedData.Version":        "monotonic notice revision for replay ordering; no clinical or identifying content",
	"patientmaster.ConsentAuthorityChangedData.AuthorityID": "opaque tenant-local reviewed-capacity receipt; no patient, subject, policy, evidence or clinical content; resolving it requires owner authorization",
	"patientmaster.ConsentAuthorityChangedData.Version":     "monotonic receipt revision for duplicate/replay ordering; no clinical or identity content",
	"mediconnect.AccountConsentData.ConsentID":              "opaque account declaration receipt reference; no clinical or legal eligibility inference",
	"mediconnect.AccountConsentData.AccountID":              "opaque account identifier for owner-scoped receipt delivery; no contact or demographics",
	"mediconnect.AccountConsentData.NoticeID":               "opaque tenant notice version reference; excludes notice content and retained review evidence",
	"mediconnect.AccountConsentData.Purpose":                "allowlisted account-processing, communications or analytics label only; no clinical, diagnostic, treatment or proxy purpose",

	"mediconnect.RegistrationReviewData.RequestID":      "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"mediconnect.RegistrationReviewData.AccountID":      "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"mediconnect.RegistrationReviewData.HospitalID":     "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"mediconnect.RegistrationReviewData.PatientID":      "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"patientmaster.RegistrationReviewedData.RequestID":  "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"patientmaster.RegistrationReviewedData.AccountID":  "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"patientmaster.RegistrationReviewedData.HospitalID": "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"patientmaster.RegistrationReviewedData.PatientID":  "opaque internal reference for tenant/hospital-scoped identity review lineage; no demographic or external identifier; resolution requires owner authorization",
	"patientmaster.RegistrationReviewedData.DecisionID": "immutable identity-review receipt metadata for replay and withdrawal ordering; no evidence content, clinical decision or consent",
	"patientmaster.RegistrationReviewedData.Outcome":    "immutable identity-review receipt metadata for replay and withdrawal ordering; no evidence content, clinical decision or consent",
	"patientmaster.RegistrationReviewedData.ReviewedAt": "immutable identity-review receipt metadata for replay and withdrawal ordering; no evidence content, clinical decision or consent",

	"mediconnect.AccountCreatedData.AccountID":          "internally minted opaque account UUID; no subject, contact, patient linkage or clinical content; resolution requires owner authorization",
	"emergency.DispatchChangedData.BaseID":              "opaque provider operating-base UUID; no address or name",
	"emergency.DispatchChangedData.DispatchID":          "opaque trip UUID requiring protected lookup; no patient, vehicle, crew or destination details",
	"emergency.DispatchChangedData.Version":             "monotonic operational revision, not clinical content",
	"privacy.DeadlineChangedData.RequestID":             "opaque tenant-local case reference for deadline processing; no subject, clinical content or external identifier; resolution requires owner authorization",
	"privacy.DeadlineChangedData.Version":               "monotonic case revision used for durable clock reconciliation; no patient content",
	"privacy.DeadlineWorkflowInput.TenantID":            "opaque internal tenant UUID for worker transaction isolation; no organization or patient profile",
	"privacy.DeadlineWorkflowInput.RequestID":           "opaque internal case UUID for deadline reconciliation; no subject, contact or case content; resolving protected case data requires owner authorization",
	"emergency.DispatchWorkflowInput.TenantID":          "opaque tenant UUID for the transaction boundary; no organization or patient profile",
	"emergency.DispatchWorkflowInput.BaseID":            "opaque operating-base UUID for the worker boundary; no location",
	"emergency.DispatchWorkflowInput.DispatchID":        "opaque trip UUID for workflow identity; protected data remains in the database",
	"emergency.ReceivingChangedData.BaseID":             "opaque provider scope returned only through an approved directed relationship",
	"emergency.ReceivingChangedData.DispatchID":         "opaque trip reference; no caller, patient, contact, location or hospital name",
	"emergency.ReceivingChangedData.RequestID":          "opaque pre-alert UUID used to validate committed lineage, not clinical communication content",
	"emergency.ReceivingChangedData.Version":            "decision revision for replay/order validation, not personal data",
	"emergency.ClinicalAccessChangedData.BaseID":        "opaque operating-base scope; no facility identity",
	"emergency.ClinicalAccessChangedData.GrantID":       "opaque verification record reference; no staff subject or evidence reference",
	"emergency.ClinicalAccessChangedData.Version":       "append-only verification version; no professional or clinical attributes",
	"emergency.CaseAssessedData.BaseID":                 "opaque operating-base scope; no address or facility name",
	"emergency.CaseAssessedData.CaseID":                 "opaque internal case reference requiring an authorized lookup",
	"emergency.CaseAssessedData.AssessmentID":           "opaque assessment record reference; no transport class, policy or clinical finding",
	"emergency.CaseAssessedData.Sequence":               "case-local ordering counter; no clinical priority or patient attributes",
	"emergency.FleetChangedData.BaseID":                 "opaque operating-base scope; no hospital identity or address",
	"emergency.FleetChangedData.ResourceID":             "opaque fleet mutation reference; no registration, staff subject or qualification",
	"emergency.FleetChangedData.Operation":              "fixed fleet mutation vocabulary; no measurements, location or verification evidence",
	"emergency.CaseChangedData.BaseID":                  "opaque operating-base scope; no address, facility name or staff identity",
	"emergency.CaseChangedData.CaseID":                  "opaque internal case reference requiring authorized lookup; no patient identity or clinical content",
	"emergency.CaseChangedData.Version":                 "monotonic version for idempotent invalidation; no patient attributes",
	"emergency.CaseChangedData.Status":                  "bounded received/closed lifecycle; no triage priority, clinical outcome or narrative",
	"supply.OperationalAccessLimitsUpdatedData.PartyID": "opaque organisation policy owner reference; no identities, limit values or reasons",
	"supply.OperationalAccessLimitsUpdatedData.Version": "monotonic operational limit revision; no grant, member or support attributes",
	"supply.CorporateUpdatedData.PartyID":               "opaque corporate-structure owner reference; no parent legal identity or contacts",
	"supply.CorporateUpdatedData.Version":               "monotonic corporate structure revision without attributes",
	"supply.TerritoryUpdatedData.PartyID":               "opaque owning organisation reference; no legal entity data",
	"supply.TerritoryUpdatedData.TerritoryID":           "opaque territory reference; no geography or assigned representative identities",
	"supply.TerritoryUpdatedData.Version":               "monotonic territory revision without assignment payload",
	"supply.SupportAccessChangedData.PartyID":           "opaque organisation scope; no legal or personal data",
	"supply.SupportAccessChangedData.SupportID":         "opaque support request reference; no beneficiary, ticket or reason",
	"supply.SupportAccessChangedData.Version":           "monotonic support decision revision without approval evidence",
	"supply.SupportAccessChangedData.Operation":         "fixed request/decision lifecycle operation; no user-provided reason",
	"supply.DeliveryTaskChangedData.PartyID":            "opaque task-owning organisation reference; no customer identity",
	"supply.DeliveryTaskChangedData.PremiseID":          "opaque dispatch-location reference; no address",
	"supply.DeliveryTaskChangedData.DeliveryID":         "opaque logistics-task reference; no destination, contact or handling details",
	"supply.DeliveryTaskChangedData.Version":            "monotonic task revision without quantities or delivery attributes",
	"supply.DeliveryTaskChangedData.Operation":          "fixed logistics lifecycle operation; no driver or business contact identity",

	"supply.BinUpdatedData.PartyID":                            "opaque owning organisation reference; no legal identity, contacts or grant recipient",
	"supply.BinUpdatedData.PremiseID":                          "opaque storage-premise reference; no address, name or location attributes",
	"supply.BinUpdatedData.BinID":                              "opaque bin reference within an organisation/premise; no patient, product, stock or storage labels",
	"supply.BinUpdatedData.Version":                            "monotonic configuration revision; no bin attributes or operational quantities",
	"workspace.CountryPolicyChangedData.ScopeID":               "opaque country-configuration scope reference; it does not identify a patient, staff member or clinical record",
	"workspace.CountryPolicyChangedData.VersionID":             "opaque immutable configuration-version reference; no source content or patient information",
	"workspace.CountryPolicyChangedData.Action":                "fixed policy lifecycle action enum; no review rationale, actor identity or clinical data",
	"workspace.CountryPolicyChangedData.Revision":              "monotonic configuration revision, not a patient measure or identifier",
	"supply.PremiseUpdatedData.PartyID":                        "opaque organisation scope; no legal identity or address",
	"supply.PremiseUpdatedData.PremiseID":                      "opaque premises reference; no location or contact data",
	"supply.PremiseUpdatedData.Version":                        "monotonic revision; no declared premises attributes",
	"supply.LicenseDeclaredData.PartyID":                       "opaque organisation scope; no business identity",
	"supply.LicenseDeclaredData.LicenseID":                     "opaque licence reference; no licence number, authority or evidence",
	"supply.LicenseDeclaredData.Version":                       "monotonic declaration revision; not proof of validity or approval",
	"supply.PartyUpdatedData.PartyID":                          "opaque private supplier reference; no legal identity, address, contacts or tax numbers",
	"supply.PartyUpdatedData.Version":                          "monotonic profile revision for invalidation; no business or personal attributes",
	"supply.PartyAccessChangedData.PartyID":                    "opaque profile scope; private lookup still requires a current explicit grant",
	"supply.PartyAccessChangedData.AccessID":                   "opaque access-record reference; no user identity or role is published",
	"supply.PartyAccessChangedData.Operation":                  "bounded granted/revoked operation; no recipient or permission details",
	"pharmacy.PayableRecordedData.PharmacyID":                  "opaque pharmacy scope; no payee identity or prices",
	"pharmacy.PayableRecordedData.OutletID":                    "opaque outlet scope; protected lookup required",
	"pharmacy.PayableRecordedData.PayableID":                   "opaque document identity; no financial amounts",
	"pharmacy.SettlementRecordedData.PharmacyID":               "opaque pharmacy scope; no financial details",
	"pharmacy.SettlementRecordedData.OutletID":                 "opaque outlet scope; no hospital or patient identity",
	"pharmacy.SettlementRecordedData.PayableID":                "opaque payable identity for idempotent downstream posting",
	"pharmacy.SettlementRecordedData.SettlementID":             "opaque settlement identity; no payer, payment method, reference or amount",
	"pharmacy.CatalogueUpdatedData.PharmacyID":                 "opaque owning pharmacy reference; does not confer tenant/outlet access",
	"pharmacy.StockMovementRecordedData.PharmacyID":            "opaque pharmacy ownership scope; no hospital or patient identity",
	"pharmacy.StockMovementRecordedData.OutletID":              "opaque outlet scope; no hospital demographics",
	"pharmacy.StockMovementRecordedData.MovementID":            "opaque quarantined-stock movement reference; no patient or financial details",
	"pharmacy.StockMovementRecordedData.LocationID":            "opaque inventory location reference; requires protected lookup",
	"pharmacy.StockMovementRecordedData.BatchID":               "opaque batch reference; no quantities, drug identity or expiry details",
	"pharmacy.BatchSafetyRecordedData.PharmacyID":              "opaque pharmacy scope; no business, hospital or patient identity",
	"pharmacy.BatchSafetyRecordedData.OutletID":                "opaque outlet scope; protected lookup requires a live grant",
	"pharmacy.BatchSafetyRecordedData.BatchID":                 "opaque lot reference; no product, expiry, temperature or concern content",
	"pharmacy.BatchSafetyRecordedData.IncidentID":              "opaque immutable concern reference; no narrative, actor or observation time",
	"pharmacy.CatalogueUpdatedData.OutletID":                   "opaque outlet scope; protected product lookup still requires a current outlet grant",
	"pharmacy.CatalogueUpdatedData.ProductID":                  "opaque private-product reference; no patient, prescriber or commercial attributes",
	"pharmacy.CatalogueUpdatedData.Version":                    "monotonic catalogue revision for replay and invalidation",
	"pharmacy.CatalogueUpdatedData.Status":                     "allowlisted product lifecycle state; no clinical decision or patient data",
	"workforce.DocumentScanInput.TenantID":                     "internal tenancy scope for processing; no organization attributes",
	"workforce.DocumentScanInput.HospitalID":                   "opaque origin hospital scope; no facility attributes",
	"workforce.DocumentScanInput.StaffID":                      "opaque protected owner reference; no personnel facts",
	"workforce.DocumentScanInput.FileID":                       "opaque immutable intent reference; no filename or content",
	"workforce.DocumentScanInput.ScanAttempt":                  "deterministic processing attempt; no personnel facts",
	"workforce.DocumentChangedData.HospitalID":                 "opaque hospital scope; no facility attributes",
	"workforce.DocumentChangedData.StaffID":                    "opaque workforce owner reference; no employment or health attributes",
	"workforce.DocumentChangedData.FileID":                     "opaque immutable file reference; no filename or storage location",
	"workforce.DocumentChangedData.ScanAttempt":                "processing attempt for deduplication; no content or personnel facts",
	"workforce.DocumentChangedData.Status":                     "bounded file processing outcome only; not a clinical result or employee outcome",
	"workforce.DocumentScanRequestedData.ScanAttempt":          "processing attempt for deterministic workflow identity",
	"workforce.DocumentScanRequestedData.HospitalID":           "internal hospital scope; no facility attributes",
	"workforce.DocumentScanRequestedData.StaffID":              "opaque workforce row reference; protected lookup requires exact tenant/hospital file scope",
	"workforce.DocumentScanRequestedData.FileID":               "internal immutable upload intent reference; no name, filename, storage key or content",
	"workforce.RosterConfigChangedData.HospitalID":             "opaque hospital scope; protected lookup required",
	"workforce.RosterConfigChangedData.ConfigID":               "opaque configuration reference; no scheduling rules or personnel data",
	"workforce.RosterConfigChangedData.RevisionID":             "immutable internal revision reference; no configuration content",
	"workforce.RosterConfigChangedData.Version":                "monotonic revision for replay/order checks",
	"workforce.LeaveConfigChangedData.HospitalID":              "opaque hospital scope; protected lookup required",
	"workforce.LeaveConfigChangedData.ConfigID":                "opaque configuration reference; no leave rules or personnel data",
	"workforce.LeaveConfigChangedData.RevisionID":              "immutable internal revision reference; no configuration content",
	"workforce.LeaveConfigChangedData.Kind":                    "closed enum of configuration categories; no content",
	"workforce.LeaveConfigChangedData.Version":                 "monotonic revision for replay/order checks",
	"workforce.StaffLeaveAssignmentChangedData.HospitalID":     "opaque hospital scope; protected lookup required",
	"workforce.StaffLeaveAssignmentChangedData.StaffID":        "opaque staff reference; no name, code or leave content",
	"workforce.StaffLeaveAssignmentChangedData.RevisionID":     "immutable internal revision reference; protected lookup required",
	"workforce.StaffLeaveAssignmentChangedData.Version":        "monotonic revision for replay/order checks",
	"workforce.LeaveLedgerChangedData.HospitalID":              "opaque hospital scope; protected lookup required",
	"workforce.LeaveLedgerChangedData.LeaveYear":               "leave-year label for re-reading balances; no personnel facts",
	"workforce.LeaveLedgerChangedData.Source":                  "closed enum posting/year_end/correction/request; no content",
	"workforce.LeaveLedgerChangedData.ReferenceID":             "opaque run or entry reference; no staff, amounts or reasons",
	"workforce.LeaveLedgerChangedData.Entries":                 "entry count for reconciliation; no staff or amounts",
	"workforce.LeaveRequestChangedData.HospitalID":             "opaque hospital scope; protected lookup required",
	"workforce.LeaveRequestChangedData.StaffID":                "opaque staff reference; no name, dates, type or reason",
	"workforce.LeaveRequestChangedData.RequestID":              "opaque request reference; protected lookup required",
	"workforce.LeaveRequestChangedData.Status":                 "closed enum pending/approved/rejected/cancelled; no content",
	"workforce.LeaveRequestChangedData.Version":                "monotonic request version for replay/order checks",
	"workforce.DutyExchangeChangedData.HospitalID":             "opaque hospital scope; protected lookup required",
	"workforce.DutyExchangeChangedData.ExchangeID":             "opaque exchange reference; no people, dates or reason",
	"workforce.DutyExchangeChangedData.Status":                 "closed enum pending/approved/rejected/cancelled/revoked; no content",
	"workforce.DutyExchangeChangedData.Version":                "monotonic exchange version for replay/order checks",
	"workforce.OvertimeSettingsChangedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.OvertimeSettingsChangedData.RevisionID":         "immutable internal revision reference; no limits or policy content",
	"workforce.OvertimeSettingsChangedData.Version":            "monotonic revision for replay/order checks",
	"workforce.OvertimeChangedData.HospitalID":                 "opaque hospital scope; protected lookup required",
	"workforce.OvertimeChangedData.RequestID":                  "opaque request reference; no person, times, minutes or reason",
	"workforce.OvertimeChangedData.Status":                     "closed enum pending/approved/rejected/cancelled/revoked; no content",
	"workforce.OvertimeChangedData.Version":                    "monotonic request version for replay/order checks",
	"workforce.AttendanceSettingsChangedData.HospitalID":       "opaque hospital scope; protected lookup required",
	"workforce.AttendanceSettingsChangedData.RevisionID":       "immutable internal revision reference; no policy content",
	"workforce.AttendanceSettingsChangedData.Version":          "monotonic revision for replay/order checks",
	"workforce.AttendancePunchChangedData.HospitalID":          "opaque hospital scope; protected lookup required",
	"workforce.AttendancePunchChangedData.PunchID":             "opaque punch reference; no person, time, direction or reason",
	"workforce.AttendancePunchChangedData.Action":              "closed enum recorded/voided; no content",
	"workforce.AttendanceCorrectionChangedData.HospitalID":     "opaque hospital scope; protected lookup required",
	"workforce.AttendanceCorrectionChangedData.CorrectionID":   "opaque correction reference; no person, time, direction or reason",
	"workforce.AttendanceCorrectionChangedData.Status":         "closed enum pending/approved/rejected/cancelled; no content",
	"workforce.AttendanceCorrectionChangedData.Version":        "monotonic request version for replay/order checks",
	"workforce.AttendancePeriodChangedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.AttendancePeriodChangedData.PeriodID":           "opaque period reference; no dates, people or figures",
	"workforce.AttendancePeriodChangedData.Status":             "closed enum finalised/reopened; no content",
	"workforce.AttendancePeriodChangedData.Version":            "monotonic period version for replay/order checks",
	"workforce.AttendancePeriodChangedData.TimesheetID":        "opaque timesheet version reference; figures stay behind the API",
	"workforce.SalaryConfigChangedData.HospitalID":             "opaque hospital scope; protected lookup required",
	"workforce.SalaryConfigChangedData.ConfigID":               "opaque head or structure reference; no amounts, rates or labels",
	"workforce.SalaryConfigChangedData.RevisionID":             "immutable internal revision reference; no configuration content",
	"workforce.SalaryConfigChangedData.Kind":                   "closed enum salary_head/salary_structure; no content",
	"workforce.SalaryConfigChangedData.Version":                "monotonic revision for replay/order checks",
	"workforce.StatutoryRuleChangedData.HospitalID":            "opaque hospital scope; protected lookup required",
	"workforce.StatutoryRuleChangedData.RuleID":                "opaque statutory rule reference; no rates, ceilings or citations",
	"workforce.StatutoryRuleChangedData.RevisionID":            "immutable internal proposal reference; no rule content",
	"workforce.StatutoryRuleChangedData.Kind":                  "closed enum pf/esi; no content",
	"workforce.StatutoryRuleChangedData.Status":                "closed enum proposed/approved/rejected/withdrawn; no notes",
	"workforce.StatutoryRuleChangedData.Version":               "monotonic approved version for replay/order checks",
	"workforce.SalaryAssignmentChangedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.SalaryAssignmentChangedData.StaffID":            "opaque staff reference; no name, code, structure or amounts",
	"workforce.SalaryAssignmentChangedData.RevisionID":         "immutable internal proposal reference; no pay content",
	"workforce.SalaryAssignmentChangedData.Status":             "closed enum proposed/approved/rejected/withdrawn; no reasons or notes",
	"workforce.SalaryAssignmentChangedData.Version":            "monotonic approved version for replay/order checks",
	"workforce.TaxRegimeChangedData.HospitalID":                "opaque hospital scope; protected lookup required",
	"workforce.TaxRegimeChangedData.StaffID":                   "opaque staff reference; no name, code or regime",
	"workforce.TaxRegimeChangedData.TaxYear":                   "tax-year label for re-reading the election; no regime or note",
	"workforce.TaxRegimeChangedData.Version":                   "monotonic election version for replay/order checks",
	"workforce.StaffIdentifiersChangedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.StaffIdentifiersChangedData.StaffID":            "opaque staff reference; no name, account, PAN or UAN",
	"workforce.StaffIdentifiersChangedData.RevisionID":         "immutable internal proposal reference; no identifier values or masks",
	"workforce.StaffIdentifiersChangedData.Status":             "closed enum proposed/approved/rejected/withdrawn; no reasons or notes",
	"workforce.StaffIdentifiersChangedData.Version":            "monotonic approved version for replay/order checks",
	"workforce.StaffIdentifierRevealedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.StaffIdentifierRevealedData.StaffID":            "opaque staff reference; no name or revealed value",
	"workforce.StaffIdentifierRevealedData.RevisionID":         "immutable internal revision reference; no identifier values or masks",
	"workforce.StaffIdentifierRevealedData.Field":              "closed enum bank_account/pan/uan naming what was viewed, never the value or the stated reason",
	"workforce.CredentialReviewChangedData.HospitalID":         "opaque hospital scope; protected lookup required",
	"workforce.CredentialReviewChangedData.StaffID":            "opaque staff reference; no name, licence number, issuer or dates",
	"workforce.CredentialReviewChangedData.CredentialID":       "opaque enrollment credential reference; no number, issuer or dates",
	"workforce.CredentialReviewChangedData.ReviewID":           "immutable internal review reference; no proposed values or reasons",
	"workforce.CredentialReviewChangedData.Status":             "closed enum pending/approved/rejected/withdrawn; no reasons or notes",
	"workforce.CredentialReviewChangedData.Version":            "monotonic reviewed version for replay/order checks",
	"workforce.CredentialValidityChangedData.HospitalID":       "opaque hospital scope; protected lookup required",
	"workforce.CredentialValidityChangedData.StaffID":          "opaque staff reference; no name, licence number, issuer or dates",
	"workforce.CredentialValidityChangedData.CredentialID":     "opaque enrollment credential reference; no number, issuer or dates",
	"workforce.CredentialValidityChangedData.ReviewID":         "immutable internal review reference; no proposed values or reasons",
	"workforce.CredentialValidityChangedData.Version":          "monotonic reviewed version for replay/order checks",
	"workforce.CredentialReminderPolicyChangedData.HospitalID": "opaque hospital scope; thresholds stay in the database",
	"workforce.CredentialReminderPolicyChangedData.Version":    "monotonic policy version for replay/order checks",
	"workforce.CredentialReminderChangedData.HospitalID":       "opaque hospital scope; protected lookup required",
	"workforce.CredentialReminderChangedData.StaffID":          "opaque staff reference; no name, licence number, issuer or dates",
	"workforce.CredentialReminderChangedData.CredentialID":     "opaque enrollment credential reference; no number, issuer or dates",
	"workforce.CredentialReminderChangedData.ReminderID":       "opaque reminder reference; no due date or acknowledger",
	"workforce.CredentialReminderChangedData.Status":           "closed lifecycle enum: open, acknowledged or superseded",
	"workforce.CredentialReminderChangedData.ThresholdDays":    "policy threshold bucket (0-365); not a date and not about a person",
	"workforce.CredentialReminderChangedData.Version":          "monotonic reviewed version for replay/order checks",
	"workforce.BenefitPlanChangedData.HospitalID":              "opaque hospital scope; protected lookup required",
	"workforce.BenefitPlanChangedData.PlanID":                  "opaque plan reference; no plan name, kind or dates",
	"workforce.BenefitPlanChangedData.RevisionID":              "immutable internal revision reference; no plan content",
	"workforce.BenefitPlanChangedData.Version":                 "monotonic plan version for replay/order checks",
	"workforce.BenefitPlanChangedData.Status":                  "closed enum active/retired; not about a person",
	"workforce.DependentChangedData.HospitalID":                "opaque hospital scope; protected lookup required",
	"workforce.DependentChangedData.StaffID":                   "opaque staff reference; no name",
	"workforce.DependentChangedData.DependentID":               "opaque dependent reference; no name, relationship, birth date or phone",
	"workforce.DependentChangedData.RevisionID":                "immutable internal revision reference; no dependent content or reason",
	"workforce.DependentChangedData.Version":                   "monotonic dependent version for replay/order checks",
	"workforce.DependentChangedData.Status":                    "closed enum active/removed; no reason",
	"workforce.BenefitEnrollmentChangedData.HospitalID":        "opaque hospital scope; protected lookup required",
	"workforce.BenefitEnrollmentChangedData.StaffID":           "opaque staff reference; no name",
	"workforce.BenefitEnrollmentChangedData.EnrollmentID":      "opaque enrollment reference; no plan, covered person or dates",
	"workforce.BenefitEnrollmentChangedData.ProposalID":        "immutable internal proposal reference; no eligibility basis or notes",
	"workforce.BenefitEnrollmentChangedData.Status":            "closed enum pending/approved/rejected/withdrawn; no reasons or notes",
	"workforce.BenefitEnrollmentChangedData.Version":           "monotonic coverage version for replay/order checks",
	"workforce.CredentialReminderInput.TenantID":               "workflow input; opaque tenant scope",
	"workforce.CredentialReminderInput.HospitalID":             "workflow input; opaque hospital scope",
	"workforce.CredentialReminderInput.StaffID":                "workflow input; opaque staff reference, no name or licence data",
	"workforce.CredentialReminderInput.CredentialID":           "workflow input; opaque credential reference, no number, issuer or dates",
	"workforce.CredentialReminderInput.Version":                "workflow input; reviewed revision number",
	"workforce.CredentialReminderStep.Done":                    "workflow result; schedule finished flag",
	"workforce.CredentialReminderStep.WakeAt":                  "workflow result; next wake instant, a threshold midnight not a personal date",
	"workforce.TaxDeclarationChangedData.HospitalID":           "opaque hospital scope; protected lookup required",
	"workforce.TaxDeclarationChangedData.StaffID":              "opaque staff reference; no name, claim amount, landlord, lender or PAN",
	"workforce.TaxDeclarationChangedData.TaxYear":              "YYYY-YY tax year the declaration covers; no amounts",
	"workforce.TaxDeclarationChangedData.RevisionID":           "immutable internal proposal reference; no declared values or masks",
	"workforce.TaxDeclarationChangedData.Status":               "closed enum proposed/approved/rejected/withdrawn; no reasons or notes",
	"workforce.TaxDeclarationChangedData.Version":              "monotonic approved version for replay/order checks",
	"workforce.TaxDeclarationPartyRevealedData.HospitalID":     "opaque hospital scope; protected lookup required",
	"workforce.TaxDeclarationPartyRevealedData.StaffID":        "opaque staff reference; no name or revealed value",
	"workforce.TaxDeclarationPartyRevealedData.TaxYear":        "YYYY-YY tax year of the revision viewed",
	"workforce.TaxDeclarationPartyRevealedData.RevisionID":     "immutable internal revision reference; no PAN or mask",
	"workforce.TaxDeclarationPartyRevealedData.Role":           "closed enum landlord/lender naming whose PAN was viewed, never the party's name or PAN",
	"workforce.TaxDeclarationPartyRevealedData.Position":       "party slot 1-4 within the revision; no name, address or PAN",
	"workforce.RosterSettingsChangedData.HospitalID":           "opaque hospital scope; protected lookup required",
	"workforce.RosterSettingsChangedData.RevisionID":           "immutable internal revision reference; no timezone or policy content",
	"workforce.RosterSettingsChangedData.Version":              "monotonic revision for replay/order checks",
	"workforce.StaffRosterAssignmentChangedData.HospitalID":    "opaque hospital scope; protected lookup required",
	"workforce.StaffRosterAssignmentChangedData.StaffID":       "opaque staff reference; no name, code or schedule content",
	"workforce.StaffRosterAssignmentChangedData.RevisionID":    "immutable internal revision reference; protected lookup required",
	"workforce.StaffRosterAssignmentChangedData.Version":       "monotonic revision for replay/order checks",
	"supply.LicenseReviewChangedData.PartyID":                  "opaque private organisation scope; protected lookup required, no legal name or business registration",
	"supply.LicenseReviewChangedData.LicenseID":                "opaque declaration row reference; no licence number, authority or document content",
	"supply.LicenseReviewChangedData.ReviewID":                 "opaque immutable decision reference; no reviewer identity, reason or authority-check reference",
	"supply.LicenseReviewChangedData.Version":                  "monotonic decision-stream sequence for replay and ordering; not personal data",
	"supply.EvidenceChangedData.EvidenceScanRequestedData":     "embedded event fields are only the classified opaque IDs and scan-attempt counter",
	"supply.EvidenceScanInput.TenantID":                        "opaque tenant UUID used only as the worker transaction boundary",
	"supply.EvidenceScanInput.PartyID":                         "opaque organisation UUID used only as the private worker boundary",
	"supply.EvidenceScanInput.LicenseID":                       "opaque licence row UUID with no declared number or evidence content",
	"supply.EvidenceScanInput.FileID":                          "opaque attachment UUID; never includes filename or storage path",
	"supply.EvidenceScanInput.ScanAttempt":                     "bounded processing attempt counter",
	"supply.EvidenceChangedData.Status":                        "bounded processing state; no filename or document content",
	"supply.EvidenceScanRequestedData.PartyID":                 "opaque organisation UUID; private metadata remains behind tenant and party authorization",
	"supply.EvidenceScanRequestedData.LicenseID":               "opaque declared-licence UUID; no number, jurisdiction or evidence",
	"supply.EvidenceScanRequestedData.FileID":                  "opaque evidence UUID; no object key, filename or bytes",
	"supply.EvidenceScanRequestedData.ScanAttempt":             "processing-attempt counter for exactly replayable workflow dispatch",

	"workforce.RosterChangedData.HospitalID": "opaque hospital scope; protected lookup required",
	"workforce.RosterChangedData.RosterID":   "opaque roster reference; no staff, dates or shift content",
	"workforce.RosterChangedData.RevisionID": "immutable internal revision reference; protected lookup required",
	"workforce.RosterChangedData.Version":    "monotonic revision for replay/order checks",

	"workforce.ShiftChangedData.HospitalID":           "internal hospital scope; no facility details",
	"workforce.ShiftChangedData.ShiftID":              "internal shift configuration reference; no personnel or policy content",
	"workforce.ShiftChangedData.RevisionID":           "internal immutable shift revision reference; protected lookup required",
	"workforce.ShiftChangedData.Version":              "monotonic configuration revision for retry/order checks",
	"workforce.StaffEnrollmentChangedData.HospitalID": "internally minted hospital scope; contains no facility name or contact details",
	"workforce.StaffEnrollmentChangedData.StaffID":    "internally minted staff row reference; resolving protected enrollment requires explicit permission and hospital manager access",
	"workforce.StaffEnrollmentChangedData.Version":    "monotonic resource revision for retry/order checks; contains no employee attributes",
	"workforce.MasterChangedData.HospitalID":          "internal hospital scope; no facility details",
	"workforce.MasterChangedData.MasterID":            "internal HR configuration reference; no employee data",
	"workforce.MasterChangedData.RevisionID":          "internal immutable configuration revision reference",
	"workforce.MasterChangedData.Kind":                "allowlisted HR configuration category; no configuration names or attributes",
	"workforce.MasterChangedData.Version":             "monotonic configuration revision; no employee data",
	"iam.MemberChangedData.Subject":                   "the iam subject identifier (e.g. GIP subject claim), already known to every module via authn.Principal.Subject; a reference, not identifying content",
	"iam.CredentialRevokedData.Subject":               "same iam subject identifier as MemberChangedData.Subject",
	"iam.MemberChangedData.RoleKey":                   "a key into the fixed role registry (docs/standards/backend.md), not clinical or identifying data",

	"medicore.VisitCreatedData.Department": "the ward/department the visit is opened in; operational routing metadata, not patient-identifying",

	// Internally-minted row ids. Listed one by one rather than matched by
	// an "ends in ID" rule: see the test's comment for why that rule was
	// removed. The reason below is the thing being asserted — it is NOT
	// true of every identifier, and an externally-issued one (Aadhaar,
	// ABHA, an insurer member number) belongs in the PHI allowlist.
	"medicore.VisitCreatedData.VisitID":              "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"medicore.VisitCreatedData.PatientID":            "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"patientmaster.PatientRegisteredData.HospitalID": "internally minted hospital scope for authorization; no name, address or external facility identifier",
	"patientmaster.PatientRegisteredData.PatientID":  "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"lab.ResultReadyData.VisitID":                    "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"lab.ResultReadyData.OrderID":                    "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"pharmacy.DispenseRecordedData.VisitID":          "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"pharmacy.DispenseRecordedData.DispenseID":       "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"reference.PingedData.PingID":                    "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",
	"reference.PingForwardedData.PingID":             "an internally-minted opaque row id, meaningless outside this platform and resolvable only by a caller already authorized for that row",

	// reference is the module that proves the platform wiring end to end
	// (issue #2); it deliberately exposes no tenant data (module.go's
	// Permissions() comment) and carries no clinical concept at all, so
	// its free-text field is operational test fixture content, never PHI.
	"reference.PingForwardedData.Message": "reference carries no clinical data by design; this is fixture text proving the directed-write wiring (#932), not patient content",
}

// loadContractPackagesWithTypes is loadContractPackages (contract_test.go)
// plus type information: this test needs to walk struct fields, which
// the AST-only mode the other contract tests use does not provide.
func loadContractPackagesWithTypes(t *testing.T) []*packages.Package {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps |
			packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo,
	}, modulesPrefix+"...")
	require.NoError(t, err)

	var out []*packages.Package
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, "/contract") {
			out = append(out, p)
		}
	}
	require.NotEmpty(t, out, "no contract packages found — this test would pass vacuously")
	return out
}

// payloadField is one exported field of one exported struct type
// declared in a contract package, identified by its pinned-map key.
type payloadField struct {
	key   string // "<module>.<Type>.<Field>"
	field string // bare field name, for the auto-safe check
}

// findPayloadFields walks every exported struct type in every contract
// package and returns every exported field. Contract packages declare
// data only (TestContractPackagesDeclareOnlyData enforces this), so
// every exported struct here is a wire payload type — there is nothing
// else a contract package is allowed to contain.
func findPayloadFields(t *testing.T) []payloadField {
	t.Helper()
	var out []payloadField

	for _, p := range loadContractPackagesWithTypes(t) {
		require.NotNilf(t, p.Types, "%s: no type information loaded", p.PkgPath)
		module := contractModuleNameT(t, p.PkgPath)
		scope := p.Types.Scope()

		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok || !tn.Exported() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			st, ok := named.Underlying().(*types.Struct)
			if !ok {
				continue // not a struct (e.g. a type alias for a subject string, if one existed)
			}

			for i := 0; i < st.NumFields(); i++ {
				f := st.Field(i)
				if !f.Exported() {
					continue
				}
				out = append(out, payloadField{
					key:   module + "." + tn.Name() + "." + f.Name(),
					field: f.Name(),
				})
			}
		}
	}
	require.NotEmpty(t, out, "no payload fields found in any contract package — this test would pass vacuously")
	return out
}

func contractModuleNameT(t *testing.T, pkgPath string) string {
	t.Helper()
	parts := strings.Split(pkgPath, "/")
	require.GreaterOrEqual(t, len(parts), 2, "contract package path %q too short to derive a module name", pkgPath)
	return parts[len(parts)-2]
}

// TestEventPayloadPHIFieldsAreAllowlisted is the arch test from D4: every
// field of every event payload struct in every module's contract package
// must be either
//
//  1. pinned in eventPayloadPHIAllowlist with the reason it must carry
//     PHI/identifying data, or
//  2. pinned in eventPayloadReviewedNonPHI with the reason it does not.
//
// There is NO automatic classification, deliberately. An earlier version
// auto-passed any field whose name ended in "ID" or "IDs", on the
// reasoning that this codebase's identifiers (VisitID, OrderID, PingID)
// are opaque row pointers. That rule silently admitted `AadhaarID` and
// `ABHAID` — proved by mutation during review of #835 Task 3, both of
// which the test passed without comment. In an ABDM/DPDP context those
// are among the most sensitive fields the platform could put on a wire:
// Aadhaar is sensitive personal data under the DPDP Act and ABHA is the
// national health identifier. "It is an identifier" is an argument for
// scrutiny, not against it.
//
// So every field is classified by a human exactly once. The cost is one
// map line per new payload field — 10 entries across five modules today
// — and that cost IS the control: adding a field to an event contract
// becomes a diff a reviewer must justify, which is what spec D4 asks for.
//
// A field matching neither map is a field nobody has decided about; this
// test fails closed and names it rather than guessing.
func TestEventPayloadPHIFieldsAreAllowlisted(t *testing.T) {
	fields := findPayloadFields(t)

	seen := map[string]bool{}
	for _, f := range fields {
		seen[f.key] = true

		_, inPHI := eventPayloadPHIAllowlist[f.key]
		_, inSafe := eventPayloadReviewedNonPHI[f.key]
		require.Falsef(t, inPHI && inSafe,
			"%s is pinned in BOTH eventPayloadPHIAllowlist and eventPayloadReviewedNonPHI — it cannot be both", f.key)
		require.Truef(t, inPHI || inSafe,
			"%s is a new event payload field with no PHI classification: it is not pinned in eventPayloadPHIAllowlist or eventPayloadReviewedNonPHI in event_payload_test.go. "+
				"Every field is classified by hand — there is no automatic pass for identifier-shaped names, because AadhaarID and ABHAID are identifiers too. "+
				"If it carries clinical or patient-identifying data, add it to eventPayloadPHIAllowlist with a reason it must be on the wire. "+
				"If it does not, add it to eventPayloadReviewedNonPHI with the reason.",
			f.key)
	}

	// The reverse direction: every pinned entry must name a field that
	// actually exists, the same way TestRateLimitPolicyRoutesAreRegistered
	// catches a typo'd route key instead of a silently-dead allowlist
	// entry that looks like coverage but checks nothing.
	for key, reason := range eventPayloadPHIAllowlist {
		require.Truef(t, seen[key], "eventPayloadPHIAllowlist entry %q (%s) does not match any field found in a contract package — check for a typo or a removed field", key, reason)
	}
	for key, reason := range eventPayloadReviewedNonPHI {
		require.Truef(t, seen[key], "eventPayloadReviewedNonPHI entry %q (%s) does not match any field found in a contract package — check for a typo or a removed field", key, reason)
	}
}

// TestEventPayloadPHIAllowlistIsPinnedExactly guards the security-relevant
// half of the classification (which fields carry PHI) against silent
// growth: if this test's expected literal and the map diverge, someone
// added or removed a PHI field without this line of the diff being the
// thing a reviewer's eye lands on.
func TestEventPayloadPHIAllowlistIsPinnedExactly(t *testing.T) {
	require.Equal(t, map[string]string{
		"mediconnect.ClaimCheckRequest.Relationship":          "exact relationship bound to patient/subject in workload-authenticated private owner HTTP only; never broadcast or logged",
		"mediconnect.ClaimCheckRequest.Subject":               "exact owner-subject selection in workload-authenticated internal HTTP only; never published as an event",
		"mediconnect.ClaimCheckRequest.PatientID":             "exact patient selection in workload-authenticated internal HTTP only; never published as an event",
		"mediconnect.RegistrationReviewData.RequesterSubject": "opaque authenticated requester identity is required solely to reject registrar self-review across owners; no contact or identity evidence; restricted same-tenant consumer",
		"mediconnect.RegistrationReviewData.Relationship":     "unverified allowlisted relationship claim is necessary for hospital evidence review and immutable request lineage; never proxy authority or consent; no free text",
		"medicore.VisitCreatedData.PatientName":               "pharmacy and lab display it on their work queues; modules may not import modules, so there is no cross-module read path to fetch it instead",
	}, eventPayloadPHIAllowlist)
}
