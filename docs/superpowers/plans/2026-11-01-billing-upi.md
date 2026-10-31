# UPI at the billing desk — implementation plan

**Goal:** a cashier takes verified UPI payments and deposits on an admission account,
each confirmed payment becoming a numbered receipt in the requesting shift.

**Spec:** `docs/superpowers/specs/2026-11-01-billing-upi-design.md`

**Architecture:** billing publishes payment requests through the outbox; payments, now
serving requester `billing`, answers with intent events; billing's outcome consumers
move the request, and a trigger writes the receipt on the paid transition.

## Global constraints

- Migrations append-only: `0011_billing` (`upi.sql`), `0002_payments`.
- Money rules live in triggers; handlers translate constraint names to HTTP errors.
- Tests use `WithTenant`; every new assertion is mutation-checked.
- Contract docs regenerated (`go run ./cmd/contractdocs`).

## Review focus

1. A paid event redelivered, or arriving before opened, makes exactly one receipt.
2. A cash payment while a UPI payment waits cannot pay the same balance.
3. A late payment on a closed shift: receipt recorded, shift's cash figures unchanged.
4. Another requester's outcome (pharmacy) never touches a billing request.
5. A UPI receipt is never voided and never counted in the drawer.

## Tasks

### Task 1: payments serves requester `billing`

- `payments/contract`: `RequesterBilling = "billing"`.
- `payments/payments_billing.sql` as `0002_payments`: requester check `IN ('pharmacy','billing')`.
- `payments/module.go`: consumers built per requester; add `payments-billing-request`
  and `payments-billing-cancel` on the billing subjects.
- Test: a billing request event opens an intent with requester `billing`; a cancel closes it.

### Task 2: billing contract and request table

- `billing/contract/events.go`: `SubjectPaymentRequested`, `SubjectPaymentCancelled`,
  purposes, `PaymentRequestedData`, `PaymentCancelledData`; archtest non-PHI entries.
- `billing/upi.sql` (`0011_billing`): `billing_upi_requests`, guards, `billing_upi_waiting()`,
  receipt `mode`/`upi_request_id`, replaced receipt, shift and application guards,
  `billing_shift_expected` cash-only, policies admitting `billing_consumer()`, consumer
  actors added to `billing_consumer()`, paid-transition trigger inserting the receipt.
- Tests (`upi_test.go`): ask, replay, conflict, over-balance with waiting UPI, cash
  refused against waiting UPI, cancel by another cashier refused, close blocked then
  allowed after 15 minutes.

### Task 3: outcome consumers

- `billing/upi.go`: handlers (put, get, cancel, list) and `upiOutcomeConsumers()` for
  the five intent subjects, ignoring other requesters.
- Tests: paid → one receipt (mode upi, numbered next, purpose), redelivery no second
  receipt, late paid on closed shift, mismatched amount pays nothing, pharmacy outcome
  ignored, UPI receipt void refused, expected cash excludes UPI.

### Task 4: account and collections

- `accounts.go`: `upi_waiting_minor` per total; receipts list carries `mode`, `rrn`, `late`.
- `collections.go` + CSV: cash columns cash-only; `upi_payments_minor`, `upi_deposits_minor`
  per shift and totals.
- Tests in `collections_test.go`, `accounts_test.go`.

### Task 5: frontend

- `packages/api/src/billing.ts`: `BillingUpiRequest`, receipt `mode`/`rrn`/`late`,
  totals `upi_waiting_minor`, collection UPI fields.
- `pharmacy/verified-upi.tsx`: export `PaymentState` with a `waitingNote` prop.
- `medicore/billing-upi.tsx`: amount + purpose, QR, follows the request, resumes a
  waiting one, cancels on demand; toasts "UPI payment received".
- `cash-payments.tsx`: UPI option; cash cap nets waiting UPI; receipts show UPI/RRN/late,
  no void on UPI. `daily-collection.tsx`: UPI columns.
- Vitest: paid flow, resume, cancel, cap, report columns.

### Task 6: docs

- `docs/api/domains/billing.md`, `payments.md`, `portfolio.md`; correct the cash-counters
  and payments specs' "not covered"; contractdocs regenerate.
