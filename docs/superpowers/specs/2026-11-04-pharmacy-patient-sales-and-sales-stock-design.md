# Pharmacy patient-wise sales and the sales stock report (#1060)

Issue: #1060 (one slice; the issue stays open). Builds on the sales by item
and receipts slice (#1548), registered-patient sales (#1386 B-2c), admission
sales (#1387) and the stock ledger.

## Why

A pharmacist needs to answer "what has this patient bought from us, from
which batch, and what came back?" (legacy PATIENTWISE SALES, 442): for a
recall, a query from the ward, or a patient's own request. An outlet also
needs each product's sales next to its stock, so slow movers and items about
to run out show on one page (legacy SALES STOCK REPORT, 755).

## Decisions (my calls; the issue fixes none of them)

1. **Reads of posted records, never a copy**, as for sales by item. Sales,
   their lines, credit-note lines and stock movements are insert-only.
2. **Patient-wise sales is one registered patient at a time.**
   `GET …/reports/patient-sales?patient_id=&from=&to=` requires `patient_id`.
   The screen finds the patient through the existing audited MRN lookup
   (`CounterPatientLookup`, as the prescription register does), so a
   pharmacist never sees a list of every patient's purchases. The pharmacy
   holds only the patient's id (patientmaster owns name and MRN), so a
   period-wide list across patients would be ids nobody can read and a bulk
   disclosure nobody needs.
3. **What a patient-wise row is.** One row per sale line of a sale naming
   that registered patient, with `sale_date` in the period: date, invoice,
   line, the admission's bed code when the sale was to an admission, product
   (SKU, brand, generic name, pack size, unit of issue), batch lot and
   expiry, units, MRP, discount, amount charged, and the units returned
   against that line on credit notes dated up to `to`. Walk-in buyers (a name
   and phone typed at the counter) are not patients and are not listed.
4. **Patient-wise reads are audited.** Every page writes an `audit_events`
   row and an audit log line through `auditRegisterRead`
   (`pharmacy_patient_sales`, `report_read`, with the period and patient id),
   in the read's transaction: no audit row, no data. Permission is
   `pharmacy.sale.read` with the outlet grant: it already shows every sale
   with its patient.
5. **The sales stock report.** `GET …/reports/stock-sales?from=&to=` gives one
   row per product of the outlet that has ever held stock (a sale takes
   stock, so this is every product sold too): SKU, brand, generic name, pack size, unit of issue; units
   sold and returned in the period (sale and credit-note dates, so they equal
   sales by item); units on hand now, in total and on sellable shelves (the
   sum of the product's stock movements). Permission is `pharmacy.stock.read`
   with the outlet grant: it already lists the sale and return movements.
   Not audited: no patient, prescriber, buyer or money.
6. **Bounds.** A period is at most 366 outlet days (`outletPeriod`). Both are
   keyset-paged, oldest first.

## Not covered

- Purchase amount (442) and the last vendor (755): purchase cost waits for
  the approved cost and margin formulas, and a receipt records no supplier.
- Walk-in buyers' purchases, and a list across patients.
- Stock as of a past date (the stock register covers that), and batch-level
  stock in the sales stock report.
- CSV export of either report.
