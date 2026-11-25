# PharmaConnect: exporting accounts to Tally

Status: the sales/credit/payment core is deployed to the pilot. Purchase tax
capture and purchase vouchers are in development and need release acceptance.
Issue: [#1690](https://github.com/tesserix/helivanta/issues/1690).

## Who can use it

Use an authorized account with accounts-export permission and access to the
correct pharmacy outlet. Recording tax evidence or ledger names also needs
Editor access. The `pharmacy_payables` release capability and Tally panel must
be enabled for the authorized pilot; a feature flag never grants permissions.

## Record the original bill first

1. Open Supplier bills and record the invoice/expense through the normal flow.
   Keep source references, currency and any acquisition receipt links accurate.
2. Open the posted bill and its Purchase tax evidence section.
3. Copy the supplier GSTIN when shown, supplier state/territory and the
   place of supply from the invoice. For wholly outside-GST documents, leave
   unprovided jurisdiction fields blank. A recorded GSTIN needs its source state.
   Do not guess jurisdiction from an address.
4. For each original line choose its source nature, base value and GST rate, then record
   CGST/SGST/IGST/UTGST and the recoverable amount of each component.
5. Check the displayed original tax and recoverable totals. All components must
   match those exact amounts; the form cannot alter a posted bill or valuation.
6. Give the source/correction reason and record the evidence. A corrected version
   preserves the earlier record. After an uncertain response, retry the same
   intent; do not invent a new command to bypass a conflict.

This is source recording, not legal tax advice or automatic ITC eligibility.
Rates are not downloaded. Unregistered suppliers cannot record positive GST;
reverse-charge inference and statutory filings are outside this workflow.

## Map and download

1. Open Supplier bills → Tally export and choose an inclusive 1–366 day range.
2. Resolve missing purchase evidence in each source bill before previewing again.
3. Enter each ledger/voucher-type name exactly as it exists in the target Tally
   company, including input-tax and nonrecoverable-tax heads when needed.
4. Save the map. It is versioned per outlet and reused for later periods. A new
   rate/category/supplier creates a new needed head; it does not need a release.
5. Download Tally XML after every needed head is named and edits are saved.
6. Have the accountant verify the target company, names, date range, totals and
   manual-numbering setup, then import the file once. Re-importing can duplicate
   vouchers depending on Tally settings; stable identities alone cannot promise
   Tally-side deduplication. No real-company import has been verified yet.

Each purchase debits net plus landed costs and the source tax components, then
credits the supplier with the original gross amount. Recoverable and
nonrecoverable tax have separate mapped heads. Payments and reversals have
separate vouchers; they do not create another purchase or move real money.

The file includes sales invoices, sales credit notes, supplier bills, supplier
payments and reversals. Supplier credit settlements are a separate workflow and
are not exported as cash payments. This is an accounting-document export, not
a complete statutory GST return or a replacement for reviewed legal books.

## Repeatability and audit

Unchanged source records/evidence and mapping produce identical bytes and a
matching checksum. A later backdated posting or corrected evidence changes the
source set, so export that period deliberately and reconcile before re-import.
Every delivered file is audited with period, map, checksum and voucher count.
Original financial records remain unchanged. The export is bounded at 5,000
vouchers and 8 MiB; choose a shorter period if it exceeds either limit.

The purchase extension also retains `pharmacy_tally_export_receipts`: an immutable,
forced-RLS record of each delivered file's period, ledger map, voucher count,
file SHA-256 and source-voucher fingerprint. It commits with the audit event before
file release; a failed receipt/audit commit releases no file. The receipt holds
no XML, patient details or duplicated monetary ledger.

Compatibility: the updated SDK requests `purchase_bills=true` for preview and
file download, verifies the full preview schema and requires the matching
`X-Export-Purchase-Bills: true` header. Omitting this option preserves the legacy
four-count response and sales/credit/payment-only file; it does not silently add
purchases to existing integrations. The option accepts one `true` or `false`
value; malformed/repeated values are 400. Export receipts record the chosen mode.
The original map/permission/date/bounds contracts remain unchanged. Consumers
wanting complete purchase export must use the updated SDK or opt in explicitly.

Purchase-enabled exports additionally cap source detail at 50,000 rows; they refuse an oversized
period before building vouchers. XML encoding checks its byte bound as each
voucher is written, returning no partial file. Detailed bills can therefore need
a shorter period even below the 5,000-document limit.

The base value is recorded from the source and cannot exceed its existing net
plus landed costs in this price-based bill workflow. A GST calculation difference
beyond one paisa per component needs a separate source explanation or corrected
entry. This is a review trigger, not a statutory rounding limit: documented
supplier rounding is preserved, and the original tax/recoverable totals must
still match exactly. GST registration status is not checked online.

The schema extension preflights all existing operating/hospital authority seals,
adds both choice-purpose restrictions to each new table, and refreshes the
choice then hospital snapshots after its own reviewed DDL. It does not bypass
pre-existing schema drift. Each audited line also has a server-generated UUID.
