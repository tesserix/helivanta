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
and a current exact-outlet editor grant. Finance credit recording/allocation uses
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
