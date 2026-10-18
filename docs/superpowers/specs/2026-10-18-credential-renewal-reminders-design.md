# Credential renewal reminders

Issue: #1108 slice B (parent #1035, epic #979). Builds on [credential validity reviews](2026-10-20-credential-validity-design.md) (slice A), the hospital roster time zone ([ADR-0019](../../adr/0019-roster-settings-and-staff-assignments.md)) and the Temporal document worker.

## Problem

Slice A tells HR what is expiring only when someone opens the register. Nothing raises a reminder before a licence lapses, and the thresholds that should trigger one are not recorded anywhere. A reminder that repeats, fires for a superseded revision or silently never fires is worse than none.

MedNet (read-only review) has no renewal reminders.

## Decision

- **Thresholds are a versioned hospital policy.** `workforce_credential_reminder_policies` is append-only: each save adds version N+1 with 1–6 distinct whole-day thresholds between 0 and 365, stored in descending order, plus the subject who set it. The latest version is current. With no policy, reminders are off, and the UI says so. There are no silent defaults.
- **One durable schedule per reviewed revision.** Approving a review publishes `credential_validity_changed` (slice A). The `workforce-credential-reminders` consumer starts Temporal workflow `wf-cred-rem-<tenant>-<hospital>-<staff>-<credential>-v<version>`, rejecting duplicates and checking the receipt the same way the document scan consumer does. A redelivered event therefore never creates a second schedule.
- **The workflow only sleeps and asks.** Each cycle runs one activity, `advance`. It reads the current revision, policy and time zone, writes any reminder that is due, and returns either "done" or the next instant to wake: the local midnight of the next threshold date, capped at 24 hours. The cap means policy and time-zone changes take effect within a day without signalling every workflow. After 20 cycles the workflow continues as new to bound its history.
- **Due means the hospital's calendar.** Threshold _t_ is due when `valid_until - t <= local today`. When several thresholds are due at once (a late approval, or a new policy), only the smallest is written, so HR sees the most urgent one and not a backlog. The workflow is done once the smallest threshold of the current policy has been written.
- **Reminders persist once.** `workforce_credential_reminders` is unique on (credential, revision, threshold, channel), so replay inserts nothing. The only channel is `hr_inbox`: the row is the delivery. External channels (email, SMS) need a notification platform the product does not have yet and are out of scope. A new reminder supersedes the open, larger-threshold reminders of the same revision. Approving a new revision supersedes every open reminder of the older revisions in the approval transaction, and the old workflow ends at its next cycle, when it sees the revision has moved. A trigger refuses inserts for anything but the current revision, so a late activity cannot resurrect a superseded schedule. Rows are never deleted; status moves `open` → `acknowledged` (by an attributable subject) or `open` → `superseded`.
- **Fail closed.** An unknown time zone, a missing policy or a revision mismatch writes nothing. Database errors are retried by Temporal (bounded backoff, then a visible workflow failure). The JetStream consumer redelivers a failed workflow start up to five times, then dead-letters it with an error log, the platform's standard path.
- **Worker identity.** The activity runs in the document worker on its own task queue, as the existing least-privilege `helivanta_document_worker` role. Additive permissive policies let that role read only the columns it needs, and only for the one credential named in transaction settings (`app.workforce_hospital`, `app.workforce_reminder_staff`, `app.workforce_reminder_credential`). Those columns are validity version and dates, the roster time zone and the policy thresholds, never number or issuer. The role may write only that credential's reminder rows, their audit rows (`service:workforce-credential-reminders`) and the `credential_reminder_changed` events for them. The worker's restrictive outbox boundary is recreated as document _or_ reminder events, nothing else. The API role is unchanged.

## API

Under `/workforce/hospitals/{hospital}`:

| Route                                                | Permission                          | Behaviour                                                                                                 |
| ---------------------------------------------------- | ----------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `GET /hr/credential-reminder-policy`                 | `enrollment.manage`                 | Current policy; `version: 0, thresholds: []` when reminders are off                                       |
| `PUT /hr/credential-reminder-policy`                 | `enrollment.manage` (manager grant) | `{expected_version, thresholds}`; identical thresholds return the current version; 409 `version_conflict` |
| `GET /hr/credential-reminders?status=`               | `enrollment.manage`                 | `open` (default), `acknowledged` or `superseded`; soonest due first; paginated                            |
| `POST /hr/credential-reminders/{id}/acknowledgement` | `enrollment.manage` (manager grant) | Idempotent for an acknowledged reminder; 409 `reminder_superseded`                                        |

All responses are `no-store`. Cross-tenant and cross-hospital ids answer 404.

## Events

- `helivanta.in.workforce.credential_reminder_policy_changed.v1`: hospital id and version.
- `helivanta.in.workforce.credential_reminder_changed.v1`: hospital, staff, credential and reminder ids, validity version, threshold days and status. Published on creation, acknowledgement and supersession.

All events go through the outbox in the business transaction and carry identifiers only.

## Platform

- **NATS:** one consumer (`workforce-credential-reminders`) on `credential_validity_changed`. The event's handler validates the envelope only; the workflow starts after commit.
- **Temporal:** task queue `workforce-credential-reminders-v1` on the document worker; workflow `workforce-credential-reminders-v1`, activity `workforce-credential-reminder-advance-v1`.
- **Valkey:** not used. Reminder state is confidential HR data and is served from PostgreSQL only.
- **SOC 2 / HIPAA:** append-only policy versions and reminders, attributable acknowledgement, audit rows on every insert and update, a least-privilege worker scoped per credential, and identifier-only events.

## Testing

- Pure tests of the due calculation: Asia/Kolkata and Pacific/Auckland midnights against UTC, the expiry day, a 29 February expiry, a late approval with several thresholds due, an unknown zone, and a policy without 0.
- Workflow tests (Temporal test suite, controlled clock): waking on each threshold, the 24-hour cap, continue-as-new, stopping on a superseded revision, and bounded retries leading to a visible failure.
- Real PostgreSQL tests as `helivanta_document_worker`: replay writes one row; the worker cannot read another credential, number or issuer, or emit any other subject; a stale-revision insert is refused; approval supersedes older reminders.
- API tests: policy versioning and conflicts, the inbox filter and pagination, acknowledgement idempotency, superseded refusal, 404 cross-hospital.
- UI tests: the policy form validation, the inbox list and acknowledgement toast.

## Not in this slice

- Email, SMS or push delivery.
- Roster credential conflicts (slice C).
- A backfill for approvals older than the stream. The new durable consumer starts from the oldest retained event (JetStream's default deliver-all policy), and the stream keeps 24 hours. Slice A merged at 04:37 UTC on 2026-10-18, so a deploy before 04:37 UTC on 2026-10-19 replays every approval. Deploy verification compares approved revisions with started schedules; any gap is filed as its own issue.
