# ADR 0021: Leave management as versioned policy and an append-only ledger

Status: accepted for implementation. Parent: #1037 / #979. Extends ADR-0016, ADR-0018 and ADR-0020; specs: [leave setup](../superpowers/specs/2026-10-15-leave-setup-design.md); plan: [leave management](../superpowers/plans/2026-10-15-leave-management.md).

## Requirements and evidence

Legacy HR > Setup > Leave Setup (leave, holiday and structure tabs), Leave Allocation, Leave Requests and reports 949/954 were reviewed read-only on 2026-10-12–13. Leave categories expose paid, half-day/short-leave, balance-check, clubbing and active flags; holidays distinguish regular, government and optional days per calendar year; structures list categories with an annual allowance; settings expose allocation year, probation, compensatory expiry, backdating and joining-date basis. No source values are copied. Planning assumptions: up to 2,000 staff and 30 leave types per hospital, 100 holidays per year, peak 20 reads/s and 2 writes/s, p99 <500 ms, one indexed PostgreSQL and no new service.

## Decision

Leave is delivered in four slices, each complete for the cases it claims:

1. **Leave setup** (this ADR's first slice): leave types, yearly holiday calendars and leave structures as one versioned configuration aggregate, `workforce_leave_configs`, with kind-specific children written only in the revision-creating transaction. Same shape as ADR-0018 roster configuration: client-chosen ID, `expected_version`, mandatory review, effective date, append-only revisions and history.
2. **Balance ledger**: leave-year settings, effective-dated staff structure assignments, and an append-only ledger of allocation, accrual, correction, carry-forward and expiry entries (consumption and reversal arrive with slice 3). Each entry has a deterministic idempotency key (staff, type, period, source), so a retried credit cannot double-count. Balances are sums over the ledger, never stored mutable counters. Postings are explicit, sequential, manager-initiated months; a year is closed once, after all twelve months are posted and the next year is open. A month can be posted only once it starts and a year closed only after it ends, judged by the date anywhere on Earth (UTC−12), and years open at most one ahead; posting history freezes assignment changes before it.
3. **Requests and approvals**: requests pin the type, structure and holiday-calendar revisions in force; overlap per person is a PostgreSQL exclusion constraint; requester and approver must differ; approval writes consumption and cancellation writes its reversal in the same transaction. Leave conflicts then feed roster population (#1036).
4. **Reports** 949 and 954 over the ledger and request history, with explicit period boundaries.

Leave amounts are integer half-days (`half_days`) and short leave is integer minutes. No floating-point balances.

Holiday calendars are one per hospital per calendar year. Department- or site-specific calendars are not covered; if needed, they become an additional scope on the same aggregate.

Rejected: per-type tables (three near-identical versioning stacks), mutable balance counters (cannot answer "why is this the balance"), and a separate leave service (ADR-0005).

## Security and compliance

Writes need `workforce.leave.configure` plus a hospital manager grant locked `FOR SHARE`, and a transaction-local `app.workforce_leave` flag in every RLS `WITH CHECK`. Reads need tenant membership and an explicit hospital grant. Forced RLS covers every table; revisions and children are append-only; children are insertable only in their revision's transaction; identity, kind, code and calendar year are immutable. Cross-tenant and foreign-hospital requests return 404, a missing permission 403, and authorization or database failure fails closed.

SOC 2 (CC6.1, CC6.3, CC7.2, CC8.1): every policy change is reviewed, attributable through `reviewed_by_subject` and an append-only `workforce_audit` row in the same transaction, and its prior state stays readable. Slice 2 separates `workforce.leave.allocate` (assignments, postings, closes) from `workforce.leave.correct` (reasoned corrections with client-chosen, replay-safe ids), so either can be granted alone. Slice 3 adds `workforce.leave.request` (file and withdraw) and `workforce.leave.approve` (decide and revoke); the decider must differ from the filer, enforced by the API and a table check. HIPAA (§164.308(a)(3), §164.312(b), (c)): leave policy carries no PHI. Request reasons and decision notes are returned only to request or approve holders and are never included in events, logs or audit payloads. Responses are `Cache-Control: no-store`.

## Platform dependencies

- PostgreSQL: authoritative. Migrations `0010_workforce_leave_setup` and `0011_workforce_leave_ledger` are additive with bounded DDL timeouts and no backfill. Every ledger write takes one hospital advisory lock, so postings, closes, corrections and assignment changes never interleave.
- NATS JetStream: `helivanta.in.workforce.leave_config_changed.v1` (hospital, config, revision, kind, version), `staff_leave_assignment_changed.v1` (hospital, staff, revision, version) and `leave_ledger_changed.v1` (hospital, leave year, source, reference, entry count) through the transactional outbox; payloads allowlisted in the event-payload arch test and never carry reasons or amounts; consumers idempotent on the reference ID.
- Valkey: not used. Policy and balances are authoritative; a cache could serve a superseded revision to an approval decision.
- Temporal: not used. Slice 2 considered it for monthly accrual and year-end carry-forward and rejected it: each posting and close is one bounded, idempotent transaction a manager starts explicitly, so there is no long-running state to make durable. Scheduled automatic posting, if wanted, would be a Temporal schedule calling the same idempotent endpoint.

## Delivery

GitOps (build → Kargo → Argo). Rollback keeps the additive schema; there is no destructive down migration. `scripts/verify-leave-setup.mjs` and `scripts/verify-leave-ledger.mjs` repeat the slice-1 and slice-2 acceptance checks against a designated synthetic tenant using synthetic codes only.
