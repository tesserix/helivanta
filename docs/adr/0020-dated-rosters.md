# ADR 0020: Dated rosters with immutable allocation history

Status: accepted for implementation. Parent: #1036 / #979. Extends ADR-0018 and ADR-0019; spec: [dated rosters](../superpowers/specs/2026-10-15-dated-rosters-design.md). Editable diagram: [dated rosters](../diagrams/dated-rosters.drawio).

## Requirements and evidence

Legacy Shift Allocation (read-only review, 2026-10-14) offers roster-wise, employee-wise and additional-shift allocation, automatic population from weekly patterns and a frozen roster. No source values are copied. Planning assumptions: up to 2,000 staff per hospital, rosters of one to six weeks, a few rosters per month, peak 50 reads/s and 5 writes/s, p99 <500 ms for changes and <5 s for a full population, one indexed PostgreSQL and no new service.

## Decision

A roster is a period of whole weeks starting on the hospital week start, pinned to the settings revision in force on its first day through a composite FK (timezone, week start, population scope). Status is `draft`, `frozen` or `discarded`. Every write is a revision keyed by a client `change_id` and guarded by `expected_version`, so retries are idempotent and concurrent editors get 409 instead of lost updates.

Allocations are immutable rows carrying `added_version` and `removed_version`, so "what was rostered at version N" is a plain predicate and history cannot be rewritten. Each row stores its resolved UTC `starts_at`/`ends_at`. Go resolves wall-clock shift times through the pinned IANA zone and accepts exactly one instant; a DST gap or repeated hour returns 400 `dst_unresolvable` rather than guessing. PostgreSQL enforces non-overlapping periods per hospital and non-overlapping `[starts_at, ends_at)` per person across every roster with GiST exclusion constraints; a deferred constraint trigger requires each additional allocation to have a primary on the same date. The API pre-checks the same rules to return the conflicting allocation IDs (409 `allocation_overlap`); the database remains the authority under concurrency.

Population reads the assignments, templates and staff as of each date, never overwrites an existing staff-date and reports skipped counts. It is bounded at 20,000 rows per call and runs in one transaction.

Unfreeze uses a separate `workforce.roster.correct` permission and a mandatory reason, so publication and correction can be held by different people.

## Security and compliance

Threats: foreign tenant, sibling hospital, reader escalating to writer, stale or replayed editor, an insider rewriting a published roster, and DST ambiguity silently shifting care cover. Reads need tenant membership and an explicit hospital grant. Writes need the permission plus a hospital manager grant locked `FOR SHARE` for the whole transaction, and a transaction-local `app.workforce_rostering` flag in every RLS `WITH CHECK`. Forced RLS covers all three tables; revisions are append-only; allocation updates are limited by trigger to one removal in the current draft version; roster identity and period are immutable. Tenant, hospital, reviewer and actor come from the authenticated principal. Cross-tenant and foreign-hospital requests return 404, a missing permission 403, and authorization or database failure fails closed.

SOC 2 (CC6.1, CC6.3, CC7.2, CC8.1): segregation between publishing and correcting, and every change attributable through the reviewer on the revision and an append-only `workforce_audit` row in the same transaction; reasons are required for discard and unfreeze. HIPAA (§164.308(a)(3), §164.312(a), (b), (c)): rosters hold workforce scheduling, not PHI, and determine who is on duty with access to PHI, so history is tamper-evident and retained. Events and audit carry identifiers and counts only, responses are `Cache-Control: no-store`, and logs carry request metadata only.

## Platform dependencies

- PostgreSQL: authoritative; migration `0009_workforce_rosters` enables the trusted `btree_gist` extension and adds empty tables, indexes, exclusion constraints and triggers with bounded DDL timeouts and no backfill.
- NATS JetStream: `helivanta.in.workforce.roster_changed.v1` (hospital, roster, revision, version) is written to the outbox inside the business transaction and published after commit, with retries through broker outages. Payloads are allowlisted in the event-payload arch test; consumers must be idempotent on revision ID.
- Valkey: not used. Rosters are authoritative scheduling state; a cache could serve a superseded version after a freeze or correction.
- Temporal: not used. Every write, including a bounded population, is one transaction with no external side effects. Leave and credential conflict checks (#1037, #1108) and notifications will reconsider durable workflows.

## UI and delivery

`/workforce/rosters` lists rosters and creates drafts; the roster page shows the allocation grid, the three change modes, population, freeze, unfreeze, discard and history. Readers get no write controls. Forms require explicit review and preserve input on rejection or conflict, which highlights the conflicting allocations. Delivery uses GitOps (build → Kargo → Argo). Rollback keeps the additive schema and data; there is no destructive down migration. `scripts/verify-rosters.mjs` repeats the acceptance checks against a designated synthetic tenant and discards only the roster it creates.

Not covered here; these remain open on #1036: leave conflicts (#1037) and credential conflicts (#1108).
