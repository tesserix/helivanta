# Posting cutoff implementation plan

Status: owner command/receipt, administrative discovery/context/history and cutoff
workspace implemented locally. Lifecycle-aware positive review, operating
consumers and complete vertical acceptance remain open. Follow
[the specification](../specs/2026-11-05-posting-lifecycle-cutoffs.md) and
[ADR-0058](../../adr/0058-retained-posting-cutoffs.md). Deliver the owner workflow
as one vertical feature, then integrate actual operating consumers individually.

1. Pin the public wire models/selectors, failure codes and client compatibility
   behavior. Recheck migration numbers; extend canonical inputs and generate
   Go/Zod/SQL. Preserve every older closed IAM template/catalog snapshot.
2. Observe a real Global API test fail for a current independently authorized
   cutoff command. Add forced-RLS immutable cutoff/head storage and exact scoped
   owner routes, command/receipt recovery, source/audit/outbox consequences.
3. Extend owner current-source traversal with target/ancestor cutoffs, lock and
   commit deadlines. Prove both race orderings, stale target revocation, scheduled
   boundary behavior, replay and preserved approval history. Inject persistence
   faults; verify complete rollback. Prove an absent cutoff guard is caught.
4. Add lifecycle-aware discovery/context and strict SDK command/recovery helpers.
   Test exact version bindings, deadline minimum, linkage, old-selector refusal
   and compatibility of unchanged historical receipts.
5. Add normal Staff administrative cutoff/history UI and independent reviewer
   lifecycle context. Test required fields, scheduled instant confirmation,
   unknown command retention, identical retry and scope/revocation clearing.
6. Run full affected tests/checks/builds, migration RLS/immutability tests,
   representative indexed plans, generation/contractdocs/inventory parity and
   secret scans. Perform real local admin/reviewer UI journeys and retained
   persistence checks; keep unperformed evidence explicit.
7. Commit/push a bounded feature PR, reconcile the existing approval/discovery
   stack, and record exact CI/browser evidence before merge readiness. Prepare
   GitOps freight and a migration manifest. Request production approval only for
   the concrete final revision/migrations; verify deployed images, ledger and
   normal-role smoke behavior before tagging that release.
8. Integrate the first actual Pharma operating command with canonical live
   posting/actor references and product-owned unit/credential checks. Repeat
   positive-role, ancestor/local cutoff, suspension/exit/rehire and dependency-
   failure acceptance there. Do not count owner-only coverage as product QA.

The shared Staff tracker remains 10 partial, 10 awaiting audit, 1 research and
0 fully accepted until the respective acceptance criteria actually pass.

## Explicit lifecycle review implementation

Continue on `codex/posting-lifecycle-review-20261105`, based on the cutoff
administration draft #1574. Preserve the five foundation requirements above.

The context opt-in is `lifecycle_review=current` with `purpose=review` and
`supervision_context=v1`. It excludes `chain_review`; an explicit
`management_review=current` additionally selects a management root and requires
its separate management assignment. Otherwise the selected posting must be
supervised. Supervised contexts additionally need current posting read authority for ancestor
summaries. Root contexts retain the existing review/Staff-actor/management three-
permission guard; they expose no ancestor directory and do not add a fourth grant.
Both require an independent reviewer. Unknown, repeated, mixed or submission selectors remain invalid.

A separately named lifecycle envelope exposes only each cutoff's immutable ID,
version and instant, with explicit nulls/version0 for absence. It does not expose
administrator reasons or actor/access evidence. Selected and ordered ancestor
nodes preserve nominal source pins. `effective_until` is the earliest nominal,
source or cutoff deadline across the full checked chain, observed at one database
instant under the existing supervision lock. A missing owner, stale source,
unrooted/cyclic/oversized chain, expired cutoff or failed private audit denies;
there is no positive authority from this read.

Subsequent approval must bind every selected/ancestor observation, recheck the
same owners and deadlines while holding current locks, and retain normalized
lifecycle evidence atomically with the unchanged approval receipt/audit/outbox.
An immutable replay returns its original receipt before dynamic source checks.
Never enable lifecycle-aware approval by simply removing legacy refusal.

Positive approval follows this read contract as a separate implementation step:
use a bounded ordered vector of observed cutoff versions (selected node then
immutable supervisor chain), plus the exact effective deadline. The selected
proposal version/revision and its retained supervision links already pin canonical
Staff/proposal/revision identities. Avoid repeating32 full identity tuples in the
4 KiB command. Recompute the vector/deadline from owner data under the same locks;
any new cutoff or earlier effective deadline conflicts, even when approval would
otherwise still be temporally eligible. The original authorized command replay
must retain its exact original normalized input and historical receipt.

Retain a separate normalized lifecycle review header and ordered source rows,
with explicit absence and exact cutoff ID/version/instant, atomically with the
approval, audit and existing identifier-only outbox. Do not alter old proposal
receipts, nominal source deadlines, chain-source evidence or closed IAM catalogs.
A candidate next Workforce migration must be reallocated against the registry
before implementing it, with forced scope/purpose RLS, immutable history,
composite foreign keys and DB guards for complete contiguous source evidence.
This paragraph is an implementation plan, not an activated approval API.
