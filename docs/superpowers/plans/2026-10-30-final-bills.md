# Final bills Implementation Plan

**Goal:** Issue final bills once discharge is ordered or the patient has left (#197 slice 4).

**Spec:** docs/superpowers/specs/2026-10-30-final-bills-design.md

## Global Constraints

- Migration `0004_billing`; consumers `billing-discharge-ordered`, `billing-discharge-withdrawn`.
- `409` codes: `discharge_ordered`, `discharge_not_ordered`, `bed_days_not_priced`.

## Review Focus

- A withdrawal delivered before its order never activates the order.
- A final bill issued while a bed day is unpriced: refused by handler and database.
- Interim after an order, final before one: refused by the database alone.
- A first final bill after an interim bill that holds everything: issued.
- Reader without the grant: 404 on the new routes.

### Task 1: Backend

`final_bills.sql`, `final_bills.go`, `interim_bills.go` (kind), `accounts.go` (consumers,
account flag), `module.go`, archtest allowlist. Tests:
`TestAFinalBillWaitsForTheDischargeOrderAndEveryBedDayPriced`,
`TestAWithdrawnDischargeReturnsToInterimBillsInEitherEventOrder`,
`TestBillTablesHoldTheFinalBillRules`.

### Task 2: Frontend

`admission-bills.tsx` (renamed from interim-bills), types, Vitest.

### Task 3: Docs

`docs/api/domains/billing.md`, portfolio, reference.
