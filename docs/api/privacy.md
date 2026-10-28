# Privacy casework API

Owner: `privacy`, routed through Global API's existing `platform` destination.
[ADR-0038](../adr/0038-privacy-case-ownership.md) defines the boundary. This is
API-only authenticated, tenant-local casework, not a legal certification, clinical
consent grant, record disclosure or erasure engine. #66/#556 remain open for their
wider acceptance criteria. No production appointment or policy is seeded.

## Authority and scope

Every route requires a verified session and tenant membership. The optional
`X-Helivanta-Expected-Tenant` header must contain exactly one nonzero UUID equal
to the verified session tenant. A stale, empty, malformed, repeated or comma-joined
value returns 409 `session_changed` with `Cache-Control: no-store` before domain
reads or writes. Omission preserves existing callers. This precondition detects
an unintended workspace switch; it grants no membership, officer or case access.
Tenant IDs, actor IDs and representative authority are never accepted from clients. The
`privacy.officer.manage` permission is tenant-admin-only by default and permits
nomination/revocation, not case access. The nominee must accept from their own
verified tenant session. Exactly one accepted active appointment is current;
validity is rechecked against database time on each officer operation. Replacement
or revocation serializes with case transactions. Older pending nominations are
superseded so they cannot later displace the latest appointment. Subject IDs and
review references are private, absent from responses. Current public contact is
available to tenant members. No contact verification or notification is claimed.

Patients can read their own cases, reply when information is requested and
withdraw nonterminal cases. The current officer may read the tenant queue and
review/respond to other subjects' cases. Officers cannot adjudicate their own
request. Tenant admins have no implicit case-read privilege. Missing, other-tenant
and other-subject objects return 404; an unauthorized action on a visible object
returns 403. Missing current officer returns 503 for new intake/publication; own
existing reads, receipts, replies and withdrawal remain available.

## Routes

All paths below have `/v1/privacy` prefix (`/api/v1/privacy` through the shell proxy).

| Method/path                                | Contract                                                                                                         |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| PUT `/officers/:officer`                   | Nominate immutable appointment; manage permission; 201 first, 200 identical replay                               |
| POST `/officers/:officer/accept`           | Nominee accepts `{}`; replaces old active officer atomically; 200                                                |
| POST `/officers/:officer/revoke`           | Manage permission; `{expected_version,review_reference}`; attributable terminal revocation, identical replay 200 |
| GET `/officer`                             | Current accepted officer's published contact and validity                                                        |
| PUT `/requests/:request`                   | Own immutable request intent; 201 first, 200 identical replay with current state                                 |
| GET `/requests/:request`                   | Own/current-officer read with audit                                                                              |
| GET `/requests`                            | Own keyset collection                                                                                            |
| GET `/queue`                               | Current officer's tenant keyset collection                                                                       |
| PUT `/requests/:request/commands/:command` | Versioned attributable command; 201 first, 200 identical replay                                                  |
| GET `/requests/:request/commands`          | Immutable principal-visible conversation and command receipts; keyset collection                                 |

All path IDs and review references are canonical UUIDv7. The request body cap is
16 KiB; unknown fields, multiple JSON objects and invalid IDs are rejected.
Nomination input: `subject` (1–512 bytes), `display_name` (1–200 bytes), canonical
bare `contact_email` (<=254 bytes), `review_reference`, future RFC3339 `valid_until` with at most microsecond precision. Responses normalize timestamps to UTC.
There is no default statutory appointment duration. A retained review reference is
an attributable attestation, not a fetched or automatically approved document.

Request input: `kind` (`access|correction|erasure|grievance`), nonblank UTF-8
`description` (<=4096 bytes). The initial appointment ID is retained as routing
provenance; consumers must not treat it as the current officer authority.

Command input: `expected_version` (1–1,000,000), `action`, nonblank UTF-8 `message`
(<=4096 bytes). Command receipts contain ID, request ID, action, message,
expected/resulting versions and creation time; no actor subject is disclosed.
Replay returns the recorded command, not a new transition. Reusing an ID with
changed data, another case or another actor returns 409 `privacy_conflict`.

| Action              | Actor                         | Transition                        |
| ------------------- | ----------------------------- | --------------------------------- |
| start_review        | Current officer, not reporter | received → under_review           |
| request_information | Current officer, not reporter | under_review → awaiting_principal |
| reply               | Reporter                      | awaiting_principal → under_review |
| respond             | Current officer, not reporter | under_review → responded          |
| withdraw            | Reporter                      | Any nonterminal state → withdrawn |

`responded` means a reasoned case response was recorded. It does **not** mean data
was supplied, corrected or erased; no such effects are implemented by this owner.
Responses must not embed clinical files or pretend to fulfil another owner's
command. `responded` and `withdrawn` are terminal; history cannot be deleted.

## Consistency, failures and evidence

`0001_privacy` adds forced-RLS `privacy_officers`, `privacy_requests` and
`privacy_commands`. New state and response-read audit share one transaction.
Database guards require attributable command history for transitions; a deferred
constraint requires each command to commit its transition. Read-audit failure
prevents response disclosure. Officer authority changes use an exclusive tenant
advisory transaction lock; case operations use its shared counterpart and then
lock the request row. SQL uses verified tenant predicates and subject/current-
officer authority. This lock is a single-Postgres transaction mechanism, not a
cross-service lease or a cache authorization source.

Lists use bounded `(created_at,id)` keyset pagination and batched read-audit
inserts. All responses have `Cache-Control: no-store`; SQL/internal errors are
sanitized. No private case payload is published to an event bus and no unconsumed
event is reported as a completed workflow. Underlying owner fulfilment, provider-confirmed notifications, proxy authority,
public intake and cross-tenant oversight are explicitly still outstanding.

Migration is additive and forward-only. Rollback disables the new routes without
deleting cases or audit. The app continues without configured casework. Clinical
or statutory readiness must not be inferred from route availability or passing
schema checks.

## Reviewed deadline extension (API/schema deployed; worker activation pending)

The API/schema release at `19b8a5f` is verified; orchestration remains disabled
until the separate worker rollout is healthy. See the
[release evidence](../delivery/mediconnect.md#deadline-release-verification--2026-10-25).

ADR-0039 and migration `0002_privacy` introduce independent `primary` and
`escalation` officer appointments. Omitted nomination role remains `primary` for
existing clients. Each role has one current accepted appointment; replacement and
pending-nomination supersession affect only that role. Accepted escalation
officers can review the tenant queue, but cannot adjudicate their own cases.
Revoking an escalation appointment does not revoke the primary officer.

Deadline routes:

| Method/path                               | Contract                                                                                                               |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| PUT `/deadline-policies/:policy`          | Requires `privacy.deadline.manage`; immutable proposal, 201 first / 200 identical replay                               |
| POST `/deadline-policies/:policy/approve` | Accepted escalation officer reviews; requires a distinct accepted primary officer and configured workflow support; 200 |
| GET `/requests/:request/deadline`         | Reporter/current officer; audited case-scoped clock and escalation receipts; 200                                       |

Policy input contains `version_label` (1–64 bytes), `acknowledgement_seconds`
(60–31,536,000), `resolution_seconds` (at least acknowledgement, at most
31,536,000), explicit boolean `pause_while_waiting`, UUIDv7 `review_reference`
and future `valid_until` with at most microsecond precision. Missing or null pause
choice is rejected with 400. These bounds are technical capacity limits, not
statutory deadlines. Policy versions are unique within the verified tenant.
Changed replay conflicts. Review references are retained privately.

Activation retires the previous active policy without erasing its attribution.
Admin permission alone cannot approve. Missing workflow configuration returns
503; no default policy or officer is seeded. `PRIVACY_DEADLINES_ENABLED` enables
API orchestration independently of document/emergency features, using the named
Temporal address and namespace. Disabling this switch blocks policy approval;
existing approved policies still require their consumer and worker to remain
running until all associated clocks finish.

Migration `0002_privacy` pins a current reviewed policy at intake and stores the
clock, pause duration, acknowledgement and terminal status. An unconfigured
case returns `monitoring: unmonitored`; policies do not backfill existing cases.
Case transitions and metadata-only outbox events commit in one transaction.
`helivanta.in.privacy.deadline_changed.v1` carries only request ID and version,
under the verified event tenant. The consumer validates the envelope and starts
or signals the matching durable Temporal workflow; duplicate delivery cannot
reopen a completed workflow. Dependency failure remains retryable, not success.

The separate `/app/privacy-worker` uses `PRIVACY_WORKER_DATABASE_URL` and the
restricted `helivanta_privacy_worker` role. Startup rejects application, owner,
inherited or bypass identities; production requires verified database TLS.
The role cannot read case descriptions, contact details or response messages.
Activities bind both tenant and case, recheck database time and committed state,
and preserve one immutable receipt per acknowledgement/resolution breach.
Missing or expired escalation officers produce `routing: unconfigured` evidence.
A deadline breach never discloses, erases or resolves the underlying case.

Clock reads return `pending`, `observed`, `escalated` or `stopped`, the last
worker observation timestamp and up to two escalation receipts. `observed`
means a historical committed observation, not a claim that the worker is currently
healthy. Audit failure prevents disclosure. Pauses extend only resolution time;
terminal transitions stop monitoring. Worker restart, stale delivery and replay
retain the original clock identity. Notifications remain unimplemented.

Rollout order is dedicated database role/credential and network access, additive
migration, compatible API image, then worker and orchestration activation. Keep
compatible workers and consumers running for accepted policies during rollback;
never remove the role or drop clock/audit tables. Verify ready replicas, exact
image source, restricted-role access and a synthetic restart/replay journey before
activating any tenant policy.
