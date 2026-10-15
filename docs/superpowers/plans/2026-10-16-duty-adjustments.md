# Duty adjustments delivery plan

Issue: #1038 (parent epic #979). ADR: [0022](../../adr/0022-duty-adjustments.md). Each slice is one PR, merged and deployed with production verification before the next starts.

## Slice 1 — duty exchanges ([spec](../specs/2026-10-16-duty-exchanges-design.md))

1. Migration `0014_workforce_duty_exchanges` (`duty_exchanges.sql`): exchange head, allocations and events with forced RLS; `exchanged` roster action and narrowed frozen-roster triggers.
2. Permissions `workforce.duty.request` and `workforce.duty.approve`; event `duty_exchange_changed.v1` in the arch-test allowlist; `contracts/workforce-duty.json` with `scripts/generate-workforce-duty.mjs` wired into the `@helivanta/api` type-check.
3. Handlers `duty_exchanges.go`: file, list, detail, history, decide, cancel, revoke; unfreeze and discard refuse pending exchanges.
4. Tests `duty_exchanges_test.go` covering every spec rule, concurrency and redaction.
5. `@helivanta/api` schemas and tests; `/workforce/duty-exchanges` page and tests; roster grid marker; zone entry.
6. `scripts/verify-duty-exchanges.mjs`; `docs/api/workforce.md`; `docs/hr-capability-coverage.md`.

## Slice 2 — overtime

Versioned, effective-dated overtime policy with daily, weekly (hospital week start) and monthly limits; requests by date and instant range outside the person's duty and approved leave; requested and approved minutes stored apart, approvers may approve fewer; limits counted over approved plus pending requested minutes; payable minutes summary for payroll (#1040).

## Slice 3 — missing-punch corrections

After #1039 delivers immutable punches: correction requests linking the source punch (or the missing slot), before/after values, reason and evidence; separated approval; corrected attendance derived, never written over the source.
