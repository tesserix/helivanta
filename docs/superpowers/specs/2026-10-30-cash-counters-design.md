# Cash counters: shifts and cash receipts on the admission account (#198 slice 1)

Resolves: #198 (slice 1 of cashier management). Builds on admission accounts and bills
(#197 slices 1–4).

## Why

The final bill says what a patient owes; nothing records what they paid. Cash is the
largest internal-fraud surface in an Indian hospital: unattributed collections, drawers
nobody reconciles. This slice gives each rupee taken at a billing counter a cashier, a
counter, a shift and a numbered receipt, and closes the shift blind against what the system
expected.

## What it does

- **Counters** per hospital: code (`[A-Z0-9-]`, up to 20) and name, set up once by a tenant
  administrator (`billing.counter.manage`) holding the hospital's billing grant.
- **Shifts.** A cashier (`billing.cash.collect`, receptionist, plus the grant) opens a shift
  on a counter with a declared opening float. One open shift per counter and one per cashier.
- **Cash receipts** against an admission's account, in the cashier's own open shift: an
  amount in a currency the account owes, never more than the balance due in it (an advance is
  a deposit, #199). Numbered per counter, contiguous: `<counter code>-<n>`. A receipt states
  the counter, shift, cashier and time.
- **Voided, never edited.** In the same open shift the cashier voids a receipt with a reason
  (the cash goes back across the counter); the receipt stays, marked void. After the shift
  closes a receipt stands; a refund is a later slice.
- **Blind close.** The cashier declares the cash counted without seeing the expected figure.
  Expected = opening float + live cash receipts. A matching count closes the shift; a
  difference leaves it `pending_authorisation` until a tenant administrator other than the
  cashier (`billing.shift.authorise`) authorises it with a reason. The variance is kept.
- **The account** shows `paid` (live receipts) and `balance` (outstanding less paid) per
  currency. A reversal after payment can leave a negative balance: shown as in credit.
- **Idempotent** by client UUIDv7 ids for counters, shifts, receipts and voids; the same
  command replays, a different one under the same id is `409 conflict`.

## Rules enforced in the database

- One open (or pending) shift per counter, one per cashier (partial unique indexes).
- A receipt only in an open shift belonging to its cashier; its number is the counter's next.
- A receipt's amount never exceeds the account's balance due in its currency, taken under the
  admission lock so two receipts cannot both fit one balance.
- A void only while the shift is open, by its cashier, once.
- A shift's float and identity never change; it closes once, with expected and declared cash;
  authorised once, by someone other than the cashier.
- Append-only otherwise; forced row security by the billing grant; audited.

## Calls I made (builder's)

- **Cash only.** UPI goes through the payments module, which needs a second requester (it
  serves pharmacy only today); that is slice 2, with card and the other modes #198 lists.
- **Single currency per shift**: INR floats and receipts, matching every rate and charge in
  the system today. A receipt in another currency is refused.
- **No overpayment.** Money beyond the balance due is an advance, which belongs to deposits
  (#199) and their liability treatment.
- **Supervisor is a tenant administrator** (no supervisor role exists); never the cashier.
- **Receipts are not on bills yet**: bills stay as issued. Netting paid amounts on bills comes
  with deposits, where the final bill's "amount to pay" is defined.

## Not covered

UPI (since added: `2026-11-01-billing-upi-design.md`), card, netbanking and wallet; handover between cashiers; denominations; mid-shift
pickups; refunds; shift-close and daily collection reports and GL export; events
(`ShiftOpened` …), which nothing consumes yet; receipts on bills; deposits (#199).
