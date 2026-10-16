# ADR 0022: Duty exchanges, overtime and missing-punch corrections

Status: accepted for implementation. Parent: #1038 / #979. Extends ADR-0020 and ADR-0021; specs: [duty exchanges](../superpowers/specs/2026-10-16-duty-exchanges-design.md), [overtime](../superpowers/specs/2026-10-16-overtime-design.md); plan: [duty adjustments](../superpowers/plans/2026-10-16-duty-adjustments.md).

## Requirements and evidence

Legacy HR > Request > Duty / OT Adjust and Missing Punch (read-only review, 2026-10-12) identify a requester and an adjuster with their dates and shifts, overtime by date and shift, and missing punches by type, start and end, each with approval states and attribution. No source values are copied. Planning assumptions: up to 2,000 staff per hospital, a few hundred exchanges and overtime requests a month, peak 20 reads/s and 2 writes/s, p99 <500 ms, one indexed PostgreSQL and no new service.

## Decision

Delivery is three slices, ordered by dependency:

1. **Duty exchanges** over frozen rosters. A `swap` trades two people's future allocations; a `cover` hands one future allocation to a colleague. Requests are filed with an attested counterpart consent and decided by a different person.
2. **Overtime** against a versioned, effective-dated hospital overtime policy (daily, weekly and monthly limits in minutes). Requested and approved minutes are separate columns; only approved minutes are payable, and rejected, cancelled or revoked requests contribute zero.
3. **Missing-punch corrections** after #1039 delivers immutable attendance punches. A correction is a separate approved row linking the source punch, before and after values, reason and actors; source punches are never edited. Without a punch source there is nothing to link, so this slice waits rather than inventing a stand-in.

An approved exchange is written into the roster itself: one revision with action `exchanged` per affected roster, which tombstones the given allocations and inserts identical ones (shift, area, instants) for the receivers. The roster stays `frozen`; only this action may advance a frozen roster's version without unfreezing. So the ADR-0020 GiST exclusion stays the single authority on one person's overlapping duty, "what was rostered at version N" still answers history, and attendance (#1039) reads one source of duty. Revocation before the duty starts writes the inverse revision.

Rejected: a separate overlay table of covers. Overlaps between covers and ordinary allocations would then need cross-table checks in triggers, and every reader of duty would have to merge two sources.

Rules for an exchange, enforced at filing and again at decision under the hospital roster lock:

- Every allocation involved is active, in a frozen roster of this hospital, primary, and starts after the decision instant; past duty is an attendance matter, not an exchange.
- An allocation takes part in at most one pending or approved exchange (partial unique index).
- Receivers keep no overlapping duty (the exclusion constraint, pre-checked to return conflicting IDs), have no approved full-day leave on the date (409), and a received allocation stays primary only if the receiver has no other primary that day; otherwise the exchange is refused (409) rather than demoting duty silently. A giver left with only additional allocations that day is refused for the same reason.
- Unfreezing or discarding a roster is refused while it has pending exchanges (409); approved exchanges are ordinary allocations after that.

## Security and compliance

Threats: foreign tenant or sibling hospital, a filer approving their own exchange, a stale or replayed decision, exchanges rewriting worked duty, consent recorded by nobody. Reads need tenant membership and a hospital grant; writes need the permission plus a hospital manager grant locked `FOR SHARE` and the `app.workforce_rostering` flag in RLS `WITH CHECK`. New permissions: `workforce.duty.request` files and cancels; `workforce.duty.approve` decides and revokes, and a table check forbids the decider being the filer. Cross-tenant and foreign-hospital requests are 404, a missing permission 403, and authorization or database failure fails closed.

SOC 2 (CC6.1, CC6.3, CC7.2, CC8.1): segregation between filing and approval, client-chosen replay-safe identifiers, append-only exchange events and roster revisions, and an identifier-only `workforce_audit` row in the same transaction. HIPAA (§164.308(a)(3), §164.312(a), (b), (c)): duty determines who is on site with access to PHI, so every change of duty is attributable and tamper-evident. Reasons and notes may describe personal circumstances: they are returned only to holders of either duty permission and never enter events, logs or audit payloads. Responses are `no-store`.

## Platform dependencies

- PostgreSQL: authoritative; migration `0014_workforce_duty_exchanges` adds the exchange tables, the `exchanged` roster action and the narrowed frozen-roster triggers, with bounded DDL timeouts and no backfill.
- NATS JetStream: `helivanta.in.workforce.duty_exchange_changed.v1` (hospital, exchange, status, version) and the existing `roster_changed.v1` for each affected roster are written to the outbox in the business transaction; consumers are idempotent on IDs.
- Valkey: not used. Duty is authoritative state; a cached roster could show superseded duty after an exchange.
- Temporal: not used. Every write is one transaction with no external side effects. Notifications to staff arrive with self-service (#1132) and may revisit this.

## UI and delivery

`/workforce/duty-exchanges` lists and files exchanges, shows history and offers decision, cancellation and revocation by permission; the roster grid marks exchanged allocations. `/workforce/overtime` shows the policy and its history, files, decides, withdraws and revokes overtime by permission, and summarises payable minutes by month. Delivery uses GitOps (build → Kargo → Argo); the schema change is additive and rollback keeps data. `scripts/verify-duty-exchanges.mjs` and `scripts/verify-overtime.mjs` repeat the acceptance checks against a designated synthetic tenant.

Not covered by slice 1: counterpart consent given by the staff member themselves (attested by the filer until #1132), credential conflicts (#1108), overtime and missing punches (slices 2 and 3). Not covered by slice 2: overtime filed by staff themselves (#1132), payroll period locks, rates and multipliers (#1040) and notifications. Missing punches (slice 3) wait for #1039.
