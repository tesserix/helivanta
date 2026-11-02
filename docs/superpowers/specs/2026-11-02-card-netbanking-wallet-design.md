# Card, netbanking and wallet receipts (#198 slice 6)

Resolves: #198 (slice 6 of cashier management). Builds on cash counters
(slice 1), the daily collection (slice 2), UPI (slice 3, #211), pickups
(slice 4) and handovers (slice 5).

## Why

A receipt today is cash or UPI. A patient who pays by card on the counter's
terminal, by netbanking transfer or from a wallet has no way to be recorded,
so the cashier either refuses the money or writes it down outside the system,
and the daily collection finance signs off is short of real takings.

## Decisions (product owner, 2026-11-02)

1. **Recorded after the fact.** The cashier records the payment once the
   terminal or the bank has confirmed it. No gateway is involved and there
   is no pending state.
2. **What is recorded.** The amount, the mode and the reference:
   - card: the approval code and the transaction reference (RRN) from the
     slip. No card digits, not even the last four.
   - netbanking: the bank's transaction reference (UTR).
   - wallet: the wallet's name and its transaction ID.
3. **Duplicates.** The same reference twice at one hospital is refused.
4. **Counters.** A counter takes only the modes it is set up for.
5. **Drawer and reports.** These receipts never count towards expected cash.
   They show by mode in the daily collection and its CSV.
6. **Mistakes.** A wrong entry is voided with a reason, as cash is.
7. **Deposits included.** A deposit can be taken by any of these modes.
8. **Out of this slice:** refunds to a card or account (refunds stay cash),
   reconciliation against terminal or bank settlement reports, card fees,
   GL export changes beyond new columns, `Shift*` events.

## What it does

- **Counter modes.** `PUT …/counters/:counter/modes/:mode` `{accepted}` by
  `billing.counter.manage`, for `card`, `netbanking` or `wallet`. A change
  appends a row (who, when); asking for the current state again changes
  nothing and answers `200`. The counter list carries `modes`, the ones it
  accepts now. A new counter accepts none of them.
- **Taking one.** The existing `PUT …/admissions/:admission/receipts/:receipt`
  and `…/deposits/:receipt` take `mode` (`cash` when absent), and for the
  three new modes `reference`, plus `approval_code` for card and `wallet`
  for wallet. Every existing rule holds: the cashier's own open shift, the
  counter's numbering, the balance due for payments (net of waiting UPI),
  an open account for deposits. Refused: a mode the counter does not accept
  (`409 mode_not_accepted`), a reference already on a standing receipt
  (`409 duplicate_reference`), a refund in any mode but cash (`400`).
  Replaying the same id with the same body answers `200`; any difference,
  reference included, `409`.
- **Void.** As cash, by its cashier while the shift is open, with a reason.
  No drawer check: the money was never in it. A deposit already applied or
  refunded is not voided, as now. The cashier reverses the payment on the
  terminal or with the bank; the system cannot see that it was.
- **Reports.** The daily collection gains, per shift and in the totals,
  `card_payments_minor`, `card_deposits_minor`, `netbanking_payments_minor`,
  `netbanking_deposits_minor`, `wallet_payments_minor`,
  `wallet_deposits_minor`, blinded on open shifts like UPI, and the same six
  CSV columns at the end. `net` stays cash.
- **Shell.** The cashier's payment and deposit forms gain a mode choice
  limited to what their counter accepts, with the reference fields that mode
  needs. A receipt shows its mode and reference and is voidable unless UPI.
  Administrators switch modes per counter. The daily collection shows the
  new columns.

## Enforcement

Migration `0015_billing`:

- `billing_counter_modes`, append-only, forced RLS, audited; a row per
  change, set by the acting subject. A counter's current mode is its latest
  row. Changes lock the counter, as receipts do, so a receipt never races a
  switch-off.
- Receipt `mode` widens to `card|netbanking|wallet`. New columns
  `reference`, `approval_code`, `wallet`, each null unless its mode needs
  it, by check constraint. `reference` is `^[A-Z0-9][A-Z0-9/-]{3,39}$`
  (stored upper case), `approval_code` `^[A-Z0-9]{6}$`, `wallet` lower case
  `^[a-z0-9]([a-z0-9 .-]{0,28}[a-z0-9])?$`.
- No card number is stored (review finding): a card's reference is its
  12-digit RRN (`^[0-9]{12}$`), and in every mode a Luhn-valid run of 13 to
  19 digits, or a masked number ending `XXXX` and four digits, is refused by
  check constraint, in Go and in the form. _Builder's call:_ a rare real
  reference that passes Luhn is refused too; storing a card number is the
  worse failure.
- A unique index over standing receipts: `(tenant, hospital, reference,
approval_code)` where not voided, whatever the mode or wallet name, so a
  respelt wallet or another mode never lets one payment clear two balances
  (review finding). _Builder's call:_ a voided receipt frees its reference, so
  an entry made against the wrong patient can be voided and entered again; a
  card reference is keyed with its approval code, since an RRN alone can
  repeat across acquirers.
- The receipt guard refuses a mode the counter does not currently accept,
  and skips the drawer check when voiding anything but cash. Refunds were
  already cash-only by check; UPI keeps its own path.
- Expected cash already counts only `mode='cash'`; it is unchanged.

_Builder's call:_ cash and UPI are not switched per counter. Cash is how a
drawer exists at all; UPI is already gated by the hospital's payment account.

## Not covered (for finance review)

- Nothing verifies a typed reference: cash recorded as card with an invented
  RRN leaves the drawer matched. Only settlement reconciliation, out of this
  slice, would catch it.
- A non-cash void needs only its cashier's reason, and frees the reference.
- Voided figures in the daily collection count every mode.

- A deposit taken by card is refunded in cash from the drawer.
- A voided card receipt relies on the cashier reversing it on the terminal.
- References are unique within a hospital, not across hospitals of a tenant.
- No settlement reconciliation: a recorded card receipt is not proof the
  acquirer paid.
