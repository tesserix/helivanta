# Privacy casework API

Owner: `privacy`, routed through Global API's existing `platform` destination.
[ADR-0038](../adr/0038-privacy-case-ownership.md) defines the boundary. This is
API-only authenticated, tenant-local casework, not a legal certification, clinical
consent grant, record disclosure or erasure engine. #66/#556 remain open for their
wider acceptance criteria. No production appointment or policy is seeded.

## Authority and scope

Every route requires a verified session and tenant membership. Tenant IDs, actor
IDs and representative authority are never accepted from clients. The
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

| Method/path | Contract |
| --- | --- |
| PUT `/officers/:officer` | Nominate immutable appointment; manage permission; 201 first, 200 identical replay |
| POST `/officers/:officer/accept` | Nominee accepts `{}`; replaces old active officer atomically; 200 |
| POST `/officers/:officer/revoke` | Manage permission; `{expected_version,review_reference}`; attributable terminal revocation, identical replay 200 |
| GET `/officer` | Current accepted officer's published contact and validity |
| PUT `/requests/:request` | Own immutable request intent; 201 first, 200 identical replay with current state |
| GET `/requests/:request` | Own/current-officer read with audit |
| GET `/requests` | Own keyset collection |
| GET `/queue` | Current officer's tenant keyset collection |
| PUT `/requests/:request/commands/:command` | Versioned attributable command; 201 first, 200 identical replay |
| GET `/requests/:request/commands` | Immutable principal-visible conversation and command receipts; keyset collection |

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

| Action | Actor | Transition |
| --- | --- | --- |
| start_review | Current officer, not reporter | received → under_review |
| request_information | Current officer, not reporter | under_review → awaiting_principal |
| reply | Reporter | awaiting_principal → under_review |
| respond | Current officer, not reporter | under_review → responded |
| withdraw | Reporter | Any nonterminal state → withdrawn |

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
event is reported as a completed workflow. Underlying owner fulfilment, reviewed
SLA policy/Temporal escalation, provider-confirmed notifications, proxy authority,
public intake and cross-tenant oversight are explicitly still outstanding.

Migration is additive and forward-only. Rollback disables the new routes without
deleting cases or audit. The app continues without configured casework. Clinical
or statutory readiness must not be inferred from route availability or passing
schema checks.
