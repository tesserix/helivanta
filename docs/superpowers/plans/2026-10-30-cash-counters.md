# Cash counters Implementation Plan

**Goal:** Counters, shifts with blind close and cash receipts against admission accounts (#198 slice 1).

**Spec:** docs/superpowers/specs/2026-10-30-cash-counters-design.md

## Global Constraints

- Migration `0005_billing`; permissions `billing.counter.manage`, `billing.shift.authorise`
  (administrators), `billing.cash.collect` (receptionist).
- `409` codes: `shift_open`, `no_open_shift`, `over_balance`, `shift_closed`, `own_variance`,
  `conflict`.

## Review Focus

- Two receipts racing for one balance: never both.
- A receipt racing a shift close: in the shift's expected cash or refused.
- A void after close: refused.
- The cashier authorising their own variance: refused.
- Reader without the grant: 404.

### Task 1: Backend

`cash.sql`, `cash.go`, `cash_test.go`, `module.go`, `accounts.go` (paid, balance), archtest.

### Task 2: Frontend

Counter setup and cash desk on the IPD page; take payment from the ward board; Vitest.

### Task 3: Docs

`docs/api/domains/billing.md`, portfolio, reference.
