# Refunds of unused deposits (#199 slice 2)

Resolves: #199 (slice 2 of deposits). Builds on deposits (slice 1) and cash counters (#198).

## Why

A final bill can now say "to refund ₹X" when the patient's deposits exceed what they owe,
but nothing pays that money back. A refund is cash leaving the drawer: it must be attributed
to a cashier and a shift, bounded by what is held for the patient, and, above a hospital's
threshold, approved by someone other than the cashier before it goes out.

## Decisions (user, 2026-10-30)

1. **A threshold per hospital,** set by a tenant administrator, defaulting to ₹10,000.
   A refund above it needs a supervisor's approval; at or below it the cashier pays out.
2. **Only after a final bill,** and only from deposits not applied.

## What it does

- **Refund policy.** A tenant administrator (`billing.counter.manage`) sets the hospital's
  approval threshold; each change is a new version, the latest applies; none set is ₹10,000.
- **Approval.** For a refund above the threshold, a supervisor (`billing.shift.authorise`)
  approves an amount for the admission with a reason, before the cash goes out. The approver
  is never the cashier who pays it.
- **Paying out.** A cashier (`billing.cash.collect`) pays a refund from their open shift: a
  receipt with `purpose: refund`, numbered with the counter's receipts. At most the surplus:
  what is held (deposits less applied less refunded) beyond what the bill still needs, applied
  or not (`409 over_held`), after a final bill
  (`409 no_final_bill`), never more than the drawer holds (`409 drawer_short`), and above the
  threshold only against an unused approval for exactly that amount (`409 approval_required`).
- **The drawer.** A refund reduces the shift's expected cash; the blind close counts it.
- **Voided, never edited,** like any receipt, while the shift is open: the cash goes back in
  the drawer and is held again; its approval stays used, so paying it again needs a new one.
  Voiding a payment or deposit is refused if the drawer no longer holds that cash
  (`409 drawer_short`): it may already have gone out as a refund. A deposit can no longer be voided below what was applied or
  refunded.
- **Balances.** The account adds `refunded_minor`; deposits held is deposits less applied less
  refunded. A final bill issued afterwards records the lower held figure.

## Rules enforced in the database

Refund receipts only after a final bill, within the surplus held beyond the bill, within the drawer, and above
the threshold only against an unused approval for the same admission and amount by someone
other than the cashier; the shift's expected cash nets refunds; a deposit void leaves held
at zero or more. Policies and approvals are append-only, attributable and audited.

## Calls I made (builder's)

- **A refund is a receipt with a negative effect** on the drawer: one numbering and one void
  path for every cash movement at a counter.
- **The approval names the amount,** so an approval for ₹15,000 cannot pay ₹50,000; a
  different amount needs a new approval.
- **Credit from a reversal after application** (deposit applied, then a pharmacy reversal) is
  still not refundable: applied money is not un-applied in this slice.

## Not covered

Refunds by UPI or bank transfer; un-applying deposits; refunds of payments (not deposits);
refund requests from the ward; events; GL posting.
