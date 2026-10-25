# Practitioner master, scheduling and patient discovery

Status: implementation in progress; not deployed or approved for clinical use.
Tracks #39, #474, #77 and #102. These stories remain open until their full
acceptance criteria are met.

The practitioner owner is a module in the existing API, not another independently
scaled service. It owns professional identity and registration/verification
history. Workforce remains the HR owner; neither employment nor a job title grants
clinical authority. Scheduling owns availability and reservations. MediConnect
consumes approved professional projections and never edits source tables.

## Isolation and authority

#39 proposes residency-wide cross-tenant identity, while #474 explicitly requires
separate tenant records. Use verified tenant isolation, consistent with the root
enterprise rules and #474. No cross-tenant search or shared clinical identity is
introduced. A future shared identity would require a separate reviewed contract
and migration; no implicit cross-hospital grant comes from shared employment.
Canonical practitioner records are administrative data, not the public discovery
projection. Initial record intake requires `practitioner.record.manage`; reads
require `practitioner.record.read`. Both have no default nonadministrator role.
An administrator can record unverified facts; they cannot use intake to claim HPR
verification or grant a clinical privilege. Verification and clinical credentialling
need separate independently reviewed evidence and current source checks.

Assets include professional identity, private verification evidence and attribution
on clinical records. Threats include another tenant, an authenticated insider and
forged client verification claims. Verified sessions, membership, route permission,
forced tenant RLS, strict input and attributable read/write audit are separate
controls. The optional expected-tenant assertion detects stale browser workspaces;
it never supplies authority. No private evidence belongs in events or logs.

## Capacity and consistency

Planning envelope, not measured production demand: 500 practitioners per tenant
(#102's materialisation workload). Assume 200 discovery reads/second and 10
administrative writes/second across one deployment, <=8 KiB intake, and a 20:1
read/write ratio. These request rates are planning assumptions, not measured or
issue-mandated traffic; #102 specifies a 200-practitioner search dataset, not 200
requests/second. At 1,000 tenants this is 500,000 base records; assuming 10% annual growth,
550,000 at 12 months and 665,500 at 36 months. Registration/role histories need a
separate measured storage budget before their release. Practitioner privilege
resolution targets the #474 p95 30ms criterion. Initial design targets are p99
under 100ms for base-record reads and 99.9% monthly availability, pending load
measurement and operational acceptance; these are not measured guarantees. One indexed Postgres
satisfies the base-record working set; no cache, shard or search store is needed.

Intake commits the immutable client UUIDv7 intent and audit atomically. Same tenant,
ID and intent replay returns the retained record; changed intent conflicts. A name
is not a deduplication key. No downstream fact is emitted until a consumer and its
contract are implemented. PostgreSQL or read-audit failure prevents disclosure;
there is no stale cache fallback. Valkey stays a cache, never registry authority.

## Delivery sequence

1. Strict unverified base-record intake/read, immutable audit, tenant/workspace
   isolation and concurrent replay tests. No verification, registration or privilege
   claims can be supplied through this command.
2. Versioned council registrations and qualifications, exact private evidence,
   independent manual verification, suspension, expiry and history. HPR absence is
   explicit; no fabricated sandbox or production success.
3. Facility owner and practitioner roles, separately reviewed privileges, live
   effective-authority checks and historical attribution. Resolve #475 dependency
   against its actual owner before accepting facility IDs.
4. Recurring schedules/exceptions, bounded Temporal materialisation, atomic slot
   reservation/holds, appointment lifecycle/rescheduling/cancellation and FHIR
   projections, with #102's concurrency/performance acceptance.
5. Curated discovery (#77), patient/proxy booking (#101), and consent-aware email/
   WhatsApp reminders (#557). SMS stays disabled. Never invent symptom mappings,
   fees, ratings, availability, cancellation policy or clinical consent.

Migrations remain additive, forward-only and undeployed until their review and
rollout gates pass. Retained identity/history must survive application rollback.
Estimated base-record storage at 500,000 records and 1 KiB per record is 0.5 GB
before indexes, audit or replicas; a 3x base/index allowance is 1.5 GB. This is a
planning estimate, not measured billing. Cost uses existing Postgres capacity with
no additional cloud service;
measure indexes/history before expansion. Rejected alternatives: a second HR-backed
practitioner master duplicates ownership; cross-tenant matching exposes identity;
independent services add failure boundaries without a demonstrated need.

## Draft base-record contract

Only the module harness currently mounts these routes. The production registry,
Global API route ownership and generated inventory are deliberately not wired yet;
complete owner integration is a release gate, not evidence of current deployment.

- PUT `/v1/practitioners/:practitioner`: canonical lowercase RFC4122-variant UUIDv7
  command ID; requires `practitioner.record.manage`. Input is `given_name`, optional
  nullable `family_name`, and `professional_type`. Names are valid UTF-8, 1–150
  bytes, no surrounding whitespace/control characters. Professional types are
  physician, surgeon, dentist, nurse, pharmacist, pathologist, radiologist,
  lab_technician, physiotherapist, paramedic or biomedical_technician. Unknown fields,
  null/array bodies, trailing JSON and bodies over 8192 bytes are rejected with 400.
- First intake returns 201; same ID/content returns 200 with the retained record;
  changed content returns 409 `practitioner_conflict`. No name-based match or merge.
- GET at the same path requires `practitioner.record.read`; invisible objects or
  invalid path IDs return 404. Responses include ID, names, professional type,
  `verification_state: unverified` and UTC creation time, with no tenant, creator,
  contact destination, professional evidence or clinical data.
- Both use the existing optional expected-tenant check and `no-store`. Missing
  session returns 401; missing permission/membership follows the platform's 403/503
  contract. Storage/audit errors return sanitized 500 and never disclose names.
- `0001_practitioner` creates the forced-RLS immutable intake record and insert audit
  trigger with bounded migration timeouts. Read/replay auditing must commit before
  response disclosure. No default role other than the platform's implicit tenant
  administrator receives either permission. Neither permission is a clinical grant.

Verification and amendments must be implemented as separately attributable commands
before runtime registration. The immutable intake intent is retained, not overwritten
when a professional's subsequently verified identity differs from an initial claim.

## Registration ownership and renewal

Council registrations belong to the same practitioner owner. The initial command
records an unverified issuer/number claim, not council verification. `council_system`
is a stable, lowercase opaque issuer namespace, not a validated official council
or a URL to fetch. Exact `(tenant, practitioner, council_system, registration_number)` claims are
unique within a practitioner; another command ID for the same claim conflicts.
Unverified claims by different practitioners remain separate: an unverified entry
cannot reserve somebody else's professional identity. The future verification
workflow must reconcile conflicting claims and enforce canonical verified ownership
before any professional/clinical authority; intake never claims that reconciliation
has occurred. Numbers retain exact case/punctuation. Issuer-specific
normalization and council verification must be approved before identity matching
or clinical use; no universal normalization is guessed.

Initial PUT `/v1/practitioners/:practitioner/registrations/:registration` requires
the administrative manage permission. Body contains `council_system`,
`registration_number`, optional nullable ISO-calendar `issued_on` and `expires_on`.
An unknown expiry is unknown, not unlimited validity. Invalid calendar dates and
expiry before issue are rejected. Initial command/revision share the caller's
UUIDv7; first creation is 201, exact replay 200/current head, changed initial intent 409.
Detail GET requires the administrative read permission and commits its read audit.
No demographics, creator or private evidence is returned.

Renewal is a new immutable command at
`PUT /v1/practitioners/:practitioner/registrations/:registration/revisions/:command`:
`expected_version` plus a complete replacement of nullable issue/expiry dates
(omitted or null means unknown, not preservation of a prior value). It serializes on
the registration, appends the new revision and advances the head atomically. Exact
command replay returns its original receipt even after later renewals; changed
intent or stale version conflicts. It cannot rewrite the council or number. Prior
claims and attribution remain retrievable. Each new revision is unverified; future
verification must bind the exact revision and cannot carry over from earlier dates.

Both records and revisions use composite tenant/practitioner foreign keys, forced
RLS and database transition/commit guards. Reads, history and replay require current
permission, scope and audit. On audit/storage failure no dates or numbers are
returned. Every owner transaction has a five-second caller-cancellable budget and
a two-second database lock budget. Cancellation/deadline or lock timeout returns
503 `practitioner_unavailable`; clients reconcile the same command ID after an
ambiguous result rather than generating another intent. Other storage/audit errors
remain sanitized 500. Failed writes leave no revision or advanced head. No provider, clinical
privilege, FHIR compliance or HPR check is implied. Registrations without expiry or
verification remain unavailable as authority for clinical actions. Migration 0002
is additive, bounded and undeployed; no retained claims may be removed on rollback.

Registration and revision collections use administrative-read permission and the
standard keyset envelope at `/registrations` and `/registrations/:registration/revisions`
under the practitioner path. Limit 1–200, default 50; immutable `(created_at,id)`
descending pagination. Each receipt carries its source version; timestamps are not
clinical validity or revision authority. Lists audit the scoped parent and fetched
resources atomically, including empty collections. Read-audit failure returns an
error with no partial page. Nonexistent/wrong-tenant parents return 404.

Local verification so far covers first intake, exact replay, changed identities,
12-way conflicting renewals and same-command replay, renewal/unknown dates,
retained receipts after later revisions, bounded pagination, rejected verification
claims and invalid calendar dates, independent forced RLS, audit-failure rollback,
empty-list audit failure, deferred commit guards and request cancellation while the
registration row is locked. This is not independent manual/council verification,
clinical privilege evaluation or runtime deployment; those remain release gates.

## Registry reviewer appointment boundary

An accepted registry-reviewer appointment is administrative authority to review
professional claims, not verification of those claims, a professional licence or
hospital clinical privileges. Nomination requires `practitioner.steward.manage`
(no default operational roles). Nominator and nominee must differ. A nominee must
accept through their own verified, currently tenant-member session. No implicit
administrator, job title or client header bypasses acceptance or current validity.
This appointment boundary does not activate council/HPR or clinical decisions.

Nomination PUT `/steward-appointments/:appointment` records `subject`, opaque UUIDv7
`review_reference` and finite RFC3339 `valid_until` normalized to UTC without discarding submillisecond
precision (up to PostgreSQL microseconds). Initial version 1 is
`nominated`; immutable intent replays return retained current state, even after
expiry/termination, without renewal. Changed intent conflicts. Reviewer consent
is explicit: own POST `/my/steward-appointments/:appointment/accept` with
`expected_version:1` advances to `active`/version 2 only before validity ends.
Acceptance replay preserves any later terminal state and never reactivates it.

Management revoke and own withdrawal are explicit version-checked POST commands
with an opaque `review_reference`. They can end nominated or active appointments;
replay returns the retained terminal state, changed commands conflict. Termination
history is retained. At most one appointment is `active` per tenant/subject. On
accepting a replacement, the owner can atomically mark an already elapsed active
appointment `expired`; it cannot supersede an unexpired appointment implicitly.
These are administrative dates chosen by the tenant, not statutory expiry rules.

Management detail/list/history require the manage permission. Own detail/list/
history/current routes declare `authz.Public` only to skip job-role permission;
platform authentication and current tenant membership still apply, followed by
subject-level RLS/handler checks. Other subjects and tenants receive 404. A current
lookup returns only a live accepted appointment. All retained views expose an
`effective` flag computed with the database clock; `status:active` alone is never
authority. Future review commands must bind and lock the exact appointment/version
and reject conflicts of interest, elapsed/revoked authority or stale evidence.

Forced RLS separates own-appointment access from professional-master access. Own
routes never enable the professional record/registration GUCs. Immutable nomination,
strict database transition guards and append-only per-version history accompany
attributable read/write audit. Empty lists require an audit as well. The existing
five-second transaction/two-second lock bounds and sanitized errors apply. Local
clock testing replaces the clock function through an isolated owner migration;
there is no client/environment clock override in production. New migration 0003
remains undeployed with the rest of this draft.

The appointment implementation is verified locally with independent nomination,
explicit acceptance, 12-way replay, expiry and replacement, conflicting live
appointments, current membership denial/outage, empty-list auditing, read-audit
rollback, immutable database history, all-route stale-workspace rejection and direct
proof that steward self-service cannot see professional-master rows. Timestamp
input accepts JavaScript ISO millisecond output and equivalent explicit offsets,
normalizing to UTC; precision beyond six fractional digits and invalid offsets
are rejected before parsing, preventing silent truncation. These appointments
still grant no council verification or clinical privileges; evidence and qualified
review workflows remain incomplete.

## Private evidence storage boundary

The shared attachment store now recognizes tenant-owned `practitioner` objects
without a hospital ID. Keys bind the tenant, practitioner, registration revision,
immutable file ID/version and server-assigned UTC upload date. Their
`o/practitioner` namespace cannot alias organisation or hospital/staff objects;
existing object keys remain unchanged. Supplying a hospital is rejected rather
than silently discarded. The owner must derive these identifiers from authorized
records before invoking storage; an object key itself grants no authorization.

Local storage protocol tests cover exact-generation scanning, immutable promotion,
lost-copy receipt reconciliation and rejection of conflicting source generations
for staff, organisation and practitioner scopes. Infected content, incomplete scan
verdicts, missing scanners and unsupported types cannot mint promotion authority.
Supported inspection remains passive PNG/JPEG only; PDF is not supported.

This is storage support inside the undeployed draft. Practitioner upload commands,
durable processing, authorized evidence reads and independent professional
verification are still required. A clean malware scan never establishes a
qualification, council registration, HPR status or clinical privilege.

Registration evidence intake binds the complete tenant/practitioner/registration/
revision tuple; renewal never transfers evidence to another revision. Dedicated
`practitioner.evidence.manage` and `.read` permissions have no operational defaults.
PUT/GET at `/v1/practitioners/:practitioner/registrations/:registration/revisions/:revision/evidence/:file`
operate on an immutable UUIDv7 file intent. Inputs contain only `content_type`
(PNG/JPEG), `size` (1–10 MiB) and lowercase SHA-256; no original filename, remote
URL, verification assertion, tenant or storage identifier is accepted. New intents
return 201; exact replay returns 200 with retained metadata; changed intent is 409.
Reads/replays are audited before disclosure. Wrong parent/revision/tenant is 404;
missing evidence permission is denied even with professional-master permissions.
Intake remains `upload_pending`, not available, verified or published. A one-hour
server-assigned upload window bounds orphan intake; this is a platform capacity
policy, not a registration-validity or legal retention period. Ten files per
revision bounds intake, enforced under the revision lock in PostgreSQL. Intent
history is retained after expiration; uploads must never renew the window on replay.

Processing is a separate required command/worker boundary: authorize and reserve
server upload time, write immutable quarantine bytes outside database locks,
recheck authority and commit the exact receipt with an outbox request, then have
the scoped processing identity run real inspection/scanning and immutable promotion.
Only a matching release receipt may enable a private read; reads reauthorize and
audit the exact generation before bytes leave the owner. Storage/scan outages stay
unavailable; retries reconcile retained bytes instead of replacing them. Opaque
IDs alone belong in event/Temporal history. No production activation occurs until
these processing and independent professional-verification gates are implemented.

Local intake verification covers separate evidence permissions, forced RLS, strict
metadata, retained server expiry, 12-way same-intent replay, 20-way competing intake
at the ten-file limit, complete keyset traversal, exact revision binding after
renewal, attempted storage-verdict/history mutations and read/write audit outages.
All 22 harness routes reject stale workspaces before domain work. Migration 0004
is forward-only and retains intake history; no destructive down migration is
provided. It has been applied only to disposable test databases. Runtime contract
inventory remains unchanged because this owner is still deliberately unregistered.
