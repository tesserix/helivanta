# Pharmacy supplier return acceptance

Tracking: [#1784](https://github.com/tesserix/helivanta/issues/1784).
Contracts: [ADR-0075](../adr/0075-pharmacy-supplier-returns.md) and the
[pharmacy API](../api/domains/pharmacy.md).

## Preconditions

Use an approved isolated synthetic outlet and owner/purchasing account. Repeat for
hospital-owned, independent and chain pharmacy ownership. Confirm the displayed
account/outlet, current reviewed outlet licence/evidence, relevant staff authority
and supported current product classification. Do not bypass a locked Stock release
capability or activate controlled-drug policies for acceptance. Hosted native test
fixtures supply their own reviewed evidence; local apps/databases are not required.

## Prepare through Accounts

1. Record an external supplier inventory receipt and its original bill through
   the normal owning APIs/UI. Include a fully paid bill in a separate scenario.
2. For the units to return, complete the physical quarantine hold first. Check
   that the hold preserves total stock while respecting reservations.
3. Open **Pharmacy → Accounts → Supplier returns**. Select the original bill by
   document and supplier name. Confirm original product, batch, expiry and issue
   units. Platform-supplier receipts must show the reverse-acknowledgement boundary.
4. Choose a positive quarantine location by name and code. Enter whole return
   units and the expected credit explicitly, including zero. Add further original
   receipts/buckets where required. Duplicate pairs must not merge credit values.
5. Recheck observations before recording. Previously dispatched quantities reduce
   the original receipt allowance; other drafts do not reserve it. The cutoff is
   issued by the server. A failed or wrong-scope read must conceal old choices.
6. Add a reason and prepare. Verify the original receipt and current return
   detail. This command creates no stock movement, settlement or actual credit.

## Recover and physically dispatch

1. The hosted browser test deliberately loses the preparation response after the
   server commits, reloads and checks the original reference. Verify one draft and
   no additional command. Recovery must still work when new Stock writes pause,
   subject to current read authority and the original account.
2. Open current return detail. Capture the reviewed outlet licence, outward
   commercial reference, recipient, actual handoff time and explicit physical
   attestation. The device-time label must be clear. Do not record a handoff that
   has not actually happened or infer statutory authority from these fields.
3. Record the complete dispatch. Verify one dedicated negative quarantine movement
   for every original draft line; reservations, original-receipt caps, stock
   balances, audit and outbox evidence must agree. No automatic financial credit
   or bill settlement follows dispatch.
4. The hosted test loses this response too, reloads and recovers the exact original
   receipt. Verify stock is removed once and the bill balance is unchanged.
5. Try stale/cancelled parents, duplicate intent/reference, another account,
   revoked outlet access, unsupported classification, future/source-preceding
   handoff time and overlapping returns. Refused actions must leave no partial
   dispatch or movement. Audit/outbox failure must roll back writes and refuse
   private reads. Check signed register/reconciliation/CSV totals.

## Printable commercial note

After a dispatch made with migration0057 installed, open its current detail and
choose **Download commercial return note**. Verify the downloaded file's human
return number, original bill, named pharmacy/outlet and supplier, outward
reference, recipient, UTC handoff, product/lot/expiry and exact issue units. Open
the file and use the browser's Print command to print or save as PDF. Check A4
layout and repeated headings for a multi-page note. No backend IDs, active
content, external resources or inferred tax/credit acceptance belong in the view.

In the disposable hosted fixture, simulate a later directory metadata migration:
retained names and bytes must remain unchanged. The current directory is
immutable; ordinary application rename attempts must remain refused before and
after this fixture injection. Do not disable live guards to perform this check.
Every file release requires a new disclosure audit. A draft, foreign
tenant/outlet or revoked grant must release no attachment. Missing historical
name snapshots, incomplete movement proof and audit failure refuse bytes; do not
backfill historical names from today's directory. A changed identity, wrong
scope/count/digest, truncated/extra file, oversize response or navigation during
download must not create a local file. Stock and bill balances stay unchanged.

The hosted browser journey downloads the actual file through Global API,
verifies its batch table and takes a print-media screenshot for visual review.
`TestSupplierReturnNote*` covers rendering/escaping, bounded evidence, both
ownership models and chain outlets, retained names, current access, missing old
snapshots and audit failure. SDK and component tests verify complete bytes,
private scope, duplicate suppression and abort-on-unmount. Hosted execution,
rendered screenshot review and authenticated live release remain Pending until
their evidence is recorded.

## Evidence and remaining scope

`e2e/tests/pharmacy-supplier-return-dispatch.spec.ts` exercises the real browser,
Global API and owning backend for preparation and dispatch response-loss recovery,
then complete commercial-note download and print-media rendering.
`TestSupplierReturn*` includes native ownership, authority, concurrency, receipt
cap and audit/outbox cases. Local compilation/targeted tests alone do not prove
hosted or deployed acceptance.

Actual supplier credit acceptance, partial/combined-note matching, retained unused
credit, eligible bill allocation and immutable reversal/correction remain open in
#1784. The private commercial-note implementation is a candidate pending its
hosted, visual and live acceptance. Do not label these remaining gates complete
from local preparation/dispatch checks. Statutory GST/ITC/GL treatment and
platform reverse acknowledgement retain their explicit review/protocol boundaries.
Record deployed image/migration evidence and authenticated live results before
closing the issue or enabling a release capability.
