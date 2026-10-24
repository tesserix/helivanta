# Registrar-reviewed hospital registration binding

The user selected hospital-registrar review of identity evidence on 2026-10-25;
patient/proxy consent remains separate. This implements that review mechanism for
#37/#552. It does not create clinical permissions, an OpenFGA patient grant,
verified guardianship, marketing consent or a national-ID verification claim.
Applies IN-ID-01, IN-ID-04, IN-ID-06 and IN-ID-07: hospital ownership, attributable
review, purpose separation and audited disclosure. No new legal rule is activated.

## Owners and boundary

MediConnect owns the authenticated account, immutable link request, submission
and account-visible review projection. Patientmaster owns hospital registration,
registrar grants, review worklist and immutable decision/evidence attestation.
Only published contracts cross these owners; neither reads the other's tables.
The existing bus commits inbox effects transactionally and retries/DLQs failures.
Events remain within the verified tenant; directed cross-tenant delivery is
rejected. No new service, Temporal workflow, cache, or privileged credential.

An explicit patient.register permission AND a current hospital registrar grant
are required to read the worklist or decide it. The existing hospitalTransaction
locks that grant through commit. Tenant-admin status alone is insufficient.
The reviewer cannot approve their own request. The producer carries the opaque
requester subject solely for that separation-of-duty check; no names, contacts,
national IDs, demographics, evidence content or evidence references enter events.

## Additive workflow

Existing link-request states and version semantics remain unchanged. A new POST
`/v1/mediconnect/account/link-requests/:request/review` with
`{"expected_version":1}` explicitly submits an owned pending request. It locks
the request, records a unique submission and outbox intent atomically, returning
202; identical replays return 200 with current submission. Withdrawal returns 409.
The new GET at the same path returns only the caller's submission, or 404.

The submission has states `submitted`, `identity_verified`, `rejected`,
`withdrawn`. Submitted means durably queued, not acknowledged by the hospital.
Successful identity verification records a hospital-scoped registration binding,
not chart access. The original relationship remains a claim; consent and legal
proxy authority are independent. A missing/mistyped registration is not disclosed
to the submitting user by automatic lookup.

Patientmaster consumes the submitted event into a hospital-scoped worklist. GET
`/v1/patients/link-reviews?hospital_id=...` is bounded/keyset-paginated; GET
`/v1/patients/link-reviews/:request?hospital_id=...` returns the scoped claim and
current registration timestamp, when the claimed registration exists in that
hospital. Only an admitted registrar may see this information, with read audits.
No new patient demographic copy is stored in the worklist.

PUT `/v1/patients/link-reviews/:request/decision/:decision?hospital_id=...`
uses a client UUIDv7 decision ID and body containing the captured account ID,
`outcome` (approved/rejected), an allowlisted reason and an opaque UUIDv7 reference
to the hospital-retained identity-review evidence. Approval also supplies the
current registration timestamp from GET and requires an exact hospital-scoped
patient match under a row lock. The evidence reference is an attributable human
attestation, not an assertion that this API retrieved or electronically validated
a document. Hospitals must retain the review evidence under their approved policy.
No file upload, raw national ID or free-form clinical narrative is accepted.

One immutable decision per request; identical decision/body replays return the
recorded receipt, changed input or a second decision conflicts. Approval requires
`identity_confirmed`; rejection reasons are `identity_mismatch`,
`registration_missing`, `insufficient_evidence`. The same transaction records the
decision, advances worklist state, writes its audit and queues the result event.
A revoked grant, self-review, stale registration, wrong tenant/hospital, absent
registration on approval, audit failure or outbox failure cannot commit approval.

## Withdrawal, ordering and recovery

MediConnect withdrawal locks the original request, marks any submission withdrawn
and queues its cancellation in the same transaction. Existing original-request
replay/version behavior stays compatible. Hospital delivery may lag, but its late
approval cannot reactivate the account-owned submission: consumer application
locks and checks the original local request before its projection. Receipts remain attributable.
Cancellation arriving before submission creates a tombstone; later submission
must match immutable identity and cannot reopen it. Duplicate business events are
idempotent even under new event IDs; changed identity/decision payloads are errors
for retry/DLQ investigation, not last-write-wins. No clinical access depends on an
asynchronously revoked tuple.

Reads are no-store and audit failures prevent disclosure. Database errors are
sanitized. RLS protects both tenant and subject/hospital on app-role paths; bus
consumers use the app pool and scoped RLS policies requiring the exact bus actor,
tenant, hospital and request delivery context, plus explicit object predicates.
Audit attribution uses the existing bus actor context. No global RLS bypass or
cross-module SQL is introduced.

## Persistence and capacity

Additive migrations: 0004_mediconnect and 0005_patientmaster (verify actual next
owner ID before coding). Composite tenant/object keys and FKs preserve ownership.
Immutable guards preserve claims/decisions. Review status updates are guarded and
terminal withdrawal is structural. Request body caps: MediConnect 4096 bytes, patientmaster 8192 bytes; list limit 1–200;
no query fan-out per listed row. Single-owner transaction plus durable handoff
bounds failure domains. Existing rate limits/outbox retry/DLQ policies apply.
Traffic forecasts and measured end-to-end review latency are unknown; hospital
human-review time is not presented as an infrastructure SLO. No extra workloads.

Migration/rollback retain records and columns. A previous binary will not service
new review routes or publish new cancellation events; therefore do not roll back
to a pre-review writer while submissions exist without a coordinated write hold
and reviewed recovery. This limitation must be recorded before deployment.

## Acceptance

Real Postgres/NATS/Global API journey: create account/request → submit → registrar
worklist → verify existing registration/evidence reference → owner sees verified
binding → withdraw → remains withdrawn despite duplicate/reordered results.
Assert cross-tenant/hospital/peer/reader denial, explicit-grant revocation,
self-review denial, nonexistent/stale patient rejection, partial JSON denial,
idempotency/conflict, cancellation-first tombstones, audit/outbox rollback and
no clinical grant/consent creation. Contract generation, consumer/schema parity,
full race/lint/build/CI and migration rehearsal precede a reviewed manual rollout.

Rollout review reproduced a mixed-version gap: older writers could withdraw only
the original request. Migration 0005_mediconnect adds narrowly scoped consumer
source-read authority, never source-write authority. Result application, review
GET and withdrawal retries atomically reconcile a missing cancellation before
returning state. Original-request → projection lock order is shared by every
path. Tests cover late/replayed approvals, read recovery, withdrawal retry,
exactly-one recovered cancellation, outbox failure and denied consumer writes.
