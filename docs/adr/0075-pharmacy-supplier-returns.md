# ADR-0075: Attributable supplier returns and retained supplier credit

Status: implementation in progress; not deployed or release eligible. Refs #1687,
#162. The local inventory-lifecycle proposal is planning input; implemented
contracts and source remain authoritative.

## Context and ownership

PharmaConnect owns outlet stock, purchase payables and their settlements. A
supplier return must not become a customer refund, a negative payment, a stocktake
correction, or a write into another tenant's stock. Existing settlements reject
amounts above a bill's remaining liability. Consequently an already-paid invoice
cannot receive another liability-reducing credit: supplier credit must be retained
until explicitly allocated to an eligible bill.

The pilot scope is India, initially Maharashtra and Odisha, for hospital-owned,
independent and chain pharmacies. Licensed-premise and controlled-drug policy are
not inferred from country, ownership, a posted invoice or a role title.

Planning assumptions, not measured demand: inherit the architecture's 1,000/3,000
peak application RPS and 80:20 reads/writes. Budget this workflow for 20 peak
commands/s, 128 KiB command bodies, at most 100 return lines and 200 rows per page.
At 50 tenants × two outlets × ten returns/day × ten lines, the twelve-month upper
case is 3.65 million lines. The 36-month envelope at 150 tenants held constant is
32.85 million lines. Use existing indexed PostgreSQL and outbox; no additional
service, datastore, worker or infrastructure allocation is proposed. Targets are
p99 500 ms for bounded reads, 2 s for ordinary commands and a 15 s command deadline;
these require hosted measurement and are not claimed SLO evidence.

## Decision

1. Add an accountable paired hold from sellable stock into quarantine. Preserve
   reservations and bucket bounds. Require exact source/destination, units,
   reason and physical-quarantine attestation. Both movements, evidence, audit
   and outbox commit together. No receipt or correction endpoint can forge it.
2. A return draft selects exact original inventory-payable receipt lines and
   quarantine buckets in the same outlet. Freeze product/batch/expiry/unit,
   supplier, bill and expected-credit evidence. Do not reserve or remove stock
   merely because a draft exists. Near-expiry sellable stock first goes through
   the hold. Reject platform-supplier receipt sources until their owner supports
   an acknowledged return protocol; do not manufacture a counterparty receipt.
3. Dispatch rechecks source lineage, quantities returned against each purchase,
   current physical stock, reservations and actor authority. An immutable
   supplier-return movement removes stock once. Expected credit is a separate
   receivable expectation and does not reduce a payable or assert receipt of
   money. Human confirmation identifies the actual outward handoff/document.
4. An actual supplier credit document has its own retained identity, supplier
   reference, date, currency, total and attributable acceptance. Matches against
   returns cannot exceed the document total or cross supplier/outlet/currency.
   Partial credit leaves a visible shortfall; over-expected matching requires an
   explicit reason. A combined document is shared evidence, not copied credit.
5. Financial allocation uses the existing payable settlement owner. Allocation
   cannot exceed both unallocated supplier credit and the selected bill's
   outstanding liability. Paid source invoices stay paid; unused credit remains
   explicitly available for eligible future bills. This is one financial owner,
   not a second payable master. Matching a return and allocating a bill are
   distinct facts and must not consume or create credit twice.
6. Corrections retain original evidence. Reverse an allocation through a linked
   credit reversal before voiding an accepted document; do not delete financial
   entries or silently reinstate physically dispatched goods. Reject a credit
   expectation with an attributable reason; a later actual document remains
   separately attributable. Bounded command limits and recovery must be explicit.
7. A printable commercial return note reads frozen named outlet/supplier,
   product, batch, expiry and quantities. It is not an automatically issued GST
   tax document or regulatory certificate. Sensitive reads/print release are
   audited and private/no-store; user labels use names and roles, not subjects.

## Authority, concurrency and failure

Reuse tenant/outlet server authorization and existing system role templates.
Pharmacists/inventory staff may hold stock with existing stock-quarantine authority;
purchasing/owner return preparation and dispatch need declared return permissions
and a current exact-outlet editor grant. The existing platform reconciler also
adds tenant administrators to every registered permission; these permissions do
not exclude that implicit grant. Tenant administration alone still does not grant
exact-outlet access. Finance credit recording/allocation uses
financial authority. Route admission never substitutes for verified tenant,
current membership, narrower outlet grant or an independently required review.
An invisible object is 404, missing route permission 403 and unavailable authority 503. Receiver/fulfiller duties do not acquire these operations implicitly.

Stock, bill/credit values and private evidence are the protected assets. Threats
include unauthenticated callers, another tenant or outlet, stale switched accounts,
forged source documents, duplicate dispatch and concurrent credit allocation.
Validate each Global API/owner boundary, reject unknown input and supplied actor
mismatch, and bind receipts/recovery to the original actor and immutable intent.

Commands use UUIDv7 identities, original input fingerprints and exact receipt
lookup. Lock parent return rows, credit documents and payable/bucket keys in a
consistent order. Source receipt return limits and supplier reservations are
checked under the same serialization boundary as posting. Every command is a
single database transaction; broker publication uses the existing outbox. A lost
response is unknown and recovers its original result. PostgreSQL, audit or outbox
failure returns unavailable with no partial stock or financial evidence. No
network call or human wait belongs inside a transaction.

Use tab-local reference-only recovery for uncertain holds, drafts, dispatches and
financial commands, scoped to tenant/actor/pharmacy/outlet. Retained reads and
non-operating cancellation/recovery remain classified independently of operational
release gates. They cannot dispatch, allocate credit or bypass current authority.

## Tax and regulatory boundary

[CBIC Circular 72/46/2018-GST](https://cbic-gst.gov.in/pdf/Circular-72-46-EXPIRED-DRUGS.pdf)
describes alternative expired-medicine return procedures. Its historic deadline
wording is not a current universal rule. Consult current
[CGST Act section 34](https://taxinformation.cbic.gov.in/content-page/explore-act/1000304/1000001)
and accountable finance review before tax treatment. This slice records supplied
commercial facts; it does not choose a fresh-supply tax invoice, generate statutory
debit notes, adjust GST/ITC or post to a general ledger. The inventory proposal's
D6 statutory/accounting mapping remains unresolved for that later integration.
Expected amounts are explicit minor-unit agreements, not inferred from MRP or an
unknown cost basis. Do not activate unverified Schedule X/NDPS return authority.

Relevant cases: IN-PH-01/03/05 and IN-RC-02/04/05. Professional, licence, supplier
and drug-source evidence remains actual owner-reviewed evidence, never fabricated
by a test fixture in the live environment. Controlled-policy activation and
statutory mapping need their own review before availability changes.

## Delivery and acceptance

Canonical payloads belong in contracts/pharmacy.json; regenerate owning Go and
Zod models. Classify every route, page and nested panel in the release manifest;
preserve provider desired values and compiled release eligibility. Migrations are
additive with bounded lock/statement timeouts, forced tenant/outlet RLS, immutable
links and database posting guards. Rollback is forward recovery: preserve posted
stock and finance; do not drop evidence tables after use.

Acceptance must cover both ownership models, foreign tenant/outlet denial, stale
actor/version, quantity/reservation conservation, duplicate/conflicting commands,
partial and excess credit, paid bills/unapplied credit, combined supplier documents,
reversal/void, audit/outbox rollback, private printing, original-result recovery,
Global API traversal and actual browser journeys. Hosted databases/browsers and
local builds/checks are required; no local application/database/container is run.
The deployed revision and remaining live release gates must be recorded honestly.

### Draft scope

Each draft binds one original inventory payable, its frozen counterparty/currency
and one or more original receipt movements from that payable. Multiple bills use
separate returns; a later actual supplier credit document can cover multiple
returns without duplicating its amount. Draft quantities and explicit expected
credit are bounded, in issue units and the payable currency's minor units.
Creating a draft neither removes/reserves stock nor settles the bill. Physical
availability and cumulative receipt-return limits are rechecked at dispatch.

### Retained history workspace

A compact audited cursor collection and the accounts Supplier returns tab expose
existing drafts and frozen line evidence using human references, supplier and
product names. This view inherits pharmacy-payables release gating and separate
return-read permission. Cancellation uses the existing immutable command with
reference-only tab-local recovery before explicit retry. Source-choice/preparation
UI and physical dispatch are the in-progress additions described below. Accepted
supplier credit and printable outward notes remain unfinished delivery work. The implementation and acceptance boundaries
are specified in
[the history design](../superpowers/specs/2026-11-21-supplier-return-history.md).

### Physical dispatch contract

A new immutable dispatch command consumes the complete draft, with no partial
posting. Input binds draft version zero, a current reviewed outlet `licence_id`,
commercial `document_reference`, `recipient_name`, explicit RFC3339
`handed_over_at` (at most microsecond precision), `handed_to_supplier` physical
disposition and reason. The time cannot precede any original receipt or be in the
future. Recording time is separate. The licence review and current approved
product schedule declarations are retained as evidence; unknown, Schedule X and
NDPS classifications refuse dispatch pending their separate controlled policies.
Licence review establishes the existing outlet evidence only; it does not certify
supplier authority, a statutory document, tax treatment or a legal return policy.
Operational availability remains subject to the existing release review.

The dispatch receipt binds the original actor, exact intent, parent return number
and one dedicated negative `supplier_return` movement per draft line. The creation
receipt stays immutable. Current detail/history becomes dispatched/version one;
cancellation and dispatch are mutually exclusive. Recovery is a private audited
actor-bound read, including when Stock is unavailable; starting a dispatch remains
Stock-gated. Financial credit is unchanged by dispatch.

Lock order is parent return, reviewed licence, sorted product schedule keys,
sorted original receipt keys, then sorted quarantine location/batch keys. The DB
checks cumulative dispatched quantities against each original receipt under those
locks, and enforces the complete set of exact movement proofs at commit. Existing
stock bucket guards preserve reservations and nonnegative balances. Every proof,
movement, audit and outbox write commits together. This append-only migration is
irreversible after use; rollback retains evidence and uses forward correction.

### Original-bill preparation and recovery

`GET /accounts/supplier-returns/preparation/:payable` is a private, audited,
Stock-gated projection under the same pharmacy/outlet prefix. It requires return
preparation authority, verified tenant/subject preconditions and current outlet
access. One original bill owns at most 100 immutable lines. Missing line evidence
fails closed; paid bills remain selectable. The projection shares its receipt
lookup with command validation, retaining original product revision, issue unit,
batch, expiry and source facts. It reports actual dispatched units separately from
other drafts, and marks platform-supplier origins unsupported for the separate
reverse-acknowledgement protocol.

`stock_as_of` is issued by the database clock for the quarantine picker. The UI
uses that cutoff with the stock-position API rather than assuming the device clock
is correct. It can refresh the bill/stock observations, conceals failed or
inconsistent reads, and names products, lots and locations instead of asking for
backend IDs. Multiple lines require explicit whole units and expected credit,
including explicit zero. Duplicate receipt/bucket pairs are refused, not merged.
Already dispatched units count toward the original receipt allowance; other drafts
do not reserve it. All quantities and current authority are checked again by the
command and by final physical dispatch.

Before issuing preparation, the browser retains only the new return reference,
scoped to tenant, account, pharmacy and outlet. Response loss freezes new submission
until a check; a missing result can retry the same reference. No bill facts,
recipient details or monetary intent are persisted in browser recovery storage.
`GET /accounts/supplier-returns/:return/preparation` is a separate baseline,
private audited, actor-bound read of the immutable creation receipt. It can recover
after cancellation or dispatch and does not imply the return is still a draft;
current detail/history remains the source for current state. New preparation and
physical dispatch remain Stock-gated. The original read permissions and active
outlet grant still govern recovery after a write pause.

The bill picker uses `GET /accounts/supplier-returns/original-bills`, a dedicated
Stock-gated, actor-bound, audited cursor page capped at 50. It selects bills with
inventory receipts regardless of payment status and publishes only original bill
identity, supplier name, issue date and currency. It does not enumerate unrelated
salary/expense bills or depend on an unaudited generic financial listing.

### Private commercial return note

`GET /accounts/supplier-returns/:return/note.html` releases one complete printable
commercial record for a dispatched return. It requires current return-read
permission, the exact active outlet grant and verified tenant/actor preconditions.
It is baseline retained evidence, so withdrawing Stock does not prevent an
otherwise authorized operator from reading an already completed handoff. No
provider value or compiled release eligibility is activated by adding this read.

Additive migration `0057_pharmacy_supplier_return_note` retains pharmacy/outlet
names and outlet code at dispatch in the existing immutable dispatch row. A
database trigger derives those values from the same tenant's current directory;
caller-supplied snapshots cannot override them. Old dispatches without historical
snapshots are not backfilled with guessed names and refuse note release. The
migration is forward-only: rollback retains the snapshot columns and evidence.
Directory rows remain immutable; the snapshot also protects against future
approved metadata evolution. Hosted metadata-change injection tests this
boundary without adding application rename authority.

The note rechecks complete dispatch/movement proof and uses frozen supplier,
original bill, product, batch, expiry, issue units, quantity and handoff facts.
The read transaction renders all rows and commits its disclosure audit before any
file headers or bytes are released. Missing evidence, audit failure or timeout
returns no partial document. The inherited planning budgets apply: at most 100
rows, 1 MiB, a 15-second deadline, and the unmeasured bounded-read latency target.
No new service, storage allocation or infrastructure is introduced.

The UTF-8 HTML attachment has escaped dynamic text, restrictive HTTP and embedded
content policy, no scripts, forms, images or external resources, and print styles
for A4 with repeated table headings. It is explicitly not a tax invoice,
statutory debit note, accepted supplier credit, settlement or regulatory
certificate. Download neither moves stock nor changes a financial balance.

Response metadata binds tenant, outlet, return, human number, row count, exact
uncompressed byte count and SHA-256. The SDK verifies scope, private/no-store
caching, MIME, disposition, size and digest before creating a download. The Shell
also verifies a response policy containing both bare `sandbox` and
`default-src 'none'`; multiple gateway/owner policy fields form a comma-separated
policy list rather than one directive list.
The Shell
aborts on unmount, refuses duplicate/in-transition downloads and releases its
temporary URL. An authorization refusal conceals private error details and needs
an explicit access recheck; it never falls back to a cached file.

### Retained supplier credit ledger (in progress)

Actual supplier documents are distinct from expected return credit, legacy manual
bill adjustments, bank receipts and customer credit notes. A received document
anchors to one dispatched return for the frozen supplier/currency and original
bill context; it does not automatically match that return or settle its bill.
The operator supplies the actual document reference, date, positive INR paise and
reason. Supported commercial document dates start in 1970, cannot precede the
anchor bill and cannot be in the future on the outlet's authoritative local date.
These are application input bounds, not a statutory tax decision.

Additive migration0058 owns immutable document and void records plus a guarded
current-position projection in PharmaConnect. The projection starts at version1
with no matches or allocations. Only nested owner ledger triggers can update it;
deferred proof reconciles its version/state/amounts with immutable evidence.
Direct edits, deletes and truncation cannot erase or manufacture credit. All
tables retain forced tenant/exact-outlet RLS, actor checks and restrictive
operating-purpose policies; stock duties do not inherit financial authority.

The supplier's normalized reference is unique in its outlet/counterparty context.
Duplicate-reference admission serializes independently of command IDs. Correction
requires an explicit link to the same supplier's unused, unmatched voided record;
one replacement advances the old record to `corrected` while preserving it. A
new record carries the corrected amount. Original command receipts are actor-bound
and immutable even after void/correction; current state is a separate read.

Financial commands use `pharmacy.accounts.adjust`; retained reads use
`pharmacy.accounts.read`, verified tenant/actor preconditions and active exact
outlet access. Audit, position evidence and the minimal supplier-credit outbox
notification commit together. Read audit failure releases no private result.
Reference-only recovery never stores credit values or supplier details locally.
The existing pharmacy-payables operational family remains release-ineligible.
The seven document/read/void/recovery routes are registered and classified in the
release manifest. Hosted native document acceptance passed after the observed
missing-route baseline; full CI, Staff-finance review, UI and live release gates
remain required. Canonical payloads alone are not integration evidence. The
identifier-only event fields have explicit privacy classifications; the event
conveys no amount, supplier details, document reference, actor or private reason.

The next candidate implements explicit partial/combined return matching and full
match reversal through migration0059 and seven registered routes. It preserves
net amount, version, actor and terminal-state proof across all document commands;
new native acceptance remains pending after the observed missing-handler baseline.
See [the matching design](../superpowers/specs/2026-11-21-supplier-credit-matching.md).
Safe eligible-bill allocation, allocation reversal and the read/selection UI remain
required commands.
Matching attributes commercial credit; allocation separately consumes the pool.
The existing payable-balance view must include net managed allocation alongside
legacy settlements, and both allocation and payment posting must serialize on
the same payable key. Retained credit on a fully paid original bill stays
unallocated. No stock-cost, GST/ITC or general-ledger effect is inferred.

### Supplier-credit allocation candidate

Migration0060 and seven registered handlers add allocation/full reversal to the
existing financial owner. It preserves original receipts and uses credit-then-
existing-payable locks, observed credit/bill versions and available/outstanding
balances. Both the payable balance view and existing payment/manual-credit guard
include net managed allocation. No v1 payable fields or settlement kinds change.
The deferred credit proof includes every allocation/reversal while preserving
matching, void and correction evidence. Target bills must have external inventory
sources of the same supplier/currency/outlet; platform receipts remain outside
this local-credit protocol. See the [allocation design](../superpowers/specs/2026-11-22-supplier-credit-allocation.md).

The actual hosted baseline observed expected200/actual404 after real paid-source,
credit and future-bill setup for all ownership models (44907683241/job136856646618).
Current wired SQL/handler behavior is pending hosted execution. Source selectors,
financial UI/SDK, expanded concurrency/rollback/report checks and live acceptance
remain required; these journals alone do not complete #1784 or activate payables.

### Correction entry in Accounts

The candidate replacement control starts from the current finance context of a
named voided note. It binds the original note and dispatched return internally;
it cannot select a supplier by name or accept raw identifiers. A replacement
enters its own reference, date, amount and reason explicitly. Existing owner
commands enforce same supplier/currency, unique correction, source validity and
the original voided state atomically. No new service, query, data master, endpoint
or infrastructure is introduced; existing one-command deadlines, bounded payloads
and financial-read/write release gates apply, with no deployment cost change.

Reuse the actual-credit form and canonical input rather than maintain a second
note schema. Isolate correction recovery by original note as well as actor/outlet,
validate recovered linkage against that original even after reload, and keep
recovery readable after the original becomes corrected or new writes pause.
Unknown outcomes retain the command identity without automatic retry. Current
disclosure failures hide the form; stale state refuses without changing history.
An application to another bill remains a separate command after replacement.
Tests must cover explicit linkage, wrong-original recovery, lost committed response,
withdrawn writes and unchanged stock/payments. Hosted and live acceptance remain
required before release; this UI does not establish statutory credit-note policy.

### Bill-scoped supplier-credit journal

Accounts reads `GET /payables/:id/supplier-credit-allocations` from PharmaConnect
through Global API. Reuse the canonical allocation view and retained reversal
identity; do not scan all credits client-side or create a second settlement master.
Verify the bill in the current tenant/exact outlet and validate retained evidence.
Each page requires a committed disclosure audit and current accounts-read authority,
including when new operating Payables admission is paused. No new write authority
or money movement is introduced. Wrong/missing bill scope is 404; unavailable
identity/audit releases no rows. Use sealed financial keyset cursors and a 50-row cap.

Add forward concurrent index migration0062 for tenant/outlet/bill/created_at/id
ordering. Existing RLS, deadlines and pool budgets apply; no infrastructure/cost
allocation change. Hosted migration/native tests and actual EXPLAIN evidence are
required; a small synthetic plan is not proof of large-cardinality performance.
The bill UI separates direct settlements from managed applications, displays owner
totals rather than partial-page sums, hides refused cached evidence, and uses
human references/named recorders instead of receipt/reversal UUIDs. Extend the
hosted journey to inspect bill history after application and paid-bill reversal.
