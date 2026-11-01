# Staff review release candidate — 2026-11-01

Status: **deployed and healthy**. PR [#1501](https://github.com/tesserix/helivanta/pull/1501)
merged as `bd1f62d709c0b238699a71f02c0a15317cabd493`, with a tree identical to the
validated `c73703a2` candidate. All PR CI jobs passed, including Go race/coverage
and 78 browser tests. Both images passed build, scan, SBOM, signing and publication.
The temporary PR-only admin bypass was removed immediately after merge and the
original active GitHub ruleset verified restored. Repository visibility is public.

Production: namespace `helivanta`, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, active account
`unidevidp@gmail.com`. Kargo promotion
`prod.01m6a85hwx453b0d0gn5pdv5rh.c3ab8b2` succeeded and verification is Successful.
All six application workloads have two ready replicas on the exact merged image
tag; API, Global API and shell are Synced/Healthy. Scheduled backups completed
before rollout. Login returns 200 and unauthenticated person-review/HR-intake scope
requests return 401. These smoke checks did not mutate real Staff/financial data.

## Persisted migrations

The [machine-readable manifest](2026-11-01-staff-review-candidate.json) records
every pending owner/ID, canonical SQL path, embed source/variable, exact SQL hash
and structured concurrent-index declaration. Before rollout, production had 166 IDs and the candidate registered 193. After
the successful migration hook, production has 193 IDs: exactly the **27 migrations**
below were added, with no missing, unexpected or removed IDs. Their committed SQL
hashes match this manifest. The set includes inherited Billing, MediCore and
Pharmacy changes.

| Owner     | Migration                                       | Canonical source                                                             |
| --------- | ----------------------------------------------- | ---------------------------------------------------------------------------- |
| iam       | `0015_iam_membership_commands`                  | `backend/internal/modules/iam/membership_commands.sql`                       |
| iam       | `0016_iam_membership_candidate_index`           | `backend/internal/modules/iam/module.go`                                     |
| iam       | `0017_iam_membership_projection_reader`         | `backend/internal/modules/iam/membership_projection_reader.sql`              |
| iam       | `0018_iam_membership_enrollments`               | `backend/internal/modules/iam/membership_enrollments.sql`                    |
| iam       | `0019_iam_membership_recovery_admission`        | `backend/internal/modules/iam/membership_recovery_admission.sql`             |
| iam       | `0020_iam_hr_intake_templates`                  | `backend/internal/modules/iam/assignment_hr_intake_templates_generated.sql`  |
| iam       | `0021_iam_employment_templates`                 | `backend/internal/modules/iam/assignment_employment_templates_generated.sql` |
| medicore  | `0009_medicore`                                 | `backend/internal/modules/medicore/nicu.sql`                                 |
| pharmacy  | `0028_pharmacy`                                 | `backend/internal/modules/pharmacy/unused_payment_refunds.sql`               |
| payments  | `0002_payments`                                 | `backend/internal/modules/payments/requesters.sql`                           |
| billing   | `0004_billing`                                  | `backend/internal/modules/billing/final_bills.sql`                           |
| billing   | `0005_billing`                                  | `backend/internal/modules/billing/cash.sql`                                  |
| billing   | `0006_billing`                                  | `backend/internal/modules/billing/deposits.sql`                              |
| billing   | `0007_billing`                                  | `backend/internal/modules/billing/refunds.sql`                               |
| billing   | `0008_billing`                                  | `backend/internal/modules/billing/positions.sql`                             |
| billing   | `0009_billing`                                  | `backend/internal/modules/billing/gst.sql`                                   |
| billing   | `0010_billing`                                  | `backend/internal/modules/billing/credit_notes.sql`                          |
| billing   | `0011_billing`                                  | `backend/internal/modules/billing/upi.sql`                                   |
| workforce | `0053_workforce_hr_intake`                      | `backend/internal/modules/workforce/hr_intake.sql`                           |
| workforce | `0054_workforce_person_reviews`                 | `backend/internal/modules/workforce/person_reviews.sql`                      |
| workforce | `0055_workforce_person_provenance_index`        | `backend/internal/modules/workforce/module.go`                               |
| workforce | `0056_workforce_person_review_browse`           | `backend/internal/modules/workforce/person_review_browse.sql`                |
| workforce | `0057_workforce_person_review_inbox_index`      | `backend/internal/modules/workforce/module.go`                               |
| workforce | `0058_workforce_person_evidence_selection`      | `backend/internal/modules/workforce/person_review_selection.sql`             |
| workforce | `0059_workforce_employment_document_provenance` | `backend/internal/modules/workforce/employment_document_provenance.sql`      |
| workforce | `0060_workforce_employment_review_submissions`  | `backend/internal/modules/workforce/employment_reviews.sql`                  |
| workforce | `0061_workforce_employment_review_decisions`    | `backend/internal/modules/workforce/employment_review_decisions.sql`         |

Apply only through the normal GitOps PreSync migration binary, using the complete
registered history. Do not execute extracted delta SQL manually. Future changes
append migrations and preserve these source bytes and durable command history.

## Validation and acceptance

Local combined web gates passed all 30 tasks. Patientmaster, Billing, Payments,
Global API, architecture and contractdocs isolated race/coverage checks passed;
full PR Go coverage and all 78 browser E2E tests then passed on GitHub. Earlier
failed local attempts remain in private validation evidence. The generic CI
browser suite is not positive scoped Staff reviewer acceptance.

The release contains scoped HR preparation, person-review protected evidence and
workspace, employment submissions/independent decisions/endings, separate scoped
reviewer assignment setup and credential/permission refresh corrections. Terms
approval does not activate employment, login, clinical access, cards or payroll.
Employment-review protected bytes/inbox/history/workspace, operational generations,
all lifecycle/consumer cutoffs and full normal browser/provider acceptance remain
open. No umbrella Staff or Access/RBAC issue is closed by this release.
See the [Staff tracker](../../qa/staff-management-progress.md).

## Promotion and recovery

The migration hook completed with exit code 0. Rollout checks passed for API,
Global API and all three workers; shell also reached two ready replicas. Current
image tags and Kargo/Argo health were checked independently of the CLI watch,
which disconnected while server-side promotion continued normally. The unsupported
CLI warehouse refresh endpoint was replaced by the existing warehouse refresh
annotation; freight is `c3ab8b2e00de75e464166926ffedc6bb48042a70`.

Preserve expanded schema, audit and review receipts. Prefer forward correction.
A live rollback requires separate approval and owner state assessment; no
destructive down migration or history deletion is part of this release.
