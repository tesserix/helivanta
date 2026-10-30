# Hospital billing

[Contract index](../README.md) · implemented: admission accounts (slice 1), bed-day charges (slice 2), interim
bills (slice 3), final bills (slice 4), cash counters (#198 slice 1), deposits and their refunds (#199 slices 1–2).

The `billing` module owns the hospital's ledger (#193). Other modules post to it through
published events; none writes its tables. Hospital invoices, payer tariffs, GST and discounts are
not built yet (#193, #197, #199, #200, #1045).

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
- **The census** runs each minute: it counts midnights for open stays that are due, batch after
  batch within a pass, and applies new setup to the admissions it affects once the setup is two
  minutes old (so a consumer already in flight is covered), one admission per transaction. A
  stay whose reconcile fails is retried later, doubling to an hour, so it never holds the queue.
  Its only cross-tenant read is the keys of due work; it does the work under each tenant's row
  security as `system:billing-bed-census`.
- **Order of events.** Every consumer takes the admission's lock first, so they and the census
  never deadlock. Two stays open at once (a transfer whose old bed's end is still on its way)
  charge each midnight once, to the stay started later. An account opens at its earliest stay,
  whichever start arrives first.
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

## Interim bills (#197 slice 3)

- **A statement, not a tax invoice.** An interim bill freezes an open account as issued:
  every entry recorded on it, numbered within the admission (Interim bill 1, 2, 3 …, no gaps).
  Medicine lines name pharmacy's GST invoice or credit note; bed days are before tax.
- **Cumulative.** A bill lists entry ids, not a time cut; an entry not on the previous bill is
  marked `new`. An entry still being recorded as a bill is issued is on the next bill.
- **Frozen.** Bills and lines are append-only, and a bill's lines are written only by the
  transaction that issued it. Its totals derive from its lines and never change.
- **Unpriced bed days** are counted on the bill, never charged as zero.
- **Nothing new, no bill:** no entry the previous bill lacks and the same unpriced count is
  `409 nothing_new`, which also absorbs a repeated click. A released account is
  `409 account_closed`.

| Route                                                                   | Behaviour                                                                                     |
| ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `POST /billing/hospitals/:hospital/admissions/:admission/interim-bills` | `201` with the bill; `404` without grant or account; `409 nothing_new`, `409 account_closed`. |
| `GET …/admissions/:admission/interim-bills`                             | A page of bills, newest first, without totals.                                                |
| `GET …/interim-bills/:number`                                           | One bill: issuer, time, totals and per-source totals, line and new counts, unpriced days.     |
| `GET …/interim-bills/:number/lines`                                     | A page of the bill's entries, newest first, each with `new`.                                  |

- **Issuing** needs `billing.bill.issue` (receptionist) and the hospital's billing grant;
  **reading** needs `billing.account.read` and the grant.
- **Not covered:** scheduled interim bills; deposits and payments on the bill (#199); a print layout; GST
  (#193); discounts (#200); an `InterimBillIssued` event, which nothing would consume yet.

## Final bills (#197 slice 4)

Design: [spec](../../superpowers/specs/2026-10-30-final-bills-design.md).

- **Discharge orders** from MediCore's `discharge_ordered.v1` and `discharge_withdrawn.v1`
  ([beds and admissions](beds-and-admissions.md)) are recorded in either order, each once. An
  order is active until withdrawn; the account shows `discharge_ordered`.
- **One series:** interim and final bills share the admission's numbers. Interim needs an open
  account without an active order (`409 discharge_ordered`, `409 account_closed`); final needs
  an active order or a released account (`409 discharge_not_ordered`), bed charging set up and
  every bed day priced (`409 bed_days_not_priced`), and every bed day counted
  (`409 bed_days_pending`): setup applied by the census, every stay's start received, no open
  stay past an uncounted midnight, and after release every stay ended. The database holds the
  same rules.
- **Reissued, never edited:** anything after a final bill goes on a new one; nothing new is
  `409 nothing_new`, but a first final bill after interim ones is always issued. A withdrawn
  discharge returns the account to interim bills.

| Route                                                                 | Behaviour                                                       |
| --------------------------------------------------------------------- | --------------------------------------------------------------- |
| `POST /billing/hospitals/:hospital/admissions/:admission/final-bills` | `201` with the bill; the `409`s above; `404` without the grant. |
| `GET …/admissions/:admission/bills`                                   | Bills of both kinds, newest first, paged, each with `kind`.     |
| `GET …/bills/:number`, `GET …/bills/:number/lines`                    | One bill and its lines, either kind.                            |

- **Issuing** needs `billing.bill.issue` and the grant; **reading** `billing.account.read` and
  the grant. The interim routes keep answering interim bills only.
- **Not covered:** deposits and payments on the bill (#199), discharge clearances (#1043),
  procedure bundles (#196), discounts (#200), GST (#193), a `FinalBillReady` event, and the
  final bill of a released patient from the ward board (the API serves it).

## Cash counters (#198 slice 1)

Design: [spec](../../superpowers/specs/2026-10-30-cash-counters-design.md).

- **Counters** per hospital, set up once by a tenant administrator (`billing.counter.manage`).
- **Shifts:** a cashier (`billing.cash.collect`, receptionist) opens one on a counter with an
  opening float; one open shift per counter and per cashier (`409 shift_open`). The close is
  blind: the cashier declares the cash counted, and the expected cash (float plus standing
  receipts) is computed then. A difference leaves the shift `pending_authorisation` until a
  tenant administrator other than the cashier (`billing.shift.authorise`) authorises it with a
  reason (`409 own_variance` for the cashier).
- **Receipts:** cash in INR against an admission's account, in the cashier's open shift
  (`409 no_open_shift`), never above the balance due (`409 over_balance`: an advance is a
  deposit). Numbered per counter, `<code>-<n>`. Voided with a reason, once, by the cashier,
  only while the shift is open (`409 shift_closed`); never edited.
- **The account** totals add `paid_minor` and `balance_minor`; a negative balance is credit.
- **The database** holds the same rules: one open shift, receipts in their cashier's open
  shift, numbering, the balance under the admission lock, the computed expected cash,
  authorisation by someone else, void only while open.

| Route                                                                     | Behaviour                                            |
| ------------------------------------------------------------------------- | ---------------------------------------------------- |
| `PUT /billing/hospitals/:hospital/counters/:counter` `{code,name}`        | Set up once; the same again replays.                 |
| `GET …/counters`                                                          | A page of counters.                                  |
| `PUT …/counters/:counter/shifts/:shift` `{opening_float_minor}`           | Open the caller's shift: `201`; a retry `200`.       |
| `GET …/cash-shifts/mine`                                                  | The caller's shift not yet closed, or `404`.         |
| `PUT …/cash-shifts/:shift/close` `{declared_minor}`                       | Blind close; the answer shows expected and variance. |
| `PUT …/cash-shifts/:shift/authorisation` `{reason}`                       | Authorise a variance; `billing.shift.authorise`.     |
| `GET …/cash-shifts`                                                       | A page of shifts, for the supervisor.                |
| `PUT …/admissions/:admission/receipts/:receipt` `{amount_minor,currency}` | Take cash: `201`; a retry `200`.                     |
| `GET …/admissions/:admission/receipts`                                    | The admission's receipts, voided ones included.      |
| `PUT …/receipts/:receipt/voids/:void` `{reason}`                          | Void in the open shift.                              |

- **Not covered:** UPI, card and other modes; handover; denominations; pickups; GL export;
  events; receipts on bills. Refunds, deposits and the daily collection report came later,
  below.
  A cashier whose billing grant is revoked mid-shift can no longer see the shift to close it;
  it keeps the counter until the grant is restored and the shift closed.

## Deposits (#199 slice 1)

Design: [spec](../../superpowers/specs/2026-10-30-deposits-design.md).

- **Taken at the counter** as a receipt with `purpose: deposit`, in the cashier's open shift, on
  an open account (`409 account_closed` after release). No cap: an advance is money beyond what
  is owed. Numbered with the counter's receipts and counted in the shift's expected cash; voided
  like any receipt while the shift is open, but never below what was applied
  (`409 deposit_applied`).
- **Held for the patient, not revenue.** A held deposit does not reduce the balance due.
- **Applied against the final bill** by the billing desk (`billing.bill.issue`) once one is
  issued (`409 no_final_bill`): the server applies the lesser of what is held and the balance
  due, recorded with that final bill's number (`409 nothing_to_apply` when either is zero).
- **Payments** stay capped at the balance due, now net of applied deposits.
- **Totals** per currency add `deposit_held_minor` and `applied_minor`; balance is outstanding
  less paid less applied. Every bill issued from now on records, set by the database, the INR
  `paid_minor` (payments and applied deposits) and `deposit_held_minor` at issue; a final bill
  shows what is left to pay or to refund.

| Route                                                                      | Behaviour                                   |
| -------------------------------------------------------------------------- | ------------------------------------------- |
| `PUT /billing/hospitals/:hospital/admissions/:admission/deposits/:receipt` | Take a deposit: `201`; a retry `200`.       |
| `PUT …/admissions/:admission/deposit-applications/:application`            | Apply held deposits against the final bill. |
| `GET …/admissions/:admission/deposit-applications`                         | A page of applications, newest first.       |

- **Not covered:** top-up alerts; deposit policies by bed class or
  procedure; required admission deposits; transfer between admissions; OPD deposits; events;
  GL liability posting.

## Deposit refunds (#199 slice 2)

Design: [spec](../../superpowers/specs/2026-10-30-deposit-refunds-design.md).

- **Threshold per hospital,** set by a tenant administrator (`billing.counter.manage`) as a new
  version each time; the latest applies; none set is ₹10,000.
- **Approval** above the threshold: a supervisor (`billing.shift.authorise`) approves one amount
  for one admission with a reason, within what is held (`409 over_held`). Never the cashier who
  pays it (`409 own_approval`).
- **Paying out** is a receipt with `purpose: refund` from the cashier's open shift, numbered
  with the counter's receipts: after a final bill (`409 no_final_bill`), at most the surplus held beyond what
  the bill still needs (`409 over_held`), at most the drawer's cash (`409 drawer_short`), and above the threshold only
  against an unused approval for exactly that amount (`409 approval_required`). It reduces the
  shift's expected cash; voided like any receipt while the shift is open (its approval stays
  used). Voiding a payment or deposit is refused if the drawer no longer holds that cash
  (`409 drawer_short`).
- **Totals** add `refunded_minor`; held is deposits less applied less refunded. A deposit can no
  longer be voided below what was applied or refunded.
- **The database** holds every rule: final bill, held, drawer, threshold and approval (same
  admission and amount, unused, someone else), and the drawer netting refunds.

| Route                                                                                 | Behaviour                                     |
| ------------------------------------------------------------------------------------- | --------------------------------------------- |
| `GET /billing/hospitals/:hospital/refund-policy`                                      | The threshold in force, or the default.       |
| `PUT …/refund-policies/:policy` `{threshold_minor}`                                   | Set a new threshold version.                  |
| `PUT …/admissions/:admission/refund-approvals/:approval` `{amount_minor,reason}`      | Approve a refund amount.                      |
| `GET …/admissions/:admission/refund-approvals`                                        | Approvals, each with the refund that used it. |
| `PUT …/admissions/:admission/refunds/:receipt` `{amount_minor,currency,approval_id?}` | Pay a refund from the open shift.             |

- **Not covered:** refunds by UPI or bank transfer; un-applying deposits (a credit after an
  application stays unrefundable); refunds of payments; ward refund requests; events; GL.

## Billing desk

Design: [spec](../../superpowers/specs/2026-10-30-billing-desk-design.md).

- `GET /billing/hospitals/:hospital/accounts?view=` lists the hospital's admission accounts,
  newest first, paged, each with `status`, `discharge_ordered`, and INR `balance_minor` (below
  zero is credit), `deposit_held_minor`, `refundable_minor` (the surplus over the bill, once a
  final bill exists) and `has_final_bill`. Views: `to_collect`, `to_refund`,
  `discharge_ordered`, `open`, `released`, or none for all; another is `400`.
- Each account's position is stored and recomputed by the database on every write that changes
  it; each view reads its own partial index. Accounts from before migration `0008_billing`
  are filled in by the billing worker. Figures are INR.
- Needs `billing.account.read` and the hospital's billing grant (`404` without it).
- The shell's **Billing** zone (`billing.account.read`) shows it with the cash desk; each
  account opens its bills, payments, deposits and refunds, including after release.

## Daily collection (#198 slice 2)

Design: [spec](../../superpowers/specs/2026-10-30-daily-collection-design.md).

- `GET /billing/hospitals/:hospital/collections/:date` reads one hospital-local day: every shift
  that opened between that date's midnights in the hospital's bed-charging timezone, with
  counter, cashier, status, float, standing payments, deposits and refunds, voided receipts (refunds voided apart),
  expected, declared, variance and its authorisation. A shift belongs to the day it opened.
- **Open shifts stay blind:** listed without any money figure, since an administrator may also be
  the cashier. Totals cover counted shifts (closed or waiting): payments, deposits, refunds, net
  cash (payments + deposits − refunds), shortages and excesses apart, and counts of open and
  waiting shifts. `final` is true only for a past date with every shift closed.
- `GET …/collections/:date/export.csv` is the same day as CSV: a row per shift and a `TOTAL`
  row, rupees with two decimals, text cells made inert to spreadsheet formulas, no patient or
  admission identifiers. Each export is audit-logged (`billing.collection.exported`) with its
  row count and SHA-256, also sent as `X-Export-Row-Count` and `X-Export-Sha256`.
- Needs `billing.collection.read` (tenant administrators only) and the hospital's billing grant
  (`404` without it). `409 timezone_unset` with no timezone; `400` for a malformed date or one
  after the hospital's today; `413 collection_too_large` above 500 shifts.
- The shell's Billing page shows it as **Collections** to holders of the permission.
- **Not covered:** finance sign-off and reopening a day; GL posting; other payment modes;
  per-cashier variance trends; date ranges; events.

## GST on rooms and the tax invoice (#193 slice 1)

Design: [spec](../../superpowers/specs/2026-10-31-gst-room-invoices-design.md). Finance or tax
review of the policy is required before production use.

- **The rule:**
  - Health care by a clinical establishment is exempt (12/2017-CT(Rate) s.74).
  - The exception is a room other than ICU, CCU, ICCU or NICU charged above ₹5,000 a day
    (proviso by 04/2022-CT(Rate)).
  - Such a room is taxed at 2.5% CGST + 2.5% SGST under SAC 9993 (03/2022-CT(Rate) entry 31A),
    from 2022-07-18.
- **The policy** is the versioned SQL function `billing_gst_room_policy(date)`, version
  `IN-ROOM-2022-07-18`. It exempts the `icu` and `nicu` classes and taxes the whole charge when
  the day's rate is above 500000 paise. An amendment is a new version in a migration.
- **Registration:**
  - `PUT /billing/hospitals/:hospital/gst-registration` `{gstin,legal_name,address,effective_from}`
    needs `billing.tax.manage` (tenant administrators only) and is set once.
  - The GSTIN's state, PAN pattern and check character are validated (`400`).
  - The effective date is the hospital's tomorrow or later, so no settled bill gains tax (`409 registration_backdated`), and the
    hospital's timezone is required (`409 not_configured`).
  - A registered hospital's bed rates are INR (`409 rates_not_inr`).
  - `GET` returns the registration, with `applied_at` once the census has applied it.
- **Tax as it accrues:**
  - The census records a GST line for each priced, live, taxable day from the registration on.
    The database computes it: per day, half up in paise, CGST and SGST each.
  - The tax is posted as a `gst` charge. When the day is voided, its tax is reversed with it.
  - Balances, receipts, deposits, refunds, positions and bills include the tax with no other
    change. A final bill waits until a new registration is applied.
- **Tax invoices:**
  - `PUT …/admissions/:admission/tax-invoices/:invoice` `{recipient_name,recipient_address}`
    (`billing.bill.issue`) issues an invoice-cum-bill of supply (CGST Rules 46, 46A) once a final
    bill exists. A retry replays.
  - It carries every standing room charge dated from the registration that is on no invoice
    yet, taxable or exempt, as snapshot lines. Pharmacy charges are not on it, because pharmacy
    issues its own GST invoice.
  - It is numbered `IP/<yyyy>/<n>` per GSTIN and financial year (April to March, by the
    hospital's date), consecutively, under a lock. The database sets the supplier, place of supply
    (the GSTIN's state), date and number.
  - `GET …/tax-invoices`, `GET …/tax-invoices/:invoice` (with totals: value, taxable, exempt, CGST,
    SGST, total) and `GET …/tax-invoices/:invoice/lines` read them.
- **Refusals:**
  - `409 gst_unregistered`, `409 no_final_bill`, `409 nothing_to_invoice`;
  - `409 credit_note_required` once an invoiced room charge is reversed (credit notes are not
    built yet).
- **The shell:** the Billing page has a GST panel for administrators. Each admission's bills list
  its tax invoices, with the desk's issue form after a final bill.
- **Not covered:**
  - credit and debit notes;
  - IRN or e-invoicing, QR codes and PDFs;
  - IGST and B2B recipients;
  - GST on other hospital services;
  - changing or backdating a registration;
  - returns and exports.
