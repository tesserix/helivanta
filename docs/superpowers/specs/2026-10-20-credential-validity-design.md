# Credential validity reviews

Issue: #1108 slice A (parent #1035, epic #979). Builds on staff enrollment (`workforce_enrollment_credentials`), the [document image envelope](2026-10-15-document-image-envelope.md) that scans and releases credential evidence, and the hospital time zone held in roster settings ([ADR-0019](../../adr/0019-roster-settings-and-staff-assignments.md)).

## Problem

Enrollment stores each staff member's licences and registrations with a number, issuer and validity dates. Anyone with enrollment access can change those dates on the enrollment form. Nobody reviews the change, and the previous validity is overwritten. Rosters and reminders cannot rely on a date that one person typed and nobody checked. HR also has no hospital-wide view of what is expiring.

MedNet (read-only review) shows licence validity on registration only. It has no review, history or expiry register.

## Decision

- **Reviewed validity is its own record.** `workforce_credential_validity` holds, per credential, the reviewed number, valid-from and valid-until dates and the review that set them. Enrollment rows remain the staff member's declared data; the reviewed record is what everything else trusts.
- **Changes go through a review.** `workforce_credential_reviews` holds proposals: the number and validity dates proposed, the credential evidence documents cited (1–5), and a status of pending, approved, rejected or withdrawn. One credential has at most one pending review. The proposer cannot decide their own review (a database check, not only a handler rule). Approving copies the proposal into the reviewed record and the enrollment row in one transaction. History is the list of reviews and is never rewritten.
- **Evidence is existing credential evidence.** A review cites documents with `record_kind='credential'` and `record_id` equal to the credential. Proposing refuses documents that are rejected, revoked, unfinished uploads or another credential's. Approving needs every cited document to be `available`, so a reviewer always decides against scanned files that open. Upload, scanning and download stay in the document pipeline; this slice adds no store.
- **Enrollment cannot bypass review.** Once a credential has a reviewed record, a trigger refuses any enrollment write that changes its type, number, issuer or dates. Removing a reviewed credential from enrollment fails at commit, through a deferred foreign key, and the API answers 409 `credential_reviewed`. Only the approval path, which sets `app.workforce_credential_review`, may write those fields. Fail closed: a missing flag means refuse.
- **Status is computed on read, in the hospital's calendar.** The local date comes from the hospital's roster time zone and one database clock reading per request. The statuses are:
  - `unreviewed`: no reviewed record yet.
  - `not_yet_valid`, `valid`, `expires_today` or `expired`: from the reviewed dates.
  - `date_unknown`: no roster time zone is set.

  The read also returns `days_remaining`, `local_date` and `time_zone`. Nothing is stored, so a status is never stale. Expiry day counts as valid, the same inclusive convention as supply licences.

- **Permissions.** Viewing and proposing need `workforce.enrollment.manage`, the existing HR record permission. Deciding needs the new `workforce.credentials.review`. Reading evidence still needs `workforce.documents.manage`. A reviewer therefore needs the document permission as well as the review permission, and keeping proposer and decider different is enforced on every review.
- **Isolation.** Both tables use the enrollment row-level security predicate (tenant plus explicit hospital grant). Cross-tenant, cross-hospital and other-staff requests answer 404.

## API

Under `/workforce/hospitals/{hospital}`:

| Route                                                                 | Permission           | Behaviour                                                                                                                    |
| --------------------------------------------------------------------- | -------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `GET /staff/{id}/credentials`                                         | `enrollment.manage`  | Each credential with reviewed record, status projection and pending review                                                   |
| `GET /staff/{id}/credentials/{credential}/reviews`                    | `enrollment.manage`  | Review history, newest first, paginated                                                                                      |
| `PUT /staff/{id}/credentials/{credential}/reviews/{review}`           | `enrollment.manage`  | Propose; identical replay returns the saved review; 409 `review_pending`, `version_conflict`, `evidence_invalid`             |
| `POST /staff/{id}/credentials/{credential}/reviews/{review}/decision` | `credentials.review` | `approve` or `reject` (rejecting needs a note); 409 `self_review`, `evidence_unavailable`, `not_pending`, `version_conflict` |
| `POST /staff/{id}/credentials/{credential}/reviews/{review}/withdraw` | `enrollment.manage`  | Proposer only; 409 `not_proposer`, `not_pending`                                                                             |
| `GET /hr/credentials?expiring_within=`                                | `enrollment.manage`  | Hospital register of reviewed credentials, soonest expiry first, optionally within 0–365 days; paginated                     |

Every response is served `Cache-Control: no-store`. Every write takes the per-credential advisory lock, then checks the version (`expected_version`) and refuses stale input with 409.

## Events

`helivanta.in.workforce.credential_review_changed.v1` is published on every proposal and decision. `helivanta.in.workforce.credential_validity_changed.v1` is published when an approval changes the reviewed record. Both go through the outbox in the business transaction. Payloads carry hospital, staff, credential and review ids, status and version only: no number, issuer or dates. Reminders (slice B) and roster checks (slice C) consume `credential_validity_changed`.

## Platform

- **NATS:** outbox events only; no consumer in this slice.
- **Valkey:** not used. Reviewed validity is confidential HR data, and status must not be served from a cache.
- **Temporal:** not used. Every write is one transaction with no external side effect.
- **SOC 2 / HIPAA:** append-only reviews, an attributable proposer and decider, audit rows for every transition, `no-store` responses and identifier-only events. Evidence remains scanned before release.

## Testing

- Pure tests of the status function: time-zone midnight differences (Asia/Kolkata and Pacific/Auckland against UTC), expiry day, the day after, future valid-from, a 29 February expiry, and an unknown zone.
- Real PostgreSQL API tests covering:
  - propose, approve, history and register
  - self-review refused
  - replay idempotent, and a conflicting replay refused
  - stale version refused
  - a second pending review refused
  - evidence that is foreign, revoked or not yet scanned
  - an enrollment PUT that changes a reviewed field, or removes a reviewed credential, refused with 409
  - cross-tenant, cross-hospital and other-staff requests answering 404
  - deciding without the review permission answering 403
  - outbox events with identifier-only payloads
- Direct SQL guards: the self-decision check, the single pending review, and the trigger refusing an unflagged change to a reviewed field.
- UI tests: the status text, proposing with inline errors, deciding, and the register filter.

## Not covered

- Reminder thresholds, durable reminders and delivery channels (slice B).
- Roster credential conflicts (slice C, closes the #1108 part of #1036).
- Staff self-service and automatic privilege or employment changes, which the story excludes.
