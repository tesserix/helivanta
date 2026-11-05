# Pharmacy department binding implementation

Branch `codex/pharmacy-department-bindings-20261105`, based on source-use candidate
`dd9e1b0a` in PR1580. Full reusable Staff scope remains active.

## Sequence

1. Verify the [binding design](../specs/2026-11-05-pharmacy-department-bindings.md)
   against actual owner ports, outlet grants and canonical IAM contracts.
2. Observe a failing real owner-route test for independent binding preparation.
3. Add the exact canonical product template and input/response contract, additive
   migrations, current source admission and immutable binding command/receipts.
4. Extend actual IAM/OpenFGA/Global API positive, independent, stale, scope,
   fault and lock-controlled concurrency cases; do not substitute mocked owners.
5. Add generated SDK and canonical-choice Staff UI with RED→GREEN coverage and
   normal-account browser acceptance. Keep professional and operating authority
   visibly separate from department relationship review.
6. Run affected full suites/static checks/generators, update the progress tracker,
   commit/push a bounded PR. Merge/deploy only after the applicable acceptance and
   exact named rollout/migration gates; preserve all five parent requirements.

## Current evidence and unresolved work

Source-use PR1580 at `dd9e1b0a` is published and all exact-head CI checks pass.
Native Chrome normal owner, separate approver and ordinary reviewer login work.
The reviewer is correctly denied review before the one-day proposal is approved.
Specific action-time grant confirmation is pending; no new reviewer access is
active. This does not block unactivated implementation of the hospital relation.

Disposable integration databases apply candidate IAM0029 and Pharmacy0034;
neither migration is applied to the browser QA fixture or production. The
closed IAM catalog introduces exact separate relationship preparer/reviewer
roles, current Staff-actor use and canonical department choices, with no
clinical, operating or private HR permission. Historical migrations are frozen.
The registry-manifest test failed before IAM0029 was registered, then passes.
Both role tests were observed failing before their corresponding policy was
implemented. Full template-history suite passes **60 tests /15.296s**.

Actual IAM/OpenFGA/Postgres through Global API passes proposal preparation and
exact/changed-content recovery **34.750s**, then independent review, stale
department refusal, self-review refusal and original receipt preservation
**64.576s**. Replacement ends the old relationship and activates the new one
under one retained command/event, without rewriting original approval receipts.
Audit and outbox fault tests exposed incorrect409 classification; distinct
evidence-unavailable503 and complete actor/business rollback pass **66.928s**.
Retained approval/proposal replays initially disclosed data with read auditing
unavailable (expected503, actual200). Those retry paths now require disclosure
audit, and receipts retain the exact independently approved action/Staff-source
assignment/version/epoch observations. The complete focused journey passes
**68.613s** after those corrections. These are backend tests, not browser
acceptance or whole-Staff completion.

The owner route was observed missing (expected503, actual404); its real no-owner
admission now fails closed. Missing/changed account-intent tests fail before the
mandatory expected tenant/subject preconditions are added; the updated admission
suite passes **5.563s**. Earlier wrong harness names, wrong outbox column, wrong
error-field assertion and duplicate generated identifier runs are failures,
corrected without weakening product validation. Reviewed actor-consumer and
permission matrix checks pass **1.709s**; scoped lint reported0 before the later
authorization-observation additions. Two current SDK contract tests pass; final
broader/static runs remain required. Bounded browse/history was observed
missing (expected200, actual404), then the complete real-owner journey passes
**71.496s**, including keyset continuation, strict query bounds and immutable
history authorization observations. Browser acceptance remains open.

The workspace must use purpose-limited canonical hospital/outlet choices. The
new roles deliberately do not read the private Staff directory or commercial
catalogue. A reusable Workforce actor-hospital projection under current actor-use
admission, and minimal Pharmacy outlet choices under relationship/explicit outlet
admission, are needed for the ordinary role UI; do not fill the gap with typed
UUIDs, a copied hospital list or broader default grants. These are implementation
tasks in the full vertical acceptance, not reasons to drop the UI requirement.

No production policy or real account grant changed. ADR0054 acceptance remains pending for standalone employers.
All existing operational/Staff acceptance gaps stay in the shared tracker.

## Reconnection and continuation evidence

The IAM Pharmacy family route was observed returning404 for both new templates;
its bounded family list and real IAM/OpenFGA projection/withdrawal journey now
pass **77.608s**. SDK routing initially requested /api/v1undefined for that family;
both role cases now pass, and the full SDK suite passes **741 /82 files**.
The Staff access UI exposes only the exact Pharmacy catalog and fails closed for
unavailable, extra or widened definitions. Its complete page suite passes
**60 tests /14.32s**. Both API and shell typechecks pass, affected ESLint passes,
and scoped Go lint reports **0 issues**. Go vet and build pass. Root template and
operational-inventory tests pass **75 /38.953s**. The configured webpack production
build passes; existing middleware/React Hook Form re-export warnings remain.
The first build invocation omitted the required Global API origin and failed;
the successful build explicitly uses the existing local Global API8198.

Expanded real-owner disclosure tests pass **128.901s**: sibling organization/
outlet paths hide records, and audit failure or revoked outlet access withholds
collection/history/receipt responses and rolls back successful Staff evidence.

The initial full IAM/Pharmacy/architecture run failed. Findings include a Temporal
test deadlock, an IAM expiry assertion, missing single-object pagination
exemptions, unclassified event fields, missing scoped-role matrix exemptions and
Staff actor resolution outside module wiring. The architecture corrections pass
the focused gate **2.894s** and full architecture **85.566s**. The licence
workflow passes unchanged in isolation **7.689s**. IAM expiry fails in a first
isolated run then passes **37.360s** with failure-only clock diagnostics added;
its cause is not established, so repeated verification is running without
weakening its assertion. Do not replace these failures with an all-green claim.

Full shell tests pass **1,563 /183 files /455.82s**. The full Global API suite passes **571.248s** on the corrected owner source,
including the complete pre-employment/posting and scoped leave journeys. The staged secret scan finds no leaks. Inventory regeneration retains **831 surfaces,
808 unclassified**; the three IAM source pins were reviewed against the additive
migration registration change and refreshed without classification promotion.
The exact Pharma binding workspace, purpose-limited choices,
writer/concurrent-command orderings and ordinary-role browser acceptance remain
open. Source publication is a draft review increment. Merge, production migration and
activation gates remain open.

### Typed reader follow-up

Published source increment **906867d4** is draft
[PR1583](https://github.com/tesserix/helivanta/pull/1583), stacked on1580;
it remains unmerged and undeployed. Five subsequent IAM expiry runs pass
**151.211s** with failure-only diagnostics; the earlier failures remain unexplained.

The typed SDK now reads exact detail, bounded relationship pages and history.
It uses Global API, expected account/tenant intent, no-store, caller cancellation
and transition cancellation. Late responses are discarded even if the transport
ignores cancellation. It verifies scope/identity, paired replacement fields,
decision/state consistency, source-state applicability, duplicate IDs irrespective
of case, pagination continuation and historical authorization observations.
Historical observations cannot authorize new commands. Existing canonical Staff
UUIDs remain supported; the reader does not demand a newly created Staff ID.

Observed RED evidence includes nine hostile/cancelled responses initially returned,
13 inconsistent paging/history/proof responses initially returned and three
actor/identifier consistency defects. All **45 reader tests** now pass. Full SDK
passes **786 /83 files /9.90s**, API/shell typechecks and reader ESLint pass.
Exact source/outlet choice APIs, mutation/receipt client, binding workspace,
concurrency/source-writer acceptance and ordinary-role browser acceptance remain
open; this is a verified reader, not full binding or Staff completion.

### Mutation/recovery client continuation

The typed SDK now prepares immutable proposals, submits approve/reject/withdraw/
end commands and recovers the exact attempted receipt without resubmission.
It snapshots normalized intent/account context before awaiting transport,
requires distinct proposal/decision command identities and verifies target scope,
actor, action, expected/result versions, private reason, source references,
replacement transition and microsecond evidence. Equivalent timezone offsets
are accepted. Input requires approval-only attestation; the canonical receipt
retains operation and replacement evidence, not a copied request attestation.
A third authorized reviewer may end a relationship without rewriting its original
approval.

Initial seven unimplemented-operation tests were RED. Initial receipt fixtures
incorrectly included request attestation and omitted the nullable replacement
receipt fields; these fixture/typecheck failures were corrected against generated
contracts and the actual owner, not by changing the wire format. A reused proposal
command ID was observed reaching transport before a preflight refusal was added.
Two real replay-history purpose cases were observed rejected: audited replays
retain prepare/review authority instead of converting it to read authority.
The reader now validates those exact recorded purposes while still requiring
one relationship permission plus current-actor observation; it does not treat
historical observations as reusable grants.

Focused reader/command coverage passes **103 tests**. Full SDK passes
**844 /84 files /14.62s**, API/shell typechecks, affected ESLint and the configured
webpack production build pass. Commands actually run: API vitest full suite,
API/shell tsc --noEmit, affected API eslint, prettier, and shell next build
--webpack with the existing local Global API origin8198. No Go owner behavior,
migration or permission changed in this continuation.

Native Chrome reconnected; the existing ordinary reviewer still correctly sees
no approved posting review hospitals. Private evidence is saved in the existing
QA evidence directory. Its pending one-day grant was not approved or activated.
The immutable browser fixture still does not include this new binding candidate.

The next owner endpoints are now specified in the binding design: reusable own
Staff-actor hospital choices and minimal explicitly authorized Pharmacy outlet
choices. They, the binding workspace, source/outlet-writer and competing-command
orderings, ordinary-role positive browser acceptance and full operational
lifecycle enforcement remain open. All five parent requirements and the shared
21-area acceptance counts remain unchanged.
