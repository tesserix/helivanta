# Tally accounting export: sales, credit notes and supplier payments (#1690)

Issue: #1690 (this slice; supplier purchase vouchers stay open, see "Not
covered"). Decision record: [ADR-0076](../../adr/0076-pharmacy-tally-accounting-export.md).
Builds on the bounded, audited exports in `report_csv.go` and `h1_register.go`,
the sales and credit-note ledgers (`sale.sql`, `credit_notes.sql`,
`service_charges.sql`, `admission_sale.sql`) and supplier settlements
(`payables.sql`).

## Why

Pharmacies keep their books in Tally. Today they retype a month of invoices,
returns and supplier payments from CSV reports. Each retyped figure is a chance
to misstate GST.

## What the data holds (verified against source, 2026-11-21)

- **Sales** (`pharmacy_sales`) are intra-state only: `place_of_supply` must equal
  the seller GSTIN's state. Every medicine line and service-charge line carries
  `supply_nature`, `rate_ppm`, `taxable_minor`, `cgst_minor` and `sgst_minor`.
  The invoice equals the sum of its lines, and its tenders sum to the invoice.
  Tender kinds are `cash`, `upi`, `upi_verified`, `card` and `admission_account`.
  A cash tender records the amount applied, not counting change.
- **Credit notes** (`pharmacy_credit_notes`) take their rate from the sale line
  or sale charge they return. They are refunded in full, by `cash`, `upi`,
  `card` or `admission_account`.
- **Supplier settlements** (`pharmacy_settlements`) are `payment`,
  `payment_reversal` or `credit`. They carry a method (`cash`, `bank`, `card`,
  `upi` or `other`), a unique `source_reference` and the `occurred_on` date
  staff enter.
- **Supplier bills** (`pharmacy_payable_lines`) record one `tax_minor` and a
  `recoverable_tax_minor`. They have no rate, no CGST/SGST/IGST split and no
  supplier GSTIN, and nothing else in the module records purchase GST.
- No sale or credit note can carry IGST. Helivanta records no inter-state sale.

## Decisions (2026-11-21)

1. **Vouchers per document, in Tally's own import format.** The export is the
   XML that TallyPrime reads under _Import › Transactions_:
   `ENVELOPE/HEADER/TALLYREQUEST=Import Data`, then
   `BODY/IMPORTDATA/REQUESTDESC/REPORTNAME=Vouchers` and one `TALLYMESSAGE` per
   voucher under `REQUESTDATA`. Each voucher is an accounting voucher
   (`OBJVIEW`/`PERSISTEDVIEW` "Accounting Voucher View", `ISINVOICE` No). Using
   accounting vouchers means Tally needs no stock-item masters.
   - **Sale**: one voucher per invoice. The tenders are debits. Taxable value is
     credited per sales head, and CGST and SGST per rate.
   - **Credit note**: the same entries, sides reversed. The refunds are credits.
   - **Supplier payment**: the supplier is debited and the payment method's
     ledger credited.
   - **Payment reversal**: the opposite of a supplier payment.

   Debits carry `ISDEEMEDPOSITIVE` Yes and a negative `AMOUNT`, and credits No
   and a positive one, as Tally's samples show. Amounts are exact minor units,
   written in rupees with two decimals, so every voucher balances to the paisa
   without a round-off ledger. The encoder refuses an unbalanced voucher as an
   internal fault rather than writing it.

2. **GST by rate, from the records.** Rates come from each line's `rate_ppm`,
   never from a table in code (IN-RC-10). A head exists only for a rate that
   appears in the records, so a rate change needs a new mapping, not a release.
   CGST and SGST are separate heads per rate. IGST has no head: no record can
   carry it, so the export never writes an IGST entry or a zero line for one.
   Nil-rated and exempt supplies get their own sales heads.
3. **Account heads, mapped once per outlet.** The export writes Tally ledger
   names the pharmacy chose, never names it guesses. The closed head grammar is:
   - `voucher_type:{sale,credit_note,supplier_payment,supplier_payment_reversal}`
   - `tender:{cash,upi,upi_verified,card,admission_account}`
   - `sales_goods:{taxable,nil_rated,exempt}:<rate_ppm>` (medicine lines and
     charges taxed as part of the principal supply)
   - `sales_service:{taxable,nil_rated,exempt}:<rate_ppm>` (charges for the
     pharmacy's own service)
   - `cgst:<rate_ppm>` and `sgst:<rate_ppm>` (rate is the line's combined rate)
   - `payment_method:{cash,bank,card,upi,other}`
   - `supplier:<payee_reference>`

   Credit notes post against the sales heads, as Tally's GST credit note does.
   Several heads may share one ledger name. Within a voucher, entries on the
   same side and ledger are merged.

4. **A ledger map is an immutable, versioned record.** It is stored in
   `pharmacy_tally_ledger_maps` and `pharmacy_tally_ledger_map_entries`
   (migration `0062_pharmacy_tally_ledger_maps`), with forced RLS, the outlet
   grant, the audit trigger, immutability and an entry-count guard.
   - A version's entries arrive in one statement. The guard checks the count
     and the replaced heads once per statement and again at commit, so a
     version is never short, never later extended and never drops a head. A
     per-row check would cost one full recount per entry: about 150 s for a
     full map.
   - `PUT …/accounts/tally/ledger-maps/:map` takes `replaces` (the current
     map's id, or null for the first) and the entries to set. The server stores
     the complete merged map. As for supplier bills, an
     `X-Helivanta-Expected-Subject` naming another session is 409
     `session_changed`, so a map is never recorded under an account that
     signed in after the form loaded.
   - A `replaces` that is not the current map is 409, and so is a second
     successor, enforced by a unique key, so concurrent edits never lose each
     other. The same id with the same intent replays its receipt. The same id
     with another intent is 409.
   - `GET …/ledger-maps/:map` returns that receipt.
   - Ledger names are 1–100 characters with no control characters. A name
     with surrounding spaces is refused with 400, never trimmed silently,
     because Tally matches ledger names exactly. The encoder XML-escapes them.
   - A map holds at most 1,000 entries. Its request body is capped at 3 MiB:
     a full map with every character `\u`-escaped is about 2.5 MB.
   - A map id that is not a UUIDv7 is 404, as for other record ids.
5. **Preview, then export, from one read.**
   - `GET …/accounts/tally/preview?from=&to=` returns the period's document
     counts, the current map id and every head the export needs. Each head has
     its label and its mapped ledger name or null, and supplier heads carry the
     supplier's latest name.
   - `GET …/accounts/tally/vouchers.xml?from=&to=&ledger_map=` writes the file.
     `ledger_map` is optional and given at most once. It is the map id the
     preview returned, or empty when the preview had none. Any other current
     map is 409 `tally_ledger_map_changed`, so a map saved between preview and
     download never puts unreviewed names into the file. The screen always
     sends it. A malformed or repeated value is 400.
   - Both run the same bounded read, so the preview lists exactly what the
     export will need.
   - The export refuses with 409 `tally_ledgers_unmapped` while any needed head
     lacks a name, and never writes a placeholder.
   - A record in a currency other than INR is 409 `tally_currency_unsupported`.
     Tally's Indian company books in rupees, and Helivanta holds no exchange
     rate.
   - No sale or credit note can be of zero value: every sale line and credit
     note is at least one paisa (`pharmacy_sale_line_discount_rounded`,
     `pharmacy_credit_notes.total_minor`). The encoder refuses a voucher with no
     debit, so a zero record from a later change fails the export rather than
     vanishing from it.
6. **Bounded like every export.**
   - Periods are 1–366 days and the read is held under an 8 s statement
     timeout.
   - At most 5,000 vouchers and 8 MiB, else 413 `accounting_export_too_large`.
     Never a cut-off file.
   - A sale voucher is about 1.1 KB, so 5,000 fit in under 6 MiB. A counter
     taking 300 sales a day exports a fortnight at a time.
7. **Repeatable and audited.** Every voucher's identity is stable:
   - `REMOTEID` is `helivanta-<document uuid>`.
   - The voucher number is the invoice, credit-note or settlement reference.
   - Vouchers are ordered by date, kind, creation time and id, and entries by
     side and head.

   The same records and map therefore give byte-identical files. Each export
   commits an `audit_events` row (`pharmacy_tally_vouchers`, action
   `accounts_export`, fields `from`, `to`, `ledger_map_id`) in its read
   transaction: no row, no file. The preview names suppliers, so it commits
   the same row with action `accounts_preview`. The export logs `pharmacy.tally_export.exported` with
   the counts and the file's sha256. The response carries `Cache-Control:
private, no-store`, an attachment `Content-Disposition`
   (`pharmacy-tally-vouchers-<outlet>-<from>-<to>.xml`), `nosniff`,
   `X-Export-Voucher-Count`, `X-Export-Ledger-Map` and `X-Export-Sha256`.

8. **Least data.** No buyer, patient, prescriber or recipient appears.
   - Sale narrations name the invoice. Credit-note narrations name the credit
     note and the invoice it returns.
   - Payment narrations name the supplier bill and the settlement reference.
   - Tender references and credit-note reasons stay out.
   - Admission-account sales post to the mapped admission-accounts ledger,
     never to a patient.
9. **Owner-only authority.** A new permission, `pharmacy.accounts.export`, is
   granted to the pharmacy owner role only. It covers all four routes.
   - The file discloses the outlet's sales, which is sale-read territory, and
     its supplier payments, which is accounts-read territory. No narrower role
     holds both.
   - Writing a map also needs the outlet's editor grant. Reads need the reader
     grant.
   - Cross-tenant or foreign-outlet requests get 404, and an unavailable grant
     store fails closed, as every outlet route does.
10. **Release.** The four routes and the new **Tally export** tab on Supplier
    bills (panel `pharmacy.tally_export`) are classified under
    `pharmacy_payables`. That capability is still release locked.
    - The tab is listed only for a holder of `pharmacy.accounts.export` while
      the panel is released; otherwise it fetches nothing.
    - It previews the outlet's month so far, or a chosen 1–366 day period.
      Without the editor grant the names are read-only.
    - Saving sends only changed, non-blank names as a new version under one
      UUIDv7 per intent, so an unanswered save retries as a replay.
    - The download pins the previewed map and is offered only when no head is
      unnamed and no edit is unsaved. A changed map, a newly needed head or a
      save conflict reloads the preview.

## Tally behaviour Helivanta cannot guarantee (review items)

- **Re-importing the same file.** Whether a second import of the same file
  duplicates vouchers depends on the importer's choice to overwrite a voucher
  that has the same GUID. Tally's documents describe that option
  inconsistently. Helivanta guarantees stable identities and bytes, not
  Tally-side de-duplication. The screen tells staff to import each file once.
- **Prerequisites in the Tally company.** The named ledgers and voucher types
  must already exist. Each voucher type must keep manually entered numbers
  (manual numbering, or automatic with manual override); otherwise Tally may
  renumber vouchers or refuse a duplicate number.
- **Not yet tried on a real TallyPrime company.** No import has been run
  against one, so the UTF-8 file without a declaration and accounting-voucher
  GST classification are unexecuted acceptance items. Supplier payments post
  on account, without bill-by-bill allocation.

## Not covered

- **Supplier purchase vouchers.** These cannot meet the issue's GST criterion
  until supplier bills record GST by rate and component, with the supplier's
  GSTIN. Producing an unsplit input-tax entry would misstate input tax credit.
  This is a follow-up issue, and #1690 stays open for it.
- Supplier credits (`credit` settlements), which belong with purchase vouchers.
  Unused-payment refunds, which belong to no invoice.
- Inventory-mode vouchers, Tally masters, direct pushes to a running Tally
  instance, inter-state sales, and point-in-time re-export of an earlier file.
  A supplier payment dated into a past period after an export appears in the
  next export of that period, under its own stable `REMOTEID`.
