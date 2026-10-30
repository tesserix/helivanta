# Deposit refunds Implementation Plan

**Goal:** Refund unused deposits from the counter, with approval above a hospital threshold (#199 slice 2).

**Spec:** docs/superpowers/specs/2026-10-30-deposit-refunds-design.md

## Global Constraints

- Migration `0007_billing`; receipts gain `purpose: refund` and `approval_id`.
- `409` codes: `no_final_bill`, `over_held`, `drawer_short`, `approval_required`, `own_approval`.
- Default threshold ₹10,000 (1000000 minor).

## Review Focus

- Two refunds at once: never more than held.
- One approval used twice.
- A refund racing a shift close.
- The cashier approving their own refund.
- A deposit voided after a refund.

### Task 1: Backend

`refunds.sql`, `refunds.go`, `refunds_test.go`; `cash.go` (purpose refund), `deposits.go`
(held), `accounts.go` (refunded), `module.go`.

### Task 2: Frontend

Refund policy, approvals and pay-out in the cash desk and payments panel; Vitest.

### Task 3: Docs

`docs/api/domains/billing.md`, portfolio, reference.
