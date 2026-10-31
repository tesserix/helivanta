# GST on hospital rooms and the admission tax invoice (#193 slice 1)

Resolves part of #193 (billing and invoicing). Builds on bed-day charges, final bills,
cash and deposits (`2026-10-30-*`). Decisions approved in chat on 2026-10-30.

## Why

Every bill so far is a statement. A registered hospital must charge GST where it
applies and give the patient a tax invoice. Health care by a clinical establishment is
exempt, except a non-ICU room above ₹5,000 a day, so today's bed-day charges are the
one hospital supply that may be taxable. This slice taxes them correctly and issues the
invoice for an admission's room supplies.

## The rule, from primary instruments

- **Exemption:** Notification 12/2017-Central Tax (Rate), serial 74 (health care by a
  clinical establishment).
- **Exception:** Notification 04/2022-CT(Rate), in force from 18 July 2022. It inserts a
  proviso into serial 74 for a room "[other than ICU/CCU/ICCU/NICU] having room charges
  exceeding Rs. 5000 per day to a person receiving health care services".
- **Rate:** Notification 03/2022-CT(Rate), same date, entry 31A, Heading 9993. The rate
  is 2.5% CGST, on condition that no input tax credit has been taken. The matching SGST
  rate gives 5% within the state.
- **Still in force:** Notification 15/2025-CT(Rate) (22 Sep 2025) amends serials 3–38 of
  11/2017 and does not touch 31A or clinical establishments. I read the four texts
  above; later amendments were not searched exhaustively. Finance or tax review is
  required before production use (recorded in the PR).
- **Rule 46A, CGST Rules:** one _invoice-cum-bill of supply_ may cover taxable and
  exempt supplies to an unregistered person. Rule 46(b): a consecutive number of at most
  16 characters (letters, digits, `-`, `/`), unique for the financial year.

## Decisions (approved)

1. **A dated policy, enabled per hospital.**
   - The policy is data in the database: a versioned SQL function, one row per version,
     each with its effective date and citation. An amendment is a new version, added
     in a reviewed migration, never an edit of the Go code.
   - A tenant administrator enables GST for a hospital by recording its GSTIN
     registration. Without a registration nothing is taxed and no tax invoice can be
     issued.
2. **HDU is taxed** above the threshold: it is not in the instrument's ICU, CCU, ICCU or
   NICU list.
3. **A new `nicu` bed class**, exempt. `neonatal` (a newborn ward) is taxed above the
   threshold.

## What the policy says (version `IN-ROOM-2022-07-18`)

From 2022-07-18:

- **Taxable** when the bed class is not `icu` or `nicu` and the day's rate exceeds
  ₹5,000 (500000 paise). A rate of exactly ₹5,000 stays exempt. Above the threshold the
  whole room charge is taxed.
- **Rates:** CGST 250 basis points and SGST 250 basis points, under SAC 9993.
- **The test is per bed day,** on that day's rate.

## Calls I made (builder's)

- **Tax is posted as it accrues.** When the census prices a taxable day for a registered
  hospital, it also records a GST line and posts a `gst` charge on the account for
  CGST + SGST. When the day is voided, the GST charge is reversed with it. Balances,
  receipts, deposits, refunds, positions and bills all include the tax with no other
  change. Bed rates stay "before tax".
- **Rounding:** each tax is computed per day as `round_half_up(rate × bp / 10000)` in
  paise, CGST and SGST separately.
- **Intra-state only:** CGST + SGST, no IGST. A room is supplied where the hospital is,
  and patients are unregistered recipients. B2B invoices to a registered recipient come
  later.
- **From tomorrow at the earliest.** A registration takes effect from the hospital's
  tomorrow or later, and needs the hospital's timezone (bed charging).
  - Today is refused (review finding): a room day dated today may already be on a final
    bill the patient has settled, and taxing it would reopen that account.
  - Once recorded, the census taxes the hospital's priced, live, taxable days dated on or
    after it, including any already priced, and a final bill waits until it is applied.
  - Days before it are never taxed or invoiced. Backdated tax is a finance decision for
    later.
- **Set once:** GSTIN, legal name and address. Changing a registration is a later slice.
- **The GSTIN is validated:** 15 characters, the PAN-based pattern and the check
  character (the standard base-36 mod-36 checksum); its first two digits are the state.
- **The invoice covers rooms only.** Pharmacy keeps its own GST invoice, so its charges
  stay on the statement but are not on the hospital's tax invoice.
- **What an invoice holds:**
  - An admission's invoice lists every standing (not reversed) bed charge, dated on or
    after the registration, that is not already on an invoice. A bed charge is on at
    most one invoice.
  - The first invoice is issued once a final bill exists. A later bed charge goes on a
    further invoice.
- **The reversal of an invoiced charge blocks further invoices** until its credit note is
  issued (`409 credit_note_required`), never a negative line. Credit notes followed in
  slice 2 (`2026-10-31-gst-credit-notes-design.md`).
- **Numbering:**
  - One series per GSTIN per financial year (April to March, by the hospital's date):
    `IP/2627/1` up to `IP/2627/9999999` (15 characters).
  - Consecutive under a lock on the GSTIN and year, and set by the database.
- **The recipient** (name and address) is entered or confirmed by the desk when issuing,
  and is required for every invoice. Rule 46 needs it for unregistered recipients above
  ₹50,000; requiring it always is stricter. Billing does not copy patient demographics.
- **Lines are snapshots** set by the database from the bed day and its GST line: date,
  bed class, SAC, value, and taxable or exempt with rates and amounts. An issued invoice
  never changes.
- **Permissions:**
  - `billing.tax.manage` (administrators only) records the registration.
  - Issuing an invoice is `billing.bill.issue` (the billing desk). Reading is
    `billing.account.read`.

## Rules enforced in the database

- **Registration:** set once; its effective date is after the hospital's today; it
  needs the hospital's timezone.
- **GST lines:** each is computed by a trigger from the bed day, the policy in force on
  its date, and the registration, never from caller values. It is refused unless the day
  is taxable. There is one line per bed day.
- **Invoices:**
  - The number and date are set by the database.
  - Lines are written only in the issuing transaction, and are recomputed or validated
    against the bed day and GST line.
  - An entry can be on only one invoice.
  - An invoice is refused without a registration or a final bill, or after an invoiced
    charge has been reversed.
- **Everything else:** append-only, forced row security under the billing grant,
  audited.

## Delivery

- **PR A:** the `nicu` bed class (medicore migration, contract, shell labels), priced
  like any class.
- **PR B:** registration, policy, GST lines and entries, tax invoices, and the shell's
  GST setup and invoice views.

## Not covered

- Credit and debit notes (credit notes for reversed days: slice 2).
- IRN or e-invoicing (IRP), QR codes, PDFs and printing.
- IGST and B2B recipients.
- GST on any other hospital service (none is charged yet).
- Changing or cancelling a registration; backdated registrations.
- Input-tax-credit accounting; GSTR returns and exports; rounding the invoice total to
  rupees.
