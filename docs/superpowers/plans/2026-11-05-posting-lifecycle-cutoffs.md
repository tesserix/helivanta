# Posting cutoff implementation plan

Status: owner command/receipt, administrative discovery/context/history and cutoff
workspace implemented locally. Explicit lifecycle context, positive owner
decisions and immutable review evidence now have local API/SDK acceptance.
Reviewer UI integration, operating consumers and complete vertical acceptance
remain open. Follow
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

## Positive lifecycle decision implementation

Continue on `codex/posting-lifecycle-decisions-20261105`, based on draft #1575.
Candidate Workforce0085 is available in the inspected registry. The additive
`lifecycle_review` decision field contains `cutoff_versions` (selected first,
then exact retained chain order;1–32 safe integers, first0) and `effective_until`.
Only approval permits it. Normalize the instant to UTC PostgreSQL microseconds
before fingerprinting; replay checks the original fingerprint before current
source/deadline validation. Older decision inputs keep byte-compatible normalized
JSON by omitting the absent field. Legacy commands still refuse retained cutoffs.

Reserve review/actor/read for supervised lifecycle commands and
review/actor/management for roots, retaining the maximum three scoped checks.
Require exact current version vector and recomputed minimum deadline while all
source, supervision and Organization locks are held. Keep nominal proposal,
management-review and chain-source records unchanged. Add immutable normalized
review/source tables with scoped RLS, exact actor/grant provenance and complete
ordered source evidence. Database deferred checks require retained lifecycle
proof when a command explicitly opts in or any ancestor has a cutoff, so omitting
application persistence cannot silently create an approval through ended sources.

Recovery for lifecycle approval replays the exact original PUT. A plain historical
proposal GET has no cutoff-vector evidence and must not be treated as proof of
that input. Do not invent a replacement command or reject a committed replay
because its formerly reviewed deadline has since elapsed. No new permission,
IAM catalog, event or runtime service is needed. This is a forward-only local
migration candidate; no production migration or rollout is authorized by the
local implementation. Full source-ending enforcement and actual Pharma consumers
remain part of the original five requirements.

## Reviewer lifecycle UI implementation

The local `codex/posting-lifecycle-review-ui-20261105` increment consumes the
explicit lifecycle SDK for root and supervised approval. Named ancestor cards
separate nominal/source intervals from retained endings; effective deadline and
server check time are explicit. Required examination belongs to the current
observation, resets after changed observations and retains rationale. Uncertain
approvals retain an independent copy of exact original context/input/command;
recovery replays the same PUT after changed/unavailable context or elapsed
eligibility. Scope/account transitions clear private state and abort requests.
No route, permission, wire schema, event, migration or dependency is added here.

Full native backend validation on parent `7b6158dd` passes Global API **576.322s**
and architecture **63.674s**, with no failed cases. Full Workforce previously
passed **470.924s**. The leave reauthentication and expiry gate corrections are
fixture changes; production revocation/deadline rules remain unchanged. Browser
reconnection still cannot read/control the local QA page, including a fresh tab;
no normal-role browser acceptance is claimed. Keep historical proof presentation,
actual Pharma/unit guards and all five reusable Staff requirements open.

Final frontend validation: shell **1,548 tests /183 files /324.82s**; focused
reviewer **54 /11.39s**; SDK **720 /81 files**; API/UI/shell type/lint/format
**9 tasks /22.754s**; final production Webpack build, affected Markdown formatting
and staged Gitleaks scan pass. Commands actually run include:

```sh
pnpm --filter @helivanta/shell test -- 'app/(workspace)/workforce/posting-review-workspace.test.tsx'
pnpm --filter @helivanta/shell exec vitest run 'app/(workspace)/workforce/posting-review-workspace.test.tsx'
pnpm turbo lint type-check format:check --filter=@helivanta/api --filter=@helivanta/ui --filter=@helivanta/shell
API_URL=http://localhost:8398 pnpm --filter @helivanta/shell exec next build --webpack
gitleaks stdin --redact --config .gitleaks.toml < /tmp/helivanta-lifecycle-review-ui-staged-20261105.patch
```

The first command forwarded a literal `--` and ran the complete shell suite;
the second explicitly ran the intended reviewer file. No tests were skipped in
either run. Prior SDK full-suite command was `pnpm --filter @helivanta/api test`.

Final reset mutation: disabling the observation-reset effect fails the existing
renewed-examination checkbox assertion. Source is restored byte for byte
(SHA-256 `01332ab5f1f7e0d1c18a44580d86c9d443c944951cbca3f90c4c81099cae802b`);
the same focused assertion passes after restoration. Logs:
`/tmp/helivanta-lifecycle-review-ui-reset-mutation-final-20261105.log` and
`/tmp/helivanta-lifecycle-review-ui-reset-restored-final-20261105.log`.

## Local runtime activation — 2026-11-05

UI candidate `1846565c` is pushed in draft PR #1577. The approved disposable local
fixture now runs that source on loopback-only ports: UI4398 → Global API8198 →
product API8398. Runtime records and binaries are under the existing private
`~/.helivanta/staff-management-20261027/posting-review-ui-20261104/` directory.
No installs/downloads, credential changes or production actions were used.

Before migration, a full custom-format backup and sorted 230-entry ledger were
captured. A compiled-registry probe compared canonical IDs/SQL hashes against the
ledger and detected an additional existing `0033_pharmacy` migration. Its SQL was
reviewed: additive item categories, review policy/assignments, indexes and tenant
RLS, without rewriting existing Staff records. The exact seven pending migrations
were checked before application: `0033_pharmacy`,
`0027_iam_staff_posting_cutoff_templates`,
`0081_workforce_posting_chain_approvals`,
`0082_workforce_posting_supervisor_choices`,
`0083_workforce_posting_supervisor_choice_index`,
`0084_workforce_posting_cutoffs`, `0085_workforce_posting_lifecycle_reviews`.
The canonical migration runner applies them successfully; the ledger is237,
with exactly those seven additions and no removals. Backup, hashes and both
ledgers remain private for local recovery and later release planning.

Current API/Global binaries were built from the candidate using cached Go with
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`; local-only source overlays change the
listener address to127.0.0.1 and do not replace auth/business behavior. The UI was
rebuilt with its existing registered public sign-in client and Global API origin.
An intermediate owner-origin build was discarded without being served. The final
compiled client ID and `/api/:path*` rewrite were checked before switching the UI.

Owner/Global health and UI login return200. Staff and cutoff-discovery requests
through UI4398 → Global8198 → owner8398 return401 without authentication. These
probes verify health, routing and denied anonymous access, not positive-role UI
acceptance. The browser bridge still fails DOM, screenshots and navigation;
no reviewer/admin acceptance is claimed. Historical proof, operating consumers,
all five reusable Staff requirements and production release remain open.
