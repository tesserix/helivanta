# Pharmacy sales worklist and sale summary (#987, slice 1)

Issue: #987 (slice 1 of 3; the issue stays open). Builds on the counter sale
(#1386), registered-patient and admission sales (#1386 B-2c, #1387), service
charges (#1388) and credit notes (C-1).

## Why

Staff need to find a sale after the counter has moved on: the invoice a
patient is asking about, today's admission sales for a ward, or every sale
still showing a balance against an admission. Each sale must then open to one
statement whose amounts add up (#987: "monetary totals reconcile with its
lines, discounts, payments and outstanding balance").

Today `GET …/sales` is a bare page of invoices, newest first, with no filter,
and the invoice read shows the lines and payments but not what was returned or
what is still owed.

## Decisions (my calls; the issue fixes none of them)

1. **Reads of posted records, never a copy.** Sales, their lines, charge
   lines, tenders, credit notes and refunds are insert-only, so the worklist
   and the summary are computed afresh and cannot drift.
2. **The worklist.** `GET …/sales/worklist?from=&to=&customer=&invoice=` is
   paged, newest first, one row per sale with `sale_date` in the period
   (`outletPeriod`, at most 366 outlet days):
   - invoice number, date, time posted, currency;
   - who it was for, as a kind only: `walk_in`, `patient` (a registered
     patient) or `admission` (billed to an admission), with the bed code for
     an admission;
   - total, discount, units;
   - credited so far (credit notes up to now), net after credits;
   - billed to the admission account and still on it (billed less reversed),
     for an admission sale; zero otherwise.

   `customer` narrows to one kind; `invoice` finds one invoice number exactly
   (no period needed then, still bounded to the outlet).

3. **No direct identifiers in either read.** Rows and summaries carry no
   buyer name or phone and no patient id, only the kind and bed code. A bed
   code with a date still points to its occupant, so a read showing an
   admission sale is audited (#1555). Anyone who needs the person opens the
   invoice through `GET …/sales/{id}`, which is audited too.
4. **The summary.** `GET …/sales/{id}/summary` is the statement for one sale:
   - each medicine line: units sold and returned, amount charged and credited;
   - each service charge: amount charged and credited;
   - the sale: total, taxable value, CGST, SGST, discount;
   - payments by kind (cash applied, UPI, verified UPI, card, admission
     account);
   - each credit note: number, date, total, refunds by kind. At most 100 are
     listed, with `more_credit_notes` when there are more (a line of a million
     units could be returned a unit at a time); the totals always count every
     note, and the paged `GET …/credit-notes?sale_id=` lists the rest;
   - balances: `collected_minor` (cash, UPI, verified UPI, card) less
     `refunded_minor` (refunds other than admission account);
     `admission_billed_minor` less `admission_reversed_minor` is what remains
     on the admission account; `net_minor` is total less credits.

   The database already holds a sale's total equal to its lines plus charges
   and its tenders, and a credit note's total equal to its lines plus charges
   and its refunds. The summary states both and the tests prove them, so
   `net_minor = (collected_minor − refunded_minor) + (admission_billed_minor −
admission_reversed_minor)` always.

5. **Who may see them.** `pharmacy.sale.read` with the outlet grant, like the
   invoice read. A sale of another outlet is 404.
6. **The screen.** A "Sales" page in PharmaConnect: the worklist with period,
   customer kind and invoice filters, each row opening its summary beside the
   list, as the other PharmaConnect lists do.

## Not covered

- Linking a sale to an OPD, IPD or casualty encounter (slice 2), and the
  payer split between patient, insurer and hospital (slice 3, with #1051).
- Store filter across outlets: the worklist is one outlet's, as every sale
  read is.
- Patient or buyer search, and showing who the buyer was in the list.
- Auditing the invoice and sale-list reads, and the bed code in these
  reads: done in #1555.
- CSV export.
