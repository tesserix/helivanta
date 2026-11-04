# CSV downloads of three pharmacy sales reports (#1060)

Issue: #1060 (its export of reports; the issue stays open for margin, indents,
wholesale, monthly aggregation and the exports below). Builds on the #1060
reports in `sales_reports.go` and the bounded CSV exports already in the
module: the stock register (#1232, [ADR-0034](../../adr/0034-pharmacy-stock-export.md))
and the prescription register.

## Why

The legacy MIS reports all download, and finance staff reconcile in a
spreadsheet. Today sales by item, the receipts and refunds register and sales
and stock can only be read a page at a time, so a month's figures are copied
by hand.

## Decisions (agreed 2026-11-05)

1. **Three reports, the ones with no personal data:** sales by item
   (`GET …/reports/items.csv`), receipts and refunds
   (`GET …/reports/receipts.csv`) and sales and stock
   (`GET …/reports/stock-sales.csv`). Each needs the same permission as its
   JSON report (`pharmacy.sale.read`, `pharmacy.sale.read`,
   `pharmacy.stock.read`) and the same live outlet grant.
2. **Same query, same filters.** Each file is produced by the JSON report's
   own query in one transaction, so a file always matches its pages. It takes
   the report's own parameters: `from` and `to` always; sales by item
   `product_id`, `category_id`, `subcategory_id` and `lot_number`; receipts
   `kind`; sales and stock `category_id` and `subcategory_id`. Any other
   parameter, a repeated one or an empty one is 400, as for the stock export.
   That includes `cursor`, `limit` and **the prescriber filter**: sales by a
   prescriber are personal and their export needs a disclosure audit (later
   slice), so the file refuses them rather than writing them unaudited.
3. **All or an error, never a cut-off file.** At most 5,000 rows and 8 MiB;
   more is `413 report_export_too_large`, before any byte of the file, and
   the screen asks for a shorter period or a narrower filter. 5,000 rather
   than the stock export's 1,000 because these reports already sum the whole
   period before their keyset on every page; the bound limits the file, not
   the work. A database failure or the 8 s statement timeout is an error with
   no file. An empty result is a file with its header only.
4. **Format.** UTF-8 with a byte-order mark, so Excel reads non-ASCII names;
   CRLF; one header row of snake_case names. Text a spreadsheet would run as a
   formula is prefixed with an apostrophe (`pkg/csvsafe`). Every row carries
   the period (`period_from`, `period_to`), and sales and stock its stock date
   (`stock_on`, the outlet's today). Units are integers.
5. **Amounts in the currency's major unit, exactly**: `1234.50`, never a
   float, with the currency beside them. The number of minor digits comes from
   the country profile (`contracts/country-profiles.json`,
   `currency_minor_digits`), generated into `pkg/countryprofile`. A currency
   no profile declares is a 500 and no file: guessing two digits could
   misstate an amount a hundredfold.
6. **Audit.** These reports hold no patient, prescriber or buyer, and their
   JSON reads are not audited, so a download writes no `audit_events` row. It
   is recorded in the structured audit stream, as the stock export is:
   `pharmacy.report.exported` with the report, the actor, the outlet, the
   filters, the row count and the SHA-256 of the file, only after the file is
   complete. A refused or failed download records nothing.
7. **Response.** `text/csv; charset=utf-8`, `Cache-Control: private,
   no-store`, `nosniff`, a server-made name
   `pharmacy-<report>-<outlet>-<from>-<to>.csv`, and `X-Export-Row-Count` and
   `X-Export-Sha256`.
8. **Screens.** Each of the three reports gets a "Download CSV" button for
   what it shows (its period and filters). Changing a filter replaces the
   button, which cancels a download in flight, so a stale file never lands.
   While sales by item is filtered by prescriber the button is replaced by a
   note that such a download is not available yet.

## Not covered

- Downloads of patient-wise sales, the sales worklist, sales by prescriber
  and the daily report: they need a disclosure audit (`report_export` rows,
  as the prescription register's) or their own file shape. Next slice.
- Files above 5,000 rows, scheduled or emailed exports, XLSX or PDF, and the
  governed export service with approval and expiry (#594, #64, #444).
- A byte-order mark on the existing stock and register exports.
