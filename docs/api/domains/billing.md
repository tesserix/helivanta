# Hospital billing

[Contract index](../README.md) · implemented: admission accounts (slice 1) and bed-day charges (slice 2).

The `billing` module owns the hospital's ledger (#193). Other modules post to it through
published events; none writes its tables. Hospital invoices, payer tariffs, GST, interim and final
bills, deposits and discounts are not built yet (#193, #197, #199, #200, #1045).

## Admission accounts (#197 slice 1)

- **One account per admission**, opened by whichever event names the admission first:
  `medicore.bed_occupancy_started.v1`, or a pharmacy charge. It closes on
  `medicore.admission_released.v1`. An account keeps its hospital and patient; an event naming
  the admission with another patient is refused.
- **Entries are append-only:** `charge` or `reversal`, keyed on (source module, source
  reference, kind), so a replay never posts twice, and a replay that differs is refused. A
  reversal names a recorded charge of the same admission and currency, and never exceeds it,
  however many arrive at once: the database takes them one at a time per charge.
- **A reversal that arrives before its charge is parked** in `billing_parked_reversals`, not
  refused (the bus would dead-letter it), and applied when the charge is recorded. Charge,
  parking and applying are serialised per charge.
- **A late charge**, after release, is still recorded. The account stays closed.
- **First source:** pharmacy's `admission_charge_posted.v1` and `admission_charge_reversed.v1`
  ([pharmacy](pharmacy.md#ward-medicine-billed-to-the-admission-1387)). The pharmacy GST
  invoice remains the tax document; the entry records that the patient owes the hospital that
  amount against it, so revenue is counted once.

| Route                                                                           | Behaviour                                                                              |
| ------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `GET /billing/hospitals/:hospital/admissions/:admission/account`                | Status, open and close times, and totals per currency: charged, reversed, outstanding. |
| `GET …/admissions/:admission/entries`                                           | A page of entries, newest first.                                                       |
| `PUT` and `DELETE /billing/hospitals/:hospital/access/:subject`; `GET …/access` | Billing grants.                                                                        |

- **Reading** needs `billing.account.read` (nurse, receptionist) plus a billing grant at the
  hospital, locked through the request. Without the grant the answer is `404`.
- **Grants** need `billing.access.manage`, which only tenant administrators hold.
- **Row security** shows a reader only the hospitals they hold a grant for. Only billing's named
  consumers write.

## Bed-day charges (#197 slice 2)

Design: [spec](../../superpowers/specs/2026-10-30-bed-day-charges-design.md).

- **Midnight census.** A bed day is charged for each midnight, in the hospital's timezone, that
  a patient spends in a bed (`started_at <= midnight < ended_at`), at that bed's class from
  `medicore.bed_occupancy_started.v1`. A transfer at midnight exactly counts the new bed. A
  stay that crosses no midnight is charged one _minimum day_, at its first bed's class, once
  the admission is released. Open stays are counted 15 minutes after midnight.
- **Days derive from stays.** Billing keeps its own record of each stay from the start and end
  events, in either order, and converges the admission's days to what the stays imply, under a
  per-admission lock: missing days are recorded, days no longer implied are voided, and a
  voided priced day is reversed on the account. Nothing is deleted or edited.
- **Not priced is not zero.** A day with no rate in force on its date is recorded without an
  amount and counted as not priced. A rate set later prices it; a priced day is never
  repriced. Bed charges are before tax: GST on room rent is #193.
- **Setup.** A hospital's timezone is set once (it cannot change: days already charged were
  counted by it); until it is, no day is computed and the account says bed charging is not set
  up, and setting it computes every day since admission. Rates are one per class per date,
  fixed once set; a correction is a rate from a later date.
- **The census** runs each minute: it counts midnights for open stays that are due and applies
  new setup to the stays it affects. Its only cross-tenant read is the keys of due work; it
  does the work under each tenant's row security as `system:billing-bed-census`.
- **Entries:** source `bed`, reference the day, document `Bed day YYYY-MM-DD`. The account
  answer adds `sources` (outstanding per source and currency) and `bed_days`
  (`configured`, `priced`, `not_priced`).

| Route                                                                   | Behaviour                                                                                   |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `PUT /billing/hospitals/:hospital/bed-charging` `{timezone}`            | Set once: the same again `200`, another `409 already_set`, an unknown zone `400`.           |
| `GET …/bed-charging`                                                    | The timezone, or `404 not_configured`.                                                      |
| `PUT …/bed-rates/:class/:effective_from` `{daily_rate_minor, currency}` | Needs the timezone (`409 not_configured`); the same again `200`, another `409 already_set`. |
| `GET …/bed-rates`                                                       | A page of rates, newest first.                                                              |

- **Setting** needs `billing.rate.manage` (tenant administrators only) and the hospital's billing
  grant; **reading** needs `billing.account.read` and the grant, as for accounts.
- **Not covered:** GST on room rent; nursing and ward-service components; consultant visits;
  repricing a stay on upgrade; payer-specific rates (#1045); correcting a priced day (#200);
  changing a hospital's timezone; stays begun before this release, whose start carries no bed
  class.
