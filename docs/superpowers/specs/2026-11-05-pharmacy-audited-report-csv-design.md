# Audited CSV downloads of the personal pharmacy reports (#1060)

Issue: #1060 (report exports; the issue stays open for margin, indents,
wholesale and monthly aggregation). Second slice after
[CSV downloads of three sales reports](2026-11-05-pharmacy-report-csv-design.md),
whose route shape, bounds, format and money rules this keeps. Audit rules
are #1555's ([audited reads](../../api/domains/pharmacy.md#audited-reads-1555)).

## Why

Slice 1 left out the reports that name or point to a person, because a
file of them is a disclosure: once downloaded, nothing the platform does
can restrict it. The legacy MIS downloads them, and finance and the
pharmacist need them (a patient's purchases for an insurer, a prescriber's
sales, the day's invoices for a ledger).

## Decisions (agreed 2026-11-05)

1. **Four more files**, each needing its report's permission:
   - patient-wise sales, `GET …/reports/patient-sales.csv?patient_id=&from=&to=`
     (`pharmacy.sale.read`);
   - sales by item **with the prescriber filter**, `GET …/reports/items.csv`
     now taking `prescriber_registration` and `prescriber_council`
     (`pharmacy.sale.read` and `pharmacy.prescription.read`, as its pages);
   - the sales worklist, `GET …/sales/worklist.csv?from=&to=&customer=&linkage=`
     (`pharmacy.sale.read`). It needs a period: one invoice is the sale
     summary, not a file, so `invoice` is refused;
   - the daily report, `GET …/reports/daily.csv?from=&to=`
     (`pharmacy.sale.read`), one row per outlet day and currency.
2. **A personal file is audited as a disclosure, in the read's
   transaction**: an `audit_events` row with the report's existing
   `resource_type` and the action `report_export` (`list_export` for the
   worklist), field names only, the values in the audit log. The row is
   written only after the whole file is built, so a refused file (too
   large) records nothing, and a failed audit write fails the download with
   no file. Which files are personal follows the screens' audit rules
   exactly:
   - patient-wise sales: always;
   - sales by item: when filtered by prescriber;
   - the worklist: when the file holds an admission sale (its bed code
     points to the occupant), checked over every row of the file, not a
     page.
   The structured audit log line `pharmacy.report.exported` is written for
   every file sent, as in slice 1.
3. **The worklist file carries what its screen carries**: invoice, dates,
   `customer` and `linkage` as kinds, the bed code of an admission sale,
   and the amounts. The worklist holds no buyer name or phone and no
   patient id, so neither does the file; adding them is not this slice.
4. **The daily report file** has every field of its JSON row, amounts in
   the major unit; it holds totals only and is not audited, like its page.
5. **Bounds, format and response as slice 1**: at most 5,000 rows and
   8 MiB, `413 report_export_too_large` otherwise, byte-order mark, CRLF,
   formula-safe text, exact amounts by `currency_minor_digits`, `no-store`
   and a server-made name. Only each report's own parameters.
6. **Screens.** Patient-wise sales, the worklist and the daily report get
   the download button; sales by item keeps its button when filtered by
   prescriber, replacing slice 1's note. Where the file is recorded, the
   screen says so beside the button.

## Not covered

- Buyer names, phone numbers or patient ids in the worklist file.
- The sale summary and credit notes as files, and printable invoices.
- Files above 5,000 rows; XLSX, PDF, scheduled exports and the governed
  export service with approval and expiry (#594, #64, #444).
