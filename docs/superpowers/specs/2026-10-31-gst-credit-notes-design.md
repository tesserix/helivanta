# Credit notes for an admission's room invoices (#193 slice 2)

Resolves part of #193 (billing and invoicing). Builds on GST on rooms and the
invoice-cum-bill of supply (`2026-10-31-gst-room-invoices-design.md`). Decisions
approved in chat on 2026-10-31.

## Why

A room day on an issued tax invoice can still be reversed: a late or corrected bed
movement changes the days a stay implies, and the census reverses the day's charge and
its tax on the account. The invoice is never changed, so the tax document for that
reversal is a credit note. Until now the reversal blocked every further invoice for the
admission (`409 credit_note_required`). This slice issues the credit note, after which
the replacement days can be invoiced.

## The rule, from primary instruments

- **CGST Act s.34(1)**, as amended by the CGST (Amendment) Act 2018 from 1 Feb 2019:
  where the taxable value or tax charged on one or more tax invoices is found to exceed
  what is payable, the supplier may issue one or more credit notes for supplies made in
  a financial year.
- **s.34(2)**: declared in the return of the month of issue, not later than **30
  November** following the end of the financial year of the supply (Finance Act 2022,
  from 1 Oct 2022), or the date of the annual return, if earlier.
- **Proviso to s.34(2)**, substituted from 1 Oct 2025 (Finance (No. 7) Act 2025): no
  reduction of output tax if the incidence of tax was passed on to another person (for
  unregistered recipients). Its effect on a patient refunded in cash is a tax-reviewer
  question, recorded here and in the PR, not decided in code.
- **s.34(1) discount clause** (Finance Act 2026, s.154): not yet in force; discounts
  are #200.
- **CGST Rule 53(1A)** (inserted from 1 Feb 2019): a credit note shows the supplier's
  name, address and GSTIN; the nature of the document; a consecutive serial of at most
  16 characters, unique for a financial year, in one or more series; the issue date; the
  recipient's name and address (unregistered); the serial number(s) and date(s) of the
  corresponding tax invoice(s) or bill(s) of supply; the value, rate and tax credited;
  and a signature.

Read on the CBIC tax information portal on 2026-10-31. No CBIC circular on the 2025
proviso was found. Finance or tax review is required before production use.

## Decisions (approved)

1. **The billing desk issues it**, like an invoice, and the issuing clerk is its
   recorded signatory. Not issued automatically, because the rule asks for a signature.
2. **One credit note per invoice per issue**, numbered `CN/<fy>/<serial>` in its own
   series per GSTIN and financial year of issue. It carries every reversed line of that
   invoice not yet credited. Taxable and exempt lines appear together, as on the
   invoice-cum-bill of supply. It covers reversals only; discounts are #200 (slice 1 refuses invoiced charges).
3. **After the deadline it is refused** with `409 credit_note_deadline_passed`. The
   admission stays blocked for new invoices, and finance resolves it outside the system.

## Calls I made (builder's)

- **One invoice per credit note.** Rule 53(1A)(g) allows several invoices, but one
  invoice fixes a single recipient, a single supply financial year and a single
  deadline. An admission with several reversed invoices gets one credit note per
  invoice. This narrows the cases handled; it does not weaken any of them.
- **The supply's financial year is the invoice's.** For a service the time of supply
  is the invoice date when the invoice is issued in time (s.13), so the 30 November
  deadline runs from the original invoice's financial year. This matches pharmacy's
  `returnDeadline`. The annual-return date can be earlier, but the system does not know
  it; the UI and docs say so.
- **The credit note copies, never recomputes.** Each line is a snapshot of its invoice
  line: value, SAC, policy version, rates and tax. What is credited is exactly what was
  invoiced, and it equals the reversal already on the account.
- **Recipient and supplier come from the invoice.** The desk enters nothing but the
  command id, so a credit note cannot name a different patient from the one invoiced.
- **The issue is a plain button, not a confirmation dialog.** Issuing a credit note
  removes nothing. A credit note is never changed or deleted.

## Rules enforced in the database

- `billing_credit_notes`: the header. The trigger sets the supplier, recipient,
  invoice number and date, issue date, financial year and serial under an advisory lock
  on the series. Unique `(tenant, gstin, financial_year, serial)`. Its caller must be
  the issuer and must hold the grant.
- `billing_credit_note_open(invoice_fy, today)`: false after 30 November following the
  end of the invoice's financial year → `billing_credit_note_deadline`.
- `billing_creditable(tenant, invoice)`: the invoice's lines whose bed charge is
  reversed and not yet credited.
- `billing_credit_note_lines`: primary key `(tenant, entry_id)`, so a charge is
  credited once, with a foreign key to the invoice line. Written only during its note's
  issue and only for a creditable line of that invoice; the snapshot is set by trigger.
- A deferred constraint `billing_credit_note_complete`: at least one line, and nothing
  creditable left on that invoice.
- The invoice block becomes "reversed and **not credited**".
- RLS, audit, append-only and fixed triggers, as for invoices.

## API

- `PUT /billing/hospitals/:hospital/admissions/:admission/tax-invoices/:invoice/credit-notes/:note`
  (`billing.bill.issue`, no body): `201` on issue, `200` on replay of the same id for the
  same invoice, `409` for the same id on another invoice. Errors: `404` for an invoice
  not found, `409 nothing_to_credit`, `409 credit_note_deadline_passed`.
- `GET …/admissions/:admission/credit-notes` (paged), `GET …/credit-notes/:note`, and
  `GET …/credit-notes/:note/lines` (paged) (`billing.account.read`).
- Tax invoices gain `creditable_lines`, the count of reversed lines awaiting a credit
  note.

## UI

In the tax invoice detail, an "Issue credit note" button appears when the invoice has
creditable lines and the user can issue. A "Credit notes" list sits beside the
invoices; each note opens to its lines and totals, and names its invoice and date.

## Not covered

- Credit notes for a discount, a deficient service or an amount agreed with the
  patient; debit notes and supplementary invoices.
- One credit note across several invoices.
- Declaring credit notes in GSTR-1, and knowing the annual-return date.
- Whether the 2025 proviso bars the tax reduction when the patient was refunded in cash
  (tax reviewer).
- IRN, PDFs and printing, IGST, and registered recipients (as for invoices).
