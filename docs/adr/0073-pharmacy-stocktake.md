# ADR-0073: Reviewed pharmacy stocktakes

Status: implementation design for [#1686](https://github.com/tesserix/helivanta/issues/1686).
This document does not enable Stock or certify a statutory register.

## Context and scope

PharmaConnect owns outlet batches, locations and the append-only stock ledger.
SupplyConnect stock remains a separate owner. Both hospital-owned and independent
pharmacies use the same outlet-scoped contract through Global API. Existing
`pharmacy.stock.read` and `pharmacy.stock.correct` permissions and the current
outlet grant are required; hospital ownership alone grants no stock authority.

Planning assumptions, not measured deployment claims: one active count per
outlet, up to 10,000 batch/location buckets, 100 count entries per command,
500 nonzero adjustments per approval and pages of 50 rows by default, at most 200. Commands
are also bounded to 64 KiB. A 3,000-product outlet averaging two buckets uses
6,000 snapshot rows. At 10 concurrent large counts, bounded application working
data should remain below 100 MB. Plan for 10 count commands/s across tenants,
read:write 10:1, roughly 4 KB commands and 50 KB pages. At 200 outlets and monthly
counts of 6,000 buckets, snapshots alone add 14.4 million rows/year; index by
tenant/outlet/count rather than scanning history. Review at 600 outlets/36 months.
Planning targets are 99.9% monthly availability, p99 below 300 ms for a page and
five seconds for start/post under these bounds. Start commands have a ten-second
request deadline. These targets need measured
database evidence before activation. Oversized requests fail explicitly.

Relevant contracts: `docs/api/domains/pharmacy.md`, `docs/api/http-contract.md`,
`docs/api/global-connector.md` and `contracts/pharmacy.json`. Domain controls:
IN-PH-01/03/05/06, ORG-AU-07 and ORG-AC-01/02/07/08. Maharashtra and Odisha use
the existing reviewed product classifications; this change invents no drug list,
legal retention period, professional designation or tax rule.

## Decision

1. Start an outlet count for all products or an explicit bounded product selection.
   Capture book quantities and movement counts per batch/location in one database
   snapshot, including the original product unit/version and reviewed schedule
   declaration. Later queries never recompute the book quantity.
2. Record counted quantities as immutable, versioned commands. Missing differs
   from zero. Each command requires a reason. Lock the submitted buckets and
   reject any whose movement count changed since the book snapshot, including
   edits to an earlier count. Every command binds its UUIDv7,
   original actor, expected version and normalized input; changed reuse conflicts.
3. Submit only when every captured bucket has a count. Submission freezes the
   reviewed revision. Reject/cancel preserves the count and evidence. A new count
   is needed after cancellation; there is no destructive restart.
4. Approval requires a current authorized person other than the starter and every
   person who recorded counts. No UI control substitutes for the server and DB
   checks. A stale expected version refuses the command. A count is a sealed
   physical observation: subsequent movements remain valid and the approved
   difference is applied to the then-current balance, under its existing guards.
   A shortage that would consume reserved or unavailable stock is refused.
5. Post all nonzero differences atomically as linked `correction` movements,
   with their audit and durable outbox records. Existing correction totals retain
   their meaning. No new balance master and no financial posting are introduced.
   Retries recover the original receipt, including after later count commands.
6. Negative differences remove units from their counted location and retain the
   existing balance/reservation guards. Positive differences go only to a named
   quarantine location. Extra units found on a sellable shelf must be physically
   moved there and attested before approval; they cannot become sellable through
   a count. Only the existing reviewed release can make them available.
7. Retain H1, X and NDPS discrepancies separately from ordinary discrepancies in
   the count register and its audited CSV export. Unknown classification is
   explicit. Retain the source declaration and exact quantities; no automatic
   controlled-drug filing, disposal, release or legal approval follows.

Expose bounded count/list/detail/line/history/recovery contracts and an audited
stocktake register export under the existing Stock capability. All new entry
remains dark with `pharmacy_stock`; eligibility and provider values are unchanged.
Use human stocktake references within the selected outlet and shared named-account
components for actors. Backend command and subject identifiers stay internal.
Existing-count status, original-command recovery and cancellation need narrowly
classified baseline continuations before operational activation. They must verify
an already sealed count and current authority; they cannot start a count, record
new counted quantities or approve stock adjustments while Stock is disabled.

## Failure, consistency and recovery

The count, count revisions, approval, adjustments, audit and outbox are local to
the pharmacy database. A single transaction decides posting. Unavailable current
authority, failed audit/outbox writes, stale count input and failed reservation
checks leave no adjustment committed. Unavailable messaging delays outbox delivery and
does not repeat stock. Transport failure after commit is recovered by the same
actor and command; another actor cannot adopt it. Reads reauthorize the exact
tenant/outlet. Out-of-scope is 404, missing route permission 403, unavailable
authority 503, and stale/changed intent 409. No human wait holds a transaction.

New stock outside the frozen buckets is not silently added to the count. The
register identifies its snapshot time and scope; a later count covers later
stock. Movement before a count is accepted invalidates that count input even if
net movement is zero. Movement after an accepted count is retained: for example,
book 100, counted 90, then sale 20 and adjustment -10 yields 70, preserving the
sale. More than 500 differences requires smaller product selections, never a
partial post. Reservations cannot be consumed by a shortage adjustment.

## Delivery and alternatives

Use an additive, forced-RLS migration with immutable evidence, narrow DB guards
and the existing checked operating-choice policy inventory. Migration rollback
is forward recovery: retain completed counts and ledger facts; do not delete
posted stock to undo a deployment. A pre-activation code rollback leaves the
additive tables unused. No new service, queue, datastore or worker is needed;
runtime cost is database rows/indexes and the current outbox. Measure storage
and query plans in the hosted database harness. Do not run a local app/database.

A direct balance overwrite loses history and is rejected. Reusing unrestricted
sellable corrections bypasses quarantine and is rejected. A distributed saga is
unnecessary for this single-owner atomic command. Reconciliation totals alone
cannot represent a physical count or independent approval.

Acceptance includes both ownership models, selection/all snapshots, zero/missing
counts, stale revisions and movements, different input/actor reuse, concurrent
approval, audit/outbox failure, exact retry, reservations, quarantine destinations,
unknown/H1/X/NDPS grouping, export audit, revoked and cross-outlet/tenant access.
Unit/contract, hosted PostgreSQL and browser tests plus local builds are required.
Keep #1686 open until deployment and applicable live acceptance are evidenced.
