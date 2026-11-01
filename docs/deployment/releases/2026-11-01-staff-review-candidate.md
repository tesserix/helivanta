# Staff review release candidate — 2026-11-01

Status: prepared, not deployed. Staff source `f1d3cdee` is reconciled with current
main `6dbb5c6c`; the final revision/CI/image evidence is required before promotion.

Production discovery: `c20a220267cc4e71cc169c47b3ef7220612da1ce`, namespace
`helivanta`, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, active account
`unidevidp@gmail.com`. Six application workloads have two ready replicas; API,
Global API and shell are Synced/Healthy. Kargo promotion is manual. Scheduled
database backups report completed. No production mutation has occurred.

## Persisted migrations

The [machine-readable manifest](2026-11-01-staff-review-candidate.json) records
every pending owner/ID, canonical SQL path, embed source/variable, exact SQL hash
and structured concurrent-index declaration. Production has 166 IDs; this
candidate registers 193, with no unknown applied IDs. The complete pending set
is **27 migrations**, including inherited Billing, MediCore and Pharmacy changes.

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

Earlier Staff source passed affected owner race and all 22 web gates. The combined
main/dependency/Billing candidate is undergoing full local Go coverage and web
lint/format/type/test/build, followed by PR CI and image build/scan/sign publication.
Earlier results do not establish validation of the combined dependency tree.

The release contains scoped HR preparation, person-review protected evidence and
workspace, employment submissions/independent decisions/endings, separate scoped
reviewer assignment setup and credential/permission refresh corrections. Terms
approval does not activate employment, login, clinical access, cards or payroll.
Employment-review protected bytes/inbox/history/workspace, operational generations,
all lifecycle/consumer cutoffs and full normal browser/provider acceptance remain
open. No umbrella Staff or Access/RBAC issue is closed by this release.

## Promotion and recovery

Keep the repository public. The user authorized admin-bypass merge and production
deployment. Finish combined-source validation, merge with that bypass, then wait
for both images to be scanned, signed and published and for `deploy` to advance.
Promote the exact merged revision with all 27 migrations through Kargo.
Verify exact images/replicas, migration IDs/timestamps, Argo health, expected
unauthenticated denial and normal synthetic Staff Admin/reviewer UI.

Preserve expanded schema, audit and review receipts. Prefer forward correction.
A live rollback requires separate approval and owner state assessment; no
destructive down migration or history deletion is part of this release.
