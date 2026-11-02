# Shift events (#198 slice 7)

Resolves: #198 (slice 7 of cashier management), its "Publication of
`ShiftOpened`, `ShiftClosed`, `ShiftVarianceRecorded`, `CashPickupRecorded`".
Builds on cash counters (slice 1), pickups (slice 4), handovers (slice 5) and
card, netbanking and wallet (slice 6).

## Why

Everything that happens to a drawer is recorded in billing, but nothing outside
billing can learn of it except by reading billing's API. The finance controller's
tooling, a future GL posting and a supervisor's alerting all need to be told when
a shift opens, closes, closes short or over, and when cash leaves the drawer for
the safe, without asking billing repeatedly.

## Decisions (product owner, 2026-11-02)

1. **The four events the issue names, plus a handover event.** A change of
   cashier is otherwise only visible as one shift closing and another opening.
2. **Nothing consumes them yet.** They are published contracts; the docs say so.
3. **Out of this slice:** the GL export (needs finance's chart of accounts);
   events for receipts, refunds or voids; an event when a handover is offered,
   withdrawn or disputed (the drawer is only paused); consumers.

## What it does

Five subjects, published by billing through the outbox:

| Subject | When |
| --- | --- |
| `helivanta.in.billing.shift_opened.v1` | A shift opens, by its cashier or from a handover. |
| `helivanta.in.billing.shift_variance_recorded.v1` | A shift's count differs from the expected cash: it waits for a supervisor. |
| `helivanta.in.billing.shift_closed.v1` | A shift is closed for good: its count matched, or its variance was authorised. |
| `helivanta.in.billing.cash_pickup_recorded.v1` | Cash is taken to the safe, or a pickup is corrected. |
| `helivanta.in.billing.shift_handed_over.v1` | A handover completes: accepted by the incoming cashier or settled by a supervisor. |

So a shift publishes `shift_opened`, any pickups, then either `shift_closed`, or
`shift_variance_recorded` followed by `shift_closed` when it is authorised. A
shift closed by handover publishes the same, and the handover publishes
`shift_handed_over`; the incoming shift's `shift_opened` names the handover.

**Payloads** (amounts in paise, `currency` `INR`, ids as strings):

- `shift_opened`: `shift_id`, `hospital_id`, `counter_id`, `cashier`,
  `currency`, `opening_float_minor`, `handover_id` (null unless opened from a
  handover), `opened_at`.
- `shift_variance_recorded`: `shift_id`, `hospital_id`, `counter_id`,
  `cashier`, `currency`, `expected_minor`, `declared_minor`, `variance_minor`,
  `handed_over`, `counted_at`.
- `shift_closed`: `shift_id`, `hospital_id`, `counter_id`, `cashier`,
  `currency`, `opening_float_minor`, `expected_minor`, `declared_minor`,
  `variance_minor`, `handed_over`, `counted_at`, `authorised_by` and
  `authorised_at` (null when the count matched).
- `cash_pickup_recorded`: `pickup_id`, `shift_id`, `hospital_id`,
  `counter_id`, `kind` (`pickup` or `correction`), `amount_minor`, `currency`,
  `corrects_id` (null for a pickup), `recorded_by`, `recorded_at`.
- `shift_handed_over`: `handover_id`, `hospital_id`, `counter_id`,
  `from_shift_id`, `to_shift_id`, `from_cashier`, `to_cashier`, `currency`,
  `agreed_minor`, `settled_by` (null when accepted), `handed_over_at`.

**Left out on purpose:** reasons (free text a cashier or supervisor typed),
counts by note and coin, and anything about patients or admissions. Cashier
and supervisor ids are kept: attribution is the point of the story.

## How it holds

- **Emitted by the database.** `AFTER` triggers on `billing_cash_shifts`,
  `billing_cash_pickups` and `billing_cash_handovers` write the outbox row in
  the same transaction as the change, as privacy's deadline events do. No
  handler, present or future, can change a drawer without its event, and a
  rolled-back change publishes nothing. Subjects are substituted from the Go
  contract constants at migration time, so SQL and Go cannot name different
  subjects.
- **Once per change.** Each event fires on a state transition the existing
  guards allow once (`open`/`handing_over` to `pending_authorisation` or
  `closed`; `pending_authorisation` to `closed`; a handover to `accepted` or
  `settled`; a row inserted). Retried requests replay from the record without
  writing, so they publish nothing. Consumers deduplicate on `event_id`, and on
  the natural key (`shift_id` with the subject, `pickup_id`, `handover_id`).
- **Blind stays blind.** No event carries an expected cash figure while a shift
  is open or being handed over; `expected_minor` is published only once the
  count is in, when the cashier is shown it too.
- **Contract parity.** Each payload has a Go type in `billing/contract`
  (`ShiftOpenedData` and so on), from which the reference schema is generated.
  The tests decode every published payload strictly into its type and require
  the payload's keys to be exactly the type's fields, so a renamed column or a
  forgotten field fails CI.

## Not covered

The GL export; events for receipts, refunds and voids; events for a handover
being offered, withdrawn or disputed, and for a shift going to or back from
`handing_over`; consumers of any of these events.
