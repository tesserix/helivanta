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

This is storage support inside the undeployed draft. Runtime processing activation
and independent professional verification are still required. Local content reads
are described below. A clean malware scan never establishes a
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
All 23 harness routes reject stale workspaces before domain work. Migration 0004
is forward-only and retains intake history; no destructive down migration is
provided. It has been applied only to disposable test databases. Runtime contract
inventory remains unchanged because this owner is still deliberately unregistered.

## Quarantine upload and authorization refresh

PUT at the evidence path plus `/content` requires the current evidence-manage
permission and verified tenant membership. Raw bytes must match the retained
content type, exact bounded size and SHA-256; inspection rejects incomplete or
unsupported content before storage. The owner reserves one server upload timestamp
and never renews it on retry. The upload window uses the database's real registry
clock, with no client/environment clock override.

Storage runs outside database locks with a 90-second overall request context.
After bytes are received and again after storage returns, the platform refreshes
tenant membership and the concrete permission using the original middleware
resolver. Revocation denies with 403; an authorization dependency outage denies
with 503. The module receives a platform authorization callback, not the resolver
or direct access to authorization internals. Evidence transactions enable only
evidence management, never professional-master management. The refresh requests
OpenFGA `HIGHER_CONSISTENCY` for both decisions; ordinary routes retain their
existing default preference. Protocol tests assert the actual request bodies.

The immutable quarantine generation and `scan_pending` transition commit in the
same transaction as one outbox event. Its canonical `practitioner/contract` Go
payload contains only practitioner/registration/revision/file IDs; the envelope
supplies the tenant. No filename, content type, checksum, bytes or professional
claim enters the event. First committed upload returns 202; identical replay
returns 200 with retained metadata. An outbox failure rolls back the receipt and
state. Lost receipts or revocation after storage leave private quarantine bytes;
a later authorized retry reconciles those exact bytes/generation instead of
replacing them. Expired intents return 409 and cannot advance; history remains
retained. Storage uncertainty is 503 and never marks content available.

Migration 0005 preserves original intent, dates and provenance, permits only
upload reservation and the quarantine transition, and audits both. It is additive,
forward-only and undeployed. Runtime registration remains gated on the complete
owner, workflow and infrastructure checks; local processing and content APIs are
described below. Local tests cover exact upload/replay, mismatched bytes,
foreign tenant, evidence-only permissions, revocation/outage during the actual GCS
protocol exchange, outbox rollback/recovery and upload expiry without sleeps.

## Scoped evidence processing

The professional-evidence worker uses its own non-inheriting, non-bypass database
identity with no role memberships. It can read/update only the exact evidence
tuple supplied by the owner's scan request; it has no professional-master,
registration, appointment or HR-table grants. Restricted insert policies bind its
audit actor/resource and lifecycle outbox to that same file and current state.
Request-role credentials cannot commit scanner verdicts or provenance.

Processing reads and audits the scoped intent, inspects/scans its exact quarantine
generation outside database locks, verifies the retained size/hash, then promotes
only the attachment package's sealed clean-scan receipt. A matching immutable
destination generation, scanner engine/definition provenance and `available` state
commit with an opaque lifecycle event. These facts indicate safe file transport,
not council verification, HPR linkage or a clinical privilege. PNG/JPEG remain the
only inspected formats. Completed file provenance and intent remain immutable.

Malware, unsupported content and integrity mismatch are terminal `rejected`
outcomes. Storage/scanner outages remain `scan_error` and retryable; subsequent
processing may recover the same immutable object. Available/rejected/revoked files
are terminal for processing replay. If copying succeeds but the database/audit/
outbox commit fails, a retry validates the retained destination instead of
overwriting it. No read route infers availability from a copied blob alone.
Every database transaction retains the five-second/two-second statement/lock
budgets; the full processing activity has a five-minute caller-cancellable budget.

The first execution uses the existing immutable file identity; it can be retried
within its durable workflow. Explicit manual retry after a terminal workflow
failure will require a separately retained attempt command before runtime
activation. Production worker-role provisioning and storage IAM belong to the
owning infrastructure repository and are not performed by an application
migration or local fixture. No production role or scanner is claimed here.

Local verification covers request-role constructor rejection, forced-RLS denial
for every mismatched scope component, lack of professional-master/private-actor
column access, restricted audit/outbox writes, real protocol scanning/promotion,
12-way concurrent replay with one retained release/event, terminal malware/format/
integrity rejection, temporary scanner/storage recovery, and reconciliation when
copying succeeds but audit/outbox commit fails. Successful scanning leaves the
professional's registration `unverified`. Migration 0006 is additive and
forward-only, applied only to disposable test databases; local fixture identities
are not production provisioning. All metadata and content release gates still
require their own runtime and deployed acceptance evidence.

The local Temporal workflow invokes only the owning activity. Activity retries
are bounded to five with exponential backoff, a six-minute start-to-close bound,
40-minute schedule-to-close bound and 30-second heartbeat timeout. The activity
heartbeats while storage/scanning runs and removes its ticker on completion or
cancellation. Invalid or missing objects and integrity/authority constraint
failures are nonretryable; dependency outages retry the same immutable intent.
Only opaque input and a bounded status or sanitized error enter workflow history.
Repeated identical scan errors do not duplicate lifecycle events.

SDK time-skipping tests run the real scoped processor against vendor protocol
fixtures, prove successful release, five-attempt exhaustion, and immediate failure
for invalid/missing objects. Reducing the retry cap to one made the five-attempt
assertion fail; the original implementation was restored and verified. These SDK tests do not establish production deployment. The local module now
registers an evidence consumer: both transaction validation and post-commit start
strictly validate version, broadcast scope, bounded payload and opaque UUID scope.
A 15-second caller-cancellable start/receipt budget bounds Temporal dependency
failure. The workflow ID includes tenant, practitioner, registration, revision and
file. Duplicate starts are acknowledged only after matching the retained workflow
type, queue and exact input; conflicting or unavailable history remains an error.
A 45-minute execution timeout and reject-duplicate reuse policy prevent unbounded
executions or accidental restart after completion.

Local acceptance tests use a real Temporal development server and the actual
committed upload outbox through real JetStream delivery, process evidence with the scoped database role
and vendor protocol fixtures, download exact bytes, and confirm repeat delivery
retains one run and one object copy, including two distinct processed delivery
claims. A conflicting revision receipt is rejected without a copy. Production
delivery configuration, worker provisioning/deployment
and independent professional review remain required before runtime activation.

Run the real dispatch acceptance with an explicitly selected local Temporal server:

```sh
HELIVANTA_TEST_TEMPORAL_ADDR=127.0.0.1:17233 go test -race -tags acceptance ./internal/modules/practitioner -run 'TestProfessional(CommittedScan|DispatcherRejects|ManualRecovery)' -count=1
```

The ordinary package suite does not run acceptance-tagged server tests.

## Private content release and retained revocation

GET at the evidence path plus `/content` requires evidence-read permission and
current tenant membership. The exact available generation is loaded and audited,
then fetched outside database locks. Size, checksum, generation and passive MIME
must match the retained intent. The platform refreshes current authorization and
the owner rechecks file state/provenance under its database lock; the final read
audit commit is the release decision point. No bytes leave on an audit, storage,
authorization or integrity failure, or when revocation occurred during storage
I/O. Nonavailable content returns 409; unavailable/uncertain storage returns 503;
invisible ownership returns 404. Success is a private, no-store attachment with
an opaque generated filename, nosniff and restrictive CSP, never a public URL.

PUT at the evidence path plus `/revocations/:revocation` requires evidence-manage
permission. An immutable UUIDv7 decision and opaque `review_reference` revoke an
available file; the reference is recorded, not asserted to be an externally
validated case. The database retains the receipt and original bytes/provenance,
audits the state change and commits one opaque lifecycle outbox event. Exact
replay returns the original receipt; changed intent or another decision against
an already revoked file conflicts. Wrong parent/tenant remains invisible. No
physical deletion, licence suspension or professional verification is implied.
The receipt cannot be changed/deleted, and a file cannot be marked revoked
without its own retained receipt. Every command and disclosure uses the verified
tenant plus exact practitioner/registration/revision/file tuple.

Local acceptance tests cover successful private release and secure headers,
separate evidence permissions, every stale-workspace route (25 total), pending
content denial, actual storage callbacks revoking a file or tenant membership,
authorization outages, initial/final read-audit failure, storage corruption and
outage, concurrent retained revocation, rejected direct state mutation without a
receipt, immutable receipt history, and audit/outbox rollback and recovery.
Expiry tests prove that the upload window neither erases retained content nor
prevents subsequent revocation. Migration 0007 is additive/forward-only and has
been applied only to disposable databases; no file is physically deleted.

## Retained scan recovery (local implementation; undeployed)

PUT the evidence path plus `/scan-retries/:retry` requires current evidence-manage
permission and an immutable UUIDv7 command. The body contains `expected_attempt`
and an opaque UUIDv7 `review_reference`. It accepts only a current `scan_error`
from temporary storage/scanner failure; rejected, available, revoked and pending
files cannot be restarted. The command receipt, next attempt, reset to pending,
audit and scan outbox commit together. Exact replay returns the original queued
receipt even after later processing; changed intent or a stale competing command
conflicts. The receipt records the previous failure without copying private bytes.

A retry may explicitly supersede a prior execution still backing off. Each event,
workflow ID and worker input pins the attempt. Workers check it before I/O and
again under the commit lock; stale success/failure returns `superseded` without
changing current state. Database guards reject verdicts from a different attempt.
This avoids making recovery depend on Temporal availability while preserving the
existing five-attempt automatic retry cap per execution. It does not clear malware
or integrity rejection, change generations, delete history or verify a professional.
The existing capacity envelope applies; the body is bounded by the owner decoder,
commands use the existing five-second transaction budget and no external call is
made inside that transaction. Recovery uses the existing database/broker/Temporal
services and adds no cloud service or cache. New migration 0008 is additive and
undeployed; rollback must retain attempt counters and receipts.

Local retry validation covers exact replay after later success, changed intent,
wrong tenant/revision and permission, all 26 stale-workspace routes, 12 concurrent
replays with one receipt/outbox, audit and outbox rollback/recovery, receipt
immutability, database rejection of unreceipted advancement, stale worker verdicts,
and old work superseded after copying. Malware-rejected and revoked files remain
closed. SDK time-skipping proves recovery after five-attempt exhaustion. Real
JetStream/Temporal acceptance proves a retained retry starts a new execution after
a terminated prior execution, old delivery does not restart it, and exact content
can be read after recovery. These are local vendor-protocol/storage fixtures, not
production provisioning or approved professional verification.

`scan_attempt` is now required in the canonical draft scan input/request and
lifecycle data; it is also returned in private evidence metadata. This module has
never been registered/deployed or published for consumers; production registration
must publish this complete reviewed contract rather than rely on earlier draft
examples. The workflow ID includes the attempt. No existing published v1 consumer
is migrated by this change.
