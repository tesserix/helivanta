# Staff intake access: source and transaction-phase inventory

2026-10-31 source snapshot: `a12216d1`; local document workspace correction
is recorded separately. This inventory is a prerequisite for the full scoped HR
intake/employment-review implementation, not evidence that it is implemented.
Read with ADR-0049 and the 2026-10-30 access/review continuation plan.

## Actual registered routes

The generated registry has 23 enrollment-management routes and 10 document-
management routes (33 total). Every row requires authenticated current tenant
membership. Registry entries declare only the primary permission; owner checks
and secondary permissions below are additional authority.

| Method | Path | Primary permission |
|---|---|---|
| `GET` | `/v1/workforce/hospitals/:hospital/hr/credential-reminder-policy` | `workforce.enrollment.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/hr/credential-reminder-policy` | `workforce.enrollment.manage` |
| `POST` | `/v1/workforce/hospitals/:hospital/hr/credential-reminders/:reminder/acknowledgement` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/hr/credential-reminders` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/hr/credentials` | `workforce.enrollment.manage` |
| `POST` | `/v1/workforce/hospitals/:hospital/staff/:id/credentials/:credential/reviews/:review/withdraw` | `workforce.enrollment.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/credentials/:credential/reviews/:review` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/credentials/:credential/reviews` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/credentials` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/document-records` | `workforce.documents.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/classification/history` | `workforce.documents.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/classification` | `workforce.documents.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/classification` | `workforce.documents.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/content` | `workforce.documents.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/content` | `workforce.documents.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/retries/:attempt` | `workforce.documents.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file/revocation` | `workforce.documents.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/documents/:file` | `workforce.documents.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/documents` | `workforce.documents.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/history` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence/:binding/content` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence/:binding/history` | `workforce.enrollment.manage` |
| `POST` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence/:binding/withdraw` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence/:binding` | `workforce.enrollment.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence/:binding` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence` | `workforce.enrollment.manage` |
| `POST` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/withdraw` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period` | `workforce.enrollment.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-periods` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/employment-source` | `workforce.enrollment.manage` |
| `GET` | `/v1/workforce/hospitals/:hospital/staff/:id/enrollment` | `workforce.enrollment.manage` |
| `PUT` | `/v1/workforce/hospitals/:hospital/staff/:id/enrollment` | `workforce.enrollment.manage` |

## Owner authority and transaction phases

| Family | Current source boundary | Required migration treatment |
|---|---|---|
| Enrollment GET/PUT | `enrollment.go`: `enrollmentScope` locks explicit hospital-manager grant; source/revision and supervision guards remain independent | Join current scoped IAM locks to each owner transaction; retain manager/object scope and required source/version checks |
| Employment source/draft/history/withdrawal | `employment_periods.go`: `employmentPeriodTransaction` checks manager, hospital supervision lock, Staff row, then owner command | Preserve lock ordering, exact replay and draft finality; intake permission cannot approve or activate employment |
| Employment evidence (six routes) | `employment_evidence.go`: `employmentEvidenceParams` refreshes document permission then requires enrollment permission; period transaction and binding/release guards also apply | Prove BOTH permissions at each owner commit/disclosure phase; a primary-only scoped middleware is insufficient |
| Staff documents (ten routes) | `documentStaff`, `documentOwner`, `staffDocuments` and classification binding enforce subject/hospital/Staff/file/purpose | Fix expected-workspace precondition; preserve staff-purpose separation from benefit and tax evidence |
| Upload | `document_upload.go`: metadata/authorization transaction, bytes validation, another authorization transaction, `EnsureQuarantine` outside transactions, final current-owner transaction/outbox | Reauthorize each phase; do not hold IAM/product DB locks through storage I/O or treat an old route receipt as authority |
| Download | `document_access.go`: owner/release transaction, `ReadReleased` outside transaction, final owner/release/audit transaction, byte response | Refresh current credential, assignment and employment basis after storage; a pre-I/O check alone cannot prove cutoff |
| Credential proposals/register/reminders | `credential_validity.go` and `credential_reminders.go` also call `enrollmentScope` | Preserve separate credential-review permission and policy ownership; granting intake must not approve credentials or grant clinical privilege |

The shared `enrollmentScope` also has leave-standing (`leave_requests.go`) and
leave-report consumers (`leave_reports.go`). A blanket helper or all-transaction
replacement would alter unrelated leave behavior. Classify and test these callers
rather than silently converting them to HR intake authority.

## Canonical template and migration boundary

`contracts/staff-access.json` currently defines only leave and identity template
families, each with one permission. `generate-staff-access.mjs` rejects other
families/multiple permissions and generates the SQL consumed by historical IAM
migration 0014. Adding an intake template by simply regenerating this file would
change a historical migration; that is not a valid forward migration strategy.
The follow-up catalog correction now preserves the exact 0014 artifact and
introduces canonical, digest-pinned definition/SQL history for additive template
versions. This adds no intake role or runtime migration. See the
[history design](../superpowers/specs/2026-10-31-staff-template-migration-history.md)
and current tracker for separate implementation/CI evidence. Server and
Go/Zod/template/SQL artifacts must agree. The current single-grant command guard
also needs a deliberate compound-permission contract for evidence; nested guarded
transactions and a copied private permission vocabulary are not substitutes.

## Remaining full-workflow gates

Implement intake template, route capabilities and actual owner/UI transactions
together; retain explicit hospital scope without manufacturing manager rows.
Current reviewed actor identity, new-hire/non-digital person basis, historical
independence, immutable employment submission/review, effective eligibility,
legacy authority classification, every consumer cutoff and normal signed-session
Admin/reviewer/worker journeys remain open. No ORG-D2/D5/D6 policy is settled by
this inventory. No production authority, migration or account was changed.
