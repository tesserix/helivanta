# MediConnect patient-link request intake — #37 / #552

API-only continuation. Account owners can submit an unverified claim about a
hospital registration, page/read their requests and withdraw a pending request.
This is intake, not profile creation or authorization. No approval endpoint,
clinical access, demographic matching or patient-owner table lookup is added.
Existing staff APIs cannot establish account identity on their own. The later
hospital-owned verification flow must confirm existence, scope, identity, notice,
authority and evidence before granting access. Family claims are not evidence.

Owner: MediConnect. Source claims: authenticated account holder. Consumers:
MediConnect clients through Global API; no downstream consumer yet. Current
session tenant and subject plus an existing account scope every operation.
Hospital/patient IDs are untrusted claimed references, never lookup authority.
Accepting nonexistent IDs is deliberately indistinguishable from accepting real
ones; acceptance means recorded for verification, not patient existence.

PUT /v1/mediconnect/account/link-requests/{UUIDv7} creates with hospital_id,
patient_id and relationship (self/child/parent/spouse/sibling/ward/other). IDs must
be nonzero UUIDs. A tenant-wide immutable request ID belongs to its original
subject; invisible collision returns 404. Identical replay returns current state,
changed payload returns 409, including after withdrawal. New submission is 202;
replay is 200. The same claim with a new request ID is a distinct request, not a
second grant. No automatic identity merge or uniqueness by phone/name.
GET the collection uses existing 1–200 keyset pagination; GET by ID returns one
owned request. POST /{id}/withdraw takes expected_version; pending→withdrawn is
terminal, version increases once, exact replay with the prior/current version
returns the same withdrawn state, stale/invalid versions conflict. Withdrawal
cancels the request only; it cannot revoke a clinical grant (none is created).

Rows have tenant, subject, account FK, UUIDv7 request ID, immutable claim,
status/version/timestamps and a withdrawn_at consistency check. Forced RLS
requires tenant AND subject. Audit trigger records writes without values; reads
and lists are audited transactionally before disclosure. No event publishes an
unverified claim, and no success implies downstream completion. Persisted
requests remain available for a future reviewed verification consumer; there is
no pretend background processor. PostgreSQL outage fails closed, no cache.

Threats: another subject/tenant enumerating claims, forged identity headers,
replaying an ID with different patient/hospital, stale cancellation, disclosure
without audit. Errors are generic and do not log SQL that might contain claims.
IN-ID-01/04/06/07 apply; no regulatory policy is activated. Append-only migration,
no destructive rollback. Existing platform budgets apply; bodies ≤4096 bytes,
one bounded page per collection request, no new runtime or service.

Tests: real Postgres through HTTP; immutable replay/conflict, same-/cross-tenant
visibility, paginated own records, absence of clinical state, withdrawal replay,
concurrent duplicate requests, missing account, invalid IDs/body, audit and RLS.
This does not close #37/#552 or complete the full MediConnect API.
