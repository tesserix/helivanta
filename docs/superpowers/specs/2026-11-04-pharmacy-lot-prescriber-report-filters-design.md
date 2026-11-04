# Sales by item by lot and by prescriber (#1060)

Issue: #1060 (its batch and clinician filters; the issue stays open for
margin, indents, wholesale and monthly aggregation). Builds on the #1060
sales-by-item report in `sales_reports.go` and #988 slice 2's category
filter.

## Why

#1060 asks for patient, item, batch, store, category, clinician and period
filters. Store is the outlet the report is scoped to, and the other filters
exist except batch and clinician. A recall asks how much of one lot was sold,
and an outlet asks what a prescriber's prescriptions brought in. Today
neither can be answered without reading every invoice.

## Decisions (agreed 2026-11-04)

1. **Lot.** Sales by item takes `lot_number`: only sale lines from batches
   with that lot number, and credit-note lines reversing them, count. The
   lot is matched trimmed and case-insensitively (my call: a recall notice
   may print it in another case, and missing a recalled sale is worse than
   merging two lots that differ only in case). Rows stay one per product,
   so a lot number shared by two products shows both. Not audited: a lot is
   no personal data.
2. **A prescriber is a registration number with its council**, exactly as
   recorded on the prescription. A name is not an identity: two doctors can
   share one, and one doctor's name can be written two ways.
   `prescriber_registration` and `prescriber_council` are given together;
   one alone is 400.
3. **Only sales on a prescription match.** A sale line counts when the
   prescription it cites names that prescriber; a credit-note line when its
   sale line does. OTC lines never match.
4. **Prescription read is needed as well as sale read.** Sales by
   prescriber are the outlet's prescription records seen another way, so a
   prescriber filter without `pharmacy.prescription.read` is 403, checked
   before anything is read. A missing permission set fails closed.
5. **Audited.** A page read with a prescriber filter writes an
   `audit_events` row (`pharmacy_item_sales`, `report_read`, with the field
   names `from, to, prescriber_registration, prescriber_council`) in the
   read's transaction, and the values go to the audit log, as patient-wise
   sales does. No audit row, no data.
6. **The screen picks a prescriber from a list, not a search.**
   `GET …/reports/prescribers?from=&to=` returns the distinct prescribers
   (registration, council and the most recently recorded name) on
   prescriptions cited by a sale line dated in the period. It holds at most
   200, ordered by name, and says when there are more. It needs
   `pharmacy.prescription.read` and `pharmacy.sale.read`, and is audited the
   same way (`pharmacy_prescribers`, `list_read`).
7. **Screens.** Sales by item gets a lot number field and, for holders of
   prescription read, a prescriber picker, beside slice 2's category picker.
   All the filters combine.

## Not covered

- Lot or prescriber filters on sales and stock or on other reports.
- A prescriber master, or linking prescribers to staff records (#83).
- A search over more than 200 prescribers in a period: the screen says to
  narrow the period.
