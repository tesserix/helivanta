# EmergencyConnect operational dispatch

Programme #1220; fleet #126, crew allocation #127, dispatch #123, crew #124,
hospital pre-alert #129, handover #130, offline #132, citizen booking #122.

## Capacity and safety boundary

Planning assumptions, not measured demand: 100 bases, 1,000 vehicles and
10,000 reports/day. At 10x diurnal peak this is 1.2 new reports/s. Budget
20 command/s and 200 metadata reads/s; commands <=8 KiB, read:write 10:1.
At 8 KiB/case, case payload is about 30 GiB/year, 90 GiB/36 months before
indexes/backups. Five-second GPS for 1,000 vehicles is separately 200 fixes/s;
it must not share the command/outbox path. Target API p99 <500 ms, monthly
99.9% availability. These are release load-test targets, not achieved SLOs.

Assets: caller/incident details, trip locations, credentials and scarce fleet
capacity. Threats: anonymous abuse, another tenant/base, compromised crew device
and an insider assigning an unsafe vehicle. Identity comes from verified
Helivanta sessions; route permission AND base/object RLS authorize commands.
Tenant administration grants no operational access. Clinical and operational
policy references must be approved by a named operator/medical lead before live
use. Code never invents an approved oxygen threshold or qualification.

## Decisions and sequence

1. Fleet is authoritative Postgres state, read through tenantdb, not a mutable
   `available` flag. Versioned vehicle certification and an explicitly approved
   readiness policy are prerequisites. Driver and clinical credentials expire
   independently. Duty intervals and readiness expire by database time. Changing
   vehicle policy or duty invalidates prior readiness. Qualifications determine
   effective BLS/ALS capability. No available vehicle is inferred from distance.
2. Readiness is attributed, immutable evidence. Failed oxygen, roadworthiness,
   equipment or defibrillator checks open persistent coded constraints. A later
   pass never clears one. Only a scoped supervisor with fleet-management
   permission can clear a constraint against a repair/evidence reference.
   Readiness, constraint and metadata-only outbox/audit writes are one transaction.
3. Reservation locks case, vehicle and crew in a stable order and rechecks the
   authoritative readiness state inside that transaction. Unique active vehicle,
   case and crew reservations settle concurrent commands at the database.
   A human approved dispatch assessment is distinct from untriaged caller input.
4. A Temporal workflow per dispatch owns acknowledgement and receiving-response
   deadlines. Outbox delivery starts it idempotently; commands persist first,
   then outbox signals advance it. Activity retry cannot release a newer
   reservation. Timeout escalates to the dispatcher; it cannot silently claim
   cancellation reached a crew device. Crew status commands are assigned-subject,
   ordered and idempotent. Explicit human reconciliation handles ambiguous jobs.
5. Destination preference, pre-alert, receiving acceptance, arrival and signed
   handover are separate attributed facts. Cross-tenant receiving is an explicit
   directed-event service agreement, never a broad hospital grant or implicit
   `WithAdmin` read. Rejection, capacity change and destination reassignment retain
   prior history. Clearing a trip cannot clear an unrelated maintenance block.
6. Citizen calling remains public and independent. Booking/status uses a separate
   audited request capability or authenticated citizen principal, bounded abuse
   controls and encrypted contact. A received request is never displayed as a
   dispatched/acknowledged ambulance. No public precise unassigned fleet positions.
7. Crew offline commands retain encrypted device-bound records and original
   command IDs, assignment generation and sequence. The server owns state and
   rejects stale/revoked/reassigned commands; local success never means delivered.

The first implementation increment below covers step 1 and 2 through API,
console, additive migration and real database tests. Subsequent steps remain
programme work, not acceptance criteria silently removed from their stories.
This specification does not assert a FacilityConnect/workforce integration exists;
manual verified evidence is explicit until those owning event contracts land.

## Failure, migration and release

Postgres down: commands/readiness unavailable; no assignment. OpenBao down:
protected case reads/intake unavailable; metadata readiness remains possible.
Temporal down: persisted dispatch intents remain visibly pending, never assigned
successfully on a speculative client response. NATS down: outbox retains intent;
authoritative fleet reads still use Postgres, avoiding stale projections.
Routing down: unavailable estimate, no fabricated ETA. GPS loss preserves last-seen
time and alerts; it never makes a vehicle available. Public dial action works
without any of these dependencies.

Add `0002_emergency` without rewriting intake migration. New tables start empty;
every tenant table has forced RLS, composite foreign keys, bounded cursor lists
and no removal path. Preserve history on rollback: disable new entry points and
roll code forward; schema/data rollback is intentionally irreversible. Test the
empty-schema upgrade and prior intake regression together. No live migration or
rollout is implied by local tests. Project's production approval rule applies.

No new datastore/service for fleet: simplest alternative is the existing modular
monolith and indexed Postgres; adopt it. Reject a cache-derived availability flag
because missed invalidation can dispatch unsafe vehicles. Existing Temporal
cluster is reused for waits; do not implement a cron-driven dispatch saga.
Initial fleet storage is <100 MiB; compute uses existing API. Additional worker,
GPS history/retention and mobile distribution costs require measured sizing before
activation, rather than claiming an unpriced full rollout.
