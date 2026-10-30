# Deposits held for the patient and applied against the final bill (#199 slice 1)

Resolves: #199 (slice 1 of deposits). Builds on cash counters (#198 slice 1) and final bills
(#197 slice 4).

## Why

Indian hospitals take an advance at admission and top-ups during a long stay. Today the
counter refuses any cash beyond the balance due, so the advance cannot be taken at all, and
a final bill cannot net off what the patient handed over. A deposit is the patient's money
held by the hospital, not revenue, until it is applied to a bill or refunded.

## Decisions (user, 2026-10-30)

1. **Held until the final bill.** A deposit stays the patient's money while the stay runs.
   It is applied against the account once a final bill exists, as its own recorded step.
2. **Optional.** No deposit is required at admission in this slice.

## What it does

- **Taking a deposit.** A cashier takes a deposit in INR against an open admission account,
  in their own open shift (`billing.cash.collect`). It is a receipt like any other: numbered
  on the counter, counted in the shift's expected cash, voided only while the shift is open.
  It has no balance cap: an advance is money beyond what is owed so far. A released account
  takes no deposit (`409 account_closed`); the final payment is a payment.
- **Applying deposits.** Once a final bill has been issued, the billing desk
  (`billing.bill.issue`) applies held deposits: the amount is the lesser of what is held and
  the balance due, computed by the server. Recorded with the final bill it was applied
  against, who and when. Nothing to apply is `409 nothing_to_apply`; no final bill
  `409 no_final_bill`. Applied once per command id; a later reissued final bill can have
  more applied.
- **Never more than held.** A deposit receipt that has been applied cannot be voided below
  what was applied (`409 deposit_applied`).
- **Balances.** The account shows, per currency: charged, reversed, outstanding, paid
  (payment receipts), deposits held (deposit receipts less applied), applied, and balance due
  (outstanding less paid less applied; below zero is credit). A payment is still capped at the
  balance due; money beyond it is taken as a deposit.
- **On bills.** Every bill issued from now on records, at issue, the INR paid and deposits
  held, set by the database; the final bill shows charged, paid, deposits held and the amount
  still to pay or, if deposits exceed it, to refund.

## Rules enforced in the database

- A deposit receipt only on an open account, in its cashier's open shift; payments still
  capped at the balance due (now net of applications).
- A void leaves held deposits at or above what was applied.
- An application only after a final bill, for at most the lesser of held and due, under the
  admission lock; append-only.
- A bill's paid and deposit figures are set by the database at issue, never supplied.

## Calls I made (builder's)

- **Deposits share the counter's receipt numbering** and the shift's expected cash: they are
  cash in the same drawer, and one series keeps the drawer auditable.
- **Application is a desk action, not automatic** at final bill issue: final bills may be
  reissued, and applying is a financial step someone should own.
- **INR only**, as for cash receipts.

## Not covered

Refunds of unused deposits (cash out of the counter under supervisor authority); top-up
alerts; deposit policies by bed class or procedure; required admission deposits; transfer
between admissions; deposits for OPD procedures; events; GL liability posting.
