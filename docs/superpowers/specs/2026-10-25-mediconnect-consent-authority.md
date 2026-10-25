# Reviewed authority to declare clinical consent

Implementation scope for #60/#552/#556, following IN-ID-04/06/07 and the current
patient/clinical, HTTP, schema and integration contracts. This is a prerequisite
within the full MediConnect delivery, not completion of those stories.

Patientmaster owns hospital registration and this separately reviewed authority.
MediConnect remains an account/claim consumer; its asynchronous identity projection
cannot authorize clinical actions. A new patientmaster record lets an independently
authorized hospital reviewer attest that the verified subject may declare their
own purpose-specific clinical consents. Identity review alone does not create it.
The initial self-authority lifecycle is followed by separately reviewed guardian
and subject-issued adult delegation, clinical consent and owner record release.
No source patient table or consent authority is copied to another product.

The reviewer needs `patient.consent-authority.review` and a current explicit
hospital registrar grant, locked through commit. The permission has no default
staff-role grant. The reviewer cannot attest their own authority. A proposal pins
an approved self identity decision, its current registration version, separate
hospital-retained capacity evidence and reviewed policy references, and an explicit
expiry. The hospital is accountable for those attestations; UUID syntax does not
verify the referenced evidence. No age, clinical exception or legal policy is
inferred or seeded. Existing registration receipts and account notices remain
outside clinical authority.

The patient must explicitly accept the proposal under their verified subject;
staff cannot perform that transition. This accepts the reviewed authority to
make later consent declarations, not treatment, record disclosure, proxy access
or processing. An independently captured clinical consent will still need its
own approved notice, purpose, recipient, resource scope and current authority.
Identity-claim withdrawal remains distinct from withdrawal of this new authority;
clients must expose the new authority's direct revoke operation. The owner also
checks the source review status before acceptance. Source review cancellation
may arrive asynchronously, so no consumer may treat this record alone as proof
that the original MediConnect claim is still current.

State is `proposed -> accepted -> revoked`, with direct proposal revocation also
allowed. Revoke is terminal and requires no reason; the subject or a currently
scoped reviewer may revoke. Expiry is evaluated against database time, even if no
timer ran. Replay returns the current state and never restores an earlier state.
Changed creation input or stale command versions conflict. History is retained.
All reads and mutations are audited; read audit failure prevents disclosure.
Business writes and audit use one PostgreSQL transaction. No network calls occur
inside transactions and no cache is an authority. Valkey remains the platform
cache; clinical authorization requires authoritative current state.

Routes under `/v1/patients`, entered through Global API:

- `PUT /consent-authorities/:authority?hospital_id=...`: reviewed immutable
  proposal, client UUIDv7, 201 first/200 identical replay.
- `GET /consent-authorities/:authority?hospital_id=...`: current reviewer receipt.
- `GET /my/consent-authorities?hospital_id=...`: bounded subject-owned cursor page.
- `GET /my/consent-authorities/:authority?hospital_id=...`: subject receipt.
- `POST /my/consent-authorities/:authority/accept?hospital_id=...`: explicit true
  acceptance and expected version, subject only.
- `POST /my/consent-authorities/:authority/revoke?hospital_id=...`: subject revoke.
- `POST /consent-authorities/:authority/revoke?hospital_id=...`: reviewer revoke.

All require verified authentication and tenant membership. Hospital IDs are
selectors; staff grants or exact subject ownership are checked server-side and
in forced RLS. Hidden objects return 404, permission failures 403, malformed
bodies 400, changed/stale state 409, audit/database unavailability fails closed.
Responses are no-store and expose no evidence reference, reviewer identity,
subject identifier or demographics to the patient. Bodies cap at the existing
patient decoder's 8 KiB, unknown fields are refused, and time precision is bounded
to PostgreSQL microseconds. No endpoint releases clinical data.

Capacity assumptions, not measured demand: 20 peak proposals/s, <4 KiB bodies,
100 peak receipt reads/s, 100k/300k authority records at 12/36 months, 99.9%
availability and p99 <300 ms excluding authentication. Keyset indexes include
tenant/hospital/subject, time and ID. This uses existing API/PostgreSQL capacity;
there is no new process or infrastructure cost. Database/auth/audit outage denies
access; missing notice/policy/clinical consumers cannot be represented as success.

Migration is additive and forward-only with bounded DDL timeouts, UUIDv7 and
composite tenant references, forced RLS, immutable lineage and audited transitions.
Old binaries ignore the new records. Retain records and compatible evaluators
once clinical consents reference them; dropping data is not rollback. Exercise
real PostgreSQL, negative tenant/subject/hospital/permission checks, self-review,
stale registration, expired/revoked source, duplicates/concurrency, audit failure,
Global API forwarding, upgrade/repeat and prior migrator compatibility. Later
clinical release must independently validate the original claim's current owner
state, authority, consent, source record/version and release status; this proposal
cannot stand in for those checks.
