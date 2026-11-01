# Cash pickups and counting by note (#198 slice 4)

Resolves: #198 (slice 4 of cashier management). Builds on cash counters
(slice 1), daily collection (slice 2), UPI (slice 3, #211) and refunds (#199).

## Why

A busy counter sends cash to the safe during the shift. Today nothing records
that, so at the blind close the drawer is short by whatever went to the safe
and the variance is meaningless. And the close is a single typed figure:
nothing shows how the cashier reached it, and a mistyped total is
indistinguishable from a shortage.

## Decisions (product owner, 2026-11-02)

1. **A pickup is recorded by the person who takes the cash**, holding
   `billing.shift.authorise`, never the cashier whose drawer it is, while the
   shift is open, and never more than the drawer should hold at that moment.
   It is fixed once recorded; a wrong one is corrected by a separate entry
   that keeps both.
2. **The close counts by note and coin.** The declared figure is the sum of
   the count; the typed-total close is replaced, not kept beside it.
3. **Out of this slice:** handover between cashiers, a safe ledger, the GL
   export, `Shift*`/`CashPickupRecorded` events, counting the opening float
   by note.

## What it does

- **Notes and coins.** One database function, `billing_cash_denominations(currency)`,
  lists what a count may contain; for INR the RBI legal-tender set (checked
  2026-11-02 against RBI's _Indian Currency_ FAQ and its December 2025 ₹2000
  release): notes of ₹2000, 500, 200, 100, 50, 20, 10, 5, 2 and 1; coins of
  ₹20, 10, 5, 2, 1 and 50 paise. ₹2000 notes are withdrawn from circulation
  but remain legal tender, so a drawer may hold one. A count is a JSON object
  keyed `note:<paise>` / `coin:<paise>` with whole counts 0–1,000,000;
  `billing_cash_count_total(currency, counts)` is its value, or null for any
  key or count outside the list. The shell reads the list from
  `GET …/cash-denominations`, so it lives in one place.
  - _Builder's call:_ the issue says the country profile. The country policy
    lives in the workspace module and billing may not read another module's
    tables, so the list is one currency-keyed database function, served to
    the shell; a second currency is a new branch in it, not code elsewhere.
- **Pickups.** `PUT …/cash-shifts/:shift/pickups/:pickup` `{counts}`: the
  count's total leaves the drawer. `201`; the same again `200`; a different
  body on the same id `409`. Refused:
  - own shift: `409 own_pickup` (database constraint as well as the handler);
  - shift not open: `409 shift_closed`;
  - nothing counted: `400`;
  - more than the drawer should hold: `409 drawer_short`, with no figure, so
    the supervisor hands the excess back and records what fits. Failing
    closed here keeps the expected cash from going below zero, which every
    refund and void check relies on.
- **Corrections.** `PUT …/cash-pickups/:pickup/corrections/:correction`
  `{amount_minor, reason}` by someone holding `billing.shift.authorise`,
  other than the shift's cashier, while the shift is open: puts back up to
  what the pickup still counts (`409 over_pickup` beyond). No count: a
  correction says the record overstated what was taken.
- **Expected cash** is float + cash payments and deposits − cash refunds −
  pickups + corrections. Refunds and voids are bounded by it, so they now
  see the safe drop too. Pickups take the counter lock, the same as refunds
  and voids, so they serialise with them, and the shift share lock, so they
  serialise with the close.
- **Close** `PUT …/cash-shifts/:shift/close` `{counts}` (the shell asks the
  cashier to say the drawer is empty rather than send a blank form): the database
  computes `declared_minor` from the count and keeps the count on the shift.
  A shift closed before this slice keeps its typed figure and no count. A
  retry with the same count replays.
- **Reading.** Each shift shows `picked_up_minor` (pickups less corrections):
  the cashier handed it over, so it reveals nothing of the expected cash.
  `GET …/cash-shifts/:shift/pickups` lists a shift's pickups and corrections
  for the supervisor.
- **Daily collection.** Each counted shift and the totals carry
  `picked_up_minor`; the CSV gains a last column `picked_up`. Pickups are a
  movement, not revenue: net stays payments + deposits − refunds.
- **Shell.** The close form counts by note with a running total; the cash
  desk lists open shifts to the supervisor with a pickup form and each
  pickup's correction; the collection report shows pickups.

## Not covered

Handover; a safe or vault ledger and bank deposit; counting the float by
note; card and other modes; GL export; events; counting pickups for a closed
shift (its variance stands for it).

## Needs finance review before production

- A refused pickup leaves no record, so a supervisor could narrow down an
  open shift's expected cash by trying amounts; audit refused pickups or
  accept the risk.
- Whether a supervisor may record a pickup without the cashier's
  acknowledgement in the system.
- Whether the drawer bound should be softened for a physically counted excess.
