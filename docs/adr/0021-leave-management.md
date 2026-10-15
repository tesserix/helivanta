# ADR 0021: Leave management as versioned policy and an append-only ledger

Status: accepted for implementation. Parent: #1037 / #979. Extends ADR-0016, ADR-0018 and ADR-0020; specs: [leave setup](../superpowers/specs/2026-10-15-leave-setup-design.md); plan: [leave management](../superpowers/plans/2026-10-15-leave-management.md).

## Requirements and evidence

Legacy HR > Setup > Leave Setup (leave, holiday and structure tabs), Leave Allocation, Leave Requests and reports 949/954 were reviewed read-only on 2026-10-12–13. Leave categories expose paid, half-day/short-leave, balance-check, clubbing and active flags; holidays distinguish regular, government and optional days per calendar year; structures list categories with an annual allowance; settings expose allocation year, probation, compensatory expiry, backdating and joining-date basis. No source values are copied. Planning assumptions: up to 2,000 staff and 30 leave types per hospital, 100 holidays per year, peak 20 reads/s and 2 writes/s, p99 <500 ms, one indexed PostgreSQL and no new service.

## Decision

Leave is delivered in four slices, each complete for the cases it claims:

1. **Leave setup** (this ADR's first slice): leave types, yearly holiday calendars and leave structures as one versioned configuration aggregate, `workforce_leave_configs`, with kind-specific children written only in the revision-creating transaction. Same shape as ADR-0018 roster configuration: client-chosen ID, `expected_version`, mandatory review, effective date, append-only revisions and history.
2. **Balance ledger**: leave-year settings, effective-dated staff structure assignments, and an append-only ledger of allocation, accrual, consumption, correction, carry-forward and expiry entries. Each entry has a deterministic idempotency key (staff, type, period, source), so a retried credit cannot double-count. Balances are sums over the ledger, never stored mutable counters.
3. **Requests and approvals**: requests pin the type, structure and holiday-calendar revisions in force; overlap per person is a PostgreSQL exclusion constraint; requester and approver must differ; approval writes consumption and cancellation writes its reversal in the same transaction. Leave conflicts then feed roster population (#1036).
4. **Reports** 949 and 954 over the ledger and request history, with explicit period boundaries.

Leave amounts are integer half-days (`half_days`) and short leave is integer minutes. No floating-point balances.

Holiday calendars are one per hospital per calendar year. Department- or site-specific calendars are not covered; if needed, they become an additional scope on the same aggregate.

Rejected: per-type tables (three near-identical versioning stacks), mutable balance counters (cannot answer "why is this the balance"), and a separate leave service (ADR-0005).

## Security and compliance

Writes need `workforce.leave.configure` plus a hospital manager grant locked `FOR SHARE`, and a transaction-local `app.workforce_leave` flag in every RLS `WITH CHECK`. Reads need tenant membership and an explicit hospital grant. Forced RLS covers every table; revisions and children are append-only; children are insertable only in their revision's transaction; identity, kind, code and calendar year are immutable. Cross-tenant and foreign-hospital requests return 404, a missing permission 403, and authorization or database failure fails closed.

SOC 2 (CC6.1, CC6.3, CC7.2, CC8.1): every policy change is reviewed, attributable through `reviewed_by_subject` and an append-only `workforce_audit` row in the same transaction, and its prior state stays readable. Later slices separate request, approval and correction permissions. HIPAA (§164.308(a)(3), §164.312(b), (c)): leave policy carries no PHI. Requests will carry only the minimum reason text and are never included in events, logs or audit payloads. Responses are `Cache-Control: no-store`.

## Platform dependencies

- PostgreSQL: authoritative. Migration `0010_workforce_leave_setup` is additive with bounded DDL timeouts and no backfill.
- NATS JetStream: `helivanta.in.workforce.leave_config_changed.v1` (hospital, config, revision, kind, version) through the transactional outbox; payload allowlisted in the event-payload arch test; consumers idempotent on revision ID.
- Valkey: not used. Policy and balances are authoritative; a cache could serve a superseded revision to an approval decision.
- Temporal: not used in slice 1 (single-transaction writes). Slice 2 reconsiders it for scheduled monthly accrual and year-end carry-forward, which are durable, retried batch jobs.

## Delivery

GitOps (build → Kargo → Argo). Rollback keeps the additive schema; there is no destructive down migration. `scripts/verify-leave-setup.mjs` repeats the slice-1 acceptance checks against a designated synthetic tenant using synthetic codes only.
