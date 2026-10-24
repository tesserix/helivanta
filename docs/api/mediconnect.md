# MediConnect account and link-request API

Issue [#37](https://github.com/tesserix/helivanta/issues/37); implementation is
`backend/internal/modules/mediconnect`. This implements accounts and unverified link-request intake, not the
complete patient portal. No patient record is linked or disclosed by these APIs.

## Transport and authority

Clients use the Global API `/v1/mediconnect` prefix. Configure owner key
`mediconnect` explicitly in `GLOBAL_API_UPSTREAMS`; during modular-monolith
operation its origin is the existing API. Development defaults include it.
An omitted owner returns 503; no fallback to another product. The account/link baseline was deployed from `30efb51` through the Global API;
release evidence is in [PR #1318](https://github.com/tesserix/helivanta/pull/1318).

All seven endpoints require a verified Helivanta session and current tenant
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
transaction, using the existing outbox retry/DLQ lifecycle. No consumer is added
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
No verification processor is implemented; pending requests stay pending until a
separately reviewed hospital-owned verification workflow exists. Nonexistent but
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
claim events or cached patient projections are created.

## Accessibility migration

`0003_mediconnect` adds three constrained, non-null text columns to the owned
account table with constant `system` defaults. It uses a 5-second lock timeout and
60-second statement timeout. Existing accounts keep their IDs, version, timestamps
and original preferences; forced RLS and audit triggers remain in place. No index
or cache changes are needed. Previous binaries continue named-column operations
and leave the new values intact, so application rollback is compatible without
removing the columns. The application does not supply a destructive down migration.

## Remaining product scope

#37 is not closed: reviewed registration binding, profile links, scoped proxy
grants, immediate expiry/revocation/majority enforcement, approved notices and
rights-request routing remain. Existing patientmaster staff APIs are not a
patient-facing disclosure API. #77/#101 require practitioner verification and
scheduling ownership; #73 requires source-authorized released/amended report
versions. The account API does not expose legacy lab completion as report release.
Patient onboarding without existing tenant membership also remains separate.
OTP, UI/mobile rendering and clinical/legal policy activation remain separate.
Deployment evidence is tracked with each reviewed source revision. See the [delivery ledger](../delivery/mediconnect.md).
