# Pharmacy report walkthrough and screen states (#1060)

Issue: #1060 (the issue stays open for margin, indents, wholesale and
monthly aggregation). This slice covers two of its acceptance items that
need no business decision:

- "Given empty results, failed loads, invalid filters or restricted access,
  the UI shows explicit accessible states without stale records."
- "A synthetic walkthrough covers every enumerated capability; intentional
  departures have documented clinical/operational rationale."

It builds no new report. It proves the built ones in the running app and
fixes what the proof finds.

## Why

Every report has API tests and component tests. Nothing yet shows, in a
real browser against the real API, that a pharmacist can post sales and
read back the same numbers on every report and in every file. Reading the
screens also turns up two suspected defects (below). The walkthrough proves
each one by failing first, before it is fixed.

## What exists (checked against source on 2026-11-05)

- **Daily sales** (`/pharmacy/daily-report`, `daily-report.tsx`) holds:
  - daily totals;
  - sales by item, with category, lot and prescriber filters;
  - receipts and refunds;
  - patient-wise sales;
  - sales and stock, shown only with `pharmacy.stock.read`.

  The page and each report have a CSV download.
- **Sales** (`/pharmacy/sales`, `sales-worklist.tsx`) holds the worklist,
  its filters, the sale statement pane and the worklist download.
- **Each section shows its own state.** In order of precedence: a failure
  (`RequestFailure`, `role="alert"`, "Try again"), then loading
  (`role="status"`), then an empty sentence, then rows.
- **Queries keep no placeholder data.** A changed filter therefore shows
  "Loading…", never the last filter's rows.
- **Periods are checked in the browser** (`periodSchema`): both days are
  needed, the last day can't be before the first, and a period is at most
  366 days. The errors show inline.

### Suspected defects

These are to be proved by the walkthrough before they are fixed.

1. **A refusal looks like an outage.** A 403 or 404 shows "Could not load
   …" with "Try again". Trying again can't help, and the screen never says
   access is the reason. This happens, for example, to an outlet the user
   has no access to, or when an access grant is withdrawn while the page is
   open.
2. **Empty results aren't announced.** The sentence that replaces
   "Loading…" (role `status`) is a plain paragraph. A screen-reader user
   hears the report load, then nothing.

## Decisions

1. **One walkthrough spec,** `e2e/tests/pharmacy-reports.spec.ts`, in the
   "specs" project. It gets its own seeded admin and pharmacist accounts,
   which `seed-dev.mjs` derives from the filename.
   - **Data.** It builds a fresh outlet through owner commands only: no
     table or projection writes, the same as `readyCounter`. The outlet
     setup shared with `readyCounter` moves into a helper both use. The
     payment-account and UPI parts stay in `readyCounter`.
   - **Sales.** The pharmacist posts, through the counter screen:
     - a cash sale to a walk-in buyer;
     - a sale to a registered patient, who is registered the way
       `clinicalFixture` (`support/opd.ts`) registers one;
     - a return of one unit from the walk-in sale.
2. **Numbers are checked against what was posted.** The expected amounts
   come from the posted sales and the return, not from re-reading the API's
   totals.
   - **Daily totals:** invoice and credit-note counts and amounts, net, and
     taken and refunded by kind.
   - **Sales by item:** units sold and returned, and the amount. Also the
     category and lot filters, each narrowing to the one product or lot.
   - **Receipts and refunds:** one receipt per sale and the refund.
   - **Patient-wise sales:** the patient's line only.
   - **Sales and stock:** units sold and the stock on hand.
   - **Worklist:** both sales, the walk-in filter and an invoice lookup.
     The statement pane shows the credit note.
   - **Every download:**
     - the server-made filename;
     - `X-Export-Row-Count` equal to the rows on screen;
     - the file's rows equal to the screen's, with amounts in rupees.
3. **States the walkthrough checks:**
   - **Empty:** a past period with no sales shows each report's empty
     sentence, announced (`role="status"`).
   - **Invalid filters:** the last day before the first, and a period over
     366 days. Each shows its inline message, and no report request is
     sent.
   - **Failed load:** the browser aborts one report's request with
     `page.route`.
     - Only that section shows its failure and "Try again"; the other
       sections keep their rows.
     - No row from the previous period remains.
     - "Try again" recovers the section once the route is released.
   - **Restricted access:**
     - **The owner role** has no prescription read, so it sees no
       prescriber filter.
     - **A role without stock read** sees no sales-and-stock section.
     - **An outlet whose access is withdrawn while the page is open**
       shows "You don't have access to this outlet's reports." No section
       offers "Try again", and no rows remain.
4. **Fix 1: refusals are their own state.**
   - `RequestFailure` gains an optional `error` prop. For an `ApiError` with
     status 401, 403 or 404, it shows the caller's `refused` sentence, with
     no retry button. Every other error keeps the message and "Try again".
   - Failing closed means a refusal never offers a retry. Retrying can't
     change an authorization answer, and offering one tells the user the
     data exists and failed to arrive.
   - The report sections pass `error` and a `refused` sentence. A Vitest
     test covers each mapping.
5. **Fix 2: empty sentences are announced.** Each report's empty paragraph
   becomes `role="status"`, as loading already is.
6. **Any other defect the walkthrough finds is fixed in this PR,** with its
   failing test first, and listed in the PR body.
7. **Departures are recorded.** The legacy reports in #1060 we don't build,
   or build differently, are listed in the pharmacy domain doc with their
   reason. That doc's not-covered lists already say most of this; this
   slice gathers them in one table against the legacy report numbers.

## Not covered

- **Admission sales and prescriber sales in the walkthrough.**
  - An admission sale needs a MediCore admission.
  - A prescription sale needs released prescription images, and #1477 is
    changing that.

  API tests cover both, including their audited files.
- **Other screens' `RequestFailure` call sites** (176 in total). Only the
  report sections pass `error` in this slice.
- **The H1 and prescription registers, and the stock register.** These
  belong to other issues.
- **Margin, monthly gross profit, wholesale, indents and the pharmacy
  summary report.** These wait on the decisions recorded in #1060, #1051
  and #987.
