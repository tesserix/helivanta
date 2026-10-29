# Bed-day charges on the admission account (#197 slice 2)

Resolves: #197 (slice 2 of admission billing). Builds on slice 1 (#1453), which
opened one account per admission and posted pharmacy charges to it.

## Why

An admitted patient's biggest charge is the bed, and nothing charges it today:
the account holds medicine only. Every bed already has a class (general,
semi-private, private, ICU, HDU, isolation, neonatal, day-care) and billing
already hears when each stay starts. This slice turns stays into priced bed
days, so the ward and the billing desk see what the stay costs as it runs.

## Decisions (user, 2026-10-30)

1. **Midnight census.** A day is charged for each midnight, in the hospital's
   timezone, that the patient spends in a bed, at the class of that bed. A
   transfer at midnight exactly counts the new bed. A stay that crosses no
   midnight is charged one day (the _minimum day_) at the class of its first bed.
2. **No GST in this slice.** Bed charges are recorded before tax and labelled so.
   Tax on room rent belongs to #193 and is checked against current primary
   sources there, not here.
3. **A simple rate table now.** A hospital sets a daily rate per bed class with
   an effective date. #1045 (tariff versions, payer categories, precedence) will
   absorb it; until then there is one rate per class per date, for every payer.

## Calls I made (builder's, marked so the user can check them)

- **Timezone is set once per hospital** as part of bed-charging setup. Nothing
  records a hospital timezone today. Until it is set, no bed day is computed
  and the account says bed charging is not set up. Bed days derive from stays,
  so setting it later computes every day since admission. It cannot be changed
  (a change would move midnights under days already charged).
- **A missing rate is "not priced", never ₹0.** The day is recorded with no
  amount and counted as not priced on the account. When a rate covering it is
  set, the day is priced. A priced day is never repriced: a later rate change
  affects only days not yet priced.
- **Rates are append-only.** A rate for a class and date, once set, is fixed;
  the same rate again is accepted, a different one is `409`. A mistake is
  corrected by a rate from a later date.
- **Correction preserves history.** If events arrive late and a day already
  charged turns out wrong (the stay ended before that midnight, or the bed
  differed), the day is voided and a reversal entry posted; the correct day is
  recorded beside it. Nothing is deleted or edited.
- **Open stays are counted 15 minutes after midnight**, so a discharge just
  before midnight has time to arrive and is not charged and reversed.
- **Who sets rates:** tenant administrators only (`billing.rate.manage`), and
  they must hold the hospital's billing grant, as readers do. Rates are read by
  anyone holding `billing.account.read` and the grant.

## Behaviour

- MediCore's `bed_occupancy_started.v1` gains `bed_class`, the class of the bed
  taken (immutable on the bed). Billing refuses a start without it.
- Billing keeps its own record of each stay (started, ended, class), filled from
  `bed_occupancy_started.v1` and `bed_occupancy_ended.v1` in either order.
- **Reconcile** (one function, per admission, serialised by an admission lock)
  computes the days the stays imply — midnights `M` with `started_at <= M <
ended_at`, or `M <= now − 15 min` while open, plus the minimum day once the
  admission is released and every stay has ended without a midnight — and
  converges the recorded days to them: record missing days (priced if a rate is
  effective on that date), void days no longer implied, price days not priced.
- It runs on every stay start, stay end and release, and from a census worker
  each minute for stays whose next midnight has passed, for every stay of a
  hospital once its timezone is set, and for days of a class once a rate for it
  is set.
- A bed day's charge is an account entry, source `bed`, document
  `Bed day YYYY-MM-DD`, so totals, the entries list and replays work as for
  pharmacy charges.
- The account answer adds per-source outstanding amounts, the count of bed days
  charged and not priced, and whether bed charging is set up.

## Routes

| Route                                                                   | Permission                     | Behaviour                                                          |
| ----------------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------ |
| `PUT /billing/hospitals/:hospital/bed-charging` `{timezone}`            | `billing.rate.manage` + grant  | Set once; same again `200`, different `409`, unknown zone `400`.   |
| `GET …/bed-charging`                                                    | `billing.account.read` + grant | The timezone, or `404 not_configured`.                             |
| `PUT …/bed-rates/:class/:effective_from` `{daily_rate_minor, currency}` | `billing.rate.manage` + grant  | Needs the timezone set (`409`). Same again `200`, different `409`. |
| `GET …/bed-rates` (paged)                                               | `billing.account.read` + grant | Rates, newest set first.                                           |

## UI

- Ward board, admission line: "Owed ₹X · medicine ₹a · bed days ₹b (before tax)",
  plus "n bed days not priced" when any are, or "Bed charging is not set up".
- IPD setup (administrators with the grant): set the timezone (Asia/Kolkata
  offered), add a rate (class, ₹ per day, effective from), see the rates.

## Not covered (scoped out, stated in the PR)

- GST on room rent (#193); separate nursing and ward-service components;
  consultant visits; repricing the whole stay on upgrade; payer-specific rates
  (#1045); deposits and payments (#199); discounts and correcting a priced day
  (#200); interim and final bills.
- Stays that began before this release carry no bed class and are not charged.
- Changing a hospital's timezone.
