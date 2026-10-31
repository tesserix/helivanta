# Workforce integration and schema specification

[Contract index](../README.md) · extend the existing [detailed workforce API](../workforce.md)

Workforce is the current hospital-scoped HR module, within the MediCore product boundary. Its staff record is not a Zitadel credential or a patient identity. The hierarchy is verified tenant → hospital → staff; hospital access grants are enforced in addition to route permissions, with separate configuration/review/reveal authorities. Preserve the distinction between staff ID and identity-provider subject.

The existing API document describes fields and operations; [generated reference](../reference/README.md#workforce) lists all current routes, events, subscriptions and table declarations. This page defines how those areas integrate and what the schema sources mean.

The current [pre-employment person-review candidate](../workforce.md#pre-employment-current-person-review-candidate)
adds Workforce-owned immutable human findings without activating employment or
manufacturing login linkage. Canonical source is
`contracts/workforce-person-reviews.json`; registered migrations are
`0054_workforce_person_reviews` and concurrent-index
`0055_workforce_person_provenance_index`. Scoped identity manager/reviewer authority
and independently reviewed active **actor** links remain required even when the
target joiner is inactive. Review-bound evidence bytes use the original scope,
actor/link/epoch, review/source and released-file/scan lineage before and after
storage; replacement authority requires a fresh request. Bounded hospital
discovery now returns independently validated submit/review capabilities from held
identity assignments and current actor links. It preserves filtered empty-page
continuation and adds no HR/document grant. Hospital inbox and manager-only Staff
ID search now use purpose-scoped read migration 0056 and concurrent-index migration
0057; they preserve original writes/deletes and expose canonical summaries only.
Manager preparation now uses mandatory source-basis input and scan-released image
metadata through purpose-scoped migration 0058, without generic document access.
The source UI also includes protected history and a persistent newer-receipt guard.
Normal browser acceptance, remaining failure/concurrency and real-scanner/browser acceptance, and employment
generation consumption remain unimplemented/unverified and must not be inferred
from these registered routes.

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

## Identity draft boundary

Workforce owns hospital/staff-scoped identity, background and photograph drafts
and immutable history (`workforce-identity-checks.json`). IAM owns current scoped
manager/reviewer authority and reviewed actor bindings. The opt-in source
projection exposes preparation capability; mandatory tenant/account request
preconditions survive the Global API. Drafts are unreviewed and convey neither
employment authority nor identity-card eligibility. Evidence binding, independent
check decisions and card issuance remain open; see the detailed Workforce API.

## Independent employment review candidate

Workforce 0060/0061 and IAM 0021 now provide immutable employment submissions,
independent reviewed hospital-scoped decision authority, approval/rejection,
preparer withdrawal and reviewer revocation. The new employment catalog preserves
historical catalog membership. Review-only source metadata cannot mutate terms or
replace generic enrollment/file access. Exact retries reauthorize, and approval
preserves inactive target status. Later preparation drafts do not cancel approved
historical terms. See the [owner contract](../workforce.md#immutable-employment-submissions-and-independent-decisions--source-candidate).
Normal reviewer workspace/protected bytes, provider/browser QA, operational
employment activation and all A01–A12 cutoffs remain required. This candidate is
not deployed or accepted as complete Access/RBAC.

## Current permissions across private file I/O

The Staff access source candidate refreshes the authenticated IAM resolver before
each shared upload/download owner phase, including after storage I/O. Staff files
require document management; employment evidence requires both document and
enrollment management from the same fresh permission result. Benefit reads/uploads
retain benefits view/manage respectively; tax reads/uploads retain payroll
view/configure. Current hospital, Staff, purpose, pending-state and exact release
checks remain additional owner conditions. Permission loss returns 403; resolver
failure returns 503 without released bytes, success audit or a new scan command.
A completed quarantine write may remain uncommitted for existing retry/cleanup.

The subsequent source candidate also revalidates the original credential and
the IAM current-identity check installed at request entry before these phases.
It pins subject, tenant, authentication time and idle deadline to that original
credential. Expired/revoked credentials or inactive identities return 401;
unavailable identity/revocation sources return 503. IAM retains its direct source
watermark check so a cached negative revocation observation cannot preserve an
old token, including after account reactivation. Storage remains outside owner
transactions. See the [identity-refresh design](../../superpowers/specs/2026-10-31-private-file-identity-refresh.md)
for the source defect, verification requirements and limits; this candidate is
not yet deployed or fully validated.

This is not a transaction-held IAM/Workforce employment cutoff. Scoped HR intake,
current-person review and every lifecycle consumer remain separate acceptance
gates. See the [source and test design](../../superpowers/specs/2026-10-31-staff-file-current-permissions.md).
The credential-refresh change itself introduces no new permission, route or
schema. The separate, unreleased scoped HR intake candidate adds explicit
directory/discovery and preparation consumers described in the
[detailed Workforce API](../workforce.md#scoped-hr-intake-unreleased-candidate);
no production activation follows from either source checkpoint.

## Event and projection boundary

Workforce publishes metadata-oriented change events for staff enrollment, HR definitions, rosters, leave, exchanges, overtime, attendance, payroll configuration, documents, credentials and benefits. Read exact subjects and consumers from the [registry inventory](../reference/inventory.json) and payload types from `backend/internal/modules/workforce/contract/events.go`; avoid copying their many definitions into another service.

Document scanning and credential reminders use conditional Temporal consumer bridges. Their presence in documentation means the capability exists in source, not that production has enabled workers. Workflow starts are idempotent, and after-commit retry must still run for already-claimed events. Reminder/roster credential checks do not replace a clinician's authoritative privilege review at the clinical command boundary.

HR pay, health evidence, identifiers and dependent data must not enter commercial CRM, general telemetry or unrestricted support exports. Consumers should retain source IDs/version and fetch only permitted fields under current hospital authority. If a review/statutory policy is missing, deny or show incomplete configuration; do not calculate a plausible default and call it approved payroll.

## Review scope

Use [HR capability coverage](../../hr-capability-coverage.md), [private-file verification](../../private-staff-files-verification-2026-10-15.md), [payroll reference](../../payroll-india-statutory-reference-2026-10-16.md) and [release checklist](../../deployment/workforce-release.md). Trace to #1037, #1038, #1041, #1105, #1106, #1107 and #1108. Statutory references require current jurisdiction/finance review; documentation does not certify compliance.

Required contract cases: historical policy selection, overnight/DST dates, double leave allocation, concurrent request approval/revocation, correction against a closed period, reopened timesheet lineage, exact rounding and overflow, no unauthorized identifier reveal, reviewer self-approval restrictions where implemented, stale tenant/hospital and expired/revoked evidence access. New consumers must define latency/freshness and reconciliation needs rather than use HR events as an implicit permission grant.
