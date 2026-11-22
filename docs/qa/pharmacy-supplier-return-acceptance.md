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

## Actual supplier-credit document candidate

The document backend, SDK and Accounts credit-entry/history workspace are
development candidates. They have not been deployed or accepted live. The native
hosted scenarios exercise:

- Actual partial credit of 750 paise against a fully paid 11800-paise source bill,
  across hospital-owned, independent and chain outlets. The bill stays paid and
  all actual credit remains unallocated; expected return credit is not replaced.
- Normalized reference admission and concurrent duplicate-reference attempts,
  actor-bound same-command replay, changed intent and another actor's recovery.
- Versioned void/correction, one replacement and immutable original receipts.
- Readonly finance access, tenant/outlet denial, revocation and bounded history.
- Invalid dates/source/actor, audit/outbox rollback and failed private-read audit.

`packages/api/src/pharmacy-supplier-credits.test.ts` exercises current-position
conservation, exact request/response scope, normalized original intent, separate
current/immutable reads, cross-actor historical read versus recovery, versioned
void evidence, bounded/unique pages and no automatic retry after uncertainty.
These SDK tests do not substitute for a real Global API/browser financial journey.
Matching and allocation/reversal have separate owner-native and SDK scenarios;
do not claim financial settlement from document-stage acceptance alone.

The matching native scenarios in `supplier_credit_matching_native_test.go` passed
at `49907284` in [CI 44907133122](https://github.com/tesserix/helivanta/actions/runs/44907133122)
and [supplier workflow 44907133061](https://github.com/tesserix/helivanta/actions/runs/44907133061).
They cover all three ownership models, partial/combined attribution without
spending credit, cross-document stale totals and explicit excess rationale,
immutable full reversal and original recovery after void, bounded history,
foreign tenant/outlet/actor and readonly denial, actual grant revocation, different
supplier/draft/unknown source, projection edit refusal and audit/outbox rollback
for both match and reversal. Owner allocation/reversal also passed its separate
head gates in [CI 44923078989](https://github.com/tesserix/helivanta/actions/runs/44923078989);
current financial contexts passed in [CI 44924265641](https://github.com/tesserix/helivanta/actions/runs/44924265641).
Keep UI, latest integrated hosted/visual evidence and deployed operational
acceptance distinct from those owner-native results. Matching alone does not
prove bill settlement. No live matching test is claimed.

The matching SDK is covered by
`packages/api/src/pharmacy-supplier-credit-matching.test.ts`: exact scope, original
intent, explained excess, version/return-total arithmetic, original-amount reversal,
cross-actor history versus recovery, bounded/unique pages, cancellation and no
automatic second intent after uncertain transport. These tests and the document
SDK suite must pass together; they do not establish browser or live acceptance.

## Record and recover actual supplier credit

1. Use an account with current accounts-read/accounts-adjust permission and an
   active grant to this outlet. Finance-only staff must not need physical stock or
   supplier-return permission. Confirm Payables is available in the reviewed release.
2. Open **Pharmacy → Accounts → Supplier credits → Record supplier credit**.
   Choose the dispatched return by return number, supplier and original bill.
   Draft returns must be absent. Page through additional choices when necessary.
3. Confirm the expected return value is informational. The actual credit amount
   starts blank. Enter the supplier note reference, issue date, actual amount and
   reason from its evidence. Do not substitute the expected value or infer tax.
4. Save once. Check the named supplier/reference and recorder, actual amount,
   matched-to-returns amount, applied-to-bills amount and unused pool separately.
   Recording the note alone must not change physical stock or the original bill's
   liability, including when that source bill is fully paid.
5. In the disposable hosted browser fixture, lose the save response after commit.
   Reload and choose **Check original credit**. Only the scoped command reference
   is retained in browser session storage; no supplier, amount or reason is stored.
   Verify the same credit, a single PUT and no duplicate document. Repeat with
   new recording disabled: retained recovery must remain available under current
   read authority. If recovery returns not found, retry only the original command
   and intent; do not start another note while its outcome is uncertain.
6. Check invalid amounts focus the inline field error. A definite refusal must
   keep the actual amount visible. Unavailable storage must prevent submission.
   Foreign/malformed source responses, history refusal or failed detail reads must
   hide previously loaded private evidence. Switching accounts or outlets must
   not expose another scope's draft, choices or recovery reference.
7. Repeat tenant/outlet/actor denial, revoked access and read-audit failure through
   the Global API. No private choices or successful financial write may escape
   failed authorization or failed audit.

The hosted spec extends the existing dispatch journey with real credit entry and
response-loss recovery, then captures `supplier-credit-recovered` for visual review.
Local component/SDK tests do not substitute for that hosted run or live acceptance.
Matching, full match reversal and bill application/reversal are development UI
candidates pending their latest hosted/visual and live acceptance. Credit void is
also a candidate control, as is explicit replacement of a voided note on its
original return. Latest hosted/visual/live acceptance remains required.
Document entry and matching are not bill settlement.

### Match actual credit to dispatched returns

1. Open the named credit and inspect **Return matching**. Retained history must
   remain readable with operating capabilities disabled, without loading choices.
2. With current financial editor authority and Payables availability, choose
   **Match credit to return**, select the named dispatched return, then enter an
   explicit amount and reason. The amount starts blank. Verify the expected
   return value, total attribution across credit documents and this credit's
   remaining attribution are separately labelled.
3. Try an amount above remaining actual credit: no command may be sent. Try an
   attribution that exceeds the return's expected value without an explanation:
   no command may be sent until explicit supporting evidence is entered.
4. Save once and inspect the named return's immutable history. The credit's
   matched amount increases; its unused pool and bill liability remain unchanged.
5. Lose the committed response in the hosted fixture, reload, reopen the named
   credit and choose **Check original match**. Verify one matching PUT, recovery
   of the same actor-bound command and no automatic retry. Repeat after turning
   new recording off and when the entire actual credit is already matched.
6. Refuse a stale observation: the actual input remains visible. Choose
   **Refresh credit and matching sources** and review fresh observations before
   creating a new command. Unknown outcomes must retain the reference. If browser
   cleanup fails even after a verified response, only the original recovery may
   continue until its reference can be cleared.
7. Check wrong-actor/source/scope data is rejected and source/history failures
   hide cached private references. Changing a retained in-memory matching intent
   must not turn an unrelated recovered receipt into a verified match.

### Review and reverse an original match

1. Open **Review match** from the named return's matching history. Verify its
   original amount/reason and the current total return attribution across credit
   documents. Review remains available without new-write authority. Current
   observations come from the exact owner context, never the original receipt's
   historical totals or a scan of eligible-source pages.
2. With current financial editor authority and Payables availability, enter an
   explicit reversal reason and choose **Reverse full match**. A stale observation
   must refuse without a partial reversal; preserve the reason, refresh current
   observations, review them and explicitly submit a new command.
3. Verify the original match is marked reversed and retains its immutable amount,
   recorder and reason. Retained reversal evidence identifies its own reason and
   named recorder. Other credit documents' attribution, unused actual credit,
   bill liability and physical stock remain unchanged. A financial reversal does
   not receive dispatched medicines back into stock.
4. Lose the committed reversal response in the hosted fixture, reload and reopen
   the named credit. The pending reversal must reopen from only its scoped
   command/match references, even when its original match has left the active
   set or new writes are disabled. Choose **Check original reversal** and verify
   the same command, one PUT and no automatic retry. Re-enter a missing original
   only with its retained reference; never generate a replacement command while
   its outcome remains uncertain.
5. Repeat after void: original and reversal evidence remains readable, with no
   new reversal offered. Wrong-source/actor/intent results or failed current
   disclosure must not be presented as verified. A storage failure must prevent
   submission or preserve the existing reference for recovery.

### Apply actual credit and reverse after bill payment

1. Fully pay the original source bill through the normal owning payment API.
   Verify the credit remains unused. Create a separate later receipt and inventory
   bill for that supplier; do not reuse the same receipt on two bills.
2. Open the named credit and choose **Apply credit to bill**. Select the named
   eligible bill. The amount must start blank; enter it, a date and reason
   explicitly. Verify unused credit and outstanding liability are distinct.
   Exceeding either balance or using a date before either document or after the
   current outlet date must prevent submission. Matching the entire credit to
   returns must not incorrectly remove its unused pool.
3. Apply INR 5.00 from a INR 7.50 credit to a INR 11.80 later bill. Verify unused
   credit becomes INR 2.50, applied credit INR 5.00 and outstanding INR 6.80.
   The original fully paid bill remains unchanged. In the disposable hosted
   fixture, lose the committed response, reload, reopen the credit and choose
   **Check original bill application**. Verify one PUT and the same original.
4. Record INR 6.80 against the later bill through the normal payment API. The
   bill is now fully paid and absent from eligible choices. From application
   history choose **Review application**: exact original context must still
   show INR 0.00 outstanding and INR 6.80 retained payment.
5. Enter a reversal reason/date on or after the original application and no later
   than the outlet date. Choose **Reverse full bill application**. Verify INR
   7.50 unused credit, zero applied credit, INR 5.00 restored liability and INR
   6.80 retained payment. Original application amount/reason/recorder remain
   immutable, with separate named reversal evidence. Stock, cash and matching
   remain unchanged.
6. Lose the committed reversal response in the hosted fixture, reload and reopen
   the named credit. The original review reopens from its scoped references;
   choose **Check original bill application reversal**. Verify one PUT and the
   same reversal. Recovery for both commands must also work when writes are
   disabled, without querying eligible choices. Reference storage contains no
   supplier, amount or reason. No unknown outcome may cause automatic retry.
7. Exercise stale credit/bill observations, scope/actor changes, source/history
   refusal and storage failure. New commands fail closed; cached private evidence
   is hidden on refusal. After confirmed not-found recovery, only the exact
   original command/intent may retry. After reload, re-entry must keep the same
   command identity. A cleanup failure retains recovery and blocks another write.

### Void an unused note without erasing evidence

1. Confirm the note has zero active return matches and zero bill applications.
   **Void unused credit** must be unavailable while either remains, when the
   note is already voided/corrected, or when write authority is unavailable.
   Reverse matches and applications through their own explicit commands first.
2. Open **Void unused credit**, enter a reason and confirm. A current version is
   required. Stale refusal preserves the reason; refresh observations and review
   before explicitly submitting a new command. No automatic replacement is made.
3. Verify the note is retained as Voided with its immutable original amount,
   separate void reason and named recorder. Unused credit becomes zero. Bill
   liability, payment history and stock remain unchanged. Original matching and
   application/reversal history remains readable under current read authority.
4. In the hosted fixture, lose the committed response, reload and reopen the
   named credit. Choose **Check original credit void** and verify one PUT and the
   same actor-bound original. Recovery remains available with writes disabled;
   only its scoped command reference is stored. Wrong retained reason/version,
   actor or scope cannot be accepted as successful recovery.
5. Refuse a refreshed retained-void read: hide cached reason/recorder and recheck
   current access. Unavailable reference storage prevents submission. Failed
   cleanup retains the same recovery reference and blocks another command.

### Replace the voided note with explicit corrected evidence

1. Open the named voided credit and choose **Replace voided credit**. Current
   financial editor authority and Payables admission remain required. Active or
   already corrected originals cannot offer another replacement. Original note
   and return references are displayed by name; there is no raw-ID entry or
   supplier-name lookup. This corrects the note on its original dispatched return.
2. Enter the replacement's reference, issue date, actual amount and reason
   explicitly. Amount and date must start blank. Confirm the owning API preserves
   the original supplier/currency and enforces a unique correction. A stale
   refusal must keep the entered values; refresh original observations and review
   before another explicit submission.
3. In the hosted fixture, replace the voided INR 7.50 note with an explicit INR
   8.00 note. Lose the committed response, reload and reopen the original, which
   is now Corrected. Choose **Check original credit**. Verify one PUT, selection
   of the new named credit, exact `correction_of` linkage in authorised API
   evidence, INR 8.00 unused new credit and zero unused original credit. The
   original amount and void evidence remain immutable. Bill liability, retained
   payment and physical stock remain unchanged.
4. Repeat recovery with new writes disabled and after closing/resuming the
   correction form. Only the original command reference is stored, additionally
   bound to the original note and current actor/outlet. A response for another
   original, return, supplier or currency must not be called verified, even when
   no submitted intent survives reload. Account/outlet changes must not expose
   another correction reference. Unknown outcomes never automatically retry.
5. Recheck current disclosure before exposing the original/form. Unavailable or
   refused current owner context hides cached evidence. A replacement is not a
   payment, return attribution, bill application, tax decision or automatic credit
   accepted from the platform supplier; those remain separate commands/boundaries.

## Review credits from the original bill

1. After applying an actual credit, open **Accounts → All bills** and choose the
   named target bill. In **Supplier credit applications**, verify the human note
   reference, original applied amount, date, reason, recorder name and Active
   status. Receipt and account UUIDs must not appear in rendered content.
2. Compare the owner's bill totals with the application; do not sum a partial
   history page as the bill balance. Direct legacy adjustments remain in their
   separate settlement history. Use read-only finance staff to confirm that
   reviewing history needs no adjustment authority or Stock permission.
3. After a retained payment and full application reversal, reopen that bill.
   Verify Reversed status and use **View reversal** to inspect its exact reason,
   date and recorder. The original applied amount remains visible, payment is
   retained, and owner liability reflects the reversal. The hosted journey
   captures `bill-supplier-credit-history` at this point.
4. Check multiple actual documents applied to one bill, pagination, a bill with
   no managed credit and a fully paid bill subsequently reopened by reversal.
   A cursor from another bill, foreign outlet/tenant, revoked current authority
   and failed read audit must not disclose journal evidence. A changed account
   must immediately lose the previous account's selected reversal.
5. Refuse a current history/reversal read after loading it: cached private rows
   and affected reversal details must be hidden until a fresh authorised retry.
   Use the hosted harness for injected failures. Never disrupt the shared live
   environment for this purpose.

The candidate reuses `SupplierCreditAllocationView` with a 50-row resource cursor,
baseline retained-read admission and accounts-read authority. Migration 0063
adds its concurrent bill-history index. Hosted native tests inspect a two-row
synthetic query plan; this is not a large-cardinality performance measurement.
Record actual hosted and live evidence before marking these cases passed.

CI retains only the named synthetic `commercial-return-note-print`,
`supplier-credit-recovered`, `supplier-credit-matched`,
`supplier-credit-match-reversed`, `supplier-credit-applied` and
`supplier-credit-application-reversed`, `supplier-credit-voided` and
`supplier-credit-corrected` and `bill-supplier-credit-history` PNGs in the
`supplier-return-visual-evidence` artifact for seven days. The capture runs before
later Playwright phases overwrite their reports; missing required images fail
capture. Download and visually inspect that artifact before accepting the UI.
No live patient, staff or supplier data belongs in these fixtures.

## Evidence and remaining scope

`e2e/tests/pharmacy-supplier-return-dispatch.spec.ts` exercises the real browser,
Global API and owning backend for preparation and dispatch response-loss recovery,
then commercial-note download/print-media rendering, actual credit entry,
matching/reversal, application/paid-bill reversal, void and replacement with response-loss recovery
through the Supplier credits workspace, plus retained applications/reversal
from the actual target bill. Inspect the latest hosted run before
recording any of these candidate journeys as passed.
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
