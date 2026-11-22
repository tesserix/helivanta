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

Use the existing sealed-count list/header and original count/decision receipt
reads as baseline continuations. They expose retained evidence only and retain
`pharmacy.stock.read`, current outlet authority and read audit. Snapshot-line and
register-file reads remain Stock-gated. Add a separate cancellation-only command
path using the existing decision input/receipt and `pharmacy.stock.correct`;
reject every decision other than cancel before dispatch. Do not classify the
general decision endpoint as baseline. A cancelled/terminal count can return an
original exact receipt but cannot accept a new operational command. The same
UUID/actor/input identity and sealed-parent checks apply. This adds no permission,
policy exception, stock movement or new data owner.

Unknown transport outcomes must survive a page reload or a release-boundary
unmount. Retain at most eight recovery markers in tab-local session storage,
keyed by verified tenant, actor, pharmacy and outlet. Markers contain only kind,
stocktake UUID and command UUID; never quantities, reasons, names or credentials.
Persist and verify a marker before dispatch; if persistence is unavailable, do
not send the command. Exact retries reuse it. Successful confirmation clears only
the matching marker. The retained-work page (`/pharmacy/stocktake-recovery`) uses the existing
`pharmacy_setup` capability and `pharmacy.stock.read`, independently of Stock and
Reports. Ordinary stock staff do not need access-management authority. It reuses
bounded organization/outlet selectors and the current outlet grant check; no
new service, data owner or business API is introduced. Its existing reads remain
paginated, private and fail closed on unavailable identity or authorization. The
same panel also lives in the opened outlet detail in **Outlets & access** for
managers. There it binds to that opened outlet, not the parent working-outlet
selection. It can inspect original outcomes even with Stock off and verifies the original actor before acknowledging a result.
An explicitly dismissed not-found result remains unknown, not a claimed failure;
refresh current state before further commands. Existing active-count uniqueness,
version, terminal-state and command-identity guards still decide every race.

Bind stocktake SDK requests and query/recovery state to the verified actor as
well as tenant/outlet. Send the standard expected-subject precondition and reject
a mismatched supplied subject before product access. Storage is a recovery hint,
never authority. A different account cannot adopt a marker or silently reuse its
pending input. Reads, cancellations and any subsequent operation reauthorize on
the server. This follows the existing Staff MFA recovery-marker pattern without
introducing a new persistence service or copying private inputs into storage.

## Failure, consistency and recovery

### Snapshot validation at the declared bucket bound

The first isolated hosted capacity run created 10,000 stocked batches through
normal APIs, but starting their count returned HTTP 500 at the existing ten-second
deadline. A second run measured 1.46 seconds for the book query and roughly
0.75 seconds for each 500-line insert. Repeated per-row snapshot validation was
still running when the deadline expired; no count was committed. These are
observations from hosted synthetic acceptance, not production latency figures.

Migration `0064_pharmacy_stocktake_snapshot_validation` replaces the snapshot
line's row trigger with an AFTER INSERT statement trigger over its transition
table. It locks every affected parent in tenant/outlet/count order and rejects
the entire statement if any parent is missing, sealed, belongs to another actor
or has another snapshot time. A set join verifies every batch's product, lot and
expiry. Existing RLS, foreign keys, row audit, immutable-line rules and deferred
seal/commit checks remain authoritative. A malformed row rolls back the complete
statement, including its audit writes; there is no accepted unchecked interval
outside the transaction. The API, snapshot source, page bounds and deadlines
remain unchanged. Retaining per-row validation was rejected after its measured
failure at the already declared limit; increasing deadlines would not meet the
five-second planning target.

Further profiling measured a 500-line insert at 188 ms: 43 ms in snapshot
validation, 27 ms in 500 audit-trigger calls and 55 ms in foreign-key checks.
Parent validation now groups inserted rows by parent and checks both the minimum
and maximum creation time against that parent's frozen timestamp. Audit writes
are set-based but retain one insert event per line, the verified actor and the
same empty changed-fields list. A missing audit actor still aborts the statement.
Foreign keys remain unchanged.

The snapshot-line tenant policy evaluates its existing outlet grant function as
an uncorrelated subquery, once per statement. Every row must still equal the
selected tenant and outlet; read visibility and editor requirements are retained.
This caches no authority across commands or transactions. It does not change
operating-purpose policies, roles, permission names or grant/revocation semantics.
Regression cases include a writer granted two outlets, missing selected scope,
another tenant, an ungranted reader, and a previously privileged writer whose
grant was downgraded before the next statement. These checks accompany the
malformed-statement and audit-cardinality cases.

The subsequent hosted profile returned HTTP 200 for 10,000-bucket start in
2.14 seconds. Its first 500-line insert measured 49 ms, including one audit-trigger
call instead of 500; the book query itself was unchanged. These isolated
measurements are not a production latency claim. The write-replay profiler is
removed from the final acceptance test; its read-only plan and strict audit
checks remain. The final workflow must still prove every gate without write
instrumentation before merge.

The migration changes no stored rows and uses a five-second lock timeout. Its
trigger replacement is transactional; a failed migration retains the old guard.
An older API uses the same inserts and remains compatible. Any later restoration
of the old trigger must be a separately reviewed forward migration; do not edit
an applied migration or drop snapshot history. The hosted regression must prove
batch/actor/time/seal refusals and complete rollback before this change can roll
out. Capacity results and live Staff/release acceptance remain separate gates.

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

## Current-read failure handling

The stocktake detail must hide previously loaded header, bucket, classification
and export/action surfaces when either current header or bucket retrieval fails.
Current refusal takes precedence over a concurrent dependency outage. A 401
requires sign-in; an authorised access recheck/retry refreshes both reads before
private detail returns. A header denial prevents new dependent bucket reads. This
preserves the existing behavior for loss of correction authority alone: an
authorised reader may retain an open entry, but saving/review stays paused. The retained-status view also hides its previous header on read failure. A failed
quarantine-location read conceals the selected location label while preserving
operator-entered start intent for a fresh authorised retry. None of this adds
permissions, changes the API contract or activates Stock.

Use the [stocktake acceptance guide](../qa/pharmacy-stocktake-acceptance.md) for
applicable hosted and live evidence. Unexecuted live cases remain pending; a
healthy deployment or a passing source check is not operational sign-off.
