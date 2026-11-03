# Pharmacy sales by item, and the receipts and refunds register (#1060)

Issue: #1060 (one slice; the issue stays open). Builds on the daily report
(#1386 C-3), credit notes (C-1), admission sales (#1387) and service charges
(#1388).

## Why

The daily report gives one line per day. An outlet also needs to know what it
sold, product by product (legacy ITEMWISE SALES, 441), and every payment taken
and refund paid, one at a time, so a till or a bank statement can be ticked off
against it (legacy SALES RECEIPTS AND REFUNDS, 450, and RECEIPT AND REFUND,
725).

## Decisions (my calls; the issue fixes none of them)

1. **Reads of posted records, never a copy.** Sales, credit notes, their lines,
   tenders and refunds are insert-only, so both reads are summed or listed
   afresh and cannot drift. Nothing is stored, cached or closed.
2. **A day is the outlet's own date**, as in the daily report: a sale counts on
   its `sale_date`, a credit note and its lines and refunds on its
   `credit_note_date`. A return taken this month of a sale made last month
   counts this month.
3. **Sales by item.** `GET …/reports/items?from=&to=&product_id=` gives one row
   per product with a sale line or a credit-note line in the period:
   - the product: SKU, brand, generic name, manufacturer, pack size, unit of
     issue, currency;
   - sold: units, amount charged (after discount), discount, taxable value,
     CGST, SGST, and the lowest and highest MRP per pack charged;
   - returned: units, amount credited, taxable value, CGST, SGST;
   - net: units and amount, sold less returned.

   A product's currency and pack size cannot change once it holds stock (the
   identity guard), so one row per product never mixes currencies or packs.
   The list is paged, oldest product first, so it is bounded whatever the
   catalogue's size. Service charges are not products and are not listed; the
   daily report shows them. So, for one period, the items' amounts plus the
   daily report's service charges equal the daily report's sales and credits:
   the tests prove it.

4. **The receipts and refunds register.**
   `GET …/reports/receipts?from=&to=&kind=` lists every tender on a sale
   (`receipt`) and every refund on a credit note (`refund`), oldest first, paged:
   date, time posted, document number (invoice or credit note), kind, the
   payment reference where there is one, and the amount. Cash is the amount
   applied to the sale; change is not income (as in the daily report).
   Admission-account tenders and refunds are listed with their kind, because
   they settle the document, but the UI marks them as not collected or paid
   out. `kind` narrows to one of `cash`, `upi`, `upi_verified`, `card`,
   `admission_account`. Its amounts per day and kind equal the daily report's.
5. **Who may see them.** `pharmacy.sale.read` with the outlet grant, like the
   daily report. Neither read carries a patient, prescriber or buyer, so reads
   are not audited (as C-3 decision 5). A payment reference is the provider's
   transaction number, not personal data.
6. **Bounds.** A period is at most 366 outlet days (the shared `outletPeriod`).
   Both lists use the platform's keyset pages.

## Not covered

- Average purchase cost and margin (legacy 441's AVG PUR; 452, 719, 749, 444):
  #1060 still needs approved cost and margin formulas.
- Patient-wise sales (442): it discloses patients, so it needs an audited read
  of its own.
- Wholesale (728, 734), indents (443, 737), the pharmacy summary with insurer
  and hospital-linked balances (752, #1051), the sales stock report (755).
- UPI payments captured but never applied to a sale, and their recorded manual
  refunds (`pharmacy_unused_payment_refunds`): they belong to the payment
  journey (#1478) and are in neither report yet.
- Category and clinician filters (there is no category master), CSV export.
- Product and kind filters on screen: the screen, under the daily report and
  sharing its period, lists the whole period; the API filters are there for
  the next screen change.
- Legacy "deleted amount": nothing is deleted.
