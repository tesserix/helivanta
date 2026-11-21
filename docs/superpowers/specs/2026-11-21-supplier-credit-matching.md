# Supplier-credit matching and reversal

Development decision for #1784; not a deployed API or release activation.
Extends [ADR-0075](../../adr/0075-pharmacy-supplier-returns.md). PharmaConnect owns
the immutable actual credit document, dispatched return, matching journal and
current position. Accounts UI/SDK consume them through Global API. No other
module reads or writes these tables.

## Scope and authority

Keep actual document value, commercial return attribution and bill allocation
distinct. One document can be matched in explicit partial amounts to several
dispatched returns. Several documents can contribute to one return. Never copy a
document or its entire value for each return. Matching alone does not spend its
unallocated pool, settle a payable or change physical stock. Allocation and its
reversals remain the next required owner commands.

Use the existing accounts-adjust permission for writes and accounts-read for
private retained evidence. Both require verified expected tenant/actor headers,
current membership, exact active outlet authority and existing operating-purpose
fences. Writes are payables-gated; retained recovery is baseline but never bypasses
current authorization. There is no new role, Staff authority or independent
review rule. ORG access boundaries and IN-RC exact-money/source-history principles
apply; this is supplier finance, not patient revenue or a regulatory tax posting.

## Commands and retained evidence

A match binds a UUIDv7 command, credit, dispatched return, positive integer INR
minor units, current credit `expected_version`, the explicitly observed total
`expected_return_matched_minor`, reason and optional nonempty `overage_reason`.
Match the same outlet, supplier reference and currency as the actual document.
The new credit matched total cannot exceed its actual amount. The new return
matched total cannot exceed 10^12 minor units; if it exceeds expected return
credit, require explicit overage rationale rather than silently capping or
automatically accepting the excess. Zero expected return credit is supported.

Store original return number, expected credit and resulting return matched total
with the immutable receipt. The command advances the credit version once. It
does not revise the original return/credit creation receipts. Current credit
positions and match views remain separate from original actor-bound receipts.

A full match reversal names the original match, current credit version, observed
return matched total and reason. It subtracts exactly that original match once,
preserves both entries, advances the credit version once and frees attribution
for correction. No physical goods or payment is reversed by this operation.
Partial correction is full reversal followed by a new explicit match. Void of
the actual document still requires zero net matching and zero allocation.

Same actor/command/intent replay returns the original receipt after later changes.
Another actor or changed intent conflicts. Actor-bound recovery discloses only the
original command; normal authorised history can show other recorders. History
pages include reversal identity so the UI need not make one request per entry.
No monetary or supplier data is persisted in browser recovery storage.

## Consistency, bounded work and failures

Acquire the credit advisory lock, then the target return-attribution lock, and
recheck both observed values. Different documents targeting the same return
serialize on that return key. Reversal follows the same order. Each command
touches one credit and one return, avoiding a multi-resource batch/deadlock cycle.
Original dispatch/movement proof is required; draft/cancelled/foreign sources fail.
Immutable source snapshots survive subsequent directory changes.

Additive migration0059 owns matches and reversals, forced tenant/outlet RLS,
immutable/audit guards and indexes. Only nested owner-ledger triggers update the
credit projection. Extend its deferred reconciliation to count all immutable
commands, net match amounts and the latest version's actor, retaining existing
void/correction proof and requiring allocations to stay zero until their ledger
exists. Do not disable the proof or make direct projection updates possible.
Audit, journal, projection and minimal identifier-only outbox notification commit
together. No synchronous external call occurs under locks.

Reuse ADR planning numbers: 20 peak commands/s, 128 KiB request ceiling,
15-second deadline, p99 targets 500 ms bounded reads/2 s commands (unmeasured).
Each cursor page is capped at 50, with tenant/outlet/credit indexes and no offset.
Inherited 12/36-month return volumes are 3.65/32.85 million lines, not forecasts
for measured traffic. No new service, datastore, worker or infrastructure cost.

Invisible source/recovery is 404; missing permission 403; stale observations,
duplicate/conflicting intent, overmatching or missing overage reason 409.
Dependency/audit/proof failure is 503 with no partial evidence or result. After
uncertainty recover the original reference before retry. Append-only evidence
cannot be down-migrated after use; roll forward while retaining schema/history.

## Acceptance

Observe hosted missing-endpoint failure before wiring. Prove partial and combined
matching, multiple credits on one return, excess rationale, stale totals across
credits, source supplier/currency/outlet and complete dispatch, duplicate/changed
intent, full reversal once, immutable original recovery, version/net/actor proof,
tenant/actor/revocation denial, audit/outbox rollback and bounded private history.
Then add SDK and real Global API/browser journeys, build locally without apps or
databases, and pass hosted native/race/web gates. Production rollout and positive
live acceptance require the existing named approval and release review.

Rejected: legacy manual bill credit, because matching is not liability settlement;
automatic matching on record, because partial/combined attribution is explicit;
deleting matches, because correction must preserve commercial evidence.
