# Final bills on the admission account (#197 slice 4)

Resolves: #197 (slice 4 of admission billing). Builds on interim bills (slice 3, #1464) and
on MediCore's discharge orders (#96 slice 1, #1468), whose events billing now consumes.

## Why

The discharge-day wait starts when the doctor says the patient may go. Until now the billing
desk had no signal for that moment and no bill that says "this is what you owe to leave".
With discharge orders published, billing can open the final bill as soon as a doctor
decides, instead of when the bed is released.

## What it does

- **Billing records discharge orders** from `medicore.discharge_ordered.v1` and
  `discharge_withdrawn.v1`, in either order (a withdrawal can overtake its order), each set
  once; a replay that differs is refused. An order is active if ordered and not withdrawn.
- **One series of bills per admission**, interim or final, numbered 1, 2, 3 …
  - **Interim** (unchanged): an open account with no active discharge order.
  - **Final**: once discharge is ordered, or after release (a release without an order is
    still allowed in MediCore). Needs bed charging set up and every bed day priced: a final
    bill never leaves out a charge it could not price (`409 bed_days_not_priced`).
- **Reissued, never edited.** A final bill freezes the account as issued, like an interim
  bill. Anything after it — a midnight still in the bed, a late pharmacy charge — goes on a
  new final bill; the first stays as issued. Nothing new is `409 nothing_new`, except that
  a first final bill after interim bills is always new.
- **A withdrawn discharge** returns the account to interim bills; an issued final bill stays
  in the history as issued.
- **Refusals:** interim once discharge is ordered `409 discharge_ordered`; interim after
  release `409 account_closed`; final before either `409 discharge_not_ordered`.
- **Routes:** `POST …/final-bills`; `GET …/bills`, `…/bills/:number`, `…/bills/:number/lines`
  for either kind. The interim routes keep answering interim bills only. The account shows
  `discharge_ordered`.
- **Ward board:** the bills panel lists both kinds and issues the kind the account is ready
  for. A final bill on a patient still in the bed says what it may still miss.

## Rules enforced in the database

The issue trigger holds the interim/final conditions, bed charging and priced days, beside
the handler. Discharge orders are written only by billing's two consumers, each half once.

## Calls I made (builder's)

- **Final bills can be reissued** rather than locking the account: patients do stay past a
  midnight after the bill is printed, and late charges happen. The latest final bill is the
  current one; none is ever changed.
- **Bed days must all be priced** for a final bill, and bed charging set up. A hospital that
  does not charge for beds has no way through yet; that is deliberately fail-closed.
- **Statement, not tax invoice**, as for interim bills (#193).

## Not covered

Deposits and payments netted on the final bill (#199); discharge clearances before the
final bill (#1043); procedure bundles (#196); discounts (#200); GST (#193); a
`FinalBillReady` event; a final bill for a released patient from the UI (the ward board
shows occupied beds only; the API serves it).
