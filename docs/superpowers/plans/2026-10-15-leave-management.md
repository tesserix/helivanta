# Leave management delivery plan

Issue: #1037 (parent epic #979). ADR: [0021](../../adr/0021-leave-management.md).

Each slice is one PR, merged and deployed with production verification before the next starts.

## Slice 1 — leave setup ([spec](../specs/2026-10-15-leave-setup-design.md))

1. Contract `contracts/workforce-leave.json` and `scripts/generate-workforce-leave.mjs` (shared emitter in `scripts/lib/contract-codegen.mjs`, also used by the roster generator) → `leave_types.go`, `packages/api/src/leave-contract.ts`; `--check` in `@helivanta/api` type-check.
2. Migration `0010_workforce_leave_setup` (`leave_setup.sql`): five tables, children-in-creating-transaction, aggregate validity, stable identity, forced RLS.
3. Handlers `leave_setup.go`: list, history, put; permission `workforce.leave.configure`; event `leave_config_changed.v1`; audit.
4. Tests `leave_setup_test.go`; arch-test payload allowlist.
5. `@helivanta/api` schemas and tests; `/workforce/leave-setup` page with types, holidays and structures tabs; zone entry.
6. `scripts/verify-leave-setup.mjs`; `docs/api/workforce.md`; `docs/hr-capability-coverage.md`.

## Slice 2 — balance ledger

Leave-year settings; effective-dated staff structure assignments; append-only ledger with idempotency keys; upfront and monthly accrual, join-month proration, carry-forward and expiry at year end (Temporal schedule if a durable batch is needed); reasoned corrections under a separate `workforce.leave.correct` permission; as-of balances.

## Slice 3 — requests and approvals ([spec](../specs/2026-10-15-leave-requests-design.md))

1. Migration `0012_workforce_leave_requests` (`leave_requests.sql`): requests with a half-day slot exclusion, per-day children in the creating transaction, append-only events, guarded transitions, separation of filer and approver; ledger `consumption`/`reversal` kinds tied to a request.
2. Permissions `workforce.leave.request` and `workforce.leave.approve`; event `leave_request_changed.v1` in the contract and arch-test allowlist.
3. Handlers `leave_requests.go`: file (client id, replay), list, detail with days, history and roster clashes, decide, cancel; year close refuses pending requests; roster population skips approved full-day leave.
4. Tests `leave_requests_test.go`, roster population and year-close cases.
5. `@helivanta/api` schemas and tests; `/workforce/leave-requests` page; zone entry.
6. `scripts/verify-leave-requests.mjs`; `docs/api/workforce.md`; ADR-0021; `docs/hr-capability-coverage.md`.

## Slice 4 — reports

Reports 949 (monthly leave by type) and 954 (leave status) with the observed filters, explicit period boundaries, pagination and reconciliation against the ledger.
