# Daily collection report (#198 slice 2)

Resolves part of #198 (cashier management). Builds on cash counters
(`2026-10-30-cash-counters-design.md`), deposits and deposit refunds. Approved in
chat on 2026-10-30.

## Why

Finance signs off a hospital's cash day by day: which shifts ran, what each took
by purpose, and whether each drawer matched. Today the only view is the
supervisor's list of shifts, which carries no totals and no day boundary, so
the sign-off is a spreadsheet. This slice is a read-only report built from
billing's own records, and a CSV of the same figures for the ledger.

## What it shows

`GET /v1/billing/hospitals/:hospital/collections/:date` (and
`…/collections/:date/export.csv`) for one hospital-local date:

- **Each shift that opened that day**: counter code and name, cashier, status,
  opened and closed times, opening float, payments, deposits and refunds that
  stand, voided receipts (count and amount), expected cash, declared cash,
  variance, and who authorised the variance and why.
- **Day totals** over shifts that have been counted (closed, or waiting for
  authorisation): payments, deposits, refunds, net cash (payments + deposits −
  refunds), shortages and excesses kept apart so they never net to zero, and
  how many shifts are still open or waiting.
- **`final`**: true only for a past date whose shifts are all closed.

## Decisions (approved)

1. **A shift belongs to the hospital-local day it opened.** Bounds are the
   date's midnights in the hospital's timezone (the one set for bed charging),
   computed by Postgres, so a DST day is 23 or 25 hours. A night shift counts
   on the day it started, so a past day's set of shifts never changes. With no
   timezone set the report is `409 timezone_unset`, never a server or IST
   default. A date after the hospital's today is `400`.
2. **Not final until counted.** Open and waiting shifts are listed and counted
   as not reconciled; the day is final only when it is past and every shift is
   closed.
3. **Administrators only**, `billing.collection.read`, granted to no role:
   there is no finance-controller role on the hospital side. The caller also
   needs the hospital's billing grant, as for every billing read (404 without).
4. **Server-built CSV** from the same rows as the JSON: one row per shift and a
   `TOTAL` row; amounts in rupees with two decimals; every text cell through
   `csvsafe.Text`. Each export is audit-logged with row count and SHA-256 and
   returned with `X-Export-Row-Count` / `X-Export-Sha256`. No patient or
   admission identifiers: per-shift totals only.
5. **UI**: a Collections panel on the Billing page, shown to holders of the
   permission, with a date field, the table, totals and Download CSV.

## Calls I made (builder's)

- **An open shift's money stays blind.** An administrator can also be a
  cashier, and the report must not hand the expected figure to a cashier whose
  shift is still open. So an open shift is listed with its counter, cashier and
  opening time only; no float, purpose totals, expected or variance. A shift
  waiting for authorisation has been counted, so its figures and variance show.
  Day totals cover counted shifts only, which `final` makes explicit.
- **Bounded**: at most 500 shifts in a day; more is
  `413 collection_too_large`, not a truncated report.
- **Voided refunds apart** from voided payments and deposits: a voided refund
  is cash that never left the drawer, not money taken back.
- **Finality window:** `opened_at` is the insert time, so a shift opened in the
  last instant before midnight and committed after a read can appear on a day
  just reported final. The window is milliseconds; a later sign-off slice
  closes it by recording the day.
- **Read in one transaction** under the billing grant, so totals and rows
  agree.

## Rules

- Read-only; no table, trigger or migration change. Uses the existing
  `billing_cash_shifts_page` index by `(tenant, hospital, opened_at)`.
- Cross-tenant and ungranted hospitals answer 404; members without the
  permission 403.

## Not covered

Finance sign-off of a day and reopening; GL posting; card and other modes
(UPI was added beside the cash columns by `2026-11-01-billing-upi-design.md`); per-cashier and per-authoriser variance trends;
date ranges; events; handover (#198 later slices); denominations and pickups since added
(`2026-11-02-cash-pickups-design.md`).
