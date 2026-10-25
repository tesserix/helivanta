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
(#102's materialisation workload), 200 discovery readers/second (#102 search load
basis), 10 administrative writes/second, <=8 KiB intake, and initially 20:1 reads
to writes. At 1,000 tenants this is 500,000 base records; assuming 10% annual growth,
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
