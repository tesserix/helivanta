# Handover between cashiers (#198 slice 5)

Resolves: #198 (slice 5 of cashier management). Builds on cash counters
(slice 1), daily collection (slice 2), UPI (slice 3, #211) and cash pickups
and counting by note (slice 4).

## Why

A counter runs longer than one cashier's shift. Today the outgoing cashier
closes, takes the drawer to finance, and the incoming cashier opens with a
typed float that nothing ties to what was handed over. If cash goes missing
between them, neither close nor open shows it, and each can blame the other.

## Decisions (product owner, 2026-11-02)

1. **A handover is a close and an open counted together.** The outgoing
   cashier counts the drawer blind by note; the incoming cashier counts the
   same cash without seeing that count; the incoming shift opens with the
   agreed count as its float.
2. **Counts that disagree stop the handover.** It waits for a supervisor
   (`billing.shift.authorise`, neither cashier), who counts the drawer and
   records a reason. Neither cashier can accept the other's figure.
3. **Out of this slice:** card, netbanking and wallet modes; the GL export;
   `Shift*` events; a safe ledger.

## What it does

- **Offer.** `PUT …/cash-shifts/:shift/handovers/:handover` `{to, counts}` by
  the shift's own cashier (`billing.cash.collect`) while it is open: `201`,
  the same again `200`, a different body on the same id `409`. The shift
  becomes `handing_over`: it takes no cash, refunds, voids or pickups (every
  one of those already requires an open shift). Refused: handing over to
  yourself (`400`), a waiting UPI payment (`409 upi_waiting`, as at close),
  nothing counted (`400`). The response carries no expected figure.
- **Withdraw.** `PUT …/cash-handovers/:handover/withdrawal` by the outgoing
  cashier while the offer stands: the shift is open again. A disputed
  handover cannot be withdrawn; the supervisor settles it.
- **Accept.** `PUT …/cash-handovers/:handover/acceptance` `{shift, counts}` by
  the named incoming cashier (`billing.cash.collect`). One attempt; the
  answer says only `accepted` or `disputed`, never either figure, so the
  count cannot be probed.
  - Counts equal note for note: in one transaction the outgoing shift
    closes with that count against its computed expected cash (a variance
    goes to authorisation as at any close), and the incoming shift `shift`
    opens on the same counter with the count's value as its float.
  - Counts differ: the handover is `disputed`; nothing else changes.
  - _Builder's call:_ equal means the same notes and coins, not the same
    total. Physical notes do not change between two counts, so a different
    breakdown with the same total is a miscount, and it is cheap to recount.
- **Settle.** `PUT …/cash-handovers/:handover/settlement` `{counts, reason}` by
  someone holding `billing.shift.authorise` who is neither cashier: their
  count is the cash. The outgoing shift closes against it and the incoming
  shift (the id the incoming cashier sent when accepting) opens with it as
  the float, for the incoming cashier.
- **Lists.** `GET …/cash-handovers/incoming` (offers waiting for the caller)
  and `GET …/cash-handovers/disputed` (for supervisors), both paginated and
  without counts. A settled or accepted handover shows its agreed count;
  an open or disputed one shows none.
- **Counter.** A shift closed by handover no longer holds its counter while
  its variance waits, since the incoming shift holds it now. An ordinary
  close still holds the counter until its variance is authorised (slice 1
  unchanged).
- **Database.** A handover is a row in `billing_cash_handovers`; its states
  and actors are enforced by triggers, as are the shift's new transitions:
  `open → handing_over` only with a live offer by its cashier;
  `handing_over → open` only after a withdrawal; `handing_over →` closed or
  pending only with the agreed count; a shift opened from a handover only
  with its agreed float, counter and cashier.
- **Shell.** The cash desk offers a handover from the open shift (count by
  note and the incoming cashier's id), shows a waiting offer to the
  incoming cashier with a blind count, and lists disputed handovers for the
  supervisor with a count and reason.

## Not covered

Card, netbanking and wallet modes; GL export; events; a safe ledger;
marking handovers in the daily collection report (each side shows as its
own shift); a handover between counters.

## Needs finance review before production

- A supervisor settling a dispute opens the incoming cashier's shift
  without that cashier acting again.
- A disputed handover holds the counter until a supervisor comes: no
  receipts at that counter meanwhile.
