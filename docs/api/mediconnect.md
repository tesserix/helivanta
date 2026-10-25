# MediConnect account and link-request API

Issue [#37](https://github.com/tesserix/helivanta/issues/37); implementation is
`backend/internal/modules/mediconnect`. This implements accounts, unverified link-request intake and explicit registrar
identity review, not the complete patient portal. Identity verification never
grants clinical access or establishes patient/proxy consent.

## Transport and authority

Clients use the Global API `/v1/mediconnect` prefix. Configure owner key
`mediconnect` explicitly in `GLOBAL_API_UPSTREAMS`; during modular-monolith
operation its origin is the existing API. Development defaults include it.
An omitted owner returns 503; no fallback to another product. The account/link baseline was deployed from `30efb51` through the Global API;
release evidence is in [PR #1318](https://github.com/tesserix/helivanta/pull/1318).

All MediConnect endpoints require a verified Helivanta session and current tenant
membership. Subject and tenant come only from that session. Account ownership is
also enforced by PostgreSQL forced RLS. Tenant admins cannot read another
subject's account through these endpoints. `authz.Public` here means no extra
staff-role permission; it does not skip session or membership checks.

| Method/path                                 | Input                            | Success                                                 | Failure                                                                          |
| ------------------------------------------- | -------------------------------- | ------------------------------------------------------- | -------------------------------------------------------------------------------- |
| POST `/v1/mediconnect/account`              | `{}`                             | 201 for first creation; 200 replay with current account | 400 invalid body; 401 invalid session; 403 nonmember; 503 membership unavailable |
| GET `/v1/mediconnect/account`               | None                             | 200 current account                                     | 404 account absent; authentication/membership failures as above                  |
| PATCH `/v1/mediconnect/account/preferences` | Complete preference object below | 200 updated account                                     | 400 invalid body; 404 absent account; 409 stale version                          |

POST is unique by tenant and verified subject, independent of any caller-supplied
idempotency key. Concurrent requests create one account and one creation event.
Replaying POST never resets preferences. After an ambiguous timeout, GET retrieves
the authoritative account. PATCH uses optimistic concurrency; after a timeout,
GET and compare the preferences/version before deciding whether to submit again.
Replaying an already-applied PATCH with its old version returns 409.

```json
{
  "expected_version": 1,
  "locale": "hi-IN",
  "preferred_channel": "email",
  "low_data_mode": true,
  "accessibility": {
    "text_size": "large",
    "contrast": "high",
    "motion": "reduced"
  }
}
```

Locale is a valid BCP47 tag, 2–35 bytes, expressing a preference rather than a
promise that translated content exists. `preferred_channel` is `none`, `email` or
`whatsapp`; SMS is rejected. It grants no messaging consent and sends nothing.
The original four fields are required on PATCH; version is an integer from 1 through 9007199254740991. Bodies are bounded to 4096 bytes, unknown fields and trailing
JSON values are rejected. Errors use existing `{error,message}` v1 conventions.

`accessibility` is an optional complete object. Its fields are `text_size`
(`system`, `large`, `extra_large`), `contrast` (`system`, `high`) and `motion`
(`system`, `reduced`). All default to `system`: no override of the device setting.
Omitting the object or sending null preserves all three saved values atomically;
sending all three as `system` resets them. An incomplete object, extra field,
invalid enum or wrong type returns 400 and changes nothing. Older callers retain
their existing contract and cannot erase settings they did not submit.

Settings share the account's expected-version guard and audit transaction with
locale/channel/low-data preferences. Concurrent writers using one version cannot
mix or overwrite each other's settings: one succeeds and the others return 409.
These values express presentation preferences only. This API does not implement
client rendering, infer a disability, or store a medical condition.

The account response contains UUIDv7 `id`, `locale`, `preferred_channel`, boolean
`low_data_mode`, the `accessibility` object, integer `version`, and RFC3339 `created_at`/`updated_at`. Defaults
are `en-IN`, `none`, false and version 1. No subject, tenant, contact destination
or demographics are returned. Responses carry `Cache-Control: no-store`.
The Go input/response types are the canonical wire definitions for this slice;
the registry-generated reference lists paths, migration and event schema.

## Persistence, audit and failures

`mediconnect_accounts` owns preferences only. No cross-module SQL or replicated
patient master. Its unique `(tenant_id,subject)` key handles concurrent creation;
updates guard the expected version. Forced tenant-and-subject RLS denies reads
without the transaction's verified actor context. Audit triggers record changes
without values. GET and creation replays append read audit records; failure to
record the read prevents disclosure.

`helivanta.in.mediconnect.account_created.v1` carries only `account_id`; envelope
type equals the subject and version is 1. It is committed in the account's
transaction, using the existing outbox retry/DLQ lifecycle. No consumer subscribes to this account-created event,
and no downstream completion is implied. If outbox/audit insertion fails, account
creation rolls back. PostgreSQL outage fails the operation; there is no stale
account cache. Valkey remains the platform cache, not account authority.

Migration `0001_mediconnect` is additive and forward-only. Reverting application
code leaves accounts and audit evidence intact; no automated data-dropping down
migration is supplied. Migrations `0001_mediconnect` and `0002_mediconnect`
were applied in the reviewed release; automatic promotion remains disabled.

## Unverified patient-link request intake

These endpoints record claims only. They do not look up patient demographics,
confirm a registration exists, approve a relationship or grant clinical access.
Intake does not automatically initiate verification. The separate explicit review
submission below invokes the hospital-owned workflow. Nonexistent but
syntactically valid identifiers receive the same intake response.

All routes below start with `/v1/mediconnect/account/link-requests` and require
an existing account. `:request` is a client-generated RFC4122-variant UUIDv7.

| Method/suffix             | Input                              | Success                             |
| ------------------------- | ---------------------------------- | ----------------------------------- |
| PUT `/:request`           | Claim object below                 | 202 recorded; 200 identical replay  |
| GET `/:request`           | None                               | 200 own request                     |
| GET collection            | `limit` (1–200), optional `cursor` | 200 standard `data`/`page` envelope |
| POST `/:request/withdraw` | `{"expected_version":1}`           | 200 terminal withdrawn request      |

```json
{
  "hospital_id": "11111111-1111-4111-8111-111111111111",
  "patient_id": "22222222-2222-4222-8222-222222222222",
  "relationship": "self"
}
```

Hospital and patient IDs must be nonzero UUIDs. Relationship is one of `self`,
`child`, `parent`, `spouse`, `sibling`, `ward`, `other`; none establishes proxy
authority. Strict 4096-byte JSON limits apply. Invalid input returns 400;
invalid request ID, absent account/request or another subject's request returns 404. A changed claim at the same ID or stale pending withdrawal returns 409.
Authentication and membership failures follow the account contract above.
Persistence/audit failure returns 500 without claim details.

Responses contain `id`, the three submitted claim fields, `status`, `version`,
`created_at`, `updated_at` and nullable `withdrawn_at`; no grants or demographics.
Initial state is `pending_verification`, version 1. Withdrawal is terminal,
version 2, with an immutable timestamp. Repeating withdrawal with its prior or
current version returns the recorded outcome. Replaying submission after
withdrawal returns that terminal state and never reopens it. After an ambiguous
timeout, GET or replay the same request ID; do not generate a new ID to retry.
Withdrawal cancels this request, not a clinical grant.

Migration `0002_mediconnect` adds `mediconnect_link_requests` with forced
subject-and-tenant RLS and a composite account foreign key. Database constraints
and a trigger prevent claim rewrites, deletion and unsupported transitions.
The same request UUID in another tenant is independent. Listing uses descending
`(created_at,id)` keyset pagination scoped to the verified account. Writes are
audited without claim values; reads and replay disclose only after their audit
transaction commits. List auditing is batched. Responses use `no-store`, and no
claim events are created by intake alone. Explicit review submission publishes the
minimal claim to patientmaster; no patient demographics are cached.

## Accessibility migration

`0003_mediconnect` adds three constrained, non-null text columns to the owned
account table with constant `system` defaults. It uses a 5-second lock timeout and
60-second statement timeout. Existing accounts keep their IDs, version, timestamps
and original preferences; forced RLS and audit triggers remain in place. No index
or cache changes are needed. Previous binaries continue named-column operations
and leave the new values intact, so application rollback is compatible without
removing the columns. The application does not supply a destructive down migration.

## Remaining product scope

#37 is not closed: profile links, scoped proxy
grants, immediate expiry/revocation/majority enforcement, approved notices and
rights-request routing remain. Existing patientmaster staff APIs are not a
patient-facing disclosure API. #77/#101 require practitioner verification and
scheduling ownership; #73 requires source-authorized released/amended report
versions. The account API does not expose legacy lab completion as report release.
Patient onboarding without existing tenant membership also remains separate.
OTP, UI/mobile rendering and clinical/legal policy activation remain separate.
Deployment evidence is tracked with each reviewed source revision. See the [delivery ledger](../delivery/mediconnect.md).

## Explicit hospital registrar review

The user selected independent hospital registrar evidence review; consent remains
separate. `POST /v1/mediconnect/account/link-requests/:request/review` accepts
`{"expected_version":1}` for an owned pending request: 202 on first submission,
200 with current projection on replay, 409 when withdrawn/stale, 404 when
invisible. `GET` at the same path returns that submission or 404. Both require
verified membership and an existing account, with no-store and read/write audits.

The original request's v1 statuses and versions do not change. The separate
projection contains `request_id`, `account_id`, hospital/patient/relationship
claim, `status`, nullable `decision_id`, `decision_outcome`, `reviewed_at`,
`withdrawn_at` and creation/update timestamps. Status is `submitted`,
`identity_verified`, `rejected` or terminal `withdrawn`. Submission means durably
queued; asynchronous delivery or hospital review can still be pending. Identity
verification is scoped to the claimed hospital registration. Relationship remains
a claim, never a proxy grant. No clinical data, evidence or reviewer identity is
returned to the account owner.

Patientmaster provides staff routes through the Global API:

- GET `/v1/patients/link-reviews?hospital_id=...`: standard keyset list, limit 1–200.
- GET `/v1/patients/link-reviews/:request?hospital_id=...`: scoped review and current
  `registration_updated_at` when a matching registration exists in that hospital.
- PUT `/v1/patients/link-reviews/:request/decision/:decision?hospital_id=...`:
  immutable decision keyed by UUIDv7. All require `patient.register` plus a
  current explicit hospital **registrar** grant locked through commit. A reader,
  tenant admin without that grant, other hospital or other tenant cannot read or
  decide the review. The requester cannot review their own claim.

Decision body requires captured `account_id`, `outcome`, `reason` and UUIDv7
`evidence_reference`. Approval requires outcome `approved`, reason
`identity_confirmed`, and `expected_registration_updated_at` from the detail GET.
The owner locks and compares the actual hospital-scoped registration. Rejection
requires outcome `rejected`, no registration timestamp, and reason
`identity_mismatch`, `registration_missing` or `insufficient_evidence`. The opaque
evidence reference attests to a hospital-retained review record; this API does not
fetch, upload or automatically validate it. No raw identity document is accepted.
Patientmaster's strict body cap is 8192 bytes. Malformed input returns 400;
invisible resources return 404; stale, self-reviewed, withdrawn or conflicting
decisions return 409. Successful decisions and identical replays return 200.
After a timeout, reuse the same decision ID/body or read the detail. Do not invent
a second decision ID. Evidence and reviewer identity remain with patientmaster.

Migrations `0004_mediconnect` and `0005_patientmaster` add separate owned review
tables, immutable lineage/decision guards and scoped RLS. Requested/withdrawn
MediConnect events carry request/account/hospital/patient IDs, requester subject
(only for independent-review enforcement) and the allowlisted relationship claim.
The patientmaster result carries those IDs except subject/relationship, plus
`decision_id`, `outcome`, `reviewed_at`. No evidence, demographic or consent data
enters these events. Contracts are in each owner's `contract/events.go` and the
[generated payload reference](reference/event-payloads.json).

The ordinary app-role event transaction uses exact tenant/hospital/request and
reserved consumer-actor context, with explicit tenant/object predicates. There is
no global RLS bypass. Decision, audit and outbox commit together; delivery errors
retry or dead-letter. Duplicate business events must match immutable lineage.
Cancellation before submission creates a tombstone. Local account withdrawal
wins over late approval; decisions remain retained for audit. No clinical grants
or consent receipts are created by this flow. No cross-owner SQL is used.

Migration is additive and forward-only; do not drop review/audit records on
rollback. Pre-review writers do not publish withdrawal events, so rolling back to
a pre-review binary while submissions exist requires a coordinated write hold
and reviewed reconciliation. Deployment is tracked separately in the delivery
ledger; passing local tests alone does not mean this workflow is deployed.

### Withdrawal reconciliation during rolling upgrades

`0005_mediconnect` allows the exact review-result consumer to read its original
request through tenant/request-scoped RLS; the original request's write policy
remains subject-owned. Result delivery locks the original request before its
projection. GET review and withdrawal retries also consult that source. If an
older writer recorded withdrawal without a cancellation event, these paths
repair the projection and publish the missing cancellation in one transaction.
The GET repairs only an already-committed withdrawal; it never initiates one.
Outbox/audit failure returns an error instead of exposing a stale verified link.
Repeated reads/results do not publish another cancellation. Late and duplicate
approvals preserve withdrawal, including when the decision predated the older
writer's withdrawal. This is bounded read-time/delivery recovery, not a background
reconciliation worker: rollback to older readers/writers still requires a write
hold and reconciliation. The initial registrar image must not be promoted without
this follow-up fix.

## Tenant notices and account consent declarations

See [ADR-0037](../adr/0037-mediconnect-account-consent.md). This is an optional
account-level workflow. It does not change account creation, publish legal text,
activate processing, send messages, establish legal capacity or parental consent,
or grant clinical/proxy access. Approved content and applicable eligibility rules
remain the tenant privacy reviewer's responsibility; no production notice is seeded.

All paths start `/v1/mediconnect` and use verified tenant membership. Publication
and retirement require `mediconnect.notice.manage` (tenant-admin only by default).
Members can read notices before creating an account. Receipt operations require
an existing account and are scoped to its verified subject, even for tenant admins.

| Method/path                                | Behavior                                                            |
| ------------------------------------------ | ------------------------------------------------------------------- |
| PUT `/notices/:notice`                     | Publish immutable reviewed text, 201 first / 200 identical replay   |
| GET `/notices/:notice`                     | Read a retained notice, including a retired version                 |
| GET `/notices`                             | Keyset list; status defaults to published; filters purpose/locale   |
| POST `/notices/:notice/retire`             | Terminal retirement with `expected_version` 1; retry accepts 1 or 2 |
| PUT `/account/consents/:consent`           | Explicit acceptance receipt, 201 first / 200 identical replay       |
| GET `/account/consents/:consent`           | Read own receipt and current notice status                          |
| GET `/account/consents`                    | Keyset list; optional status active/withdrawn and purpose           |
| POST `/account/consents/:consent/withdraw` | Terminal withdrawal with expected version; idempotent retry         |

`:notice` and `:consent` are canonical RFC4122 UUIDv7 values. Publication body:

```json
{
  "purpose": "communications",
  "locale": "hi-IN",
  "version_label": "reviewed-version-label",
  "title": "Tenant-provided reviewed title",
  "text": "Tenant-provided reviewed plain text, in the exact selected language.",
  "review_reference": "019a1b52-0000-7000-8000-000000000001"
}
```

This example is not a privacy notice. Publication is an attributable tenant
attestation to a retained review record; the service does not fetch or certify
that record. Purpose is account_processing, communications or analytics only.
Text is at most 8192 UTF-8 bytes, title 200, version label 64; all are nonblank and
contain no NUL. Locale is a canonical BCP47 tag of 2–35 bytes. Text is retained
exactly and must be rendered as text, never HTML. Request body cap is 16384 bytes;
unknown fields/trailing JSON are rejected. Other commands retain the 4096-byte cap.

The server returns `content_hash` (64 lowercase hex characters), computed from
its serialized immutable notice content. Treat it as an opaque version proof:
clients echo the supplied value after displaying that exact notice, rather than
recomputing it with another JSON serializer. The response retains notice ID,
content, status, version and timestamps; it excludes reviewer evidence and subject.
The same purpose/locale/version label cannot be republished at another ID. Changed
content or review reference under an existing ID conflicts. Retirement preserves
text and hash, sets version 2 and an immutable timestamp, and never reactivates on
publication replay. Multiple published versions may coexist until the tenant
explicitly retires them; no locale fallback or silent notice replacement occurs.

Acceptance body:

```json
{
  "notice_id": "019a1b52-0000-7000-8000-000000000001",
  "content_hash": "0000000000000000000000000000000000000000000000000000000000000000",
  "accepted": true
}
```

The illustrative hash above is not valid for a real notice. A receipt pins account,
notice, purpose, locale, version label and exact content hash. Only one active
receipt per account/purpose is allowed; changing the notice requires explicit
withdrawal and a new receipt ID. Replays return the original current receipt,
including after withdrawal or notice retirement, and never create a new grant.
Retired notices reject new receipts. A retired notice may still have an active
historical declaration: responses expose `notice_status` so it cannot be mistaken
for a current processing permission. Responses are snapshots, not authorization
leases; future processing owners must recheck current authority atomically before use.

Invalid input returns 400; missing permission 403; invisible resources 404; stale
hash/version, retired notice on a new acceptance, reused keys with different input
and duplicate active purpose return 409 `consent_conflict`. Dependency/audit failure
returns 500 without content or evidence. All responses are no-store. Lists use the
standard 1–200 limit and opaque cursor; a maximum-sized notice list can reach
approximately 1.6MB of text, so clients should filter by locale and purpose.

`mediconnect_notices` and `mediconnect_account_consents` are owned by MediConnect.
Migration 0006_mediconnect adds forced RLS, immutable history, composite scoped
foreign keys, bounded DDL and indexes. Account lock precedes notice/receipt locks;
new acceptance holds a notice share lock through commit so retirement cannot race
it. Withdrawal, audit and outbox commit together. Sensitive reads audit before
response; audit failure prevents disclosure and rolls back a mutation's receipt.

Events `helivanta.in.mediconnect.account_consent_recorded.v1` and
`helivanta.in.mediconnect.account_consent_withdrawn.v1` carry only consent_id,
account_id, notice_id and the allowlisted account purpose. They contain no notice
text, review evidence, identity subject, demographics or clinical information.
NATS outage leaves durable outbox delivery pending. No consumer, notification,
clinical processing, guardian grant or legal certification is implied. Consent
records and notice history are retained on rollback; no data-dropping down path.
