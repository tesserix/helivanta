# ADR 0019: Version roster settings and staff weekly-template assignments

Status: accepted for implementation. Parent: #1036 / #979. Extends ADR-0018; spec: [roster settings and assignments](../superpowers/specs/2026-10-15-roster-settings-assignments-design.md). Editable diagram: [roster policy](../diagrams/roster-settings-assignments.drawio).

## Requirements and evidence

Legacy Roster Setting (read-only review, 2026-10-14) exposes automatic population for all or fixed-shift employees and an effective weekday; legacy Shift Allocation's weekly mode assigns an employee a weekly template. No source values are copied. Dated rosters need three reviewed facts before they can resolve wall-clock shifts to instants: the hospital IANA timezone, the roster week start and the population scope. Planning assumptions follow ADR-0018: ≤1 settings head per hospital and ≤1 assignment head per staff member, monthly revisions, peak 50 reads/s and 5 writes/s, p99 <500 ms, one indexed PostgreSQL and no new service.

## Decision

One settings head per hospital (`timezone`, `week_start` Monday=0…Sunday=6, `auto_population` = `disabled` | `fixed_shift` | `all_assigned`, `effective_on`) and one assignment head per staff member (pinned weekly-template revision, `fixed`, `active`, `effective_on`), each with an immutable revision table. Heads reference their current revision through a deferred tenant/hospital-scoped FK, so state and revision commit together or not at all. There is no default policy: a hospital without reviewed settings returns 404 and dated rostering must refuse to run.

Timezones are canonical IANA area names. Go validates against embedded `time/tzdata` (the distroless image has no zoneinfo) and a database trigger re-checks `pg_timezone_names`; `Local`, lowercase, `posix/`/`right/` aliases and unknown zones fail closed. `effective_on` is monotonic per head, so as-of resolution is the newest revision effective on or before a date and backdating cannot silently rewrite the policy an earlier roster used.

A new pin must be the current revision of an active weekly template that is effective on or before the assignment. An unchanged pin stays valid after the template is revised or retired. A generated `template_kind` column plus a composite FK makes it structurally impossible to pin an area or group. Active assignments require active staff in the same hospital; ending one is always allowed.

## Security and compliance

Threats: foreign tenant, sibling hospital, unauthorized reader, stale editor and an insider rewriting history. Reads need tenant membership and an explicit hospital grant. Writes need `workforce.roster.manage` and a hospital manager grant locked `FOR SHARE` for the whole transaction, plus a transaction-local `app.workforce_roster_policy` flag in every RLS `WITH CHECK`, so no other code path can write these tables. Forced RLS covers all four tables, revisions reject UPDATE/DELETE/TRUNCATE, and heads reject DELETE/TRUNCATE. Input never supplies tenant, hospital, reviewer or actor; they come from the authenticated principal. Cross-tenant and foreign-hospital requests return 404, a missing permission returns 403, and authorization or database failure fails closed.

SOC 2 (CC6.1, CC7.2, CC8.1): every change is attributable through the reviewer subject on the revision and an append-only `workforce_audit` row in the same transaction; history endpoints show who changed what and when. HIPAA (§164.312(a), (b), (c)): these tables hold workforce scheduling policy, not PHI. Events and audit rows carry identifiers and versions only, responses are `Cache-Control: no-store`, and nothing is logged beyond request metadata.

## Platform dependencies

- PostgreSQL: authoritative; migration `0007_workforce_roster_policy` adds empty tables, indexes and triggers with bounded DDL timeouts and no backfill.
- NATS JetStream: `helivanta.in.workforce.roster_settings_changed.v1` and `helivanta.in.workforce.staff_roster_assignment_changed.v1` are written to the outbox inside the business transaction and published after commit, with retries through broker outages. HTTP success does not claim consumer completion. Payloads are allowlisted in the event-payload arch test.
- Valkey: not used. Roster policy is authoritative scheduling state and must not be served from a cache that could outlive a revision; rate limiting and sessions are unchanged.
- Temporal: not used. Each change is a single transaction; population execution and freeze workflows will add durable workflows in the next slice.

## UI and delivery

`/workforce/roster-policy` shows the reviewed policy, assignments and both histories. Readers get no write controls. Forms require explicit review, preserve input on rejection or conflict, and keep superseded pinned revisions selectable. Delivery uses GitOps (build → Kargo → Argo). Rollback keeps the additive schema and data; there is no destructive down migration. `scripts/verify-roster-policy.mjs` repeats the acceptance checks against a designated synthetic tenant without backdating or deleting history.

Not covered here; these remain open on #1036: dated rosters, allocation modes, DST instant resolution, overlap exclusion, population execution, freeze/unfreeze/corrections, and leave (#1037) and credential (#1108) conflicts.
