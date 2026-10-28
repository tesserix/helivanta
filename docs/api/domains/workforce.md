# Workforce integration and schema specification

[Contract index](../README.md) · extend the existing [detailed workforce API](../workforce.md)

Workforce is the current hospital-scoped HR module, within the MediCore product boundary. Its staff record is not a Zitadel credential or a patient identity. The hierarchy is verified tenant → hospital → staff; hospital access grants are enforced in addition to route permissions, with separate configuration/review/reveal authorities. Preserve the distinction between staff ID and identity-provider subject.

The existing API document describes fields and operations; [generated reference](../reference/README.md#workforce) lists all current routes, events, subscriptions and table declarations. This page defines how those areas integrate and what the schema sources mean.

| Capability                                                                               | Canonical source / implementation                                                                                                | Integration rule                                                                                                                                   |
| ---------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Staff enrollment, addresses, emergency contacts, credentials, qualifications, experience | `workforce-enrollment.json`; enrollment handlers and tables                                                                      | Sensitive staff facts remain hospital-scoped; no automatic clinical permission from a job title                                                    |
| HR masters, shifts and roster configurations                                             | HR masters/shifts handlers; `workforce-roster-config.json`                                                                       | Stable record ID plus immutable revision/version, effective dates and explicit membership/schedule composition                                     |
| Dated rosters and staff assignments                                                      | Roster handlers, revisions and allocation tables                                                                                 | Draft/frozen/discarded workflow, overnight local dates, clashes and audited correction; published schedule is not attendance                       |
| Leave setup, allocation and requests                                                     | `workforce-leave.json`; ledger, years, runs and request events                                                                   | Half-day units/effective policies, durable allocations, request approval/cancel/revoke and immutable accounting                                    |
| Duty exchanges and overtime                                                              | `workforce-duty.json`, `workforce-overtime.json`                                                                                 | Request and independent review authority, holds/clashes and attributable reversals; approved overtime is not paid payroll                          |
| Attendance, missing-punch correction, periods and timesheets                             | `workforce-attendance.json`; punches/voids, correction events, period snapshots                                                  | Recorded evidence differs from derived totals; corrections and period reopen/finalize preserve source evidence                                     |
| Payroll configuration and previews                                                       | `workforce-payroll.json`; salary heads/structures, statutory rules, salary assignments, tax regimes/declarations and identifiers | Exact paise/minutes/policy version; review/withdraw/reveal controls; calculation preview is not a posted payslip, bank payment or statutory filing |
| Credential validity/reminders                                                            | `workforce-credentials.json`; reviews, standing and acknowledgement                                                              | Reviewed current authority and expiry evidence; reminder acknowledgement is not credential renewal or permission grant                             |
| Dependents and benefits                                                                  | `workforce-benefits.json`; versioned plans/dependents/proposals/enrollments                                                      | Effective eligibility and reviewed decision; relationship is not permission to view a patient's clinical chart                                     |
| Private documents, benefit/tax evidence                                                  | `workforce-documents.json`; registered exact-generation files and scan workflows                                                 | Current grant, clean exact-content verdict and release gate at download; labels/URLs alone do not establish access                                 |

These JSON files use the repository's custom generation dialect. They are not OpenAPI or directly valid JSON Schema. See [schema index](../schemas.md) before selecting a validator. Generated Go/Zod definitions share wire names, while handler/database rules add cross-record and effective-date invariants.

Staff profile master connections use canonical enrollment metadata for department,
designation, grade, preferred language, exit reason and contract type. References
pin immutable revisions in the verified tenant and hospital; contract type is a
separate classification from engagement type. Language and exit-reason legacy
text stays readable. Older-client omission preserves pins; explicit empty
reference values clear them. See the [profile connection design](../../superpowers/specs/2026-10-28-staff-profile-master-connections.md)
and detailed owner contract for null compatibility and mixed-version writers.
Use the versioned opt-in profile projection for the new metadata; the updated
editor requires response confirmation instead of treating unknown pins as blank.
Qualification, relationship and private-document category consumers remain
separate work; neither a master choice nor an exit reason completes employment
approval, offboarding or payroll.

## Event and projection boundary

Workforce publishes metadata-oriented change events for staff enrollment, HR definitions, rosters, leave, exchanges, overtime, attendance, payroll configuration, documents, credentials and benefits. Read exact subjects and consumers from the [registry inventory](../reference/inventory.json) and payload types from `backend/internal/modules/workforce/contract/events.go`; avoid copying their many definitions into another service.

Document scanning and credential reminders use conditional Temporal consumer bridges. Their presence in documentation means the capability exists in source, not that production has enabled workers. Workflow starts are idempotent, and after-commit retry must still run for already-claimed events. Reminder/roster credential checks do not replace a clinician's authoritative privilege review at the clinical command boundary.

HR pay, health evidence, identifiers and dependent data must not enter commercial CRM, general telemetry or unrestricted support exports. Consumers should retain source IDs/version and fetch only permitted fields under current hospital authority. If a review/statutory policy is missing, deny or show incomplete configuration; do not calculate a plausible default and call it approved payroll.

## Review scope

Use [HR capability coverage](../../hr-capability-coverage.md), [private-file verification](../../private-staff-files-verification-2026-10-15.md), [payroll reference](../../payroll-india-statutory-reference-2026-10-16.md) and [release checklist](../../deployment/workforce-release.md). Trace to #1037, #1038, #1041, #1105, #1106, #1107 and #1108. Statutory references require current jurisdiction/finance review; documentation does not certify compliance.

Required contract cases: historical policy selection, overnight/DST dates, double leave allocation, concurrent request approval/revocation, correction against a closed period, reopened timesheet lineage, exact rounding and overflow, no unauthorized identifier reveal, reviewer self-approval restrictions where implemented, stale tenant/hospital and expired/revoked evidence access. New consumers must define latency/freshness and reconciliation needs rather than use HR events as an implicit permission grant.
