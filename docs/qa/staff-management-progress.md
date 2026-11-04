# Staff Management delivery tracker

## Reviewer terminal decisions deployed — 2026-11-04

[#1547](https://github.com/tesserix/helivanta/pull/1547) merged as `e7588e65` after
all required checks passed and is now deployed through Kargo. Independent
rejection, original-submitter withdrawal and retained-command recovery are live.
Seven deployments are 2/2; all 14 application containers match signed images with
zero restarts; Argo is Synced/Healthy. The migration ledger is unchanged at 223.
The [release record](../deployment/releases/2026-11-04-posting-decisions.md)
contains exact source, Freight/Promotion, digests and smoke evidence. This
supersedes earlier candidate/draft/undeployed statements for this slice only.

Normal submission UI is under development on top of the released reviewer slice.
Its first test failed at the missing submission action, then passed with mandatory
rationale/attestation and exact source/account pins. Further tests exposed inline
canonical-validation and pagination selection defects; both were corrected.
Current 26 focused checks pass, including future generations, source outages,
changed/missing generations, missing-receipt reauthorization, exact retry,
reviewer-only denial and account-change cancellation. Removing the draft/current
Staff-version predicate makes the changed-generation test fail; original source
was restored and hashed. Exact active department names/revisions are checked
through bounded Organization choices, including empty continuation pages. New
RED→GREEN checks cover loss of submit authority with retained read access and
admission503 without losing the immutable command. Receipt404 rechecks current
hospital admission as well as draft, department and employment sources.

The final full shell suite passes **1,457 tests /177 files (326.64s)**; SDK passes
633 /78 files, with SDK source hashes unchanged since that run. Types, lint,
package format and the final registered-client webpack build pass. The isolated
UI restarted with readiness200 on loopback4398. No dependency was downloaded,
identity/assignment fabricated, API/model/permission changed or migration added.
Candidate branch: `codex/posting-submission-ui-20261104`. It is not deployed and
normal real-account UI acceptance is still open.

Authenticated normal browser acceptance still needs owner sign-in. Supervision,
positive effective approval and departmental/Pharma lifecycle consumers remain
open. Counts remain **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## Reporting-line leave routing — 2026-11-04

#1536 lets HR choose who decides leave first. A versioned, reviewed routing
policy (`hospital` or `reporting_line` with an escalation interval) is pinned to
each request when it is filed; a reporting-line request gets one step per
eligible supervisor, opening level by level. A scoped reviewer reads and decides
a routed request only through an open step held by their own staff member, and
the hospital leave desk keeps final authority. Migration
`0078_workforce_leave_routing` adds the policy revisions, the pinned route and a
restrictive review policy; `GET`/`PUT /hr/leave-routing` are new.

Local browser acceptance on the `helivanta-dev-*` stack (API and UI built from
the #1536 branch), Synthetic Staff QA Hospital 20261027:

1. The owner opened **Leave balances**, chose reporting-line routing and saved
   without input; the panel showed the hours, source and review errors. With 24
   hours, a padded source reference and the review box it saved version 1 with
   the source trimmed.
2. The owner filed 1 day for SYN-QA-EMPLOYEE-20261027, whose supervisor is
   SYN-QA-REVIEWER-20261027, and 1 day for SYN-FULL-UI-20261101, who has none.
   Request review showed the first pinned to level 1, open since filing, and the
   second as routed with no eligible reporting officer. A request filed before
   the policy showed no route.
3. The reporting officer, a second person holding the scoped leave-reviewer
   grant, saw only the first request in **Review leave**, marked **Direct
   report**, and approved it. Their own request and the out-of-route request
   were absent; reading or deciding the out-of-route request through the API
   returned 404, and saving the routing policy returned 403.
4. A hospital leave desk approver, neither the filer nor on the route, approved
   the out-of-route request from **Leave requests**.
   Read-only SQL shows both approvals with `routing_version` 1, the officer's
   with `route_level` 1 and their assignment evidence, the desk's with
   `route_level` `desk`, and no audit row for the refused read.

Screenshots and runtime records stay in the private local QA directory.
Production promotion awaits named approval.

## Incremental production release — 2026-11-04

Posting proposals and the read-only reviewer inbox are now **merged and deployed**:
[#1544](https://github.com/tesserix/helivanta/pull/1544), source `1d6becdf`.
Kargo succeeded; exactly three persisted migrations advanced the ledger 220→223.
All seven application deployments are 2/2; all 14 containers match signed image
digests with zero restarts. Argo is Synced/Healthy. Public landing/login GETs
return 200; unauthenticated posting routes return 401 through Global API.
See the [release evidence](../deployment/releases/2026-11-04-posting-proposals.md).
This checkpoint supersedes earlier draft/unmerged/undeployed statements for the
#1544 slice only; historical validation notes below are retained.

Independent rejection and submitter withdrawal UI subsequently shipped in
[#1547](https://github.com/tesserix/helivanta/pull/1547); see the newer deployment
checkpoint above. Its nine reviewer tests, 1,440 shell tests and 633 SDK tests
passed locally, and all exact-head CI checks passed before merge.
The isolated local runtime is ready with a registered OIDC client; normal browser
acceptance remains at owner sign-in. Earlier Temporal/readiness setup blockers
are resolved. No new IdP app or grant was created to bypass that sign-in.

Positive effective posting approval remains blocked by
`posting_supervision_required`; supervision, lifecycle and departmental/Pharma
consumer acceptance remain open. **10 partial, 10 awaiting audit, 1 research,
0 fully accepted**. Incremental delivery does not mark these broader areas done.

## Protected posting context checkpoint — 2026-11-04

The source-owner coverage correction [#1545](https://github.com/tesserix/helivanta/pull/1545)
merged normally as **`a12f6a30`** after all required checks passed. Go CI completed
in 30m08s, including the seven real fixture regressions and complete backend
gate; its main-base Workforce source-owner result was **83.3% >= 70%**. Web,
scripts, secrets, E2E and image checks passed. The repository remains public.
This CI correction is not a production Staff API rollout.

The posting candidate now adds a protected exact-head context view. Current
independent review authority or the original submitter's current submission
authority is required before loading the selected immutable draft rationale,
original submission rationale and nullable terminal rationale. The view shares
the existing decision predicates and guarded source lock order, audits disclosure
through commit, and does not change business heads, revisions or outbox rows.
No SQL or migration file changed. Existing public receipts remain private-field
free. Canonical Go/Zod/SDK models require the exact purpose/version/revision and
reject mismatched scope, draft/department/interval pins and malformed rationale.

The real gateway RED returned **404 instead of 200** (90.357s package). The
initial implementation passed (97.655s), then the strengthened real PostgreSQL/
OpenFGA case passed in **98.276s**. That case covers later draft edits retaining
the submitted rationale, terminal reasons, contributor/historical-account denial,
stale/hidden targets and audit withholding with actor-use rollback. Removing only
the context's independence call produced **200 instead of required 403** for a
retained contributor; the mutant failed in **88.239s**. Original context source
was restored and SHA256 verified. A real OpenFGA outage assertion was then added.
The complete restored Global API race/profile suite passed in **454.878s**,
including that assertion, and removed three owned fixtures without fallback SSH
forwards. Its Workforce-only instrumentation reports 28.8%; this gateway-only
metric is not combined source-owner coverage for the current addition.

The SDK test first failed on its absent helper. Current **630 tests / 78 files**,
API typecheck, lint, package-wide formatting, Go build, scoped Go vet/lint and
contractdocs check passed. The SDK also verifies that 500 Unicode code points are
accepted and 501 refused for each retained rationale. The pagination gate first refused the new route, then
passed with the explicit single-object/three-bounded-reasons rationale (2.592s).
All 15 operational inventory tests passed. Registry comparison proves exactly one
new authenticated, membership-required route; all previous routes, grants,
migrations, events and canonical model contents are unchanged. Sixteen module
source pins were reconciled while preserving every classification/rationale and
remaining obligation. The ledger is **794 registered / 771 unclassified**; the
ten proposal surfaces remain unclassified.

The complete current Workforce race/profile suite passed in **700.117s**, removing
four owned fixtures; the complete architecture race suite passed in **49.919s**,
removing three. Neither required fallback SSH forwards. Combining the complete
current Workforce and gateway profiles with the merged source-owner parser gives
**12,204 / 14,672 statements = 83.179%**; Go's independent report confirms **83.2%**
rounded. This is code coverage, not Staff feature acceptance. The code is rebased
onto `a12f6a30` as **`6714efd1`**. All **1,418** tested runtime/contract source
hashes are byte-identical across that rebase, and the complete upstream opening
Leave acceptance section was retained alongside the posting history. Rebased
contractdocs/inventory checks and shell lint passed. Exact-head CI remains pending.
The pre-context result below is historical evidence,
not a full-suite run of this addition. Hospital proposal inbox, normal reviewer UI
and browser acceptance, supervision/root policy, effective departmental postings,
lifecycle consumers and Pharma duties remain open. Overall acceptance remains
**10 partial, 10 awaiting audit, 1 research and 0 fully accepted**.

## Historical-account and coverage checkpoint — 2026-11-04

Draft #1544 at `0cf52a1a` passed web, scripts, secrets, E2E and image CI checks.
Its Go behavioral suites passed, but the coverage gate failed at Workforce
**69.2% < 70%**. The gateway workflow executions were not counted toward their
source owner. A separate CI correction, commit `8264c08f`, counts the combined Go
profile by source-owner package, retaining the 70% floor and no-owner-test
refusal. Separate [PR #1545](https://github.com/tesserix/helivanta/pull/1545),
latest head `83db033c`, has seven real Go fixture regressions, including refusal
to let a fully covered sibling hide another owner's coverage failure. Those
regressions, shell checks and CI lint passed; the complete CI gate remains in
progress. No merge is claimed for that correction yet.

The historical-account regression now imports an old revoked association between
a contributor's subject and an otherwise current reviewer's canonical Staff.
Current reviewer generations, links and assignments still come from the real
public workflow. Rejection returns 403, the pending head/history remain unchanged,
no decision receipt appears, and the original submitter can withdraw. The target
remains non-digital and has no login link. This tests a legacy imported association,
not a modern account-rebinding UI or every historical-target association branch.

The baseline PostgreSQL/OpenFGA run passed in **87.337s**. Removing only the
reviewer-Staff/contributor association exclusion produced **200 instead of 403**
and a rejected version-2 receipt; the mutant failed in **83.673s**. Original SQL
was restored and SHA256 verified. The restored focused Global API run passed in
**105.106s**, with race detection and Workforce instrumentation; three owned
fixtures were removed and no fallback SSH forwarding was required.

The combined implementation's earlier full Workforce race suite passed in
**576.956s**. The further complete Workforce race run with an owner coverage
profile passed in **685.470s**, removing four owned fixtures without fallback SSH
forwards. Its own binary reports 69.2%, reproducing the CI metric. Combining this
profile with the restored focused real gateway profile and using the new gate's
actual parser gives **11,775 / 14,611 Workforce statements = 80.590%**. This is
measured source execution across those two runs, not complete backend CI or UI
acceptance. The full current Global API race suite including the new alias case
subsequently passed in **565.391s**, removing three owned fixtures without fallback
SSH forwards. Combining the two complete-suite profiles with the actual gate
parser gives **12,147 / 14,611 Workforce statements = 83.136%**. Full backend CI,
normal reviewer UI acceptance and production rollout remain unproven.
Overall acceptance remains **10 partial, 10 awaiting audit, 1 research and
0 fully accepted**.

## Posting decision validation checkpoint — 2026-11-04

The first web CI run for draft #1544 stopped at API formatting in
`staff-posting-review-workspace.test.ts`. The same package-wide `format:check`
was reproduced locally, then passed after formatting that one test with cached
Prettier 3.9.10 under Node 24.11.1. All six tests in the changed file passed.
The correction changes only test layout; rerun CI remains required.

The API/SDK slice is committed and pushed in draft [PR #1544](https://github.com/tesserix/helivanta/pull/1544),
code commit `80dbc610`, on top of merged documentation `6a09287c`. The full
restored Global API race suite passed in **411.298s** and removed its three
owned disposable fixtures. That binary preceded the rebase which inherited
main's separate Leave change `96a47d0b`; its posting implementation is unchanged.
The inherited delta is six Leave implementation/test/contract files. Do not
represent the preceding complete-package results as complete-package execution
of that inherited delta.

On combined code head `80dbc610`, all **625 SDK tests / 78 files**, API typecheck
and lint under cached Node **24.11.1**, full Go build, scoped vet/golangci-lint and
contractdocs check passed again. Cached dependencies were linked with local
API/config sources and an identical lockfile, without installation. The current
head's complete Workforce race suite is now running, and combined-head CI is
pending. Both the staged and committed secret scans passed. Main's complete
tracker and catalog review rationales were retained through the rebase.

The draft does not request production rollout. Its nine new surfaces remain
unclassified. Historical-alias proof, protected proposal context, hospital inbox,
normal reviewer UI, supervision and positive approval remain open, alongside
broader departmental/lifecycle consumer work. No full Staff completion label is
warranted.

The candidate is now isolated on `codex/scoped-posting-proposals-20261104`, based
on current main; 1,498 tracked backend/API/contract/script files and all 27 changed
source hashes matched the tested worktree before the expiry fixture correction.
Main's catalog review notes and all 19 existing authority classifications were
preserved when reconciling the source ledger. The documentation-only release
record PR #1541 passed all required checks and merged normally as `6a09287c`, with
no ruleset exception or production mutation.

The subsequent full Global API run failed in the pre-existing expiry fixture
(303.162s): the blocked save returned the required 403, but the fixture compared
only PostgreSQL's clock against the host-enforced deadline. Diagnostics reproduced
both expiry cases: host observations were 6–7ms after the exact cutoff while DB
samples were 20–22ms before it. The fixture now accepts expiration enforced by
either real owner clock, with no tolerance or altered cutoff. It retains actual
SQL blocking, exact 403, no receipt and audit/actor-use rollback checks. Cleanup
now uses an independent bounded context and nonfatal error reporting instead of
panicking inside `sync.OnceFunc` after the test context is cancelled.

A mutation moved the shared source timer one hour late. The original exact-status
assertion rejected it with 503 instead of 403 (105.516s); the private wrapper had
expected a timeout message, so its stricter text oracle also failed. Inspection
confirmed the actual mutation failure at the intended expiry response assertion.
Original shared-command bytes were restored and SHA256 verified; no production
clock, timeout or authorization behavior changed. The full restored-source API
suite subsequently passed in 411.298s, before the separate Leave rebase noted above.

The complete architecture race suite passed in **56.617s**, including migration,
RLS, OpenFGA matrix and owner-interface checks; three owned disposable fixtures
were removed. All 15 operational inventory tests passed after the reviewed source
pin reconciliation. The full Workforce race suite passed in **622.627s**, with four exact task-owned
disposable fixtures removed and no fallback SSH forwards. The full Global API
run on the latest discovery source is now running, in addition to the focused proof above and the earlier 400.053s parent
run on pre-discovery source.

Reviewer discovery's corrected real Global API journey passed in **58.785s**
(three owned disposable fixtures removed). It proves reviewer-only capability
flags, retained read-only draft access and denied editing, administrator-only
empty discovery, a 20-row limit, audit-failure response withholding and actor-use
rollback, OpenFGA outage refusal, and immediate same-session discovery cutoff
after the reviewed assignment is withdrawn. The SDK passed **625 tests / 78 files**
under cached Node **24.11.1**, with no installation. Typecheck/lint, Go build,
scoped vet/lint and regenerated contract checks passed. The new endpoint and
canonical DTO remain unmerged; source inventory is **793 registered / 770
unclassified**, with no new runtime acceptance classification.

The complete parent journey on restored terminal-decision source passed in
**400.053s**, with three exact task-owned disposable fixtures removed. The prior
full-package failure (208.119s) in an existing activation-source read remains
recorded; the passing parent-only rerun does not establish its cause. The first
90 seconds of read-only database observations found no prolonged lock waits.

Reviewer discovery now has a separate canonical capability projection. Its owner
unavailability assertion was observed failing with absent-route 404, then passed
with 503 after implementation (5.684s package). The first positive fixture
incorrectly expected the immutable reviewer catalog to lack draft reading; the
actual catalog includes reading and excludes editing. Its assertion and design
note were corrected to preserve read-only preparation and explicitly refuse
edits. No IAM snapshot or test authorization requirement was changed.

Mutation proof removed only the retained draft-contributor branch. The new
assertion failed with **200 instead of 403** and a rejected version-2 receipt;
the mutant package failed in 96.847s. Original SQL bytes were restored and their
SHA256 verified before the full restored-source Global API run. The existing
IAM migration-count test was observed failing (24 versus 25), reconciled to the
already-merged main test that pins migration 0025, and passed in 2.667s.

The next focused real PostgreSQL/OpenFGA run passed in **109.950s** on the existing
Staff QA VM, using cached images and three disposable session-owned fixtures
removed after capture. No fallback SSH forwards were needed. It covers fixed and
ongoing submissions, non-digital target identity invalidation, explicit rejected
and withdrawn revisions, retained retry/recovery, and audit/outbox rollback with
positive canonical-event counts. The added contributor case keeps an earlier
draft author ineligible after a different preparer edits/submits, verifies the
pending head/history and missing decision receipt, and allows the original
submitter to withdraw. This is not full reviewer UI or positive approval evidence.

The submitter/reviewer IAM prerequisite is now deployed as `e9ea7281`; its
separate [release record](https://github.com/tesserix/helivanta/pull/1541)
is in PR #1541. Proposal API/SDK work in this branch is still unmerged.
Rejection/withdrawal have exact pending-revision checks, original-key recovery and
atomic history/audit/outbox; positive approval remains blocked on supervision.
The [source review](2026-11-04-posting-proposal-source-review.md) records remaining
historical-alias, source-race, RLS and UI proof obligations.

On resumption, the full SDK suite passed **619 tests / 77 files**. The reason
control-character regression passed with race detection in **2.656s**; full Go
build, contractdocs check and scoped Workforce/Global API/architecture vet passed.
The focused real Global API run failed **before assertions** at OpenFGA HTTP
readiness, despite service-start logs. Package failure time was 263.948s; wrapper
447.9s, with one exact task-owned disposable fixture removed. It is not counted
as a pass. A concurrent full coverage run in the separate leave-opening-balances
worktree was using the same Docker VM; Docker listing took 12.68s and VM SSH timed
out at 15s. Its containers/processes were preserved. No transport fix or successful
new fault-injection run is claimed.

## Separate non-digital placement target verification — 2026-11-04

Scoped preparation PR [#1532](https://github.com/tesserix/helivanta/pull/1532)
is merged as `091f2287`; GitHub reports all required checks successful and the
repository remains public. This supersedes the older draft-PR status in historical
entries. It does not release the independent-review worktree.

A real Global API/PostgreSQL/OpenFGA journey now separates a current reviewed
digital submitter from a newly reviewed, activated **non-digital target**. The
target has no declared account or login link. Source readiness, canonical
department draft, pending submission, exact retry and immutable history pass;
administrator-only reading and an unrelated generation are refused. Changing and
restoring the target's identity invalidates its generation even with a fresh
draft and current Staff concurrency pins. A new submission returns 409, while
the original non-operating receipt remains readable and no login is created.
The focused parent run passed in 62.224s. Its initial fixture failed required
employee-code length validation before any posting assertion; the synthetic
scenario/code was shortened rather than changing the product limit.

Mutation proof bypassed only the different target's `identity_changed` result:
the new submission returned **200 instead of required 409**, failing the public
API assertion. Exact original owner bytes were restored and hash-verified.
An earlier actor-versus-target HR-purpose mutation survived; it is not claimed
as a tested security boundary or a product defect. Private logs preserve both
observations. No runtime binary was rebuilt from either mutation.

The preceding full Global API race suite passed in 410.914s after the ongoing
placement correction; the subsequent complete run including the non-digital
target passed in **448.467s**, with zero fallback forwards and three exact owned
fixtures removed. All 616 SDK tests across 77 files, canonical-generator
checks/typecheck, SDK lint, Go build, scoped vet/lint (0 issues), contractdocs
check and whitespace check pass. The full architecture run exposed missing
permission declarations and three singleton GET classifications. The declarations
now preserve scoped-only assignment authority and the approved legacy permission
matrix; the singleton entries explain why no collection cursor applies. The full
architecture suite then passed in **71.684s** with real PostgreSQL/OpenFGA,
including migration/RLS checks. The initial direct invocation also lacked the
Docker endpoint; its infrastructure failures are not product regression evidence.
After registry corrections, the focused real fixed/ongoing/non-digital submission
and parent setup run passed in **79.689s**, with zero fallback forwards and three
exact owned fixtures removed.

The operational inventory initially refused stale source pins as intended. The
[source review](2026-11-04-posting-proposal-source-review.md) compares all prior
Workforce/IAM routes and grants against refreshed main, preserving all of them.
Nineteen existing reviews were reconciled for the additive module changes and
preserved wrappers. Parity passes at **790 registered / 767 unclassified**;
all 15 inventory tests pass. No new surface was classified as runtime accepted.

The existing loopback browser separately verified current scoped Preparer
discovery, the canonical Pharma target, explicit draft-only wording and empty
form errors for department, UTC start and reason. Cancel returned to the unchanged
empty draft list. No business data or access was saved. This browser uses the
existing preparation runtime and is not independent-review UI acceptance.

Independent approval/supervision, target source races in both orders, transfers
and lifecycle consumers, department-scoped operating permissions, full review UI,
Pharma duties and production release remain open. The overall ledger stays
**10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## Reviewed posting submissions in development — 2026-11-04

The [independent posting feature](../superpowers/plans/2026-11-03-independent-staff-posting-review.md)
now has real pending submission/source/read/history/retained-command handlers,
canonical SDK models and an additive forced-RLS proposal schema in the current
worktree. A proposal references the existing canonical Staff, reviewed generation
and Organization department; it grants no operating access. Fixed-term, source,
retry/duplicate, required-field and audit/outbox rollback paths have public API
proof. Ongoing employment now permits a null placement end while retaining its
separate finite reviewed-source cutoff; fixed appointments reject unbounded or
outlasting placements. The focused fixed/ongoing run passed in 89.620s after the
new ongoing case was observed failing with 409.

Later validation results and registry corrections are recorded above. Earlier
finite-end-only passes are explicitly marked superseded in the detailed plan. The owner-only
missing-decision test remains RED because the decision path is not implemented.
Independent approval/supervision/lifecycle, different actor/target cutoff proof,
normal browser acceptance and release gates remain open. No production mutation
or full Staff/Pharma completion is claimed. Overall ledger remains 10 partial,
10 awaiting audit and one research area.

## Independent posting review started — 2026-11-03

The [next vertical feature](../superpowers/plans/2026-11-03-independent-staff-posting-review.md)
is isolated in `helivanta-effective-posting-review-20261103`, based on pushed
scoped-preparation head `0b27e927`. The candidate specification pins ownership,
new scoped submit/review actors, retained command paths, source/supervision and
failure requirements. Its first owner-router tests are RED: submission and
decision routes are absent (404), rather than 503 when current owners are not
composed. The real reviewed-source Global API fixture separately reaches
submission and gets absent-route 404 instead of current-assignment refusal 403. This is evidence of missing functionality, not a security bypass.
No placeholder handler, approval or operating authority was added. The next
closed IAM catalog and its additive migration are under development on this
isolated branch.
Workforce canonical models, positive independent-review fixture, persistence and
UI remain to implement together on that branch. The template generator tests
pass (51). Combined existing/new role catalog and real OpenFGA projection tests
pass (117.945s); scoped IAM vet/lint, generator/contractdocs checks, the SDK
suite (609 tests) and API typecheck pass. Commands and UI are still unimplemented.

The unchanged evidence-download admission case also passed five consecutive
four-transfer runs (140.672s total package time), with zero fallback forwards and
three exact owned fixtures removed. The earlier isolated 503 remains an unresolved
observation; these passes do not prove its cause or a fix. All exact-head scoped preparation CI for `0b27e927` now passes, including Go,
web and E2E; deploy advancement was skipped. No merge, production rollout or completion tag occurred.

## Opening leave balances — 2026-11-04

#1537 (PR #1542, merge `96a47d0b`) lets HR record an opening balance for a staff
member whose assigned leave structure has no ledger entries yet. Balance reads
list each balance-tracked type of the structure in force on `as_of` at zero with
`in_structure`; a correction outside that structure, or dated before any
assignment, is refused with 400 `invalid_reference`. No routes, permissions,
events or migrations change.

Local browser acceptance on the `helivanta-dev-*` stack (API and UI built from
`ccf60518`), Synthetic Staff QA Hospital 20261027:

1. The owner assigned Synthetic Staff Annual Allowance (SYNSTRUCT2026) v1,
   effective 2026-11-03, to SYN-FULL-UI-20261101. **Leave balances** then listed
   Synthetic Annual Leave at 0 days with **Correct balance**, whose type picker
   offered only that structure's types.
2. The owner recorded a +3 day (6 half-day) opening correction effective
   2026-11-04; the ledger showed the entry and the balance read 3 days.
3. The structure entry is not probation-eligible, so the owner recorded start
   date 2026-01-05 and probation end 2026-06-30, then filed 1 day of annual
   leave for 2026-12-03.
4. The hospital leave reviewer, a second person, approved it from **Review
   leave** with no roster clashes. Read-only SQL shows the correction (+6) and
   the consumption (−2), leaving 4 half-days, and a decider different from the
   filer.

CI on PR #1542 passed every required check, including the full `go` coverage
gate (37m37s) and `e2e`. Screenshots and runtime records stay in the private
local QA directory. Production promotion awaits named approval.

## Bounded posting role-catalog prerequisite — 2026-11-04

Following the user's request for smaller releases, the first isolated change adds
independently approved hospital-scoped posting submitter/reviewer catalogs through
append-only IAM migration `0025_iam_staff_posting_review_templates`. Historical
catalogs and SQL remain byte-identical. This slice does not introduce proposal
routes, an effective posting, department operating rights or Pharma duties.
The owner API/SDK, reviewer UI and lifecycle consumers remain separate follow-ups.

Local validation passed: 609 SDK tests/76 files, API typecheck/lint, 66
catalog/inventory checks, Go build/scoped IAM vet/lint (0 issues), generated
contracts/check and whitespace checks. The full serial IAM race suite passed in
462.333s after correcting the migration-count assertion from 24 to 25. All PR
#1539 checks subsequently passed. Registry parity remains 784 surfaces / 761
unclassified; catalog registration does not establish operating acceptance.

PR #1539 merged as `e9ea7281` and its approved production promotion succeeded.
Exactly IAM migration 0025 was added, moving receipts from 219 to 220. All seven
application deployments were 2/2 ready, all 14 application containers were ready
with zero restarts, and API/Global API/shell were Argo Synced/Healthy. Published
image digests match the running container image IDs. See the [release evidence](https://github.com/tesserix/helivanta/pull/1541).
Authenticated production catalog UI acceptance is pending the owner sign-in
handoff. No effective posting, operating authority or full Staff acceptance is
claimed. Proposal submission/terminal-decision API work remains unmerged.

## Membership alone grants no posting access — 2026-11-03

Normal Admin UI withdrew the last Reader assignment to v6, preserving ordinary
membership and the reviewed digital Staff source. Fresh recipient login and
Posting preparation show **No approved posting hospitals on this page**, with no
protected Staff target, draft history or editor. Read-only proof verifies active
Staff v2/epoch2, matching identity epoch, current approved employment/person
reviews, effective generation, active reviewed link v2 and IAM employment basis.
Six historical assignments remain: five withdrawn and one active-but-expired;
zero are currently effective. Canonical Pharma target and three existing
commands/revisions/save audits remain unchanged. Private credential JSON records
the acceptance without credential changes. See the [UI proof](2026-11-03-scoped-posting-positive-ui.md#follow-up-membership-and-current-staff-source-without-posting-authority).

## Isolated-session direct withdrawal UI acceptance — 2026-11-03

Direct preparer withdrawal now passes without a recipient logout/account change.
[Detailed UI proof](2026-11-03-scoped-posting-positive-ui.md#follow-up-direct-withdrawal-with-isolated-browser-sessions)
records a captured/restored test-only OIDC callback on a second loopback hostname.
The proposing Admin and independent reviewer used normal login/UI there while the
recipient retained its original session. Two bounded synthetic assignments were
independently approved to active v5, then withdrawn to v6 through the proposer UI.

The loading-department case cleared the old panel before Save was available. The
separate fully loaded case explicitly observed current canonical choices and an
enabled Save before withdrawal. After withdrawn v6 was visible in Admin UI, Save
was clicked in the recipient's unchanged session. The access-change cutoff cleared
protected Staff/editor state and required reopening. No response status was directly
captured; no exact HTTP status is claimed from the UI text alone.

Read-only persistence still shows one unchanged canonical Pharma Staff target,
draft v3, three commands/revisions and three save audits. Reader-only discovery and
v1–v3 history recover without Edit controls. The temporary Admin origin signed out
and closed; its original OIDC settings were restored exactly. Credentials remain
private and their JSON checkpoint was updated without changing credential values.

All pushed-head `11cf4101` CI checks pass, including complete Go, web and E2E jobs.
The [source-writer review](2026-11-03-posting-source-writer-review.md) now maps current
owner locks, writers and evidence. It identified that earlier source-ending order
cases exercise retained receipts. Two additional different-target **new draft
write** cases now pass with exact revision/audit/source-use counts and unchanged
target versions. The complete parent baseline passed in 329.231s. A temporary
actor identity-epoch bypass made both new cases return 200 instead of required
403; source-first created an unauthorized synthetic draft. Exact source bytes
were restored and hash-verified before final validation. No application binary
was rebuilt from the mutation.
The first fixture inherited a retired department and was corrected to active/v0.
The next fixture left that new department active for the following shared case;
owner-API retirement cleanup was added without weakening pagination assertions.

A subsequent run failed before assertions because its disposable PostgreSQL
container exhausted VM disk space while committing a migration. Its log reported
`pg_wal/xlogtemp: No space left on device`; this was not counted as a product or
test pass. The task's captured, exited disposable fixtures were reconciled against
exact container IDs/session labels before cleanup: **213 containers and 79 exclusive
anonymous volumes** removed, with state retained privately in
`owned-stopped-test-fixtures-before-cleanup-20261103.json`. Named QA/rehearsal services
and cached images were preserved. Volume use fell from **16.47 GB to 11.45 GB**.
All 15 named services, API/gateway readiness, UI login and persisted Staff proof
were reverified. The private runner now captures and removes its exact disposable
session fixtures, including startup failures; its durable private copy is
`shared-staff-foundation-20261102/posting-test-runner.py`. The final complete Global API race suite passed in **363.724s** (22 top-level /
130 including subtests), zero fallback forwards; its three owned disposable
fixtures were removed. Scoped vet/lint (0 issues), full Go build, contractdocs
check, inventory parity (784 registered / 761 unclassified) and whitespace checks
passed. Private mutation and final-suite logs preserve the negative/positive proof.

Remaining account/foreign-scope browser cases and the earlier intermittent
evidence-admission observation remain open. Its cause is unproven; neither a
successful rerun nor disposable-fixture cleanup is claimed as its fix. Full
Staff, effective departmental posting and Pharma operating lifecycle acceptance
are not implied by these results.

## Same-session expiry and refreshed local runtime — 2026-11-03

The normal UI expiry case now passes. Admin proposed a separate synthetic posting
preparer assignment ending **10:32 UTC**; the independent Admin approved it through
current hospital verification to active v5. The recipient signed in normally,
opened the canonical Pharma target's v3 editor and entered an unsaved reason.
That same recipient session remained signed in across expiry. After the deadline,
discovery displayed Reader and had already removed the dirty editor and Save
control. The attempted click found no Save control, so **no save request was sent**.
Reader history v1–v3 remained readable. Read-only SQL confirms exactly three
commands, revisions and save audits; no additional draft write occurred. Only the
independent reader assignment is currently effective. Expired/withdrawn grants
remain historical records. This closes same-session expiry, not the distinct
direct-withdrawal case with isolated administrator/recipient browser sessions.

The local stack was then rebuilt from merged head `b2cdb065`: owner API 8399,
Global API 8199 and production-style shell 4399. Prior runtime metadata, migration
ledger and previous UI build were captured privately. API startup applied only
main's additive **0031_pharmacy** migration; all earlier migration IDs remain.
Both APIs pass readiness HTTP 200 and shell login serves HTTP 200. Normal Reader
discovery and immutable v1–v3 history pass after full reload on the refreshed
stack; the same read-only proof passes after restart. Production is unchanged.
The complete merged-source Pharma race suite passes in **256.765 s**; its runner
used zero fallback forwards and stopped only its three test containers.

Exact-head CI then exposed a shared UI gate that the separate API/shell runs had
not covered: Staff directory and Posting preparation both used `Contact`, failing
the existing per-zone icon uniqueness test (21 distinct icons for 22 pages).
The same test failed locally before the fix. Posting preparation now uses the
existing `Building` icon. All **278 shared UI tests**, typecheck, lint and format
checks pass. The assertion remains unchanged; no dependency or permission changed.

Additional commands actually run:

```sh
# backend/
python3 /tmp/helivanta-posting-test-runner.py ./internal/modules/pharmacy
go build -o /tmp/helivanta-scoped-posting-merged-api ./cmd/api
go build -o /tmp/helivanta-scoped-posting-merged-global-api ./cmd/global-api
# repository root, cached Node 24.22.1
mise exec node@24.22.1 -- pnpm --filter @helivanta/ui test src/zones.test.ts
mise exec node@24.22.1 -- pnpm --filter @helivanta/ui test
mise exec node@24.22.1 -- pnpm --filter @helivanta/ui type-check
mise exec node@24.22.1 -- pnpm --filter @helivanta/ui lint
mise exec node@24.22.1 -- pnpm --filter @helivanta/ui format:check
python3 /tmp/helivanta-scoped-posting-retained-editor-expiry-proof.py
```

Private `scoped-posting-expiry-and-merged-runtime-checkpoint.json`, expiry before/
after screenshots, `scoped-posting-account-change-cleared-ui.png`,
`scoped-posting-merged-runtime-reader-ui.png` and runtime/migration captures retain
the evidence. The credentials JSON was updated atomically with secrets preserved.
The source-writer audit, remaining account/foreign-scope UI cases, direct
withdrawal in isolated sessions and the earlier intermittent admission failure
remain open. No full Staff acceptance or production release is claimed.

## Current-main integration and account-change browser cutoff — 2026-11-03

Merged current main `bdb2560c` into the candidate as `b2cdb065`. The only conflict
was generated `staff-operational-access-inventory.json`; regeneration from the
combined canonical registry/review inputs preserves both sets of registrations
without changing review pins. The ledger now has **784 registered surfaces / 761
unclassified**. Contractdocs generation/check and inventory parity pass.

On the combined source, the complete SDK suite passes **609 tests** and shell
passes **1,429 tests / 176 files** in **321.11 s**. API/shell typechecks (including
the canonical generators) and lint pass. Repository Go build and the complete
real-fixture architecture race suite pass (**60.653 s**); all **62** template and
inventory script tests pass. Existing Vitest native-loader and JSDOM navigation
warnings remain. No dependency installation was required.

Commands used cached Node 24.22.1 through `mise exec node@24.22.1 --`:

```sh
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/shell test
pnpm --filter @helivanta/api --filter @helivanta/shell type-check
pnpm --filter @helivanta/api --filter @helivanta/shell lint
node --test scripts/staff-template-migrations.test.mjs scripts/staff-operational-access-inventory.test.mjs
node scripts/staff-operational-access-inventory.mjs
node scripts/staff-operational-access-inventory.mjs --check
```

Go commands ran from `backend/`: `go run ./cmd/contractdocs`, the matching
`-check`, `go build ./...`, and
`python3 /tmp/helivanta-posting-test-runner.py ./internal/archtest`.
The runner used zero fallback forwards and stopped only its three fixture containers.
The pushed PR no longer reports a merge conflict; exact-head CI is running.

A new short-lived synthetic preparer proposal was created by Admin and
independently reviewed to active v5, preserving the earlier withdrawn assignment.
The recipient opened the real v3 editor with an unsaved reason. Signing out in a
second tab immediately cleared the original editor and returned that tab to login.
The replacement assignment was withdrawn to v6. This verifies cross-tab session
clearing, **not** retained-editor role-withdrawal behavior: the session change
occurred before the old form could attempt a save. A separate uninterrupted-session
expiry case is in progress. Local Staff browser evidence still uses the prior
production-style bundle; this main merge changes Pharma surfaces, not the tested
Staff source. The combined deployment artifact is not claimed accepted here.

## Closed Staff actor architecture boundary — 2026-11-03

Added three architecture checks for ADR-0052: the exact reviewed source-guard
consumers, explicit provider wiring/injection references, and the single typed
authorization callback signature. The initial wiring check also rejected the two
intended `deps.StaffActors` field injections because the existing scanner checks
all selector references. Its exact-file set was corrected to include those
existing injections. A temporary unrelated Organization source file then exposed
bare provider/consumer method references: both new checks failed for that file.
The probe was removed and the restored checks pass. No production source or
authorization allowlist was widened; these tests add a new architectural gate.

Commands run from `backend/`:

```sh
go test -count=1 ./internal/archtest -run '^TestStaffActor'
go test -race -count=1 ./internal/archtest -run '^TestStaffActor'
python3 /tmp/helivanta-posting-test-runner.py ./internal/archtest
go vet ./internal/archtest
golangci-lint run ./internal/archtest/...
go build ./...
go run ./cmd/contractdocs -check
```

The complete real-fixture architecture race suite passes in **52.193 s**, affected
lint reports zero issues, and vet/build/contractdocs checks pass. An initial runner
invocation from the repository root failed before tests because `go.mod` is under
`backend/`; rerunning from that directory passed. The runner used zero fallback
forwards and stopped only its three test containers. Repository-root source
inventory and whitespace checks pass. Source-writer lock-graph review and the
remaining browser matrix remain open; no production or full Staff acceptance is
implied by this architecture gate.

## Scoped posting recovery and reader downgrade — 2026-11-03

Additional normal-browser acceptance now passes on the same canonical Pharma
Staff target: a real owner commit followed by deliberately lost response, exact
retained-command receipt recovery, Admin withdrawal of preparer authority,
fresh-session reader-only discovery/history and an unrelated reviewer receiving
no posting hospitals. [Detailed evidence](2026-11-03-scoped-posting-positive-ui.md#follow-up-lost-response-downgrade-and-unrelated-reviewer)
records the temporary loopback relay, exact command reconciliation and restoration
of original Global API routing. No authorization check was bypassed.

The new draft is now v3, with three distinct commands/revisions and three save
audits; receipt recovery added no duplicate. The old separate draft remains v2
and target Staff remains v2/epoch1/Inactive. Preparer is withdrawn v6; reader remains
active v5 with only posting-read/Staff-actor-use permissions and no legacy hospital
grant. Reader history v1–v3 survives a full reload with no Prepare/Edit controls.
Private JSON credentials are preserved and its acceptance checkpoint updated.

No application code, schema or production state changed. Head `9b70f71b` passed
API/shell builds and security scans; deployment advancement was skipped. Remaining
gates include retained-session cutoff and other account/foreign-scope UI cases,
the source-writer/architecture/generator audit and the unresolved intermittent
admission failure. PR #1532 remains draft. Effective posting, supervision, lifecycle,
departmental/Pharma operating consumers and full Staff acceptance remain open.

## Fresh source and positive scoped posting UI — 2026-11-03

Normal positive reader/preparer core journeys now pass using a newly reviewed
digital Staff actor. [Detailed browser and persistence evidence](2026-11-03-scoped-posting-positive-ui.md)
records the complete local onboarding sequence, separate account and officer
assignments, identity/employment approvals, protected downloads, activation and
independent login/assignment decisions. All application business mutations used
normal UI. The old revoked-review source remains unchanged as negative evidence.

Reader-only access discovers one hospital, reads the existing shared Pharma
Staff draft/history across a full reload and exposes no Prepare/Edit controls.
Adding independently approved preparer authority deduplicates the hospital choice.
Required-field validation, current canonical department selection, create, edit,
unsaved-navigation confirmation/Keep editing, immutable history and full reload
pass. Read-only proof finds one unchanged canonical Pharma Staff target, one new
head with two distinct commands/revisions and attributable source/assignment audits;
the earlier draft remains untouched. No legacy hospital grant or operating access
was added to the actor.

Two local setup problems were resolved without changing domain eligibility: a
stale seed credential was reconciled against the active local identity fixture,
and the saved owner API config now persists the authorized activation flag after
its default-off response. The same binary remains on local 8399; production is
unchanged. The observer timeout during identity download was reconciled against
owner audit and exact downloaded bytes; no review decision was repeated.

No application code changed in this turn. Head `829de653` has successful API/shell
builds and scans, with deployment advancement skipped. Exact response-loss recovery,
preparer withdrawal/reader downgrade and the remaining negative UI matrix remain
open, as does the recorded intermittent admission failure. PR #1532 stays draft;
no full E2E tag, merge or deployment is claimed. Effective posting and the complete
Staff/departmental/Pharma parent objective remain incomplete.

## Posting department-choice UI cutoff — 2026-11-03

A component regression exposed an access-change propagation gap: after a scoped
preparer opened the editor, a 403 from the department-choice owner was treated as
a recoverable outage. The form and enclosing Staff panel remained visible.
The new assertion failed with `Current departments are unavailable` instead of
the access-change cutoff. The editor now classifies department errors alongside
save errors, and latches any editor access change into the enclosing panel.
It clears the form and Staff label and notifies the workspace exactly once.
A preparer downgrade therefore requires reopening under fresh discovery; it does
not keep an old preparation panel or fall back to another route.

A separate recovery test proves a 503 remains an availability failure: saving is
disabled, a typed reason survives, and retry restores the canonical department
choice without notifying access loss. All eight focused posting panel/workspace
tests pass. The full shell suite passes **1,414 tests** in **328.50 s**; shell
typecheck and full lint pass. These are rendered component/API-boundary tests,
not normal authenticated browser acceptance.

Commands actually run from the candidate worktree with cached Node 24.22.1:

```sh
pnpm --filter @helivanta/shell test 'app/(workspace)/workforce/posting-drafts-panel.test.tsx'
pnpm --filter @helivanta/shell test 'app/(workspace)/workforce/posting-drafts-panel.test.tsx' 'app/(workspace)/workforce/posting-workspace.test.tsx'
pnpm --filter @helivanta/shell test
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/shell lint
```

The first command was observed red before the fix and green afterward. The full
suite log is `/tmp/helivanta-posting-shell-access-cutoff-tests.log`. No backend,
permission, canonical schema or migration changed. Normal-role browser acceptance
and the broader effective-posting/departmental/Pharma gates remain open.

Local build preflight additionally found the existing UI listener's `.next`
directory absent, with `/login` returning HTTP 500. It stopped before mutation.
The owned loopback fixture was rebuilt from source with `next build --webpack`,
restarted on 127.0.0.1:4399 and verified with login HTTP 200. The production-style
build passed with the existing webpack export/standalone-tracing warnings; those
warnings are not claimed resolved. Capture, build/server logs and runtime metadata
are private under `shared-staff-foundation-20261102/`. Production is unchanged.

Normal browser navigation and sign-in now work; Chrome's prior
`ERR_BLOCKED_BY_CLIENT` is not a current blocker. Admin and independent reviewer
sessions completed a short-lived reader proposal and independent approval through
the UI, reaching active version 5. The recipient still had a revoked historical
login link. A fresh link was proposed and independently approved through the UI,
retaining the old revoked link. The recipient's normal session still revealed no
posting hospital. Read-only SQL established the source reason: its underlying
employment review is revoked. IAM allows both posting-read and Staff-actor-use;
identity/employment epochs match, so neither an active assignment, fresh link nor
an Active Staff snapshot revives that revoked review. No source fact was rewritten
to force positive acceptance.

The private `posting-reader-ui-checkpoint.json` and the updated existing
`local-staff-ui-qa.json` retain canonical IDs, observed states and screenshots;
credentials remain outside Git. All business mutations used normal UI; persistence
verification was read-only. Positive eligible reader/preparer create/edit/history/
retry and the full browser matrix remain open. A newly reviewed source is required
for those positive cases. Draft PR #1532 remains unmerged and no E2E tag is applied.

## Posting credential cutoff and authorization availability — 2026-11-03

Extended the scoped posting acceptance fixture with administrator credential
revocation while its independently reviewed posting assignment stays active.
All seven protected GETs, command PUT and scope discovery reject the old signed
session with 401, without a successful Staff-source-use audit. The compound IAM
command guard also refuses the old authentication time before entering its
callback. The independent administrator can still retrieve the historical
receipt. A newly minted signed session with an authentication time after the
revocation watermark can read/retry the unchanged receipt, with one history
revision; the original token remains denied afterward. This models fresh session
issuance; it is not browser sign-in acceptance. The fixture deliberately keeps
its outer authentication revocation checker permissive, so this demonstrates
the scoped owner-command watermark check independently.

A controllable proxy around the real disposable OpenFGA server returns 503 during
authorization failure. The seven GETs, retained-command retry, a new command and
scope discovery fail closed; no successful source-use audit is added. Direct
owner scope checks also report authorization unavailable. After recovery the
attempted new command is 404, and the original receipt/history survive unchanged.
HTTP failures in this fixture occur at the outer authorization resolver; the
direct scope checks additionally exercise owner authorization unavailability.
No owned repository or authorization decision is mocked.

The first complete parent integration run passed in 310.346 seconds. A temporary
local mutation bypassed the IAM credential watermark only for the synthetic
recipient. The new case failed at its first protected read: expected 401,
actual 200 with posting source data. The exact original file was restored and
SHA256 verified before the final complete Global API package run. All 22
top-level tests passed with the race detector in 332.417 seconds, including
the added failed-new-command and direct owner-authorization checks.
No runtime or deployment was rebuilt from the mutation. Private evidence:
`/tmp/helivanta-posting-credential-first-pass.log`,
`/tmp/helivanta-posting-credential-mutation-failure.log` and
`/tmp/helivanta-posting-credential-mutation.json`.

Commands actually run from `backend/` (the private runner configures the local
Docker socket and releases its own disposable fixtures):

```sh
python3 /tmp/helivanta-posting-test-runner.py ./internal/globalapi -run '^TestPreEmploymentPersonReviewPreservesInactiveJoiner$'
python3 /tmp/helivanta-posting-credential-mutation.py
python3 /tmp/helivanta-posting-test-runner.py ./internal/globalapi
go vet ./...
go build ./...
golangci-lint run ./internal/globalapi/...
go run ./cmd/contractdocs -check
```

The runner executes `go test -race -v -count=1 -timeout=20m`; the final full-package
log is `/tmp/helivanta-posting-globalapi-verified.log`. The mutation run is the
intentional red check, not an unresolved product failure. Vet, build, affected
lint (zero issues), contractdocs check, changed-file gofmt and `git diff --check`
also pass. No routes, schemas, permissions or migrations changed in this
acceptance follow-up.

Normal reader/preparer browser acceptance remains open: a fresh Chrome tab to
`http://helivanta.localhost:4399/login` was again blocked with
`ERR_BLOCKED_BY_CLIENT`. No browser protection was bypassed. Draft PR1532 stays
unmerged. No production rollout, migration or operating authority changed.
Effective posting approval, departmental clinical access, supervisors/transfers,
contractor/end/rehire lifecycles and Pharma operating consumers remain separate
open gates; this acceptance evidence does not complete #417/#418/#429.

## Reviewed source and appointment expiry — 2026-11-03

Two additional independently reviewed digital Staff fixtures now exercise the
actual earliest source deadlines: reviewed-person validity before the appointment
end, and an approved fixed employee appointment ending before person validity.
Each fixture waits for its persisted business cutoff, then proves a real posting
save has passed admission and is blocked at its save-audit insert in PostgreSQL.
The combined IAM/Workforce guarded root cancels that in-flight save at the cutoff
without releasing the audit lock. It returns 403 with no receipt and rolls back
the pending draft, save audit and successful source-use audit. Independently
current administrative reads confirm no command or draft was committed.

Post-expiry scoped retries, receipts and department choices are denied; discovery
reveals no hospital. Staff and login-link snapshots remain Active, and the same
session still reaches ordinary IAM self-service. Effective authorization therefore
does not wait for physical link cleanup or globally disable the identity. These
cases prove reviewed-person and fixed-appointment cutoffs for this consumer;
they do not certify pharmacy registration/duty expiry, contractor/rehire workflows
or every lifecycle variation. IAM's employment basis mirrors the same earliest
source cutoff, so this is combined-boundary evidence, not proof that Workforce's
deadline clamp is independently necessary in these two particular fixtures.

The complete parent passes with real PostgreSQL/OpenFGA and race detection:
**276.052 s after exact source restoration** (the preceding complete expiry run
also passed at 262.171 s). The test deliberately observes PostgreSQL's calendar
boundary rather than advancing a Go-only clock; no arbitrary sleep or production
timeout change is introduced. Command: `python3 /tmp/helivanta-posting-test-runner.py
./internal/globalapi -run '^TestPreEmploymentPersonReviewPreservesInactiveJoiner$'`
from `backend/`, executing `go test -race -v -count=1 -timeout=20m`.

A temporary local mutation bypassed only the effective-state check in
`currentDigitalActorGeneration`. The complete parent with `-failfast` failed in
`activation_actor_enforcement`: expected 403, actual 503 `posting_source_unavailable`
after a source change. This establishes sensitivity to the denial contract; the
mutation still failed closed and is **not** evidence of a private-data disclosure.
The exact original file was restored and SHA-256 verified before the final pass;
no runtime or deployed binary was refreshed from the mutation. Private mutation
metadata and failure output are retained under
`/tmp/helivanta-posting-effective-source-mutation*`.

No product code, canonical migration, permission, dependency or production policy
changed. Vet and affected Go lint are checked separately. A fresh normal local
`/login` navigation was rechecked and is still blocked by Chrome
`ERR_BLOCKED_BY_CLIENT`; no browser warning or protection was overridden. Normal
Admin/reviewer/reader/preparer browser acceptance, remaining credential/recovery/
lifecycle cases, the recorded intermittent evidence-admission failure, effective
posting approval and operating duties remain open. Full Staff Management and the
broader reusable-foundation goal remain incomplete; no full acceptance tag,
merge or deployment is claimed.

## Shared Staff login-link and membership cutoff — 2026-11-03

Real signed-session/PostgreSQL/OpenFGA cases now cover login-link revocation
independently of IAM assignment withdrawal. The preparation assignment remains
current after the link is revoked, but all new protected GET routes and retained
PUT refuse access: source, list, detail, command receipt, history, Staff choices
and department choices. Hospital discovery returns no scope. Denied attempts add
no successful source-use audit. The revoked link cannot be reapproved. A new
proposal and independent approval create a different active link bound to the
same canonical Staff ID and reviewed generation; the original link remains
revoked, and the original immutable posting receipt is unchanged and readable
under fresh valid authority. A separately authorized administrator retains
historical receipt access during cutoff.

A third independently reviewed actor now exercises attributable tenant membership
removal. The retained pre-removal session is refused. The fixture first observes
normal graph membership removal, then deliberately reinstates only an obsolete
OpenFGA role tuple (not an IAM membership row). That graph membership is verified
present both before and after the private requests. Owner scope authorization
still refuses every protected read/PUT and the old IAM source evidence. With the
fixture's raw graph outer gate deliberately stale, owner-filtered discovery
returns no hospital rows. The normal outer gate returns 403 before that stale
fixture tuple is introduced. Legacy membership regrant is refused, failed uses
commit no successful source-use audit, and an independent administrator still
reads retained history. This tests owner defense in depth; it does not redefine
production middleware's current-membership status contract or prove membership
recovery/rehire.

No application/schema/catalog/production change was needed. The complete
`TestPreEmploymentPersonReviewPreservesInactiveJoiner` parent passes with real
fixtures and race detection: **138.242 s** (previous membership/link run also
passed at 137.199 s). Exact command: `python3 /tmp/helivanta-posting-test-runner.py
./internal/globalapi -run '^TestPreEmploymentPersonReviewPreservesInactiveJoiner$'`
from `backend/`; the wrapper executes `go test -race -v -count=1 -timeout=20m`.
Vet and affected Go lint are checked separately. The broader Staff goal remains
active and incomplete. Source-expiry/remaining credential and lifecycle variants,
normal browser acceptance, the recorded intermittent evidence-admission failure,
effective posting approval and product operating duties remain open. No full
acceptance label, merge or production deployment is claimed.

## Posting source ordering and preparer downgrade — 2026-11-03

Two independently reviewed digital Staff actors now exercise both PostgreSQL
supervision-lock orderings for a retained posting-command retry versus an
identity-source edit. A source edit queued first withholds the actor's retained
receipt (403). A retained retry queued first returns the original receipt and
commits one successful source-use audit before the edit. After either source
change, later scoped GET/PUT attempts are denied; restoring old identity fields
cannot revive the reviewed generation. An independently authorized administrator
can still read the immutable historical receipt. This proves retained-command
release ordering; it does not claim every source-ending/write variation.

The same actors now exercise preparation-role withdrawal and fresh approval:
withdrawal immediately denies retained PUT and department-choice access; stale
IAM evidence cannot enter the source callback. Independently approved reader
access still reads the original receipt, and hospital discovery reports
`can_prepare=false`. A new independently reviewed preparer assignment plus fresh
current evidence restores preparation without changing the retained receipt.
Source audit assertions are now bound to the exact Staff actor, so one actor's
successful uses cannot satisfy another actor's audit check. The two fixtures reuse
the hospital's existing verified Organization registration rather than attempting
a second registration; the API correctly rejects duplicate registration intent.

No product code, schema, role catalog, dependency or production policy changed.
The complete parent `TestPreEmploymentPersonReviewPreservesInactiveJoiner` passes
with real PostgreSQL/OpenFGA and `-race -v -count=1 -timeout=20m` through the local
runner: **121.897 s**. The preceding both-ordering run also passed (114.767 s).
The new fixture initially returned the correct 409 for a second hospital
registration; only fixture reuse was corrected, not that API assertion/contract.
Vet and affected Go lint are checked separately. CI for `31f08186` passed API/shell
builds, Trivy and GitGuardian; deployment advancement was skipped.

Remaining gates include source-expiry and remaining membership/link/assignment
revocation cases, normal Admin/reviewer/reader/preparer browser acceptance, the
recorded intermittent evidence-admission failure, effective posting approval and
operating duties. The local Chrome block and broader Staff ledger remain open.
No runtime acceptance tags, merge or production rollout are claimed.

## Shared Staff source boundary and posting races — 2026-11-03

The reusable source seam now has direct real-IAM/PostgreSQL evidence that a valid
source command cannot be borrowed for a different tenant, subject or hospital,
used on another owner database, or invoked after its guarded callback completes.
None of those rejected uses reaches the product callback or creates source-use
audit evidence; only the original authorized use commits its audit.

Scoped posting retries now run behind observed PostgreSQL supervision-lock
barriers. Identical concurrent commands return one immutable receipt and history
revision. Concurrent conflicting intent produces one success and one 409, with
only one business mutation audit. A source-changing identity edit queued before
a posting retry commits first; the retry returns 403 without disclosing its
retained receipt. Restoring the old identity fields still cannot revive the
reviewed generation. At that checkpoint the complementary command-before-source-
ending ordering was still open; the newer ordering/downgrade section above now
proves that behavior for retained retries. Source expiry/remaining assignment and
membership revocation cases, normal positive-role browser journeys and production
acceptance remain open.

These are acceptance tests for existing owner-guard behavior; no application,
schema, permission, dependency or production configuration changed in this
follow-up. The complete `TestPreEmploymentPersonReviewPreservesInactiveJoiner`
parent passes with `go test -race -v -count=1 -timeout=20m` through the same private
local runner: final duration **103.248 s**. This includes the earlier fixture
cases needed to create and independently approve the real digital Staff actor.
`go vet ./internal/globalapi` and affected Go lint are checked separately.

An unchanged earlier employment-evidence admission scenario returned 503 after
storage resumed on one intermediate run. Its assertion was not relaxed or
skipped. Both unchanged subsequent parent runs passed; the intermediate failure
is retained privately in `/tmp/helivanta-posting-globalapi-employment-admission-failure.log`.
That intermittent failure is not claimed fixed. Runtime acceptance tags remain
withheld. CI for `53bbd2ba` passed API/shell builds, Trivy and GitGuardian;
deployment advancement was skipped. Full Staff Management remains incomplete.

## Scoped posting workspace candidate — 2026-11-03

PR #1532 now implements the scoped Workforce reader/preparer consumer and
`/workforce/postings` workspace locally. This supersedes the earlier “no scoped
consumer” checkpoint below. **Not deployed or normal-browser accepted.** Overall
status remains **10 partial / 10 awaiting audit / 1 research / 0 fully accepted**;
no completion percentage or acceptance tag is justified.

Implemented: separate `workforce.posting.read`; six additive `/posting-staff/:id`
draft routes; bounded `/posting-scopes` (20 hospitals) and `/posting-targets`
(50 Staff); current scoped department choices; canonical hospital/target DTOs;
separate SDK paths/cache keys; Staff navigation; reader/preparer UI and save flow.
Targets expose only ID, employee code and display name. Maximum escaped fields
plus continuation fit 64 KiB. Expected account headers and no-store apply.

Commands refresh the principal, compare current operation/actor receipts and hold
IAM/current digital Staff authority through the root commit. Responses follow
commit; writes acquire the supervision gate before actor/target locks. Audit
retains assignment/version/epochs and actor/link/generation. Scoped denial never
falls back to a legacy grant. Candidate append-only migration
`0074_workforce_scoped_postings` keeps exact-target draft purpose separate from
directory purpose. It retains one permissive tenant policy and existing Staff
write checks, and adds a restrictive DELETE policy. No profile-edit/delete right
is granted. Historical migrations are unchanged.

The full migration test initially exposed the one-permissive-policy invariant;
the candidate was corrected without weakening the validator. The unpublished
loopback QA policy experiment was corrected with private before-state capture;
no migration history, Staff records or production resources were deleted.
A second full-suite regression exposed that replacing the original Staff policy
expression erased later HR-intake/identity/leave purposes. The final migration
extends the current `pg_get_expr` expression instead, preserving all earlier owner
purposes and write checks. Focused identity-case/source, scoped-leave and posting
RLS regression tests pass. The existing statement-level no-delete trigger also
remains mandatory; the new test correctly expects its rejection.

Source-review pins were refreshed after reviewing actual router/module additions
and the legacy-preserving posting transaction refactor. Legacy posting reviews
now also pin the extracted transaction helper and overriding migration function.
Inventory is **778
registered surfaces / 755 unclassified**; no new runtime acceptance is asserted.

Normal browser acceptance is open: candidate shell/API run locally, but Chrome
blocked normal sign-in with `ERR_BLOCKED_BY_CLIENT`. Mocked UI and HTTP integration
tests do not count as normal login acceptance. No dependencies were downloaded.
An optional broad script sweep found four mobile-tooling suites unavailable due
to missing Expo dependencies in this worktree; affected posting/template/inventory
checks are tracked separately.

Remaining: normal Admin/independent reviewer/approved reader/preparer browser
journeys, persisted results and role-revocation UI evidence, the remaining scoped
commit-order/expiry matrix, and approval for the named revision plus IAM `0024_iam_staff_posting_templates`
and Workforce `0074_workforce_scoped_postings` before production.
Issue #429 remains open for clinical grouping, roster-derived access, referrals
and sensitivity controls. Effective postings, supervisors/transfers, contractors,
lifecycle administration and Pharma operating duties remain parent-ledger work.

### Verified candidate commands and results

The private local runner executes real `go test -race -v -count=1 -timeout=20m`
against isolated PostgreSQL/OpenFGA fixtures; all final runs used zero fallback
forwards and stopped only their own fixture containers. Final commands:

- `python3 /tmp/helivanta-posting-test-runner.py ./internal/globalapi -run '^TestPreEmploymentPersonReviewPreservesInactiveJoiner$'`
  — pass, 104.437 s, including scoped audit-failure rollback with no receipt/history revision.
- The same runner for `./internal/modules/workforce` — 319 top-level tests pass,
  642.930 s; `./internal/modules/organization` — pass, 18.021 s;
  `./internal/archtest` — pass, 44.382 s; `./internal/platform` — pass, 11.827 s.
  Three exact single-object scoped GETs have explicit pagination-allowlist reasons;
  lists/history retain cursor pagination. No general pagination gate was relaxed.
- `pnpm --filter @helivanta/api test` — 609 tests pass;
  `pnpm --filter @helivanta/shell test` — 1,411 tests pass, followed by the final
  focused scoped-workspace suite — 3 tests pass (reader, preparer, required-field save).
- API/shell `type-check` and `lint`, `go build ./...`, affected `go vet` and
  `golangci-lint run` — pass. Canonical generation checks included in API typecheck.
- `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check` — pass;
  `node scripts/staff-operational-access-inventory.mjs --check` — pass.
- `node --test scripts/staff-template-migrations.test.mjs scripts/staff-operational-access-inventory.test.mjs`
  — all 62 pass. The optional unrelated broad script sweep cannot run four Expo
  suites because their mobile dependencies are absent; no installation was attempted.
- `API_URL=http://127.0.0.1:8199 pnpm --filter @helivanta/shell exec next build --webpack`
  — pass, including `/workforce/postings`. Existing React Hook Form export/standalone
  tracing warnings remain; this does not accept the portable deployment artifact.
- `git diff --check` — pass. Repository visibility verified **PUBLIC**.

These commands ran from the candidate worktree (Go commands from `backend/`)
using cached Node 24.22.1. Current owner API, gateway and final shell run locally;
production was not changed. Passing suites do not complete the open normal-browser,
remaining cutoff/order matrix or production rollout gates listed above.

## Scoped departmental-choice consumer candidate — 2026-11-03

The first real consumer now composes the registered current Staff actor guard:
Organization's new hospital-bound posting-department choices projection. A reader
is denied; an independently approved preparer can read current active canonical
heads without governance/history or mutation authority. Legacy tenant-admin
permission alone is also denied. No duplicate department or Staff master is
created and no operating posting is activated.

The real Global API reviewer/employment parent test first returned 404 for the
missing endpoint. The restored test now passes reader/preparer separation,
wrong-hospital denial, retired-head exclusion, legacy-admin denial, scope-epoch
changes and identity-source invalidation. A newly added preparer grant correctly
invalidates the previously captured IAM receipt; the test now asserts that denial
and refreshes current evidence before further commands. Injected Organization
audit failure returns no data and rolls back Workforce source-use evidence in the
same root transaction. The fixture uses the repository's test migration harness,
not a raw/unavailable database handle.

Pagination uses the existing canonical envelope/keyset, default/max 50. Repeated,
empty, unknown and oversized query options are refused. Canonical-model
serialization verifies the 64 KiB budget including maximum field lengths, JSON
HTML escaping and a 2,048-character continuation. The SDK uses the new projection
and rejects retired, foreign, duplicate, private-field and invalid-continuation
responses. It does not fall back to the governance endpoint.

Final local race runs passed: the complete reviewer/employment parent (98.314s),
Organization (18.462s) and architecture (49.801s). The independent permission
matrix explicitly classifies the hospital-scoped capability without adding a
legacy job-role grant. Build and affected lint passed; final SDK/typecheck results
are recorded with this checkpoint. No normal browser acceptance, production
rollout, new SQL migration, acceptance label or issue closure is claimed. Scoped
draft read/preparation purpose RLS, bounded posting workspaces/targets, source-end
and ordering evidence and normal UI journeys remain open. Full Staff counts
remain **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## Scoped posting catalogs and actor boundary candidate — 2026-11-03

Work continues in `feat/scoped-posting-actors-20261103`; this is an unmerged,
unreleased candidate. Full Staff Management and departmental acceptance remain
incomplete: **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.
No completion percentage is inferred from test counts.

- Added append-only IAM migration `0024_iam_staff_posting_templates` from
  `contracts/staff-access.json`, preserving earlier catalog snapshots. The
  `staff-postings-v1` catalog introduces distinct hospital posting reader and
  preparer templates. Both include Workforce's current-actor source-use
  permission; neither grants operating posting approval or structure management.
- Real PostgreSQL/OpenFGA catalog/projection tests pass for both complete
  permission snapshots. The reused projection fixture originally synthesized a
  single permission; it now reads the full published template. The source-use
  permission has an explicit Workforce declaration and independent matrix entry.
  Source use still requires exact current IAM callback evidence: a tenant-wide
  permission alone cannot consume the guard.
- SDK posting-family reads previously resolved to `/api/v1undefined`. Both new
  regression cases reproduced that defect and pass with the explicit bounded
  posting assignment endpoint. Assignment administration offers the two
  templates only when the server's exact immutable catalog matches. Four UI
  regressions were observed failing before implementation; missing, duplicated
  and broader catalogs remain unavailable.
- Workforce's candidate actor guard rejects absent IAM evidence and historical
  reviewed links without digital employment generations. The historical-link
  test now uses a real independently proposed/approved/projected IAM assignment
  in bootstrap. An earlier test setup issued evidence directly from Workforce;
  the architecture gate rejected it and that setup was replaced without widening
  the issuer allowlist. A resource-contention timeout occurred on the first real
  fixture run; the subsequent exact test passed. No source-use audit is retained
  on denial and the historical link remains active for its existing consumers.
- Canonical generators, contractdocs generation/check, SDK/shell typechecks,
  596 SDK tests, 45 assignment UI tests, affected Go build/vet/lint and 62
  template/inventory script tests passed. The full shell suite (1,409 tests), Global API reviewer/employment parent
  workflow, full IAM/Workforce/bootstrap race suites and architecture checks passed.
- The configured webpack production build passed using the existing cached
  dependencies. The first build lacked `API_URL`; the configured Turbopack build
  then refused dependencies linked outside this worktree. Webpack emitted
  existing React Hook Form export warnings and warned that 27 external-cache
  files were omitted from standalone tracing. This build is not evidence of a
  portable deployment artifact. No dependency download/install was performed.
- Reviewed source diffs before refreshing 39 pins: IAM adds only the forward
  catalog migration in its module file; Workforce adds the source-use permission;
  link/generation helpers preserve existing historical compatibility while
  exposing current generation deadlines for the new boundary. Inventory remains
  769 registered surfaces, 746 unclassified, with no additional runtime acceptance.

The owner-provider seam is now registered in production bootstrap and the local
test harness through the shared typed interface. The complete real reviewer /
reviewed digital-employment parent workflow passed with successful actor use,
non-identity-edit compatibility and changed/restored identity rejection under a
still-live IAM assignment. The success/rollback/denial audit assertions also pass in the complete parent
workflow rerun. No normal browser journey or posting consumer is implied by
that boundary test.

Local infrastructure verification: the first broad run found an outdated
23-migration assertion (fixed to include exact migration 24). Later broad runs
reported connection refusal and timing failures. A fresh OpenFGA container
returned HTTP 200 inside the `helivanta-tests` VM but its Mac published port
refused connections; Lima logged failed SSH forwarding. Restarting only the VM's
`lima-guestagent` restored fresh fixture health. A diagnostic loopback port also
collided with the separate withdrawal VM's existing SSH forward, which was left
untouched. The private sequential runner captures only its own fixture metadata,
stops its test containers and removes any fallback loopback forward after each
package. Subsequent successful workflow runs used **zero** fallback forwards.
Named local QA services, images, volumes and the other VM were preserved. Recovery
metadata is at `/tmp/helivanta-posting-20261103-test-container-state.json`; runtime
logs and the temporary runner are under `/tmp/helivanta-posting-*`. No production
infrastructure was changed. Run the complete
`TestPreEmploymentPersonReviewPreservesInactiveJoiner` parent: the legacy nested
activation fixture depends on preparer grants seeded by earlier parent cases.

Commands actually run for this candidate (the private sequential wrapper supplies
only the local Docker socket, records logs and stops its own fixtures):

```sh
# backend/; the private runner exports these local fixture settings
export DOCKER_HOST=unix:///Users/samyakrout/.colima/helivanta-tests/docker.sock
export TESTCONTAINERS_RYUK_DISABLED=true
go test -race -v -count=1 -timeout=20m ./pkg/authz
go test -race -v -count=1 -timeout=20m ./internal/globalapi -run '^TestPreEmploymentPersonReviewPreservesInactiveJoiner$'
go test -race -v -count=1 -timeout=20m ./internal/modules/iam
go test -race -v -count=1 -timeout=20m ./internal/modules/workforce
go test -race -v -count=1 -timeout=20m ./internal/bootstrap
go test -race -v -count=1 -timeout=20m ./internal/archtest
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
go build ./...
go vet ./internal/globalapi ./internal/bootstrap ./internal/platform ./internal/modules/iam ./internal/modules/workforce ./pkg/authz
golangci-lint run ./internal/globalapi/... ./internal/bootstrap/... ./internal/platform/... ./internal/modules/iam/... ./internal/modules/workforce/... ./pkg/authz/...

# repository root; Node 24.22.1 from the existing mise cache
pnpm --filter @helivanta/api exec vitest run
pnpm --filter @helivanta/shell test
pnpm --filter @helivanta/api type-check
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/api lint
pnpm --filter @helivanta/shell lint
API_URL=http://127.0.0.1:8199 pnpm --filter @helivanta/shell exec next build --webpack
node --test scripts/staff-template-migrations.test.mjs scripts/staff-operational-access-inventory.test.mjs
node scripts/generate-staff-access.mjs --check
node scripts/staff-operational-access-inventory.mjs --check
git diff --check
```

Still required before merge/release: compose the registered owner guard in
both real consumers; narrow current Organization department choices; posting
read/preparation admission and purpose RLS; bounded hospital/Staff discovery;
exact SDK projections; real digital-actor positive/negative, source-ending and
rollback/race coverage; and normal Admin, independent reviewer, preparer and
reader browser journeys with persistence checks. The existing browser tab is
still the earlier posting-draft build and logged out; inspecting it does not
count as acceptance of this candidate. No production rollout or database
migration was performed. Effective postings, supervision/transfers, contractors,
suspension/end/resume/rehire and Pharma operating consumers remain parent work.

## Shared cutoff merged; scoped actor evidence candidate — 2026-11-03

[PR #1530](https://github.com/tesserix/helivanta/pull/1530) merged as
`cd962d4456132c2f73d9f2bb9f579642fceaf5e3` after exact-head CI passed. Final
source `95196dc4` passed local race suites for tenantdb (24.489s), IAM (328.815s),
Organization (17.434s), Workforce (580.118s) and Global API (131.032s), plus full
backend build/vet/lint, contractdocs and source-inventory validation. This closes
the observed deferred-commit expiry defect described in the earlier candidate
entry. No production rollout was performed for this revision in that work.

The next feature branch adds callback-bound IAM evidence for the existing
current, locked assignments. A real PostgreSQL/OpenFGA compound-command test
first failed without evidence and passed after IAM became the sole issuer.
Exact tenant/hospital/subject/action matching, immutable grant snapshots, callback
escape and stripped cancellation are covered. Architecture tests additionally
rejected an unauthorized selector call and ordinary/raw-string dot-import issuer
probes. The raw-string probe initially escaped the check; decoding import paths
before comparison corrected it. All temporary probes were removed and the
restored gate passed. Full local authz, architecture and IAM race suites passed
(6.856s, final architecture 72.605s, and 327.120s). An architecture rerun initially
omitted the Docker endpoint; the corrected run used the existing cached test VM.

This is a candidate foundation component. The current Staff actor provider,
new immutable role catalogs, source-purpose RLS, owner projections and positive
reader/preparer browser acceptance are still unimplemented. The
[specification](../superpowers/specs/2026-11-03-scoped-staff-posting-preparation.md)
proposes a separate Workforce-owned source-use permission composed with each
consumer's own permission; reference data is not reusable authorization.
Effective postings, standalone employers, supervision/transfers, lifecycle
enforcement and Pharma extensions remain mandatory parent work. Full Staff
counts remain **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## Shared command source-cutoff candidate — 2026-11-03

Implementation for the scoped posting foundation adds owner source deadlines to
the existing shared command; IAM now consumes that mechanism while retaining its
current denied/expired semantics. It grants no permission, creates no employee
master and adds no migration. An expired source cancels the root even if an
owner ignores its error or uses an already-canceled child context. Later sources
cannot extend an earlier cutoff; absent, foreign-database/tenant and escaped
command contexts are rejected.

Real PostgreSQL testing exposed a commit-boundary defect: while a deferred
constraint trigger blocked COMMIT, the source timer returned expiry but the row
subsequently committed. The candidate drains deferred constraints inside the
cancellable command, before the final context check and before issuing COMMIT.
It also checks the effective cutoff against the database clock after those
constraints finish, and closes owner participation before that finalization.
The same test now observes the canceled backend transaction ending and verifies
zero persisted rows. Ordinary lost COMMIT acknowledgments still require retained
command recovery; this change cannot promise that every transport error means
rollback. The existing compound IAM grant/expiry test also passed locally.

Broader owner regression checks are in progress. No new runtime revision has been
merged, deployed or accepted by this entry. Scoped posting reader/preparer roles,
their normal positive-role UI acceptance, effective departmental postings and the
remaining parent lifecycle/Pharma work remain open. Full Staff counts remain
**10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## Scoped posting source review — 2026-11-03

The [next specification](../superpowers/specs/2026-11-03-scoped-staff-posting-preparation.md)
and [delivery plan](../superpowers/plans/2026-11-03-scoped-staff-posting-preparation.md)
record the verified gaps after the approved `305294ab` release. New scoped
preparer/reader roles need current reviewed digital Staff actors, independent IAM
assignment approval, an Organization-owned narrow choice projection, purpose RLS,
root command deadlines and normal positive-role UI acceptance. Adding catalog
permissions alone would not complete those controls. Existing historical
nil-generation actor compatibility must not become the new posting admission rule.
The exclusive supervision gate also protects simultaneous retained-command retry;
any lock change needs deterministic race evidence.

Affected Organization/API guidance now distinguishes deployed structural metadata
and non-operating drafts from effective postings and operating authority. The
legacy reconciler still appends tenant-admin permissions; its documented clinical
default discrepancy remains open. These documentation/source-review changes add
no runtime authority, migration, production rollout or new acceptance result.
Full Staff counts remain **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## Posting preparation release — 2026-11-03

The first implementation slice is now a non-operating preparation draft against
canonical Staff and the current Organization department revision. It adds no
employee master, operating posting, login link or access assignment. The candidate
[contract](../api/workforce-postings.md) records six owner routes, the separate
`workforce.posting.prepare` permission, exact retry/recovery, immutable history,
source binding and forward migration `0073_workforce_posting_drafts`. Migration
0073 was explicitly approved and applied to the loopback QA fixture and production;
installed-schema and release evidence is recorded below.

Observed public-handler RED/GREEN evidence includes missing source-provider
503 behavior, canonical source/draft creation, the minimal concurrency-source
projection, and an original-command receipt scan fix. A new validation regression
observed 200 where 400 was required when a fractional timestamp truncated to the
zero date. Post-normalization validation and the SQL constraint now refuse it.
Real PostgreSQL/OpenFGA integration tests pass for required/invalid fields,
foreign tenants, reader-write denial, revoked roles/hospital grants, changed
intent/actor retries, stale Staff and retired structural sources, retained
historical receipts, paged history and concurrent commands. Fault injection proves
heads, revisions, saved audits and outbox events roll back together. Direct SQL
tests prove exact Staff/hospital/purpose RLS and immutable history/removal guards.

Owner-guard tests prove the shared restructuring gate and exact source row remain
locked until the outer product commit, and reject unguarded, escaped,
foreign-tenant and different-database contexts. Six SDK tests pass for validation,
scope-bound source/collection reads, exact-intent command recovery, receipt checks
and invalid history pages. Cached dependency links were reused; no dependencies
or container images were downloaded. Targeted Go race tests, API build, affected
Go vet, Go lint and SDK typecheck/lint passed. The full affected race suites
passed: bootstrap 62.039s, Workforce 672.595s, Organization 20.424s and platform
15.146s. The full API package suite passed 594 tests across 75 files.
Generator/registry checks and all 15 inventory tests passed. The ledger now
contains 765 surfaces: 23 source-classified and 742 unclassified; no runtime
acceptance is asserted. Six new preparation routes have their own source reviews,
and the ten prior Workforce module pins were reviewed against unchanged prior
route/grant/migration records rather than described as a migrations-only refresh.

A second regression reproduced host-timezone serialization losing seconds from
a historical UTC instant. All draft receipt paths now normalize timestamps to
UTC and preserve microseconds. The complete posting test selection passed after
that fix (bootstrap 42.670s, Workforce 7.414s). A lock mutation was observed
failing the deterministic owner-lock assertion, then restored; the owner-guard
suite passed again. The full architecture suite passed after explicitly declaring
the three singleton GETs, identifier-only event fields and tenant-admin preparation
matrix. Global API tests passed (139.310s); generation did not replace owner
authorization or end-to-end proof.

The directory now opens a required-field preparation editor with current
Organization choices and an explicit UTC interval. Immutable history displays
department, start, end, Staff source pins and recording time. Command failures
retain the exact command/intent for retry and receipt recovery, with navigation
protection and cancellation on a tenant switch. Reader controls and source outages
are covered by UI tests. Eleven focused directory/panel tests passed; the full
shell suite passed 1,396 tests across 172 files before the final history-only
presentation adjustment. The focused tests and configured production-style
webpack build passed after that adjustment. SDK/shell typechecks and relevant
linters passed. No container images or dependencies were downloaded.

Normal owner browser QA against the loopback Global API blocked empty required
fields and an end before start, created version 1, edited version 2, displayed both
retained revisions, and persisted version 2 after a complete reload. Read-only SQL
then verified one head, two immutable revisions and two save audits, unchanged
Staff version/epoch, inactive employment and zero login links. The local fixture
had 213 migration receipts before refresh and 214 afterward, with exactly 0073
added. The existing restricted reviewer signed in normally and could see the
canonical inactive Staff record but had zero posting preparation/edit controls.
No reviewer roles, memberships or grants were changed. Positive scoped
preparer/reader catalog acceptance remains open; this negative browser result is
not independent posting approval. Private evidence and screenshots are in
`~/.helivanta/staff-management-20261027/posting-preparation-20261103/`; the private
QA JSON was updated without changing credentials.

The scoped posting-preparer IAM catalog, independent posting review, effective
transfer/supervision/contractor lifecycle, standalone employers, Pharma duties and
operating cutoff acceptance remain open in the parent plan. Whole-Staff counts
remain **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**. This preparation
slice merged in PR #1524 as `305294ab` and is deployed after specific user
approval of the revision and 0073. The previous verified release was `6e9c4816`.

PR [#1524](https://github.com/tesserix/helivanta/pull/1524), application head
`e061b955`, incorporates current main. Its full API suite/typecheck (594 tests),
full shell suite (1,396 tests), configured production-style build and normal
owner browser reload/history smoke passed again. CI scripts, security checks and
image builds passed. CI E2E exposed an inherited Pharma payment test whose broad
text predicate returned while the current QR still awaited payment: the unused
payment list already carried an RRN. Its helper now requires the current paid
status region; the timeout and sale, invoice, stock and return assertions are
unchanged. This repairs the acceptance signal rather than permitting an unpaid
sale. The failure artifact is retained privately; no dependencies or images were
downloaded for diagnosis. Corrected CI
[41456474994](https://github.com/tesserix/helivanta/actions/runs/41456474994) passed
every job, including Go coverage and E2E, on `f852ce7c`. The merge tree exactly matches
that checked head. Main image workflow
[41460365395](https://github.com/tesserix/helivanta/actions/runs/41460365395) passed
build/scan/sign/publish and deploy-branch advancement. Production rollout of `305294ab`
plus 0073 was explicitly approved and succeeded as
`prod.01m6eanw1cfa4s587sv6rx21qc.0477a4c`. All seven deployments are 2/2 updated/ready;
API/Global API/shell are Argo Synced/Healthy. The ledger has 214 receipts with exactly
0073 added; four canonical function bodies, forced RLS, six triggers, eight indexes and
constraints were verified. Normal production owner create/edit/history/reload passed
against the existing synthetic inactive Pharma Staff, with one master/head, two
revisions/save audits, unchanged Staff version/epoch and zero login links. Existing
restricted reviewer normal sign-in denied hospital/posting access without grant changes.
Full Staff acceptance remains open. See the [release
journal](../deployment/releases/2026-11-03-staff-posting-preparation.md).

## Effective posting design — 2026-11-02

The [proposed posting specification](../superpowers/specs/2026-11-02-effective-staff-postings.md)
and [delivery plan](../superpowers/plans/2026-11-02-effective-staff-postings.md)
now trace canonical Staff/employment/posting references, transfers, contractors,
independent approval, supervision and Pharma duties to current-source enforcement
through commit. The source audit confirms that current scoped IAM requests are
hospital-only and legacy Pharma dispense rows lack hospital/outlet lineage;
neither gap is repaired by the existing outlet helper. The proposed owner-guard
composition is recorded in [ADR-0052](../adr/0052-staff-source-command-authorization.md)
with a closed provider boundary and explicit shared-database limitation. No
effective posting, standalone employer migration or operational acceptance is
claimed by this design-only entry. The merged preparation slice is recorded above;
whole-Staff completion counts remain unchanged.

## Approved foundation rollout — 2026-11-02

PR #1521 passed all required checks and merged as `058ea37a` after explicit
approval of head `1a632554` and its six additive migrations (Organization
0001–0004; Workforce 0069–0070). Main starts the image/deploy-branch pipeline;
the image build/sign/publish and deploy-branch advancement succeeded. Preflight
also found four pending Billing migrations (`0012_billing`–`0015_billing`) on
main; the user separately approved that additional scope before promotion.
Kargo promotion `prod.01m6dwe2xg0twhysa8xd97jjg0.1565f0b` succeeded. Production
runs `058ea37a`; the migration job succeeded and read-only receipt verification
confirmed all ten approved migrations. API, Global API, shell and document worker
are each 2/2 ready and updated. The three application Argo resources were
Synced/Healthy. Pre-rollout captures and promotion/migration receipts are private in
`~/.helivanta/staff-management-20261027/foundation-rollout-1a632554/`.
Credential migrations 0071/0072 were excluded from that foundation approval;
their later separately approved rollout is recorded below.

Normal production owner UI created and edited one inactive synthetic pharmacist;
Pharma's read-only canonical source displayed that same edited record after
reload. A read-only database check confirmed exactly one matching record at
version 2, inactive. Normal Organization UI registered the existing synthetic
hospital reference, received its verified source observation, created a synthetic
Pharma department and saved revision 2. History retained both names and UTC
effective times; the edited department survived reload. No new account link,
professional acceptance, operating grant or Staff activation was created.
See [production acceptance evidence](2026-11-02-staff-foundation-production.md).

## Credential subject review correction merged — 2026-11-02

A source review for Pharma specialization reproduced self-subject credential
approval despite proposer separation (200 instead of 409). Workforce migration
0071 and handler/SQL guards now refuse decisions through any retained Staff login
association and bind the decision actor. Focused tests include association races,
direct SQL and independent positive review. Local browser QA passed both subject
decision refusals, normal withdrawal and unrelated positive review with retained
history. The full owner race suite passed in 640.319s, and the final merged-base run
passed in 596.737s. Vet/lint, build and contract parity passed. A fresh 21-task
web gate passed; the source-drift CI refusal was repaired by re-reviewing ten
unchanged route/permission source pins, with all 15 ledger tests passing.
PR #1522 passed every required check on `95de52b3` (CI run 41431224601) and merged
as `6e9c4816`. The user then approved that exact production rollout and Workforce
0071/0072 after preflight proved those were the only pending migrations and all
211 historical definitions were unchanged. Kargo promotion succeeded; both
receipts, the subject guard and valid concurrent index are verified. API, Global
API, shell and document worker are each 2/2 at `6e9c4816`, with Argo Synced/Healthy.
Normal owner sign-in, credential-panel smoke and canonical Pharma source/reload
checks passed without new business records or grants. See the
[verified release](../deployment/releases/2026-11-02-credential-subject-independence.md).
The independent subject/proposer decision matrix remains local evidence; no full
production four-eyes or completed Pharma duty workflow is claimed.
See [local evidence](2026-11-02-credential-subject-independence-local.md).
Counts remain **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.

## HR department handoff — 2026-11-02, active source candidate

Workforce now retains explicit versioned HR-to-Organization mapping requests,
without changing canonical Staff or old profile/payroll references. Organization
observes the exact active/current structural revision through retained messages;
matched snapshots remain historical evidence, never a posting or operating grant.
HR setup includes a canonical picker, required reason, history and safe retries.
See [the source contract](../api/workforce-department-links.md).

Focused real Postgres/OpenFGA/NATS tests pass, including owner-to-owner matching,
stale revision rejection, contradictory/replayed answers, explicit false, role
revocation and outage. Audit/outbox failure was reproduced as an incorrect 400,
then corrected to generic 500 with rollback. A lost-reply UI regression reproduced
an unintended new command/version after history refresh; unchanged retries now
keep the original command. The final full Workforce/Organization/bootstrap and architecture race suites pass,
with 588 API tests and 14 final focused UI tests (663 broader affected shell tests
before the navigation correction). Normal manager browser create/map/reload/edit/
historical-reference and unsaved-navigation checks passed on the production bundle.
See [exact local evidence and remaining acceptance](2026-11-02-staff-department-handoff-local.md).

Additional recovery tests now cover malformed/mismatched and reordered replies,
retained first-answer repair, Organization audit/outbox rollback and Workforce
observation-audit rollback/replay. Both identity guards were mutation-tested.
The existing reviewer passed normal local sign-in, read-only HR/history and
mapping-control denial without expanded permissions. The later authorized
mapping-reader check passed after narrowing an existing local manager grant,
including reload and retained historical receipts. A retained manager form was
also refused after its hospital grant became reader; read-only database proof
confirmed no third mapping or success audit, and the captured grant was restored.
No role or membership was expanded. The CI formatting failure is corrected locally; a fresh
21-task web lint/format/typecheck gate and scoped Go vet/lint passed. The full
follow-up race run passed (Workforce 578.404s; Organization 17.593s), with
contract/generator parity and a clean changed-file secret scan. Additional command
tests cover actor rebinding, stale HR revisions, stable historical retries and
concurrent distinct command keys committing one pending version. The actor guard
was mutation-tested; the refreshed full Workforce race suite passed.

Two additive migrations are persisted: Workforce 0070 and Organization 0004.
They were initially applied to local synthetic QA/disposable tests and are now
deployed with the approved foundation rollout recorded above. Source inventory is
759 surfaces, 17 classified and 742 unclassified. Whole Staff remains 10 partial,
10 awaiting audit, 1 research and 0 fully accepted. Canonical effective postings,
full HR-reference migration and Pharma specialization remain open; no issue is
closed; this handoff does not complete postings or operational authority.

## Pharma OpenFGA boundary — 2026-11-02, local regression

Actual Pharma catalogue and prescription routes now have a real OpenFGA/Postgres
regression for action permission intersecting with current explicit outlet scope.
It covers inventory write and owner prescription denial, foreign/unposted scope,
same-identity role revocation, independent outlet revocation, outage/recovery and
single-change audit/outbox persistence. A weakened write-permission mutation
failed with actual 200 versus expected 403; the restored route passed the focused
race test. See [detailed evidence and boundaries](2026-11-02-pharma-shared-staff-local.md#existing-pharma-authorization-boundary--real-openfga).

The full Pharma race suite passed in 203.057 seconds. Go build, scoped vet and
lint passed (zero issues). Browser recheck on the standalone production bundle
confirmed the same inactive canonical pharmacist and persisted department
history with UTC timestamps. No additional dependency/image downloads were needed.

This is verification of existing guards. Professional profiles, effective Staff
postings, operational lifecycle enforcement and scoped FGA projection acceptance
remain open. Whole-Staff counts and issue completion status are unchanged.

## Organization departments — 2026-11-02, local acceptance

The isolated shared-Staff branch now implements platform-owned hospital
references, bounded departments/sub-units, immutable revisions and normal owner
UI. Real Postgres protocol tests, architecture/bootstrap, Global API routing,
build/vet/lint and the production webpack build pass. Real OpenFGA tests verify
permitted manager, denied pharmacist, same-identity manager revocation with
retained reader access, and fail-closed outages.
See [local evidence](2026-11-02-organization-departments-local.md).

Normal browser writes created a synthetic Pharma department and counter sub-unit,
kept their parent link through edits, retained history and refused parent
retirement while the child remained active. Read-only verification found root
version 4, child version 2, six snapshots, correct current audit resource targets
and the installed deferred revision-integrity trigger.

Forward migrations `0001_organization`, `0002_organization_departments`,
`0003_organization_department_revision_integrity` and Workforce 0069 are persisted
and applied to the local synthetic QA runtime and disposable tests. Production is
unchanged. The ledger contains 754 surfaces: 17 classified, 737 unclassified; no
new operational or lifecycle surface is classified as accepted.

The [Pharma specialization and audit scope](../architecture/pharma-staff-specialization.md)
records pharmacist, assistant/PIC, manager/reviewer, owner, inventory/purchasing
and auditor responsibilities. Canonical Workforce postings, legacy HR handoff,
professional verification, effective outlet duties, scoped access projections,
cutoff enforcement and the Staff audit workspace remain open. Whole-Staff
completion counts remain unchanged; no issue has been closed or tagged complete.

## Pharma consumer acceptance — 2026-11-02, local only

Pharma now consumes the common Staff source at `/pharmacy/staff`. Normal local
UI onboarding created one inactive synthetic pharmacist; an owner edit advanced
that same canonical record to version 2 and Pharma displayed the change. A fresh
pharmacy-only account with no hospital Staff grants saw no Staff table or source
hospital. See [the exact local journey, checks and limitations](2026-11-02-pharma-shared-staff-local.md).

The affected suites passed: 566 API, 278 shared UI and 654 Pharmacy/Workforce
tests, with typechecks, lint and production build. The UI consumer itself introduces no regulated pharmacy
authorization; the separate organization foundation above adds local migrations.
Postings, standalone employers and full Pharma Staff specialization remain open;
whole-Staff completion counts below remain unchanged.

## Shared Staff reuse — 2026-11-02, active objective

The requested direction is one canonical Staff foundation with department/product
specializations, beginning with basic Staff management. The shared
`@helivanta/ui` `StaffDirectory` now renders the existing Staff workspace; owner
fetching, scoped authorization and protected actions remain in the workspace.
See [reuse boundaries](../architecture/shared-staff-foundation.md) and the
[full delivery plan](../superpowers/plans/2026-11-02-reusable-staff-foundation.md).

Verified locally: all 278 shared UI tests, all 397 Workforce workspace tests,
UI and changed-workspace lint, shared-package/shell typechecks and the webpack
production build. A negative stale-row-disclosure mutation failed at the intended
privacy assertion; the original component was restored byte-for-byte and all nine
component tests passed again. A fresh local owner login exercised
persisted directory records, filtering, the existing protected editor and cancel,
and the no-match state. No Staff record, grant or migration was changed by this
browser check. Production activation from the previous work remains a separate
completed rollout; no new deployment is claimed here.

Basic structural metadata now has local acceptance as recorded above. Workforce
postings, standalone employers and Pharma credential/designation specialization
remain open. Existing
HR department reference revisions must be reconciled with the single platform
owner rather than copied. Whole-Staff completion counts below are unchanged.

## Current status — 2026-11-02

Approved source `02b1e2b7` and all four recorded migrations are deployed. The user
has now explicitly authorized production source activation, which is enabled through
[GitOps PR #1619](https://github.com/tesserix/tesserix-k8s/pull/1619), preserving
that image and adding no migration or production role grant. Both owner pods
carry activation=true; all six application deployments are 2/2 ready and API Argo
is Synced/Healthy. Ledger remains 201 and production generation count zero.
See the [verified configuration release](../deployment/releases/2026-11-02-employment-activation-enablement.md).

New acceptance: the full UI-created **digital** applicant journey now passes
independent person/employment approvals, explicit activation, fresh independently
reviewed login link and generation-bound role, recipient admission, retained-session
account-disable cutoff and source-revocation cutoff while the assignment remains
recorded active. Five deterministic signed activation/recipient lock races also pass;
a negative shared-lock mutation fails cleanly and the full Global API race suite
passes in 113.627s. Temporary roles were withdrawn and the synthetic login link
revoked; all history remains. See the [complete evidence and limits](2026-11-02-employment-activation-enablement.md).

Whole Staff remains **10 partial, 10 awaiting audit, 1 research, 0 fully accepted**.
Legacy/governance migration and complete suspension/exit/contract-expiry/rehire
consumer enforcement remain open. Source command availability grants no login,
assignment, payroll or clinical authority. Earlier dated entries below are
historical evidence; use this section and the linked release record for current state.

## Operational activation implementation — 2026-11-01, release candidate

Production remains at approved `7e5b7800`. Operational activation is incomplete;
whole Staff remains 10 partial, 10 awaiting audit, 1 research and 0 fully accepted
areas. No new production migration or rollout is claimed.

Implemented locally:

- Separate independently reviewed activation template and assignment UI. Exact
  source eligibility and activation PUT require current activation plus enrollment
  grants, original workspace and an active independently reviewed Staff/login actor.
- Exact approved source consumption records immutable old/new epoch lineage.
  Staff transition, generation receipt, IAM employment basis/history, audit and
  enrollment outbox commit together. Exact retry preserves the receipt; changed
  intent or second consumption conflicts. Audit/outbox fault injection rolls back.
- Private identity counters on person findings and employment reviews prevent
  changed/restored identity from laundering historical approval. Historical findings
  retain unknown provenance. Contact edits preserve counters and profile rows.
- Fresh digital login links and scoped assignment proposals bind the generation.
  Stale/unbound authority denies once a current basis exists; non-digital workers
  cannot obtain a fallback login. Legacy role elevation on bound employment returns
  `409 employment_scope_required`. Activation grants no membership, login or role.
- Activation UI requires a reason and explicit examination; freezes uncertain
  commands for exact retry; displays effective dates in UTC and a verified receipt.
- `WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED` defaults to false. Both new activation
  endpoints return authenticated `503 employment_activation_unavailable` while
  disabled. Existing generation authorization enforcement is never disabled by it.

Candidate migrations are `0022_iam_employment_activation_templates`,
`0023_iam_employment_bases` and `0066_workforce_employment_generations`. They are
forward-only and retained with their owners; do not remove immutable evidence on
rollback. All three have now been applied to the local-only browser QA database.

Recorded checks: 548 API tests across 71 files; 1,351 shell tests across 167
files; 12 focused activation/review UI tests after lint corrections; API/shell
typechecks; contractdocs generation/check; backend build/vet; API/shell lint and
scoped Go lint (zero issues); shell production build. Full IAM race passed in
365.913s and signed Global API race in 123.458s. Tenantdb race rerun inside the
test VM passed in 16.443s; full Workforce VM race passed in 434.152s.
Final architecture/authorization suites pass. The final complete Global API VM
race rerun passes in 90.083s after adding a PostgreSQL clock witness to the
existing leave-expiry test; no 403 or rollback assertion was weakened.
PR #1510 is draft while CI runs.
The first broad backend attempt hit a host port collision with an unrelated local
app before tenantdb assertions; it is not passing evidence. Raw logs remain in the
private reviewer-acceptance directory outside Git.

Local non-digital browser acceptance now passes independent person and terms
approval, explicit workspace-role switching, required reason/examination, activation
receipt, reopen/consumed refusal and immediate private-data cutoff on activation
assignment withdrawal. Owner reads confirm active Staff v2/epoch2. Submission
setup used a separate synthetic API fixture over prior UI-created applicant,
terms and released evidence; this is not a claim that every record was UI-created.
See the [activation QA matrix](2026-11-01-employment-activation-acceptance.md).

Remaining acceptance: digital recipient and fully UI-created source activation,
deterministic lock-order races, explicit legacy/governance cohorts, external-read
and every affected consumer cutoff, suspension/exit/contract-expiry/rehire journeys.
The release flag must remain off in production until these gates pass. See the
[delivery plan](../superpowers/plans/2026-11-01-staff-operational-activation.md).

## Reviewer workspace deployed — 2026-11-01

The approved `7e5b7800` rollout and all four Workforce migrations completed through
Kargo/Argo. The production ledger is exactly 197 entries, with only 0062–0065
added and both concurrent indexes valid/ready. Six workloads are 2/2; all 12
application pods use exact published digests with zero restarts. Argo and Kargo
are healthy. [Release evidence](../deployment/releases/2026-11-01-employment-review-workspace.md)
records the persisted migration manifest and verified target.

Fresh owner/reviewer production sign-ins pass. Both correctly have no employment
review hospitals without scoped authority, while the reviewer retains explicit
Staff/payroll denial. Positive independent decisions/provider/scanner workflows
remain locally accepted; no production grant or business record was created for
this smoke test. Operational generation, explicit legacy/governance review and
lifecycle enforcement remain open. Whole Staff remains 10 partial, 10 awaiting
audit, 1 research and 0 fully accepted areas.

## Reviewer workspace merged — 2026-11-01

[PR #1507](https://github.com/tesserix/helivanta/pull/1507) passed every required
Go, web, browser, script and secret-scan check and merged normally as
`7e5b7800b88f60a2579f6bacfd2f8dc23ea571ca`. Its Git tree exactly matches tested
head `176d16f4`. The merged image build, scans and signing jobs passed; API digest
is `sha256:0efea2483f833b1324ac3031070eb5f4c24fbb514ef916f10d1e9507f5c61dab`
and shell digest is
`sha256:4acf246f94ecf7c6d6cc257f6371256f3019b9957350c38396730cde6b7addef`.

The deploy-branch freight names that exact merge. Production remains on
`b6ba32e5`, with no live grant or migration change. Named approval was requested
for this new rollout and Workforce migrations 0062–0065: narrow employment inbox
and finding-selection read contexts plus their two concurrent indexes. The
expected ledger moves from 193 to 197. Read-only database readiness, completed
backup/continuous-archiving status and exact migration/image manifests are saved
in private QA evidence; no restore rehearsal is claimed. Operational-generation
activation and lifecycle enforcement remain open.

## Operational session boundary audit — 2026-11-01

Three shared IAM session surfaces are now source-reviewed as mixed authority:
renewal, activity and self sign-out. Their sessions carry no hospital employment
generation, and self sign-out has global effects. Hospital suspension, exit and
contract expiry therefore require owner-side employment cutoff while preserving
other-hospital and governance access. No session bypass or runtime activation was
added. The [consumer ledger](staff-operational-access-inventory.md) now records
17 source classifications out of 724 surfaces, 707 unclassified and zero full
cutoff acceptances. Whole Staff remains 10 partial, 10 awaiting audit, 1 research
and 0 fully accepted areas.

## Local scanner recovery — 2026-11-01

Fresh runtime diagnosis found the local scanner unhealthy: `clamd` was a zombie,
its 2 GiB cgroup recorded one OOM kill and TCP PING returned no response. The
existing synthetic fixture was restarted with a 4 GiB memory limit, preserving
its pinned official image and signature volume. Its private runtime JSON now
persists that limit. Fresh protocol controls pass: PONG, clean stream OK and
EICAR test-signature detection. This validates scanner recovery, not a new complete
browser upload journey or production capacity. Production was unchanged. Raw
failure/recovery receipts remain in the private reviewer-acceptance evidence store.

A fresh read-only Global API check also confirms that the withdrawn employment
reviewer sees an empty hospital-discovery page and receives 403 for the inbox.

## Employment-review workspace candidate — 2026-11-01

The normal scoped hospital inbox and protected reviewer workspace are now wired,
along with preparation from an exact employment draft/evidence screen. HR staff
select an owner-verified approved person finding and selected released employment
files without copying IDs or receiving general identity-review authority. Review
capabilities remain owner-supplied; commands pin the original workspace and freeze
uncertain retries. Private data clears on access cutoff. The
[acceptance matrix](2026-11-01-employment-review-ui-acceptance.md) distinguishes
completed local UI checks from remaining browser journeys.

Local browser QA has saved a draft, uploaded/scanned a PNG, bound its exact
revision, selected the approved person basis and recorded a pending submission.
The preparer sees withdrawal without approval; a fresh protected download exactly
matched the original 179-byte specimen. An empty-field browser check exposed raw
schema messages for policy/reason; a failing-then-passing regression and live
recheck verify clear guidance. Independent approval, immutable history, original-preparer withdrawal,
independent rejection/revocation and live reviewer-assignment/private-data cutoff
now pass. The target stays inactive at Staff version/epoch 1 with no login links.
One upstream 503 and native Chrome Save-dialog/relay interruptions are retained
separately from verified fresh reviewer downloads.

Global API race passes (95.779s), API 541 tests across 70 files, API type-check/lint,
Go build/vet/affected lint and all 15 workspace UI tests pass. Workforce/architecture/contractdocs race suites pass. The full shell suite
passes 1,343 tests across 166 files and its production build passes. Four persisted migrations (two read contexts and
two concurrent indexes) have been applied only to the backed-up local QA database.
Production remains at approved `b6ba32e5`; evidence-download PR #1506 is merged
but undeployed. No later revision is promoted under that earlier approval.

Source classification is 14 of 724 surfaces, with 710 unclassified and zero full
cutoff acceptances. Operational generation, legacy/governance review and lifecycle
enforcement remain open. Whole Staff remains 10 partial, 10 awaiting audit,
1 research and 0 fully accepted areas. No umbrella issue is closed.

## Protected employment-evidence checkpoint — 2026-11-01

The selected-review byte API and typed SDK now release only a frozen employment
file under current scoped HR-intake or independent employment-review authority.
The owner audits before storage access, releases database locks during the bounded
provider read, and rechecks the original actor, grants, review and source before
an audited release. Released generation, length and raw SHA-256 must match.
Four concurrent transfers per handler process bound payloads to 40 MiB, excluding
runtime/provider overhead; each file remains capped at 10 MiB. The shared browser
client reads into one authorized-size buffer and cancels overflow or cancellation.
No permission, schema, migration or production authority changes are included.

The initial signed regression observed the missing route's 404. Global API race
(**85.215s**) and workforce race (**563.241s**) pass, covering selected-file release,
legacy-only/wrong-mode denial, corrupt bytes/provider failure, replaced actor link,
review withdrawal during storage, failed release audit and transfer admission.
The architecture run initially flagged the single-file route as unclassified for
pagination; an explicit bounded-single-file entry resolves it, and the complete
architecture race rerun passes (**54.893s**). Contractdocs race also passes.
API validation passes **522 tests / 67 files**, type-check and lint. The full
shell suite passes **1,328 tests / 164 files**, its type-check and production
build pass. The build uses explicit local build-only API/auth configuration. Go build/vet,
affected Go lint, canonical model checks and all **15 inventory tests** pass.

A normal local person-review UI download exercised the shared streamed client:
the fresh downloaded PNG exactly matched the original bytes and checksum. This
checks the existing person UI, not employment-review UI acceptance. The new
employment route was exercised through the signed owner/storage protocol harness;
the normal employment-review inbox, selectors and decision workspace remain open.
Source classification is **11 of 721 registered surfaces**, with **710 unclassified
and zero full cutoff acceptances**. Operational activation and lifecycle
cutoff remain open. Whole Staff remains **10 partial, 10 awaiting audit, 1 research,
0 fully accepted areas**. No umbrella issue is closed by this prerequisite.

Production remains on the approved `b6ba32e5` release. Its rollout/evidence PR
[#1505](https://github.com/tesserix/helivanta/pull/1505) passed required checks and
merged. The history prerequisite PR [#1504](https://github.com/tesserix/helivanta/pull/1504)
is merged but undeployed; the new bytes prerequisite is a separate candidate.

## Protected employment-history candidate — 2026-11-01

The next bounded reviewer-workspace prerequisite adds a selected-review history
endpoint and canonical typed SDK. Every page uses current scoped HR-intake or
independent employment-review authority, a reviewed active actor link and exact
original tenant/subject assertions; SQL and forced RLS retain hospital/Staff/
review scope. Immutable receipts retain their frozen terms/evidence metadata and
require sensitive-read audit before release. They convey historical status,
without current capabilities, private bytes or employment activation.

The signed Global API regression first observed the missing route's 404. The final
serial owner race run passes, including bounded pagination, strict mode/query
shape, foreign Staff/review denial, denied legacy-only authority, audit failure
without data release, retained revoked/stale receipts and immediate refusal after
the scoped employment-review assignment is withdrawn. A fixture ordering failure
was corrected by waiting for the unaffected identity permission's new scope
projection before continuing the existing person-review cases. An overlapping
race attempt also hit the bounded authorization timeout; final owner checks ran
serially without weakening that timeout.

The SDK regression first failed for the missing method and then caught inconsistent
frozen sources across otherwise valid receipts. Canonical owned scope, order,
uniqueness, continuation, within-page source consistency and cancellation checks
now pass in the full API package suite (**513 tests / 67 files**), alongside its
type-check/lint. Workforce race tests, Go build/vet and affected Go lint pass.
Generated contract documentation, the canonical employment model generator and
the operational inventory are checked. No schema, SQL migration, permission or
production authority changed. Source classification is now **10 of 720 registered
surfaces**, with **710 unclassified and zero full cutoff acceptances**.

This is an undeployed API/SDK candidate, without employment-review UI acceptance.
The [implementation plan](../superpowers/plans/2026-10-31-independent-employment-review.md)
still requires the normal employment-review inbox, target/evidence selection,
protected bytes and Staff Admin/preparer/reviewer browser journey. Read-only
production readiness metadata is retained privately; existing production grants
were not inferred to be reviewed employment or governance authority. Operational
generation, legacy/governance review and lifecycle cutoff remain open. Whole Staff
remains **10 partial, 10 awaiting audit, 1 research, 0 fully accepted areas**.

## Person-review browser checkpoint — 2026-11-01

The [acceptance matrix](2026-11-01-person-review-ui-acceptance.md) records actual
local Staff Admin → scoped preparer → independent reviewer UI checks with real
released scanner evidence. Positive approval, original-preparer withdrawal,
independent rejection/revocation, protected download byte verification and
immutable history passed. A historical contributor's approval was correctly
denied; the original finding stayed pending and private detail cleared. A fresh
applicant avoided that contributor conflict without altering any provenance.
Approval left the target inactive at Staff version/epoch 1 with no login links.

Browser testing exposed raw schema validation messages for required preparation
fields. A failing-then-passing regression now verifies clear field guidance.
All **24 person-review tests**, the full shell suite (**1,328 tests / 164 files**),
shell type-check/lint, affected formatting and diff checks pass. The acceptance
matrix distinguishes local callback/download relay failures from verified
application results and records remaining browser scenarios. Private credentials,
raw receipts and screenshots remain outside Git. No production change occurred.

Person-review acceptance does not complete employment-review UI acceptance.
Employment inbox/history/protected bytes/selection, reviewed terms → operational
generation, legacy/governance migration and lifecycle cutoff remain open. The
operational source check passes for **719 surfaces / 9 classified / 710
unclassified**; strict classification completion correctly refuses activation.
Zero runtime cutoff acceptances are claimed. Whole Staff remains **10 partial,
10 awaiting audit, 1 research, 0 fully accepted areas**. #1035 and #417 remain open.

The user-approved validation-guidance rollout is now **deployed and verified** at
`b6ba32e5`; see the [release evidence](../deployment/releases/2026-11-01-person-review-validation.md).
All six workloads are 2/2 on the exact published digests, Argo/Kargo are healthy,
and all 193 migration IDs are unchanged. Normal production owner/reviewer sign-in,
scoped person-review discovery and reviewer Staff/payroll denials passed. No
production grant or operational activation was introduced; positive review
acceptance above remains a local result. The separate employment-history
candidate in #1504 is excluded from this rollout.

## Staff review foundations deployed — 2026-11-01

PR [#1501](https://github.com/tesserix/helivanta/pull/1501) merged as
`bd1f62d709c0b238699a71f02c0a15317cabd493`; its tree exactly matches the locally
validated `c73703a2` candidate. The user authorized admin-bypass merge and rollout.
A temporary PR-only repository-admin bypass was removed immediately after merge;
the original active main ruleset was restored and verified with no bypass actors.
The repository remains public.

All five PR CI jobs passed, including full Go race/coverage and **78 browser E2E
tests**. Local combined web validation passed all 30 lint/format/type/test/build
tasks. Local owner race reruns passed patientmaster (79.8%), Billing (77.6%),
Payments (77.7%) and Global API (86.7%), plus architecture/contractdocs. Earlier
parallel-load consent/event-wait failures and Docker startup failures remain
recorded as unsuccessful attempts; they are not counted as successful tests.

Both merged-revision images passed build, scan, SBOM, signing and publication.
Kargo promotion `prod.01m6a85hwx453b0d0gn5pdv5rh.c3ab8b2` succeeded. API, Global API,
shell and all three workers now run that exact revision with two ready replicas
each; Argo reports Synced/Healthy and Kargo verification is Successful. The
production ledger increased from **166 to 193**: exactly the **27 migrations** in
the [persisted manifest](../deployment/releases/2026-11-01-staff-review-candidate.md),
with no missing, unexpected or removed IDs. Canonical committed SQL hashes match.
Production login returns 200; the correct person-review/HR-intake scope APIs return
401 without authentication. No real Staff or financial records were mutated for
these smoke checks. The local UI now runs the validated release checkout; its
older integration checkout had an obsolete discovery URL and must not supply
acceptance evidence for this release.

These checks do **not** establish complete Staff browser acceptance. Positive
scoped preparer/reviewer review/evidence journeys, the employment-review workspace,
operational activation and lifecycle cutoff still need completion. Whole Staff
remains **10 partial, 10 audit pending, 1 research, 0 fully accepted areas**. The
operational ledger has 719 surfaces, nine source classifications, 710 unclassified
and zero full cutoff acceptances. Full Access/RBAC remains incomplete.

## Independent employment decisions candidate — 2026-11-01

Access/RBAC A01–A12 remains **incomplete**. Whole Staff remains **10 partial,
10 audit pending, 1 research, 0 fully accepted areas**. This checkpoint supersedes
older statements below that independent employment decisions are unimplemented;
older test results remain historical evidence for their named source only.

Persisted source now includes Workforce **0061_workforce_employment_review_decisions**
and IAM **0021_iam_employment_templates**. The exact reviewer template holds only
`workforce.employment.review`; `employment-v1` has five definitions while frozen
legacy/HR catalogs retain three/four. The Staff Access assignment area validates
its separate catalog, rejects extra/broader/missing definitions, and disables
retained commands when verification disappears. This is assignment setup, not the
normal employment-review workspace or browser acceptance.

Reviewer owner commands require exact tenant/subject and a single explicit
`employment_review=v1` projection, current scoped assignment and independently
reviewed active actor link. No legacy tenant-admin or identity-review permission
fallback supplies authority. Preparer withdrawal instead requires both current
HR-intake grants and the original proposer. Pending → approved/rejected/withdrawn
and approved → revoked preserve immutable source, contributors, child sets and all
command receipts. Self, declared subject, frozen contributors and historical login
associations cannot review/revoke. Ending stale records remains possible. Exact
replay reauthorizes; changed intent/stale version returns 409. Ending attestations
are false on the new action; original human approval remains in history.

Approved terms remain separate from operational employment. Approval leaves the
target inactive with no login/access activation. A later preparation draft does
not invalidate an approved historical terms record. GET distinguishes recorded
status/current applicability and computes legal capabilities. Review-only RLS sees
selected terms/binding histories, finding/evidence/classification/provenance and the
specific contractor sponsor. It retains original write/delete boundaries and
cannot edit source terms or obtain general enrollment/private-file authority.

Observed evidence so far:

- The signed Global API journey failed at reviewer GET with **400 instead of 200**
  before implementation; it subsequently passed independent approval, self/earlier
  author denial, exact decision/submission replay and draft-history preservation.
- Ending-route regression failed with **404 instead of 403** before implementation;
  withdrawal/rejection/revocation, stale/conflicting commands, mode validation,
  unavailable-evidence revocation and historical approval retry now pass (40.951s).
- A direct owner SQL approval incorrectly succeeded after evidence revocation.
  The reproduced test now passes (27.054s): the database approval guard locks and
  validates current Staff/draft/finding and selected file/binding/release/scan/
  classification/provenance, in addition to application validation.
- Review-only source visibility and forbidden source-edit probes pass. An active
  contractor sponsor initially appeared unavailable; selected-sponsor-only RLS
  corrects the regression (27.801s), while inactivation derives source_changed.
  This remains a synthetic legacy sponsor fixture, not full contractor lifecycle QA.
- SDK **33 employment-review tests** and full API **503 tests** pass. Separate
  preparer/reviewer scope modes, receipt state/version/actor/intent/attestation
  checks, cancellation and current-state/capability validation are covered.
- Staff Access **33 page tests** pass, including the separately verified employment
  role and rejection of incomplete/broader catalogs. Old HR fixtures now use their
  canonical frozen catalog instead of all latest templates.
- Template/inventory Node tests: **55 pass**. Permission-matrix declaration,
  workspace type checks, Go vet/build and Go lint (**0 issues**) pass.
- Full current-source Workforce race passes (**476.356s**), using
  `go test -race -timeout=20m -json ./internal/modules/workforce -count=1`.
  Corrected full Global API/arch/contractdocs race passes (**162.995s / 99.914s /
  6.666s**); IAM race passes (**411.506s**). Signed independent-decision audit and
  outbox failure probes prove head/history/evidence/outbox rollback and exact retry.
- The first broad Workforce run exposed scanner-role permission denial from SQL
  policy expansion and reached Go's default ten-minute timeout. The review-file
  helper now returns false before review-table access outside reviewer mode,
  without extra scanner grants. Its isolated race regression passes (**8.639s**).
  The next broad run had no failed assertions but also reached the default timeout;
  the isolated twenty-minute full run above completes successfully.
- Full monorepo `lint type-check test build` passes **22/22 tasks**, 18 cached,
  **7m4.148s**. Current totals: API **503**, shell **1,316**, UI **268** tests.
  The first web run reproduced a calendar-dependent pharmacy test: its fixed
  November 1 wait became today's wait. Pinning only that test's clock to November
  3 preserves the intended prior-day case; isolated **8/8** and full web pass.
- Canonical generator checks, contractdocs generation/check, source ledger check,
  affected formatting and `git diff --check` pass. The private unapplied binary
  is `helivanta-staff-employment-decisions-api`; its receipt/source manifest is
  retained in the existing private QA directory. It is not a rollout approval.

The source-review ledger now has **704 registered surfaces**, **9 classified**
and **695 unclassified**, with **zero runtime cutoff acceptances**. New review
commands remain hospital-operational actor consumers; they receive no employment
exemption. The source hashes/remaining obligations were reviewed and regenerated.

Still required: reviewer inbox/history/protected-byte owner and SDK/UI flows;
normal Staff Admin → independent reviewer → recipient/provider/scanner browser QA;
reviewed terms → operational employment generation; provisioning/contact verification,
membership recovery, renewal/suspension/resumption/exits/contract expiry/rehire;
legacy/governance review and every HTTP/background cutoff. No live fixture upgrade,
production migration/rollout, commit, push, merge or deployment occurred here.
New migrations are forward-only; application rollback must preserve review history.

## Immutable employment submission source — 2026-11-01

Persisted Workforce **0060_workforce_employment_review_submissions**, canonical
`workforce-employment-reviews.json`, generated Go/Zod models and the typed SDK.
Two Global API owner routes now prepare/read an immutable pending employment
submission: exact current Staff version/epoch/hash, open terms revision, approved
person-finding revision and one to twenty released employment-evidence bindings.
Required policy reference, reason and human attestations have no invented defaults.
Submission leaves employment status, login binding and operational access unchanged.
Independent employment decisions and operational activation are not implemented.

The new owner path forces both current scoped HR-intake permissions and a reviewed
active actor link; it never takes the old administration fallback. Existing intake
callers retain their earlier behavior. A Staff Admin without a scoped grant is
denied; the same administrator works after independently reviewed scoped authority.
The target can remain inactive/non-digital. Source/evidence changes derive explicit
unavailability while exact command replay returns the original immutable receipt.

Owner SQL freezes selected evidence-history/classification references and an exact
contributor set from Staff preparation, terms history, bindings/documents and the
person finding. Earlier terms authors remain recorded after a later preparer takes
over and their role is withdrawn. The SQL completion guard rejects missing/extra
contributors; child sets are transaction-bound and immutable. Scoped person-finding
read purpose sees only the selected finding and cannot mutate it. All head/history/
evidence/contributors, minimized success audit and identifier-only outbox effects
commit together; both audit and outbox failure probes roll back every durable row.
Exact original retries recover once without duplicate success audit or event.

Observed red/green evidence and commands actually run:

- Signed Global API regression first returned **404 instead of 200** (14.091s).
  Initial owner attempts caught an unexported embedded GORM binding and a
  classification query assuming a nonexistent Staff column; both were corrected
  against the actual canonical tables. Basic creation/replay/read passed (15.648s).
- Expanded mandatory-field/scope/immutability/atomicity tests passed (17.472s).
  Reviewed Staff Admin, finding revocation and current-state tests passed (26.789s).
  Distinct earlier-author/withdrawn-role history passed (30.488s).
- An agency sponsor source becoming inactive incorrectly left the view `pending`
  (red, 21.318s). Submission/read and SQL source/commit checks now derive/refuse
  that source correctly; the complete person-review case passed (29.808s).
  This uses an explicitly synthetic legacy sponsor fixture; it does not prove
  reviewed suspension, resignation or contract-expiry workflows.
- `go test -race -json ./internal/modules/workforce ./internal/globalapi
./internal/archtest ./cmd/contractdocs -count=1`: Workforce **516.755s** and
  Global API **75.593s** pass. The first arch run found missing bounded-single-GET
  and event-field declarations. The corrected `go test -race -json
./internal/archtest ./cmd/contractdocs -count=1` passes in **58.675s / 8.944s**.
  No test rule was weakened: one twenty-evidence-bounded GET and five individually
  classified opaque scope/record/version fields were explicitly documented.
  Staff identifiers remain protected personal references, not public data.
- `API_URL=http://127.0.0.1:8199 mise exec node@24 -- pnpm turbo run lint
type-check test build`: **22/22 tasks pass**, 16 cached, 36.497s. API **488**
  tests include **18 new receipt/cancellation cases**; script suite has 82 tests.
  Existing shell/UI results are 1,312/268. The first run exposed the earlier
  ledger's reserved local `module` variable; it now uses `ownerModule`/`moduleName`
  while preserving the generated wire shape. Interrupted tasks were rerun.
- `go vet`, `go build ./...`, affected golangci-lint (zero issues), gofmt,
  contractdocs generation/check, employment-review generator/check, affected
  Prettier and `git diff --check` pass. The consumer ledger's 15 tests pass;
  it now tracks **700** registered surfaces, **six** source classifications and
  **694** unclassified surfaces. No surface is accepted for complete cutoff.

These tests use signed sessions, actual IAM/OpenFGA/Postgres/NATS and GCS/ClamAV
protocol fixtures. They do not establish actual provider/scanner or normal-browser
Staff Admin/reviewer acceptance. No existing local QA runtime/database, account,
credential, membership, grant or production state was changed. No commit, push,
merge or deployment occurred. Private candidate/validation receipts retain source
hashes and the unapplied binary; older upgrade artifacts require revalidation and
no earlier named approval is broadened to migration 0060.

Next: separately declared employment-review authority/template/catalog, independent
decisions and protected reviewer reads/bytes, preparation/inbox/history UI and
normal role/provider QA. Reviewed operational generation, full provisioning/contact
verification/recovery, renewal/suspension/exit/expiry/rehire, legacy/governance review
and every HTTP/background consumer cutoff still remain. All A01–A12 and full
Access/RBAC acceptance remain open; whole-Staff counts stay 10 partial, 10 audit
pending, one research and zero fully accepted.

## Employment evidence and complete creation provenance — 2026-10-31

Persisted additive Workforce migration **0059_workforce_employment_document_provenance**
in the owner registry. Enrollment employment evidence now retains attributable
preparers/state-change contributors alongside identity evidence. A private
`creation_captured` flag records attributable INSERT origin; existing rows stay
false, and later writes/scanning cannot promote incomplete historical provenance.
Workers remain excluded from preparer attribution. Person-review submission,
current applicability and protected evidence authorization require that origin;
unproven history returns a provenance conflict or `evidence_unavailable` while
preserving the original receipt. A deferred SQL approval guard checks complete
provenance in the owner transaction. No employment/access activation is added.

Actual regressions and recovery evidence:

- The scoped HR-intake Global API test initially found **zero contributors** for
  a newly prepared employment document (red, 9.549s). With 0059, the test passed
  (14.142s); expanded scope/immutability/atomic recovery checks passed (14.321s).
- A before-0059 owner fixture creates real identity/employment upload intents,
  applies the actual forward migration, then uploads/scans through the API and
  worker protocol fixture. A later edit originally allowed the legacy identity
  file into person review with **200 instead of 409** (red, 23.752s). Captured-
  creation checks now reject that file with `person_review_provenance_unavailable`.
  Both file categories retain incomplete historical origin; a known new identity
  file has captured origin in the same inspection context, avoiding hidden-zero
  assertions. Combined focused race tests pass (28.574s).
- The current person-review test, including direct SQL approval without the
  required private provenance context, passed in 19.265s. Removing only the SQL
  approval trigger made that assertion fail: approval returned no error (11.277s).
  The original source was restored byte-for-byte before final build/validation.
- Current-source full Global API race passes in **66.267s**, arch tests in
  **48.696s**, contractdocs tests in **8.4s**, and the complete current-source
  Workforce race suite in **457.787s**. All four packages passed.
- `go vet ./internal/modules/workforce ./internal/globalapi ./internal/archtest
./cmd/contractdocs`, `go build ./...`, affected golangci-lint (zero issues),
  affected gofmt, contractdocs generation/check and person-review generator check
  pass. The regenerated consumer ledger and its 15 tests pass; all 698 registered
  surfaces remain represented, with 694 still unclassified.

Fault injection proves a failed contributor INSERT rolls back the document
revocation, audit and outbox together. Retrying after removing the fault records
one revocation and the new actor while retaining the original preparer; exact
replay creates no extra audit/event/contributor. An otherwise authorized manager
cannot rewrite/delete contributor history. Foreign tenant/mismatched hospital
contexts and an identity-review-only purpose cannot disclose employment origins.
These are real signed Global API/IAM/OpenFGA/Postgres tests using GCS/ClamAV
**protocol fixtures**, not real-provider/scanner or normal-browser acceptance.

The first broader run found prohibited direct schema-owner calls in both the new
fault probe and the earlier person-review atomicity probe. Their fault DDL now
uses explicit disposable fixture migrations; observations remain tenant-scoped.
No architecture allowlist or isolation rule was relaxed. The first Workforce
pass (499.564s) preceded the creation-marker correction and is retained only as
that earlier checkpoint; current-source evidence above supersedes it.

A new private API binary was built as `294ee4df2122…`, with a protected build
receipt naming 0054–0059. It has not replaced the existing QA runtime. The older
0054–0058 ready artifact now carries an explicit revalidation notice because it
lacks the creation-provenance fix; an earlier named approval request cannot be
silently broadened to the new artifact/0059. No existing database, account,
credential, membership, grant or production state was changed. No commit, push,
merge or deployment occurred.

The next implementation is specified in
[independent employment review](../superpowers/specs/2026-10-31-independent-employment-review.md)
and its [delivery plan](../superpowers/plans/2026-10-31-independent-employment-review.md):
exact terms/person-finding/evidence submission, separately declared reviewer
scope, immutable decisions and normal preparer/reviewer UI. Those APIs and the
operational-generation transition are **not implemented** by this checkpoint.
All A01–A12 provisioning/recovery/renewal/lifecycle/consumer gates remain open.
Counts stay 10 partial, 10 audit pending, one research and zero fully accepted.

## Registered operational-consumer source ledger — 2026-10-31

Added the [checked consumer ledger](staff-operational-access-inventory.md) for
ADR-0049/A04/A06/A07–A11. The current contractdocs source actually uses
`format_version`, `modules` and `unauthenticated_routes`; the checker consumes
those current fields rather than a remembered inventory shape. It covers all
644 registered module HTTP routes, 43 async subscriptions, one broadcast and ten
unauthenticated allowlist entries (698 total). Four directly inspected MediCore
encounter handlers are source-classified hospital operational; 694 remain
unclassified. Their concrete `inHospital` helper locks a hospital access grant,
but does not establish reviewed employment or effective cutoff. Each review
pins the registration and relevant source hashes and retains the missing
legacy/owner-guard/signed-session/UI obligations. No runtime acceptance follows.

The new generator/checker defaults every registration to unclassified, refuses
stale source/registration reviews, removed/duplicate reviews, duplicate HTTP
routes across owners and unknown registry fields. Permission names, `public`,
sessionless routes and worker registration confer no automatic exemption. CI now
runs its tests and drift check. Its optional classification gate is explicitly
insufficient for activation and currently refuses with 694 unclassified surfaces.
Background/Temporal/periodic/external wiring remains outside the registered input;
the companion note records concrete source candidates needing separate review.

Validation actually run:

- `mise exec node@24 -- node --test scripts/staff-operational-access-inventory.test.mjs`:
  all 15 pass. Initial tests failed before implementation; later assertions
  reproduced acceptance-marker, duplicate-owner-route and unknown-registry-field
  omissions before their fixes. Source/registration drift preserves prior output.
- Generator write, `--check`, both `node --check` commands and affected Prettier
  checks pass. `--check --require-classified` correctly refuses, not an acceptance
  pass. `go run ./cmd/contractdocs -check` passes from `backend/`.
- `git diff --check` passes. Actionlint found an existing SC2016 finding at CI
  line 106, outside the four added lines. Running actionlint on the HEAD
  baseline reproduced the same finding; `actionlint -shellcheck=''
.github/workflows/ci.yml` passes syntax validation. No finding was suppressed
  in the workflow.

This changes offline QA tooling, docs and CI only. No application behavior,
migration, existing QA runtime, account, grant or production state changed. Full
web/Go runtime suites were not repeated for this tooling-only delta; previous
application evidence is retained in the checkpoints below. No commit, push,
merge or rollout occurred. Counts remain 10 partial, 10 audit pending, one
research and zero fully accepted. Full A01–A12 remains the objective; independent
employment decisions/person-finding consumption and all lifecycle/consumer/UI
acceptance remain open.

Updated: 2026-10-31. Owner: Helivanta Staff Management delivery. This is the summary tracker for the full user-requested scope. The [workflow acceptance ledger](staff-management-acceptance.md) holds detailed scenarios and evidence; this document does not replace it.

## Person-review audit/outbox atomicity proof — 2026-10-31

Added fault injection to the signed Global API journey using actual Postgres
triggers in the disposable test database. Submission and independent approval
are each exercised with an audit INSERT failure and an outbox INSERT failure.
The source application/SQL/binary did not change in this checkpoint.

For failed submission, the owner returns 500/unknown-result and GET remains 404;
there is no head, history, bound evidence, contributor snapshot, successful write
audit or business event. For failed approval, the exact original pending head,
history/evidence/contributors, successful write audits and event counts remain
unchanged. After removing the injected failure, retrying the exact review ID,
command and payload succeeds, and repeated retries leave one transition/audit/
event per successful command. A known committed review is visible through the
same inspection context before checking failed commands, preventing RLS-hidden
zero counts from masquerading as rollback proof. Probe findings are explicitly
revoked after verification; their history is retained.

Fixture corrections are preserved in the failed logs:

- The system pool intentionally lacks SELECT on private review tables. Inspection
  now uses the owner-authorized tenant/Staff read context; no application/system
  privilege was widened. Helpers receive the current child test explicitly.
- The event contract uses `review_id`, and the decision audit action is
  `workforce.person_review.decided`. Earlier field/status-name assumptions did
  not activate the intended fault and correctly failed the required 500 assertion.
  The final triggers use the actual handler/contract values.
- Early probe findings remained approved and affected a later inbox expectation;
  retiring probes via the legitimate revocation workflow isolates the checks
  while preserving immutable evidence.

Logs: `/tmp/staff-person-transaction-failures.log`,
`/tmp/staff-person-transaction-failures-corrected.log`,
`/tmp/staff-person-transaction-failures-canonical.log` (still a fixture failure),
and passing `/tmp/staff-person-transaction-failures-verified.log` (14.120s).
The subsequent **complete Global API** race run passed in 57.905s at
`/tmp/staff-person-atomicity-globalapi.jsonl`; `go vet ./internal/globalapi/...`
and affected golangci-lint passed with zero issues
(`/tmp/staff-person-atomicity-lint.log`). No web code changed, so the preceding
22-task web result remains the relevant frontend evidence.

The existing local 0054–0058 schema/API upgrade approval remains pending.
No live fixture, scope grant, account, credential or production mutation occurred.
These tests close the direct audit/outbox INSERT rollback proof only. They do not
prove broker outage delivery, owner lock waits, all historical/duplicate-person/
expiry/dependency cases, large-fixture plans, normal UI/scanner acceptance or
reviewed employment/operational consumer cutoff. The full A01–A12 goal stays open.

## Manager preparation, evidence selection and receipt history — 2026-10-31

The source workspace now completes manager preparation without raw file IDs or
general document access: scoped Staff-ID/name lookup, a freshly verified owner
identity basis, source-bound released PNG/JPEG selection, and mandatory policy,
finding, observation, validity and identity/association attestations. Digital
account input remains an explicit unverified declaration, not provisioning or
linking. Source/access conflicts clear private context; uncertain submissions
retain the same review ID, command, workspace, source and payload. Completion
notices reset on access/scope changes. No policy or validity is fabricated.

Added canonical selection input/options and registered
`POST /v1/workforce/hospitals/:hospital/identity-staff/:id/person-review-evidence/search`.
Migration `0058_workforce_person_evidence_selection` admits only current-source
identity-image metadata through an explicit manager read context. Live identity,
held IAM/source/actor authority and per-row audits remain mandatory. The policy
preserves old WITH CHECK/delete restrictions and adds no upload, general document,
classification, contributor or binary authority. Listing scan release does not
prove identity or contributor history; submission still revalidates/finalizes it.

Protected history now shows actor/time/reason/receipt reference separately from
effectiveness, using the existing owner endpoint. SDK rows bind the original
review/Staff/hospital/workspace and reject duplicate revisions, out-of-order
versions, foreign rows and noncanonical private fields. A newer receipt blocks
new actions/downloads until reopening, even after older pages or history closure.
History never grants access or independently resolves an uncertain command.

Actual validation (preserve failed attempts as evidence):

- The selector first failed at the absent route (404), then its first integration
  attempt returned 500. Explicit bigint-to-text binding for the transaction-local
  source version was corrected and live identity refresh added; the selector
  passed in 12.419s. Logs: `/tmp/staff-person-evidence-selection-red.log`,
  `/tmp/staff-person-evidence-selection-green.log` and
  `/tmp/staff-person-evidence-selection-corrected.log`.
- Expanded signed Global API/real IAM/OpenFGA/Postgres and protocol-fixture storage/
  scanner journey passed in 17.613s: strict source inputs, current-source exclusion,
  no reviewer/general document access, unreleased/unrelated-category/other-Staff
  exclusion, direct RLS tenant/version filtering, read audits, SQL UPDATE denial,
  manager-link revocation and independently approved link recovery. Only
  `/tmp/staff-person-evidence-selection-expanded-corrected.log` contains these
  expanded assertions; the earlier similarly named run preceded the test edit.
- Full owner race run passed Workforce 484.340s, Global API 69.195s,
  architecture 54.397s and contractdocs 10.260s:
  `/tmp/staff-person-evidence-selection-owner-suites.jsonl`. After adding explicit
  destructive-operation denial (23514), the complete Global API race rerun passed
  in 57.191s: `/tmp/staff-person-evidence-selection-delete-globalapi.jsonl`.
- Shared framework race verification also passed all nine test-bearing packages,
  including IAM 356.440s, authz 10.832s, platform 18.436s and tenantdb 26.562s:
  `/tmp/staff-person-review-framework-suites.jsonl`. Contractdocs generation/check,
  vet/build and affected golangci-lint passed with zero issues.
- Manager UI first failed on absent preparation. Its fixture then used an exact
  filename label that omitted the accessible file metadata; querying the actual
  checkbox corrected that fixture. Twenty preparation/decision tests passed,
  including mandatory inputs, uncertain same-review-ID retries, local-time instant
  conversion, stale-basis privacy clearing and no manual-file fallback. Logs:
  `/tmp/staff-person-manager-preparation-red.log`,
  `/tmp/staff-person-manager-preparation-green.log`,
  `/tmp/staff-person-manager-preparation-corrected.log` and
  `/tmp/staff-person-manager-preparation-boundaries.log`.
- History client/UI first failed because missing. A newer-receipt test then exposed
  stale enabled actions; the largest observed version now survives pagination.
  History refusal clears private detail. All 23 page tests passed. Logs:
  `/tmp/staff-person-history-client-red.log`,
  `/tmp/staff-person-history-ui-red.log`,
  `/tmp/staff-person-history-newer-red.log` and
  `/tmp/staff-person-history-newer-green.log`.
- Broad checks caught a React compiler callback-placement issue, a parameterized
  history test passing individual rows instead of arrays, and a history surface
  bypassing the shared Panel primitive. All were corrected without disabling
  rules. The corrected history table now exercises actual foreign/duplicate/
  out-of-order arrays. Final full lint/typecheck/test/build passed all 22 tasks
  in 4m46.636s: 1,312 shell / 470 API / 268 UI tests.
  `/tmp/staff-person-manager-history-final-web.log`. Earlier failures remain at
  `/tmp/staff-person-manager-preparation-full-web.log`,
  `/tmp/staff-person-manager-history-full-web.log` and
  `/tmp/staff-person-manager-history-full-web-corrected.log`.

Read-only runtime discovery confirmed live API PID 5300 is
`/tmp/helivanta-staff-hr-intake-api` on 8399. Its database is `helivanta`, reached
through SSH to **local Colima `helivanta-tests`**, not established merely from a
localhost address. Authenticated app role `hms_app` could read the migration
ledger; only the named 0054–0058 registrations are pending. The isolated test
Docker profile remains separate. No Kubernetes/cloud mutation was performed.

A source-matched API binary (SHA-256 prefix `99de450d8eb1`) is built, and a protected
consistent database snapshot plus password-excluded role capture was restored in
a new network-isolated, ephemeral same-image container. All 321 table counts
matched the exported source snapshot. Artifacts are under private
`person-review-premigration-20261031T103756Z/` (directory 0700/files 0600).
This verifies database recreation only, not whole-system failover; no live restore
is authorized. The existing QA database/runtime remain unchanged.

A specific approval request is pending for applying **0054–0058** to this existing
local QA database and switching API 8399 to the tested binary. The supplied
AGENTS.md requires explicit named database-migration approval. Silence is not
approval; no account, membership, scoped grant, credential or production change
is included. Normal positive manager/checker/scanner browser acceptance follows
only after the fixture upgrade and current role discovery. Expired proposals are
not reused or extended.

Still open: staff-specific review collection/filtering as needed by normal UI,
large-fixture query plans, real storage/scanner/provider acceptance and remaining
provenance/expiry/dependency/audit/outbox/lock-wait gates. Exact approved finding
consumption into independently reviewed employment and all A01–A12 provisioning,
renewal, suspension/resumption, exits, expiry, rehire and consumer cutoffs remain
the full goal. No complete Access/RBAC, merge, release or production rollout claim.

## Person-review decision workspace candidate — 2026-10-31

Added the public navigation entry `/workforce/person-reviews`; actual discovery,
private reads and actions still require owner-held scoped authority. The new
workspace uses canonical hospital capabilities and summary inboxes, preserving
continuation through empty filtered pages, recorded-status selection and original
tenant/subject/hospital/Staff query keys. Private queries have no retained cache
lifetime or automatic retries. Detail shows the original policy/finding and separate
current effectiveness; approve/reject require the appropriate human attestations,
withdrawal is limited to the submitting manager, and approved findings can be
revoked after identity-source changes. Evidence download uses the protected
purpose-bound SDK, verifies bytes and aborts before publication after unmount/
workspace change. Blob URLs are released after download handoff or on unmount.

Submitted commands freeze UUID, payload, version and original workspace for
uncertain retries. Form/navigation guards preserve unsaved reasons and block
scope/filter/inbox changes while editing or resolving a command. A definitive
400 permits correction; 401/403/404/409 clears the private review context. The
shared Staff cutoff hook now accepts an owner-specific classifier while retaining
its original default for existing enrollment/evidence/classification consumers.
The UI confirms a recorded receipt separately from present effectiveness.

TDD and current evidence:

- The first page test failed because the route/component was absent. The initial
  green attempt then found a test fixture missing the real shell's navigation
  provider; adding that provider preserved the guard. Logs:
  `/tmp/staff-person-review-ui-red.log`,
  `/tmp/staff-person-review-ui-green.log` and
  `/tmp/staff-person-review-ui-green-provider.log`.
- Unsaved-edit tests reproduced an enabled inbox/access refresh that could discard
  the reason; dirty form state now holds parent controls and Close uses the
  existing confirmation. Red/green logs:
  `/tmp/staff-person-review-ui-dirty-red.log` and
  `/tmp/staff-person-review-ui-dirty-green.log`.
- Conflict/privacy and invalid-input tests found retained 409 private context and
  a 400 command locked as uncertain. All 32 focused tests across this page and
  the existing employment/evidence/classification consumers passed after the fix:
  `/tmp/staff-person-review-ui-refusal-red.log` and
  `/tmp/staff-person-review-ui-refusal-green.log`.
- All 16 page tests now pass: approve/reject, manager withdrawal, changed-source
  revocation, protected evidence download, bytes arriving after tenant transition,
  exact uncertain retry, self-review exclusion, empty-page continuation, unsaved
  input and definitive refusals. The revocation fixture initially attempted a
  native select operation on the real Radix control; the existing `chooseOption`
  helper corrected that fixture, with no production relaxation. Logs:
  `/tmp/staff-person-review-ui-actions.log` and
  `/tmp/staff-person-review-ui-actions-corrected.log`.
- Navigation first failed for the absent entry, then caught a duplicate page icon.
  A distinct document-review icon preserves the icon rule; all 51 navigation
  tests pass (`/tmp/staff-person-review-navigation-icons-green.log`). Shell
  typecheck passes. Full lint/typecheck/test/build passed all 22 tasks in
  4m54.313s, with 1,305 shell, 457 API and 268 UI tests. Command:
  `API_URL=http://127.0.0.1:8199 mise exec node@24 -- pnpm turbo run lint type-check test build`.
  Log: `/tmp/staff-person-review-ui-full-web.log`. No new Go source changed in
  this UI checkpoint; the preceding full backend results remain separately recorded.

The existing normal-login local Admin session opened the new navigation entry.
The actual page rendered the explicit person-review access-verification failure,
with no false empty queue or positive approval claim. Private full-page screenshot:
`staff-person-review-ui-unavailable-20261031.png`. This is route/error rendering
smoke evidence only; it does not establish the real scoped reviewer journey.
The older synthetic intake proposal is visibly expired; it was not approved or
extended. No new access grant, migration application or production action occurred.

Still open: manager preparation and a purpose-bound eligible-evidence selector
without general document-management access; immutable history/recovery UI; positive
normal Admin/independent-reviewer/scanner acceptance; large-fixture query plans and
remaining provenance/expiry/dependency/audit/outbox/lock-wait gates. Exact approved
finding-to-reviewed-employment consumption and every A01–A12 lifecycle/consumer
cutoff remain the full objective. No complete Access/RBAC, merge or release claim.

## Person-review inbox and Staff selection candidate — 2026-10-31

Added manager-only `POST /person-review-targets/search` and identity manager/
reviewer `GET /person-review-inbox`, under the verified hospital. The canonical
contract now generates search input, minimal Staff targets and review summaries.
An explicit empty Staff ID browses the bounded list; a nonempty value performs
case-insensitive exact lookup through the existing casefold index. Names accompany
Staff IDs for human selection. No technical UUID entry is required by these reads.
Inbox summaries expose recorded status/version/validity and current Staff name/
code, excluding private findings, policy, account associations and evidence.
Protected detail/current-state checks remain necessary before decisions.

Registered source migrations `0056_workforce_person_review_browse` and
`0057_workforce_person_review_inbox_index` preserve forced RLS, one permissive
policy, original WITH CHECK expressions and deletion guards. New purpose context
is set only after held IAM and supervision/active-actor authorization. It admits
only the owner summary reads; per-row audits commit with the read. No profile,
document, history or contributor permission, Staff edit or review transition is
granted. The migrations have run in disposable tests, not the UI fixture or
production. Their SQL/index metadata are persisted with the owner source.

TDD and verification:

- Both signed Global API tests first failed 404 rather than 200 at the absent
  routes (`/tmp/staff-person-browse-red.log`), then passed in 15.240s.
- The expanded full person journey passed in 17.808s after correcting a test
  assumption: Staff deletion is rejected by the existing statement-level guard
  with SQLSTATE 23514, rather than returning a successful zero-row delete.
  The failed fixture expectation remains in
  `/tmp/staff-person-browse-expanded.log`; passing evidence is
  `/tmp/staff-person-browse-expanded-corrected.log`.
- Direct forced-RLS checks prove Staff UPDATE and an otherwise valid review
  withdrawal fail with SQLSTATE 42501 under browse context, even with a manage
  mode flag. Inbox context sees no private documents/history/evidence/contributors;
  foreign-tenant reads see neither Staff nor review heads. Revoked reviewer links
  deny the inbox. Required Staff-ID fields reject missing/null/invalid/unknown
  fields. Additional three-page Staff browse, two-page inbox and approved-status
  transition assertions passed in the complete Global API race rerun (61.711s):
  `/tmp/staff-person-browse-pagination-globalapi.jsonl`.
- Client discovery/browse tests first failed on missing transport implementation;
  all 32 focused person-review tests pass. The clients pin workspace/hospital,
  keep Staff-ID search in JSON, validate canonical projections and requested
  code/status/cursor consistency, and check cancellation.
- The first full web run failed on a TypeScript tuple inference in the new test
  assertion and cancelled the unfinished build. Correcting that test type yielded
  all 22 successful lint/typecheck/test/build tasks in 26.632s, with 1,289 shell
  and 457 API tests. Retain `/tmp/staff-person-browse-full-web.log` and
  `/tmp/staff-person-browse-full-web-corrected.log`.
- Contractdocs generation/check, `go vet ./...`, `go build ./...` and affected
  golangci-lint pass (zero issues). The full owner race run has passed Global API
  in 79.771s, architecture in 66.445s and contractdocs in 16.314s; Workforce is
  subsequently passed in 540.466s at
  `/tmp/staff-person-browse-owner-suites.jsonl`. All four test-bearing packages
  passed for this backend checkpoint.

Still required: professional manager/reviewer UI and normal local browser QA,
actual scanner/storage acceptance, staff-specific review history/discovery as
needed by that UI, large-fixture query plans, remaining provenance/expiry/
dependency/audit/outbox/lock-wait proofs, exact finding-to-reviewed-employment
consumption and every A01–A12 lifecycle/consumer cutoff. No full Access/RBAC,
commit/merge/release, live migration or production rollout is claimed here.

## Person-review hospital discovery candidate — 2026-10-31

Added `GET /v1/workforce/person-review-scopes` and canonical generated
`PersonReviewHospitalScope`, plus `listPersonReviewScopes` through Global API.
The bounded filtered scan uses only the caller's existing reviewed active links;
held identity assignments determine independent submit/review capabilities.
It grants no HR/document authority and adds no migration or permissive SQL policy.
Both original workspace assertions are mandatory. Limits default/cap at 20;
continuation remains valid through empty filtered pages, with caller/tenant/route
binding. Authorization is limited to four concurrent checks and five seconds
capped by session idleness. Visible rows require locked unchanged actor links and
a successful audit; authorization failures do not become empty success.

TDD: the signed real IAM/Postgres/OpenFGA/Global API discovery test first failed
404 rather than 200 (`/tmp/staff-person-scope-discovery-red.log`), then passed in
13.456s. Expanded full person-review journey passed in 19.373s, proving distinct
manager/reviewer capabilities, no legacy-manager discovery, required workspace
headers, an empty first page for a linked-but-unassigned hospital followed by the
assigned hospital, cross-actor/route cursor refusal and discovery cutoff during
actor-link revocation. Log: `/tmp/staff-person-scope-discovery-expanded.log`.

The client first failed six tests because discovery was absent; all 26 focused
person-review/client tests now pass. It verifies canonical capabilities/cursor
consistency, rejects unrequested response fields without echoing them, preserves
empty-page continuation and refuses results after cancellation. Logs:
`/tmp/staff-person-scope-client-red.log` and
`/tmp/staff-person-scope-client-green.log`.

Full web lint/typecheck/test/build passed all 22 tasks in 49.342s, including
1,289 shell and 451 API tests (`/tmp/staff-person-scope-full-web.log`).
Contractdocs generation/check, `go vet ./...`, `go build ./...` and affected
Workforce/Global API/architecture golangci-lint passed (zero lint issues).
The full Global API race suite passed in 96.270s, architecture in 88.908s
and contractdocs in 20.932s. After adding valid second-tenant/same-reviewer
isolation and attributable-discovery-audit assertions, the complete Global API
race suite passed again in 59.837s
(`/tmp/staff-person-scope-tenant-audit-globalapi.jsonl`). The broad Workforce race
suite subsequently passed in 567.371s; all four test-bearing packages passed
(`/tmp/staff-person-scope-owner-suites.jsonl`). This validates the discovery
checkpoint before the new browse migrations below.

Still open: hospital review inbox/target search and manager/reviewer UI, actual
scanner/browser acceptance, remaining failure/race/provenance/query-plan gates,
reviewed employment consumption and every A01–A12 lifecycle/consumer cutoff.
No deployment, migration application, merge, release or complete Access/RBAC
acceptance is claimed by this source checkpoint.

## Person-review evidence release: recovered validation — 2026-10-31

The owner now has purpose-bound private evidence bytes at
`GET /v1/workforce/hospitals/:hospital/staff/:id/person-reviews/:review/evidence/:file/content`.
Identity manager/reviewer authority does not confer generic document access.
Authorization pins the original assignment, actor link/employment epoch, review
revision, identity source and immutable evidence metadata across storage I/O;
replacement authority cannot rescue an old request. Returned generation, size and
SHA-256 are verified before release. Denials and corrupt/unavailable storage do
not record a successful-download audit. The API client additionally verifies
workspace, review/file membership, content type, length, digest and cancellation.

Real signed Global API/IAM/OpenFGA/Postgres tests with storage/scanner protocol
fixtures cover assignment/link replacement, source changes, review approval and
revocation during reads, evidence revocation, unavailable/corrupt bytes and fresh
request recovery. A missing current actor link now returns 403; an unknown target
remains 404. Controlled mutation runs fail when original-authority comparisons
are removed (unexpected 200 instead of 403) or review-revision comparison is
removed (unexpected 200 instead of 409). Source was restored after each run.
Logs: `/tmp/staff-person-evidence-pin-checks-red.log` and
`/tmp/staff-person-evidence-review-revision-red.log`.

Recovered terminal results from the race/atomic-coverage run:

- Workforce passed in 554.318s, architecture in 61.601s and contractdocs in
  6.712s. The aggregate command did **not** pass: Global API's evidence-revocation
  test used the wrong POST URL. Source declares PUT document revocation; correcting
  the fixture preserved the existing production contract.
  Log: `/tmp/staff-person-evidence-owner-suites.jsonl`.
- The corrected **complete Global API package** passed in 61.761s with race and
  atomic coverage: `/tmp/staff-person-evidence-corrected-globalapi.jsonl`.
  `go tool cover -func=/tmp/staff-person-evidence-corrected-globalapi-coverage.out`
  measures evidence authorization at 85.4%, download at 86.5% and its audit helper
  at 100%. These are function statement metrics, not workflow completion.
- Full web lint/typecheck/test/build passed all 22 tasks: 1,289 shell and 445 API
  tests, plus the other package suites. Log:
  `/tmp/staff-person-evidence-full-web.log`. Focused client tests passed 20 cases;
  the test Blob reader was corrected for jsdom's missing `arrayBuffer` method.
- Contractdocs generation/check, `go vet ./...`, `go build ./...` and affected
  golangci-lint passed: `/tmp/staff-person-evidence-final-static.log`.

The normal API `type-check` script now also checks the directory and person-review
canonical generators, closing an omitted standard validation gate. This is a
single package-script line; no application behavior or migration changes in that
follow-up.

Still open: bounded discovery/inbox and professional manager/reviewer UI, actual
scanner/storage/browser acceptance, remaining historical-person/contributor,
expiry/dependency/outbox/lock-wait proofs and query plans. Independently reviewed
employment must then consume the exact finding/source generation, followed by
all A01–A12 lifecycle and consumer cutoffs. Migrations 0054/0055 remain persisted
source tested only in disposable databases. No UI-fixture application, production
rollout, commit, merge, release or full Access/RBAC acceptance is claimed here.

## Pre-employment person-review backend/client candidate — 2026-10-31

The inactive-joiner prerequisite now has source implementation, rather than design
alone: canonical Go/Zod models, immutable submission, private detail and cursor
history, independent approve/reject, reasoned withdrawal/revocation, protected
identity-document contributor/evidence snapshots and minimized atomic audit/outbox.
Migration `0054_workforce_person_reviews` and concurrent-index migration
`0055_workforce_person_provenance_index` are registered source artifacts for future
application. They have run only in disposable tests, not the existing UI fixture
or production. Existing active-employment identity-case/login-link guards remain.

The signed Global API/IAM/OpenFGA/Postgres journey proves that an inactive worker
requesting no login can receive an independent human person finding while staying
inactive and receiving no fabricated login link or tenant-wide business permission.
It also verifies receipt/decision replay and conflicting intent, cursor history,
required account-field presence, missing workspace/action and unknown-object denial,
bound identity-only file RLS, foreign-tenant denial, forbidden document editing,
immutable history, provenance frozen across transactions, withdrawal, changed-source
effectiveness and revocation after correction. This is owner/protocol evidence;
the scanner/storage harness is protocol-based, not real-scanner or UI acceptance.

The prospective provenance hook initially affected benefits/tax evidence. Full
owner tests caught that regression; restricting it to enrollment `identity_evidence`
restored the existing owner paths. Architecture checks separately required the
single bounded detail-route declaration and per-field identifier-only event
classification; their checks remain enabled. The original failed-run evidence is
`/tmp/staff-person-review-owner-suites.jsonl`.

Current-source verification:

- `go test -race -json ./internal/modules/workforce/... ./internal/globalapi/... ./internal/archtest/... ./cmd/contractdocs/...`
  passed all four test-bearing packages: 699 test/subtest pass events, no skipped
  test cases; the contract data package has no tests. Workforce completed in
  575.308s. Log: `/tmp/staff-person-review-final-owner-suites.jsonl`.
- `go run ./cmd/contractdocs`, `go run ./cmd/contractdocs -check`, `go vet ./...`,
  `go build ./...` and affected Workforce/Global API/architecture golangci-lint
  passed. Log: `/tmp/staff-person-review-final-static.log`.
- All 17 shared-emitter dependent generators passed `--check`; canonical array
  minima now enforce required evidence and the pre-existing nonempty Staff
  template permission list. All 38 template-history tests passed without skips.
- Full web `API_URL=http://127.0.0.1:8199 mise exec node@24 -- pnpm turbo run lint type-check test build`
  passed all 22 tasks in 34.156s (17 cached), including 1,289 shell and 439 API
  tests. Log: `/tmp/staff-person-review-final-web.log`.

The exported API client pins tenant/subject/hospital/Staff/command/version/source/
evidence, retains the original request identity for uncertain retries, distinguishes
an original approved receipt from present effectiveness and rejects an approved
label missing independent findings. Client checks supplement owner authorization;
they do not replace it or establish an operational employment grant.

Still required before closing this prerequisite: hospital/target discovery and
manager/reviewer UI, original-authority-pinned evidence byte release, actual scanner
acceptance, the remaining historical/duplicate-person/contributor and dependency/
outbox/lock-wait races, measured source-query plans, and normal browser acceptance.
Then independently reviewed employment must consume the exact source-to-operational
generation, followed by every A01–A12 lifecycle/consumer gate. The local intake
assignment confirmation is still pending, with no activation or recipient acceptance
claimed. Full Access/RBAC remains open; no rollout, commit, merge or release is
claimed by this candidate checkpoint.

## Local onboarding proposal and readiness regression — 2026-10-31

Normal password login succeeded for the existing synthetic Admin, followed by
the explicit server-confirmed Staff onboarding catalog and proposal UI. A local
`workforce.hr_intake.v1` proposal for the existing synthetic pharmacist/reviewer
at the designated QA hospital was recorded with validity 17:00–20:00 Melbourne
on 31 October. Its asynchronous hospital verification completed and the proposal
advanced from version 1 to version 2, awaiting independent review. A fresh normal
login as the separate synthetic approver exposed the decision form. No approval
was submitted: the specific new confidential-data access is awaiting user
confirmation required by the browser safety rules. Private screenshots and
`staff-hr-intake-ui-evidence-20261031.json` retain the precise resource and actors.
This is proposal/verification/independent-review preparation evidence, not active
intake authority, recipient/scanner acceptance or production evidence.

Two current-source component regressions then reproduced a retained onboarding
confirmation remaining enabled after the supported-template catalog changed or
became unavailable. The UI now requires a successful settled catalog before
onboarding commands are enabled. It retains the selected family, entered decision
and request state while blocked, with Refresh outside the disabled controls.
Legacy assignment families retain their existing behavior; owner authorization
continues to decide every actual command. Red: 2 failures/27 passes at
`/tmp/staff-intake-catalog-refresh-red.log`. Green: all 29 Staff access tests at
`/tmp/staff-intake-catalog-refresh-green.log`.

Full verification after the fix passed all 22 lint/typecheck/test/build tasks
in 4m26.544s (18 cached), including 1,289 shell and 425 API tests. Command:
`API_URL=http://127.0.0.1:8199 mise exec node@24 -- pnpm turbo run lint type-check test build`.
Log: `/tmp/staff-intake-catalog-refresh-full-web.log`. No backend route, schema or
permission changed in this regression fix.

The next current-person prerequisite is documented in the
[source-based design](../superpowers/specs/2026-10-31-pre-employment-person-review.md)
and [implementation sequence](../superpowers/plans/2026-10-31-pre-employment-person-review.md).
Existing identity-case draft and login-link guards require active employment;
neither is weakened to bypass inactive onboarding. The new review remains design,
with historical independence, human findings, evidence, no-login workers and
source-to-operational epoch consumption explicitly required. Full A01–A12 remains
open; no Staff area is marked complete and no rollout/merge is claimed here.

## Local candidate browser continuation — 2026-10-31

The existing candidate API and gateway are listening on 8399 and 8199, and the
4399 UI serves the integration worktree. A fresh browser reload reproduced a
startup failure in middleware: the new local UI launcher omitted
`NEXT_PUBLIC_ZITADEL_ISSUER_URL`. Relaunching with the existing fixture issuer,
client and Global API configuration restored the workspace without changing
application code, identity-provider settings, memberships or grants. The private
runtime receipt records the corrected launcher PID and configuration repair.

The existing synthetic pharmacist/reviewer session then loaded six Staff records
with read-only directory access and no enrollment/edit controls. Direct normal
browser navigation to Staff access showed assignment administration unavailable;
payroll setup showed an explicit payroll-access denial. These are current local
restricted-account checks, not maker/checker/intake-user acceptance or production
evidence.

Direct read-only PostgreSQL inspection confirms application-fixture migrations
`0009_billing`, `0009_medicore`, `0020_iam_hr_intake_templates` and
`0053_workforce_hr_intake`, applied at 05:37:32–33 UTC. The private evidence bundle
contains `staff-hr-intake-migration-receipts-20261031.json`; the inspected Staff,
login-link, document and IAM assignment tables retain enabled and forced RLS.
Earlier statements below that the
migrations had run only in disposable tests describe the preceding checkpoints.
No production rollout or migration is established by this local receipt.

All 38 canonical template-history tests passed without skips. Both changed
generators passed `--check`: `scripts/generate-staff-access.mjs` and
`scripts/generate-workforce-directory.mjs`. Commands used `mise exec node@24`;
the script-test log is `/tmp/staff-hr-intake-current-template-tests.log`.

Current-source web verification passed all 22 tasks in 4m0.439s (18 cached).
Command: `API_URL=http://127.0.0.1:8199 mise exec node@24 -- pnpm turbo run lint type-check test build`.
The run reports 1,287 shell, 425 API, 267 UI, 72 configuration and nine emergency
access tests passing. Log: `/tmp/staff-hr-intake-current-web-verification.log`.
Existing jsdom navigation and unrelated list-key warnings are present; no task
failed. Tracker formatting initially detected a blank-line issue in the acceptance
ledger; Prettier corrected it. Full Access/RBAC A01–A12, including reviewed
employment and every effective consumer cutoff, remains the completion objective.

## Scoped HR workspace integration — 2026-10-31

The combined owner run is terminal: Workforce 445.865 seconds (521 passing
case/subcase events), Global API 40.451 seconds (38), journey 48.784 seconds (26)
and architecture 41.570 seconds (124). IAM's only failure was its old hard-coded
19-migration assertion. The canonical append is 0020; the test now retains every
historical ID assertion and additionally requires the twentieth ID and SQL. Its
focused rerun passes, followed by the complete IAM race suite in 316.926 seconds
(300 passes; eight conditional provider skips). The active Staff fixture separately
passes all eight real-provider tests in 27.573 seconds, no failures/skips. Earlier
provider attempts used obsolete worktree credentials/default callback configuration
and failed; the private fixture controller identified the matching seed path and
4399 callback. No credential was printed or rotated to obtain this result.

Logs: `/tmp/staff-hr-intake-combined-owner-suites.jsonl`,
`/tmp/staff-hr-intake-iam-corrected-suite.jsonl` and
`/tmp/staff-hr-intake-real-provider-staff-fixture.jsonl`. Preserve the original
migration assertion failure; the passing IAM rerun supersedes it for current
source acceptance without removing the test or any historical migration checks.
Full static checks and affected lint also passed (zero issues). The first web
run omitted required `API_URL` and failed build, cancelling unfinished tests;
with `API_URL=http://127.0.0.1:8199`, all 30 web gates passed in 3m59.516s. These
results precede the client/workspace additions below.

The canonical projection client now composes `hr_intake=v1` at final request
boundaries, preserving cursors and other projections; conflicting selectors are
rejected. Seven client tests pass. Bounded scope discovery validates canonical
capabilities and pagination and retains empty-page continuation. The Staff page
keeps a discriminated legacy/intake hospital selection rather than manufacturing
manager/reader grants. The new regression first showed no Staff directory; all
five directory-page tests now pass. Scoped HR opens profile/terms/file actions
without global permissions; identity/credential/account-link actions stay on
their original authority. A new hire starts inactive with status editing disabled.
Account-bound setup/supervisor reads preserve the intake selector.

Terms command and read regressions reproduced omitted selectors; draft/withdraw
commands and source/list/history/sponsor reads now compose the generated selector,
retain lifecycle projection and separate legacy/intake cache keys. All three
terms-client and four terms-panel tests pass. Staff-file command/byte requests
pass nine client tests; metadata/record choices pass 14 UI tests. Evidence and
classification child requests and cutoff/privacy negatives are being completed.
A classification fixture initially reused a consumed response; fresh responses
then reproduced the actual missing-selector failure, and the restored client
passes the classification request tests. No normal browser/real scanner acceptance
for this candidate, application fixture migration, merge or rollout is claimed.

Full Access/RBAC A01–A12 remains the objective. Current-person/employment review,
legacy/governance reconciliation and suspension/resumption, accepted exits,
contract expiry/rehire and every operational consumer cutoff remain required.
Counts remain 10 partial, 10 audit pending, 1 research and 0 fully accepted.

## Scoped HR intake continuation — 2026-10-31

Integration `4d0d5f7c` has green exact-head Go/web/E2E, scripts, secret and
image/security CI. All 30 combined web gates passed. The unchanged Global API
suite's earlier ledger-blocker observation failure passed a bounded rerun
(32.249 seconds); retain the first failure as timing evidence.

The current uncommitted candidate adds the explicit `workforce.hr_intake.v1`
template with only enrollment/document permissions, preserving the frozen
legacy discovery catalog and historical migration hashes. Migration SQL is
persisted in source and registered as `0020_iam_hr_intake_templates` and
`0053_workforce_hr_intake`; the canonical registry has 20 IAM / 173 module
migrations. They have run in disposable tests only, not the existing application
fixture or production.

Twenty-four enrollment, employment preparation/evidence and Staff-file routes
use explicit `hr_intake=v1` admission. Real IAM/Postgres/OpenFGA/Global API tests
prove independently approved scoped intake without a hospital-manager grant,
inactive new-hire preparation and rejection of employment activation. Protocol
storage tests reproduced private-byte release after in-flight assignment or
actor-link replacement; request-bound source checks now deny both old requests
while allowing independently authorized fresh requests. Storage/scanner protocol
fixtures are not real-scanner or normal-browser acceptance.

Recovered terminal validation of this candidate: complete Global API race suite
49.830 seconds; journey 52.745 seconds; authz 7.006 seconds; platform 12.360
seconds, all passing with no skips/failures. `go vet ./...`, `go build ./...`,
affected `golangci-lint` (zero issues), and contract generation/check completed
successfully. Logs: `/tmp/staff-hr-intake-bounded-integration-suites.jsonl` and
`/tmp/staff-hr-intake-final-static.log`. Canonical script tests previously passed
38 cases. These results precede the next directory/discovery changes.

Directory list/detail now use opt-in held intake authority. A real regression
first returned zero rows instead of two; the fix passed the complete synthetic
intake journey in 13.831 seconds. Hospital discovery first returned 404; the
bounded filtered scope route and generated capability response passed in 13.748
seconds. These checks cover no legacy-manager grant, unsupported selectors,
unknown Staff, unchanged legacy discovery and actor-link cutoff. Additional
workspace-negative and broader owner regressions are the next validation gates.

The assignment SDK now derives its family type from the canonical template
catalog and selects a separate intake list. Its regression first selected the
legacy leave endpoint; all six assignment-client tests now pass. Full API package
validation under Node 24.11.1 passes 414 tests, typecheck and lint; shell typecheck
also passes. The matching IAM family route now passes a real signed-session journey in
13.437 seconds: maker/checker read only the intake family, legacy leave/identity
lists remain closed and ordinary intake users cannot administer assignments.
Assignment UI integration is still pending, so this is not an enabled
end-to-end workflow.

The complete directory/discovery backend checkpoint is now terminal green:
Workforce 463.939 seconds (521 passing test/subtest events), Global API 40.525
seconds, journey 50.324 seconds and architecture 41.018 seconds. No suite reported
a failure or skip. Log: `/tmp/staff-hr-intake-directory-discovery-suites.jsonl`.
This source includes original-workspace negatives and precedes the next IAM
family-list/read-only-master changes.

An additional A04 source defect was reproduced: supplying another expected
account to assignment reads returned 200 with private proposals. The owner now
rejects mismatched, empty and repeated supplied account assertions with 409
before proposal access; the SDK supplies both original tenant and subject. The
real owner and six client tests pass. Command/no-write mutation proof and broader
regressions are still being completed; this does not close old-client or full
lifecycle acceptance.

The retained-form command regression also reproduced unauthorized actor adoption:
removing the account guard created a proposal under the replacement checker
(200). The guard was restored; the command/no-write journey passes in 13.679
seconds, and mismatch/empty/repeated read assertions remain covered. Raw red and
green logs are `/tmp/staff-assignment-account-write-mutation-red.log` and
`/tmp/staff-assignment-account-write-green.log`.

Read-only HR setup choices/history now use the explicit intake projection and
held enrollment authority. The initial GET returned 404. A first SQL approach
was rejected by the harness's exactly-one-permissive-policy gate; the corrected
migration preserves original write checks and adds restrictive deletion guards.
The real owner journey now persists/reloads the selected department revision,
denies master maintenance at the API, and directly proves SQLSTATE 42501 for a
master update under intake context even with a master-context flag. The complete
focused journey passes in 14.557 seconds; log:
`/tmp/staff-hr-intake-master-write-policy-green.log`.

Staff Access now offers the intake family only after the explicit server catalog
matches canonical keys, definitions and permission bundles. The initial UI test
could not find the onboarding option; catalog parsing caught a missing public
contract export, which was corrected. All 27 assignment-page tests pass,
including unsupported legacy/changed-permission catalogs and the existing
independent-review/renewal/uncertain-command flows. This is assignment UI evidence,
not the ordinary intake user's directory/forms/files browser acceptance.

Complete current-source IAM/Workforce/Global API/journey/architecture race suites
are running sequentially in `/tmp/staff-hr-intake-combined-owner-suites.jsonl`.
Final static and full web gates follow separately. No merge, rollout, application
fixture migration or production change occurred.

Next: finish the intake user workspace and broader scope discovery negatives; finish canonical client/UI integration and normal independent
Admin/approver/HR browser acceptance. Current-person and reviewed employment
activation, legacy/governance reconciliation and all A01–A12 lifecycle/cross-owner
cutoff requirements remain open. No feature merge, production migration,
rollout, release or completion claim follows from these local checks. Counts
remain 10 partial, 10 audit pending, 1 research and 0 fully accepted.

## Integration and complete owner-suite checkpoint — 2026-10-31

Credential-refresh source `a8e149eb` is pushed to draft PR #1489. Complete
Workforce race/coverage passes: 521 passing test/subtest events, zero skips or
failures, 83.7% statement coverage, 467.639 seconds. Complete IAM race/coverage
passes: 300 passing events, 82.4%, 339.735 seconds; eight conditional provider
skips are separately resolved by eight real-provider passes in 29.983 seconds.
The complete final journey race suite passes in 57.342 seconds, including all
19 file-boundary scenarios. Authn/harness races, full vet/build, affected lint,
contract freshness, formatting/diff and staged redacted secret scanning pass.
Normal local Admin download and fresh restricted-reviewer UI evidence are
recorded below; the local API remains on `a8e149eb`.

GitHub reports the PR conflicting with current main, so quality CI did not start
for that source; its separate image workflow is live. Integration with main
`4587b55b` is prepared in an isolated worktree, retaining the original live test
checkout. The sole conflict was this tracker; both complete histories are
preserved. Main's leave privacy fix and unrelated billing/MediCore changes are
retained. Canonical contract generation/check, full vet/build and clean conflict/
diff checks pass on the combined source. Its bootstrap/Global API/architecture/
journey races and web validation remain separate pending gates.

Integration `4d0d5f7c` is now pushed to the draft. GitHub reports MERGEABLE and
Go/web/E2E quality CI is running; scripts and secret scanning pass. Local combined
Global API validation failed `TestScopedLeaveThroughGlobalAPI` at line 336: the
one-second expected ledger-blocker observation was not seen; cleanup then reported
context cancellation. The same complete package passed on `a8e149eb`. The test
source is unchanged by integration. Preserve both logs and diagnose under bounded
load after the currently live backend/web runs finish; do not weaken the blocker
or expiry assertions or treat the prior pass as combined-source acceptance.
The other combined suites remain running, not terminal on observation timeout.

The combined canonical registry has **19 IAM / 171 module migrations**. Main
contributes `0009_billing` and `0009_medicore`; their source SQL/registrations are
retained. This integration adds no new Staff migration, and neither inherited
migration has been run against the existing application fixture or production.
Historical 169-migration checkpoints remain accurate for their earlier source.
No merge to main, production rollout, release, issue closure or A01–A12 completion
is claimed. Full scoped HR/person/employment and every lifecycle cutoff remain
required.

## Original credential and identity across private file I/O — 2026-10-31

Real signed-session IAM/PostgreSQL/OpenFGA/Global API regressions reproduced
private bytes returning 200 when the credential was revoked, the account became
inactive, or the identity provider became unavailable during storage reads.
Permission refresh alone did not prevent these disclosures. The candidate now
revalidates the original credential and the IAM identity check installed at entry
before each shared file-owner phase. It pins the original subject, tenant,
authentication time and idle deadline, retains IAM's uncached source watermark
check, and keeps all provider/storage calls outside owner transactions.

The full journey race suite passes in 57.342 seconds, including 19 file-boundary
scenarios: the earlier Staff/employment/benefit/tax permission cases and revoked,
inactive and unavailable identity during both read and quarantine upload. Denial
releases no bytes/filename, records no false successful read, and binds no upload
generation or second scan command. Reactivation still rejects the old credential.
Complete authn and harness race suites pass (3.006 / 8.631 seconds). The harness
regression first returned 401 because its default fake token changed claims on
every verification; immutable claims fix that fixture without weakening principal
checks. A subsequent test-only correction explicitly serializes timestamps that
Principal intentionally omits from JSON. The public StaticVerifier and explicit
custom verifiers remain unchanged.

Normal local Admin UI reopened the six-row synthetic directory, opened contractor
evidence and completed one download through the original native Save dialog. The
179-byte PNG matches the canonical SHA-256. Fresh normal restricted-reviewer
sign-in retains six-row read-only directory access with no document/edit controls.
Private screenshots and `staff-file-identity-refresh-browser-evidence-20261031.json`
retain the evidence. API and Global API readiness return 200. This local runtime
uses the candidate source, not a production rollout or independent employment
approval. Larger owner/architecture suites and final validation are still running
at this checkpoint; their terminal evidence must be recorded before release.

The complete IAM race/coverage suite passes in 339.735 seconds. Its eight
conditional provider skips are covered by a separate real localhost Zitadel run:
all eight integration tests pass, zero skips/failures, in 29.983 seconds. The
first provider invocation omitted the configured Docker socket and used an
outdated seed PAT; the corrected run uses the existing local fixture credential
and test VM without modifying tests, credentials or permissions. Private red and
green logs are retained. Workforce and remaining package checks continue below
as separate gates.

Full vet/build, affected lint and contract freshness have passed; a context-key
name that triggered a false-positive hardcoded-secret lint finding was renamed
without suppression. The previous PR head `5e1aede0` now has passing Go/web/E2E,
scripts, secret and image checks, but those results do not validate this new head.
The identity-owner dependency remains draft with green checks at `f75bb0e`; the
console dependency remains draft at `209c2aa`. Current GitHub annotations confirm
its build jobs did not start because account payments/spending limits need owner
attention. No billing, runner, visibility or production changes were made.

See the [design](../superpowers/specs/2026-10-31-private-file-identity-refresh.md)
and [implementation plan](../superpowers/plans/2026-10-31-private-file-identity-refresh.md).
All A01–A12 requirements remain active: scoped HR intake/person/employment review,
legacy cohort reconciliation, renewal, suspension/resumption, resignation,
termination, contract expiry, rehire and every consumer cutoff still need full
acceptance. Counts remain 10 partial, 10 audit pending, 1 research, 0 accepted.
No new route, permission, SQL migration or FGA model was introduced.

## File cutoff fix pushed and local browser checked — 2026-10-31

`5e1aede0` is pushed to draft PR #1489 with the shared file permission refresh,
real source/gateway regressions, and the retained SQL lock-observer correction.
Exact-head CI is running. The API is built from that source and healthy on the
existing localhost fixture; no production rollout or new migration occurred.

Normal restricted-reviewer directory access remains read-only. A normal Admin
sign-in opened existing contractor evidence and completed one UI download: the
native Save dialog was completed for the original attempt, and the 179-byte PNG
matched its canonical SHA-256. The updated
[local UI record](2026-10-31-staff-membership-recovery-local-ui.md) distinguishes
startup/event observation timeouts from terminal failures and records private
runtime/artifact evidence. No bypass, extra grant or repeat download was used.
Full A01–A12, scoped HR/person/employment integration, remaining family/race and
all lifecycle/cross-module/deployed gates remain open.

## Current file permissions after storage — 2026-10-31

A real IAM/PostgreSQL/OpenFGA/signed-session journey reproduced a private Staff
download returning 200 after IAM administrator-role removal during the storage
read. Ordinary membership, the hospital-manager row and the stale graph grant
remained, isolating the permission-source gap. The shared file pipeline now
refreshes current permissions before owner phases and after storage I/O. Staff,
employment, benefit and tax gates declare their existing read/write requirements;
both employment permissions must remain current from one resolver result.

Five real-service cases pass through the Global API: role loss during Staff
read/upload; loss of either employment permission independently; and an actual
HTTP-level graph outage during storage. They verify denial/no private bytes or
filename, no-store responses, no false successful-read audit, and upload remaining
unbound to a source generation with no second scan command. Focused gateway race
run: 18.967 s. Full cross-module journey race suite passes (40.741 s); full Global
API and architecture races pass (75.720 s / 34.974 s). Full vet/build, affected
lint and contract freshness pass. The full Workforce race suite passes: 521
passing test/subtest events, zero failures/skips, 83.7% statement coverage
(461.510 s); cross-module journeys have 12 passes and no failures/skips. Benefit/tax mid-I/O IAM cases remain distinct gates;
existing positive/owner-boundary tests must also pass.

The earlier recovery PR Go failure was a lock observer matching obsolete joined
SQL. It was reproduced locally and corrected without weakening blocker-PID or
403/no-write assertions; the focused Global API journey passes (26.437 s).
The observer-only fix is `e65f7810`. Test fixture corrections (file RLS context
and deterministic graph unavailability instead of soft store deletion) retain
all original denial assertions.

See the [design](../superpowers/specs/2026-10-31-staff-file-current-permissions.md)
and [plan](../superpowers/plans/2026-10-31-staff-file-current-permissions.md).
This is current permission refresh across file I/O, not a transaction-held IAM
or employment cutoff. Full scoped HR intake/owner/storage/UI integration,
current-person basis, independent employment review, legacy cohort classification
and every A01–A12 lifecycle/cross-module journey remain open. No new role, route,
SQL migration, production rollout or complete Staff area is claimed.

## Membership recovery UI and compound expiry — 2026-10-31

The candidate source `e848beaf` is now running behind the existing local Global
API and registered UI origin. Older local APIs/UI were captured before switching;
a private database backup preceded canonical local migration startup. Migration
receipts and exact registered SQL/index definitions remain saved for replay.

Normal UI QA passed Admin first enrollment and retained receipt/reload;
recipient login with administration/payroll denial; Admin removal; removed
recipient fresh-login denial; explicit ordinary recovery with retained removal
and recovery history; recovered recipient login with continued administration/
payroll denial; and existing restricted reviewer sign-in with unchanged six-row
read-only directory. Read-only source evidence confirms a new ordinary membership
ID, generation 2 and no hospital grant, Staff link or scoped proposal. The IdP
verified-email state was setup, not contact-verification acceptance. No employment
or privileged access was approved.

Two-grant earliest-expiry rollback now has direct real-service race evidence.
The restored test passes (25.230 s); selecting the later expiry makes it fail
with deadline rather than authority denial (27.510 s). Vet/lint pass. The
[local UI record](2026-10-31-staff-membership-recovery-local-ui.md) separates exact
source, private evidence, observation limits and all remaining full A01–A12 gates.
No whole Staff area, production release or lifecycle cutoff is marked complete.

## Compound command authority — local implementation, 2026-10-31

Full Access/RBAC A01–A12 remains open. IAM now has an additive bounded compound
command guard; the existing single-grant method delegates to it. One or two
server-required permission/evidence pairs must share the current account,
tenant, hospital, membership generation and acknowledged scope epoch. Duplicate
requirements, conflicting receipts and mixed scopes fail before owner work.
Active proposal rows lock in stable ID order before membership and scope; the
credential watermark lock and all source locks stay held through owner commit.
Every exact permission/snapshot is rechecked after waits and before commit, with
the earliest grant expiry bounding the command. Caller input is cloned.

The real-service capability assertion failed before implementation
(`/tmp/staff-compound-red.log`). Two independently reviewed assignments pass,
including missing permission, malformed/mixed evidence, revoked credential,
unavailable dependency and shared owner rollback checks. PostgreSQL observed an
actual withdrawal lock wait until the owner commit; retained evidence then denied.
Removing the source-lock call made that assertion fail
(`/tmp/staff-compound-missing-locks-red.log`); the implementation was restored.
Existing leave/identity projection race tests pass (88.966 s); the extended
compound lock-wait test passes (10.457 s). Architecture race tests pass
(57.327 s), as do full Go vet/build, affected lint and contract/schema freshness.
The full IAM race/coverage suite passes: 300 passing test/subtest events, no
failures, 82.3% statement coverage (346.121 s). Its eight conditional provider
skips were resolved by the separate real-provider run below. The final expanded
compound test, including reversed input and the observed withdrawal wait, passes
again after restoring locks (9.855 s). Authz and tenantdb race packages pass;
architecture passes in its separate correct-package invocation. The first combined
command named nonexistent `internal/arch` and therefore exited nonzero despite
those package passes; `internal/archtest` is the correct architecture package.
A later support-only invocation omitted the Docker environment and failed fixture
startup; the configured rerun passed authz (75.5%, 6.887 s), tenantdb (81.8%,
18.548 s) and architecture (44.803 s). No failing test was skipped
or changed to hide either invocation error.

The Forge upstream fix from PR #1494 is also included on this draft branch.
Frozen pnpm installation, the production high-severity audit and all 54 script
tests pass. The audit retains lower-severity findings and the existing decoder
exception; no additional ignore was introduced. The internal guard is committed
at `cb9392a8`; the dependency replacement is `f1c47701`. Exact-head CI, browser
and deployed acceptance remain separate pending gates.

All eight real localhost Zitadel integration tests pass with race detection,
zero failures/skips (30.383 s). Credentials were loaded only into the child
process. Mode-0600 evidence remains in the private QA bundle as
`compound-guard-zitadel-provider-20261031.jsonl`.

This is an internal prerequisite, not activated HR intake, employment review or
cutoff. No new route, permission, template, SQL migration, FGA model or production
role/rollout is introduced. Compound expiry/lock-wait/activation combinations,
query-plan/latency acceptance and actual Workforce storage/owner/UI integration
remain gates before consumer activation. Continue the
[implementation plan](../superpowers/plans/2026-10-31-compound-staff-command-authority.md),
then person identity, independent employment decisions and every effective
suspension/resumption/exit/contract-expiry/rehire and cross-module cutoff. No
A-item, Staff area, browser or deployed acceptance is newly marked complete.

## Access/RBAC checkpoint — 2026-10-31: frozen template migration history

The actual generator regression proved that editing an existing template permission
could successfully rewrite historical migration 0014. An independent regression
also proved that pinning SQL alone missed family changes. The canonical Staff
contract now stores an ordered template-migration catalog with normalized public-
definition and exact SQL digests. The generator validates the complete history,
unique versioned keys, numeric migration phases and safe output paths before any
model/artifact writes. Invalid or changed history fails closed without partial
output. Existing generated Go/Zod/UI artifacts and migration 0014 remain byte-for-
byte unchanged. No intake role or runtime migration is introduced.

A scratch PostgreSQL test applies the generated-equivalent additive fixture,
verifies all three original permission mappings, the new test-only mapping and
unknown-key denial, then proves public template discovery stays unchanged and
an undeclared application template cannot be proposed. The fixture is under
`backend/internal/modules/iam/testdata`; it is not a registered production migration.
The actual CLI tests cover unchanged normal/check runs, additive emission, rejected
historical permission/family/snapshot changes, bad digests, unsafe/reused paths,
reused IDs/phases/keys, missing history and no partial writes.

Validation: 54 script tests passed with zero skips; full IAM race/coverage passed
in 296.939 seconds at 82.0%, with 289 passing test/subtest events, zero failures
and eight existing conditional real-provider skips. The separate real PostgreSQL
append/discovery/proposal check passed. Full Go vet/build/lint, contractdocs
freshness and canonical generation/check passed; public artifact diff is empty.
All 21 frontend lint/format/typecheck tasks passed; final script formatting and
lint were rechecked successfully after the last test-only addition. No runtime
frontend files changed, so the prior full UI tests/build are not newly claimed.

Commands run: `node --test scripts/staff-template-migrations.test.mjs` (RED then
GREEN), `pnpm --filter @helivanta/scripts test`, normal/check Staff generation,
`go -C backend test -race ./internal/modules/iam -run TestAppendOnlyTemplateSQLPreservesPublishedDefinitions -count=1`,
`go -C backend test -json -race -cover ./internal/modules/iam -count=1`, full Go
vet/build/lint, `go -C backend run ./cmd/contractdocs -check`, and
`pnpm turbo lint format:check type-check`. Exact source review remains in
[draft PR #1489](https://github.com/tesserix/helivanta/pull/1489).

The [compound command design](../superpowers/specs/2026-10-31-compound-staff-command-authority.md)
records the actual two-permission evidence consumer and source-lock ordering to
implement next. It is a design, not implemented compound authority or HR workflow.
The catalog does not establish old/new consumer compatibility or activate a new
template. Scoped HR intake, current reviewed actor/person basis, immutable employment
review, legacy-governance classification, every employment lifecycle/cross-module
cutoff and normal Admin/recipient/reviewer browser acceptance remain required.
Counts stay 10 partial, 10 audit pending, 1 research, 0 fully accepted. Full A01–A12
remains active. No production schema, grant, account, rollout or release changed.

## Access/RBAC checkpoint — 2026-10-31: Staff document account binding

The source/phase audit now records all **33 HR intake primary-permission routes**
(23 enrollment, 10 document), secondary evidence permissions, shared helper
consumers, storage authorization phases and the historical template migration
boundary in [the inventory](staff-intake-access-inventory.md). Scoped HR intake,
compound command authority and immutable employment review remain to implement.

A real PostgreSQL/NATS file regression returned private synthetic metadata with
200 despite a different expected account. The common document parameter helper
now reuses the shared Staff principal precondition. All ten document routes
reject empty/invalid/repeated/joined/mismatching tenant/account assertions with
no-store `409 session_changed`; actual foreign scope stays 404. This explicitly
corrects the old mismatching-tenant 404 response. Real released-file checks cover
100 invalid-header/route combinations, unchanged file/classification/audit/outbox,
zero released-byte reads on denial, matching/headerless downloads and foreign scope.

Separate client regressions were RED for missing subject headers and same-tenant
account cache collisions. The Staff document UI and credential-evidence selector
now inherit the original verified Staff account; SDK headers/cache keys and
classification form keys bind that account. Tests cover aborted file preparation
without PUT after account change, no uncertain classification command carried
across accounts, and hidden private state plus parent context invalidation on
session_changed. Network uncertainty retains exact retry within the original account.

Focused backend race and 29 related UI tests pass. Full Workforce race/coverage
passed in 449.125 seconds: 83.8% statement coverage, 521 passing test/subtest
events, zero failures and zero skips. Full Go vet/build/lint and 21 frontend
lint/format/typecheck tasks pass. API suite: 413 tests in 60 files passed.
The five remaining frontend test tasks also pass. Complete shell tests pass:
1,258 tests in 160 files. Production shell build and Expo web/iOS/Android exports
pass. The first build correctly refused an absent API_URL; the successful rerun
supplied the canonical local Global API origin `http://localhost:8081`, without
changing the configuration guard. Existing Vite/jsdom navigation and Next
middleware-deprecation warnings were reported; no test or guard was suppressed. An initial
client typecheck identified the credential-evidence selector as another consumer;
it now inherits the account too. No failing assertion was removed or skipped.

Commands actually run (Go 1.27.3; Node 24.22.1 for final frontend checks):

- RED/GREEN: `go -C backend test ./internal/modules/workforce -run TestStaffDocumentRequestsRejectChangedExpectedAccount -count=1` (200 before the guard; 409 after).
- `go -C backend test -race ./internal/modules/workforce -run 'TestStaffDocumentRequestsRejectChangedExpectedAccount|TestDocumentRequestsCannotCrossAnExpectedTenantBoundary' -count=1` (passed).
- `go -C backend test -json -race -cover -timeout=20m ./internal/modules/workforce -count=1` (passed, results above; disposable Colima PostgreSQL/NATS).
- `go -C backend vet ./...`, `go -C backend build ./...`, and from backend `golangci-lint run ./...` (passed).
- `go -C backend run ./cmd/contractdocs -check`, canonical staff-access check and Go formatting/diff checks (passed).
- `pnpm turbo lint format:check type-check` (21 successful tasks).
- `pnpm --filter @helivanta/api exec vitest run --maxWorkers=2` (413 passed).
- `pnpm --filter @helivanta/shell exec vitest run --maxWorkers=2` (1,258 passed).
- `pnpm turbo test --filter='!@helivanta/api' --filter='!@helivanta/shell' --concurrency=2` (remaining five test tasks passed).
- `API_URL=http://localhost:8081 pnpm turbo build` (both build tasks passed).

Membership guard source `a12216d1` is pushed to [draft PR #1489](https://github.com/tesserix/helivanta/pull/1489),
with exact-head CI [41136443845](https://github.com/tesserix/helivanta/actions/runs/41136443845)
still running at the latest check. This continuation adds no migration, SQL/FGA
change, permission, endpoint, production account or rollout. Browser handoffs,
console CI billing gate, production registration, legacy grant migration, all
employment lifecycles and cross-module cutoff remain open. Counts stay 10 partial,
10 audit pending, 1 research, 0 fully accepted; A01–A12 remains the full objective.

## Access/RBAC checkpoint — 2026-10-31: ended-membership admission

The current legacy POST regression reproduced implicit restoration after final
membership removal: expected 409, actual 202. Candidate migration
`0019_iam_membership_recovery_admission` now rejects zero-to-positive continuity
without explicit command/removal-basis context, using the already serialized
continuity row. The handler maps only the named integrity violation to no-store
`409 membership_recovery_required`. SQL is persisted beside its embedded module
migration and recorded in the generated contract inventory; no live migration
has been run.

Real PostgreSQL/NATS HTTP checks pass for ordinary/clinical/admin-role legacy
restoration refusal, unchanged epoch/membership/audit/outbox, legitimate explicit
recovery, duplicate repair retaining the recovered ID, direct runtime-role
inserts with absent/partial contexts, forged non-admin context refusal, tenant
isolation and an observed final-removal lock wait followed by refusal. These
are synthetic API/database checks, not normal browser authentication evidence.

Validation checkpoint: full build, full vet, full Go lint (0 issues), contract
generation/check, contractdocs tests and focused IAM membership journey passed.
The complete architecture suite and IAM loginclient/contract race suites also
passed. Full IAM runs exposed historical raw/legacy POST reenrollment in two
projection fixtures plus the obsolete migration count; both positive fixtures
now use public removal/recovery commands while preserving stale-projection,
pending-approval and generation assertions. Targeted leave/identity/projection
race checks pass. The final full IAM race/coverage run passed in 300.193 seconds
with 82.0% statement coverage, 288 passing test/subtest events, zero failures
and eight pre-existing conditional skips. Eight real-Zitadel integration tests skip
without their separate dev credential environment; this is unrun acceptance,
not a passing real-provider journey. No new skips were introduced. One full
architecture invocation omitted the required Docker socket environment and
failed fixture startup; its corrected full rerun passed without test changes.

Commands run from `backend/` with the established disposable Colima socket for
container-backed suites:

- RED: `go test ./internal/modules/iam -run TestLegacyMembershipGrantCannotReopenEndedMembership -count=1` (202 instead of 409).
- Final: `go test -json -race -cover ./internal/modules/iam -count=1` (passed; conditional skips above).
- `go test -race ./internal/modules/iam/loginclient ./internal/modules/iam/contract -count=1` (passed).
- `go test ./internal/archtest -count=1` (passed).
- `go test ./internal/journey -run TestIAMGrantMembership -count=1` (passed, in the combined focused architecture/journey invocation).
- `go test ./cmd/contractdocs -count=1`, `go run ./cmd/contractdocs`, `go run ./cmd/contractdocs -check` (passed).
- `go vet ./...`, `go build ./...`, `golangci-lint run ./...` (passed; scoped lint rerun after fixture changes also passed).
- `gofmt -l internal/modules/iam`, `node ../scripts/generate-staff-access.mjs --check`, `git diff --check` and staged redacted Gitleaks scan (passed).

Full A01–A12 remains open: first legacy enrollment/privilege expansion and
external migration, provisioning/recovery browser acceptance, employment
suspension/resumption, resignation/termination/contract expiry/rehire and
cross-module cutoff. Counts remain 10 partial, 10 audit pending, 1 research,
0 fully accepted. Console CI 41132417027 was rechecked: GitHub's annotation still
says jobs did not start because payments/spending limits need owner attention.
No merge, production migration, rollout, release tag or issue closure occurred.

## Access/RBAC checkpoint — 2026-10-31: operator recovery UI candidate

Console [draft PR #669](https://github.com/tesserix/tesserix-home/pull/669)
contains implementation `d9117af4a21526ebd9f642cb1eb96ee7723f18de` and QA-gate
notes at `209c2aa`. It adds exact retained command/request inspection, timestamped
source observations, required proposal evidence, independent review decisions and
matching-account reconciliation through the existing same-origin BFF. Current
reader/writer capabilities, server-held credentials, source owner reauthorization,
no-store, CSRF, exact route/method scope, query refusal and strict UTF-8 stay enforced.
Uncertain requests/decisions freeze their UUIDv7/body/key; session-scoped browser
storage retains identifiers/action/digest without private reasons or credentials.
Reload and retained-outcome recovery preserve the same successor. Confirmed
operator denial refreshes current reads and hides revoked private evidence;
authorization-source outages are reported separately as unavailable.

Canonical Go-derived models are published and byte-verified at owner
`f75bb0e5479446c3a36015516a2bf661aee357a2`; exact owner CI 41131295353 passed.
Source/schema ownership is unchanged: schema PR #1616, chart 0.3.62 / `325012b`.
No new migration, SQL/FGA change, production account/grant or rollout occurred.

Local console acceptance: **5,572 tests in 295 files passed with no skips** using
`--maxWorkers=2`; typecheck, lint, production build, immutable artifact/provenance,
staged redacted secret scan and diff checks pass. One earlier parallel rerun hit
10-second setup hooks in six existing database files; no tests/timeouts were
changed and bounded complete reruns passed. Read the console's
`docs/qa/staff-recovery-console.md` for exact commands and evidence boundaries.

Real local sign-in used separate synthetic provider/client/operator and PostgreSQL
fixtures with auth bypass disabled. It reached the console callback, which Chrome
blocked with `ERR_BLOCKED_BY_CLIENT`; human handoff is requested. No warning was
bypassed, callback code committed or human session manufactured. Separately,
GitHub refused to start console CI 41131982143, secret scan 41131982554 and mobile
check 41131982158 because failed account payments/spending limits require owner
attention. No remote tests ran. Billing/capacity handoff is requested; no billing,
visibility or runner changes were made. New-head checks must pass before merge.

**Access/RBAC A01–A12 remains incomplete.** Normal console/HMS Admin → recipient →
independent reviewer acceptance, source/route coverage, production registration
(owner #12), profile correction/activity, legacy membership/link/grant handling,
renewal, all employment/contract lifecycles and cross-module cutoff acceptance
remain open. Staff Management counts stay 10 partial, 10 audit pending, 1 research,
0 fully accepted. No merge, production deployment, issue closure or release tag.

## Access/RBAC checkpoint — 2026-10-31: reconciliation and independent decisions

Follow-up source-integrated HTTP evidence is pushed at `17e3dc90323a3d2c6ac410488b3c1295d792fc4d`
in owner PR #22. The tagged local PostgreSQL/Zitadel race test passes through real
organization/project/grant proof, independent absent-account approval, one late
original creation, matching orphan reconciliation, immutable retry, no successor
dispatch or reopened original dispatch, and actual grant withdrawal/restoration.
Tagged API vet/lint and redacted secret/diff checks pass. Console tokens remain
asserted fixtures; real login and normal HMS UI acceptance are not proven.
Implementation-head CI run 41125307163 is now **passed**; the follow-up head's CI
has not yet been observed at this update. No merge or deployment occurred.

Identity-owner draft [PR #22](https://github.com/tesserix/onboarding-service/pull/22)
now contains pushed `293aa359a97ba7a3d853d102b12253914f913ab2`.
Fresh console write authority guards creation reconciliation and immutable independent
review decisions. Matching source accounts retain original creation facts and block
replacement; removed accounts retain historical birth. Approval serializes exact
birth/absence checks with receipt admission, preventing a stale absence decision
after a receipt arrives. Replacement atomically prepares a fresh bound successor
without dispatching an account or granting membership. Entire recovery route family
bypasses cached response replay. Authority-withdrawal tests cover before commit,
after commit without disclosure, and authorized recovery of retained history.

Local evidence: full PostgreSQL Go race/coverage suite, vet/build, default and
provider-fixture lint, four delivery Python tests, redacted Gitleaks and diff checks
passed. Decision/reconciliation HTTP tests use callback source proofs; actual-provider
adapter tests from the preceding candidate remain separate evidence. New CI run
[41125307163](https://github.com/tesserix/onboarding-service/actions/runs/41125307163)
is queued at this checkpoint; previous `a3eab89` run 41120222174 passed.
No new migration/schema change, merge, production rollout, tag or issue closure.

**Access/RBAC remains partial.** Next gates include full real-source owner HTTP,
consumer UI and Admin → recipient → independent reviewer acceptance, followed by
membership recovery/removal, renewal, all employment/contract lifecycles and
cross-module cutoff acceptance. No whole-area completion percentage is claimed;
Staff Management counts remain 10 partial, 10 audit pending, 1 research, 0 accepted.
The earlier checkpoint sections below describe their own evidence dates.

Updated: 2026-10-31. Owner: Helivanta Staff Management delivery. This is the summary tracker for the full user-requested scope. The [workflow acceptance ledger](staff-management-acceptance.md) holds detailed scenarios and evidence; this document does not replace it.

### Exact-account proof and fresh reviewer source view — 2026-10-31

Owner `a3eab89` implements exact frozen-account classification and a freshly
authorized reviewer source-view POST. Absence requires two actual missing reads
and stable empty authoritative history. Matching requires retained birth/contact
and sealed command binding. Removal additionally requires terminal removal, actual
missing reads and bounded ownership history proving one human birth and the latest
causal nonce. Unknown, contradictory, stale or saturated proof fails closed.
No universal contact availability, verified contact or operational access is claimed.

The source view derives all input from the exact private dispatched command,
validates returned scope/freshness, rechecks live authority and canonical registry,
then commits mandatory disclosure audit. It exposes only disposition and observed/
birth time. A replay regression hid changed facts; explicit cache exclusion now
requires fresh reads and denies revoked authority. It creates no account, retained
creation receipt, decision, closure or successor. Approval must obtain new proof.

Actual isolated v4.15.3 race tests passed absent/matching/removed proof and the
previous operator checks. Email delivery advanced source history during immediate
matching proof; a later fresh proof passed with creation dispatched once. Final
matching used two proof attempts within the fixture's three-attempt bound.
An actual projection fault captured the synthetic target's parent/child rows
privately, produced real GetUser 404 with retained creation history, and verified
that recovery denied absence. Restore initially failed on a generated notification
column; the correction recomputes it, verifies every captured row, and restores
matching proof. Source event history stayed retained; all captured target projection rows are restored.

Full owner PostgreSQL race/coverage passed: 56.0% overall, API 66.0%, store 66.7%,
provider 87.8%; classifier 93.6%, removal ownership 95.8%, cross-package source-view
service 85.7%. Vet/build, normal and fixture-tag lint (zero issues), four delivery
tests, secret/diff checks and both actual-provider race fixtures passed. These
figures are test coverage, not completion. Configuration/time-budget documentation
was corrected to actual console environment names and the unchanged two-second
creation budget. No schema/production change, migration, merge or release occurred.

Database/HTTP source-view tests use callbacks; actual source-protocol/fault proof
is separate. Real-source full owner HTTP and normal HMS Admin/recipient/independent-
reviewer UI remain unproven. New-head CI is tracked separately. Next: matching-
orphan receipt reconciliation and fresh independent decision service/API, explicit
conflict handling, activity, prepared-command/profile correction, consumer UI,
recipient setup/sign-in, canonical registration (#12), legacy grants and remaining
A01–A12 lifecycle/cross-module cutoff acceptance. Counts stay 10 partial,
10 audit pending, 1 research, 0 fully accepted. Full Access/RBAC stays active.

### Exact request review checkpoint at 2c61d2a — 2026-10-31

Owner `2c61d2a` adds current-human exact-request GET for reviewer evidence:
maker, requested action, reason/evidence reference, optional proposed successor
initiator and retained decision/successor command ID. Original source account,
nonce, contact/profile and machine bindings are omitted. Two live operator/source
checks surround mandatory audited inspection, followed by a fresh transactional
read. Request, decision and lineage use one statement snapshot; a decision
committed during reverification appears in the final view. No approval authority
or membership eligibility is inferred from a request read.

Tests pass for undecided/final rejection/complete replacement snapshots, incomplete
approval denial, foreign or missing requests, malformed input, audit rollback,
source outage, revoked authority/entitlement and canonical rotation. A new regression
reproduced proposal/decision commits paired with `denied` audit outcomes; both
stores now reject those outcomes and retain no business action. Full final
actual-PostgreSQL race suite, vet/build/lint (zero issues), four delivery tests,
redacted secret scan and diff checks passed. Coverage: API 65.6%, store 66.7%;
request store read 90.0%, HTTP handler 94.1%, cross-package service 89.7%.
These are coverage figures, not a Staff completion percentage.

Pinned v4.15.3 primary-source research now records why empty user projection or
search checkpoint cannot prove absence: rows/checkpoint are read separately, and
projection progress is not payload conformance. Next source proof must classify
the exact frozen account and reconcile matching orphans; replacement preparation
must not claim universal contact availability or operational access. The source
checkout was preserved, and findings were checked with `git show` at the pinned
tag. Owner canonical-registration issue #12 was rechecked and remains OPEN.

New HTTP/database tests use source callbacks. Real-source full HTTP and normal
HMS Admin/recipient/independent-reviewer UI remain unproven. Parent 74abd3d CI
41111788806 passed; new-head CI is tracked separately. No schema/production change,
migration, merge, deployment, release or completion tag is claimed. Next: exact-
object source conflict/orphan proof, independent decision service/API, activity,
prepared-command/profile correction, consumer UI and remaining A01–A12 lifecycles/
cross-module cutoff acceptance. Counts stay 10 partial, 10 audit pending,
1 research, 0 fully accepted. Full Access/RBAC remains active.

### Recovery read/proposal checkpoint at 74abd3d — 2026-10-31

Owner `74abd3d` connects current human console proof to an audited exact-command
recovery read and immutable proposal PUT by UUIDv7 request ID. Source organization/
project and canonical active tenant/product/entitlement are checked under the real
operator principal. Original initiator/machine/provider facts survive canonical
machine rotation. Recovery proposals derive maker from verified identity and
require reason/evidence; they create no decision, closure, replacement, user,
membership or dispatch. Independent review remains required.

A test reproduced generic cached success after maker revocation. Explicit route
exclusion now forces fresh authorization on every retry, with no generic cached
response. Tests cover rejected forged authority/credentials and malformed inputs,
read-only operator denial for proposals, live proof withdrawal, source/registry
changes during I/O, mandatory audit binding/rollback and committed proposal
retention without disclosure after revocation, followed by exact retry recovery.

Full actual-PostgreSQL owner race/coverage passed (55.4% overall; API 64.8%, store
65.6%). Vet/build/lint (zero issues), four delivery checks, redacted secret scan
and diff checks passed. Cross-package integration coverage confirms service
execution: receipt 94.1%, scope 79.5%, proposal 80.6%, validation 100%. These are
test coverage figures, not a completion percentage. Callback source proofs drive
the new HTTP/database tests; previous real v4.15.3 adapter proof is separate.
Full real-source HTTP and normal HMS Admin/recipient/independent-reviewer UI are
unproven. New-head CI is tracked separately. No schema/production change, migration,
merge, deployment, release or completion tag is claimed.

Next: minimized recovery request detail/activity, source conflict/orphan checks,
independent decision service/API, prepared-command/profile correction and consumer
UI; recipient setup/sign-in, canonical registration (#12), legacy grants and all
remaining A01–A12 lifecycle/cross-module cutoff acceptance. Counts remain 10 partial,
10 audit pending, 1 research, 0 fully accepted. Full Access/RBAC stays active.

### Recovery-operator authority checkpoint at 2e68596 — 2026-10-31

Owner `2e68596` adds a bounded source verifier for active/verified human,
configured platform organization, direct console-project grant, authoritative
organization/project/grant state and current role definitions/keys. Token-time
roles and grant projections alone cannot authorize recovery. Relevant source
heads must remain unchanged across snapshot reads; stale, malformed, ambiguous,
changing or saturated proof fails closed. No source roles/memberships are changed.

HTTP authentication now retains verified console-token origin; registered
machines remain products. A regression reproduced the lost origin before the
fix. Recovery policy rejects legacy-origin admins, product machines and customer
credentials; operators can only read, administrators can propose/decide subject
to later live proof and target checks. These new actions/source proof are not yet
wired to operator service endpoints.

An explicitly selected actual isolated Zitadel v4.15.3 test passed current proof,
grant deactivation denial/reactivation, removed-role-definition write denial with
read preservation and disabled-human denial. Backend-created synthetic operator
authority is a protocol fixture, not human-login or normal HMS Admin/recipient/
independent-reviewer UI acceptance. TLS tests additionally cover foreign scope,
stale contact/state, missing/malformed history, churn/saturation, redirects,
ambient cookies and provider errors.

Full actual-PostgreSQL owner race/coverage passed: 56.4% overall, API 63.8%,
authz 95.2%, store 64.9%, provider 86.7%, operator verifier 94.0%. Vet/build/lint
(zero issues), four delivery tests and redacted secret/diff checks passed.
No schema changed. New-head CI is tracked separately from local evidence;
no merge, production migration, rollout, release or completion tag is claimed.

Next: audited service integration using the actual operator principal and fresh
canonical registry/source scope; conflict/orphan proof; authenticated recovery
endpoints/activity and prepared-command/profile correction; normal operator/HMS
Admin/recipient/reviewer UI; recipient authenticator setup, canonical production
registration (#12), legacy grants and all remaining A01–A12 lifecycle/cross-module
cutoff acceptance. Counts remain 10 partial, 10 audit pending, 1 research,
0 fully accepted. Full Access/RBAC remains the active objective.

### Atomic reviewed recovery and historical self-review — 2026-10-31

Owner `a99551f7ee120e4890e9d342dd626aedd2a279c9` is pushed in draft PR #22.
Independent decision, prepared successor, permanent root/parent lineage and audit
commit atomically; approval itself performs no provider dispatch. Exact retry
retains reviewer/reason/successor ID and original server-selected provider ID/nonce.
Final rejection, one competing replacement winner, audit rollback and bare-approval
failure are verified. Generation-linked FKs and unique parent/request/successor
prevent branching/cycles with indexed point reads. Historical root/intermediate
creators and accounts remain excluded from later-generation review; actual tests
reproduced that bypass before the fix and now reject it.

Normal creation admits the reviewed successor and blocks closed/superseded parents.
Receipts preserve late/historical creation facts and add only recovery state and
successor command ID. Observation/creation deny authority before/during-source
closure; no private operator reason/identity or child provider binding is exposed.
Actual application-role decision/retry and lineage mutation denial pass. These
are store/contract/race checks, not verified current operator authorization or
normal operator/HMS Admin/recipient/independent-reviewer UI acceptance.

Canonical schema `325012bc6fbaf199413dbce6ed58406be4ca66e5`, chart 0.3.62,
SHA-256 `aa89167dad24dcfb3fd71e305cad29f573d1604ffc99ee75f9e8f459b4880756`,
is published, hash-verified and pinned for owner CI. Actual local catalog checks
confirm six new indexes valid/ready, seven lineage FKs valid and onboarding
SELECT/INSERT with UPDATE/DELETE/TRUNCATE denied. Bootstrap ON_ERROR_STOP=0 means
Job success is not migration proof; actual state validation remains a release gate.

Fifteen added tests (11 transaction/lineage, four API scenarios) plus fourteen
previous recovery tests pass. Full actual-PostgreSQL race/coverage: 55.1% overall,
API 63.8%, store 64.9%, decision 83.6%, successor/root helpers 83.3%, shared scope
100%. Vet/build/lint (zero issues), four delivery tests, redacted secrets and
shell/diff checks pass. Schema local Helm lint/template and seven selected tests
pass. Exact owner head passed CI 41102254872. Exact schema head passed Repository Tests
41101963585, PR Validation 41101963451 and Helm 41101963412. These current-head
passes do not prove operator/source authorization or full normal-role UI acceptance.

Next: verified-current operator role/source proof, conflict/orphan handling,
authenticated operator endpoints and minimized activity; prepared-command/profile
correction; normal operator/HMS Admin/recipient/reviewer UI; recipient authenticator
setup/sign-in; canonical production registration (#12); legacy staff/link/grant
classification; remaining A01–A12 lifecycle and cross-module cutoff acceptance.
No operator write endpoint is exposed and no merge, production migration, rollout,
release, issue closure or completion tag occurred. Full Access/RBAC remains open;
Staff Management area counts are unchanged.

### Recovery requests and independent decision records — 2026-10-31

Owner `713076bd9ef832730ca69088637c03261e9170fa` (runtime unchanged from
`fd65aca6e37faa95dafeeb8e8fdf1e378b5711b5`) is pushed in draft PR #22.
Its new store operation retains exact recovery proposal input and mandatory audit
atomically. Current canonical machine/entitlement/source scope is rechecked;
original command/initiator/provider-user facts survive machine-credential rotation.
Changed requester/action/reason/evidence conflicts. Audit actor/type/product/tenant/
command/action must match the request. Failed new/replay audit releases no private
result and retains no new request. No operator endpoint is exposed yet.

Canonical schema `8fa241651362392d8c90322e97947066688bb832`, chart 0.3.60,
SHA-256 `b9bfb2a34e7e8ff0708ce4bccaf52121e8a64557c7ff21a160189332e4a0b14c`,
is persisted in schema-owner PR #1616; owner CI pins it and the published immutable
source hash was verified. New append-only request/decision ledgers bind an actual
dispatch and exact source/actor facts. Maker, original initiator, original source
account and replacement initiator cannot review. Rejected requests remain final;
one replacement approval wins concurrently. Closing unresolved investigation
permits later reviewed replacement and preserves the original dispatch/root.
An approved row alone authorizes no dispatch, linking or operational membership.

Fourteen new actual-PostgreSQL tests passed, including self/target review,
forged/NULL bindings, mandatory evidence, final rejection, closure/replacement,
twelve concurrent replacement approvals with one winner, actual application-role
mutation denial, machine rotation, withdrawal/scope drift, immutable retry and
audit rollback/binding. Actual application-role proposal and exact retry also
passed. Full owner race/coverage passed: 54.5% overall, API 63.8%,
store 62.7%, proposal operation 87.0%. Vet/build/lint (zero issues), four delivery
checks, redacted secrets and shell/diff checks passed. Schema local Helm
lint/template and seven selected repository tests passed. Latest owner CI 41095777733 is pending. Schema head passed PR validation
41094188936, repository 41094188917 and Helm 41094188898. Earlier owner passes
are historical, not substitutes for the new owner head.

Still required: atomic reviewed decision execution/successor/root lineage,
current verified operator authority and owner/consumer endpoints, conflict/orphan
handling, full Admin/recipient/independent-reviewer UI, recipient authenticator
setup/sign-in, canonical production registration (owner #12), legacy
staff/link/grant classification and all remaining A01–A12 lifecycle/cutoff gates.
No merge, migration, rollout or release occurred; area counts stay unchanged.

### Actual recipient contact UI verification — 2026-10-31

A fresh browser visit reproduced a missing fixture dependency: the isolated
v4.15.3 provider redirected to Login V2, but that UI was absent. Added a separate
loopback-only Login V2 v4.15.3 container, pinned to upstream image digest
`sha256:906081d8c5b8760321fa811d670e680247f768e5c4bdd4edb73d3ea942800385`.
Its dedicated IAM_LOGIN_CLIENT machine has a private, two-day credential; it is
not a Staff or operational access grant. Original instance-feature configuration
was saved privately before changing only this isolated fixture's Login V2 base
URI. Added pinned Mailpit local capture on the fixture network; its UI is
loopback-only and no external email delivery was configured.

Normal browser actions used the existing role-free synthetic account: enter
login name, request a fresh invitation, read the delivered email and submit its
code. The provider changed to active/verified; the unchanged owner adapter then
observed active plus email_verified=true and the exact retained creation time.
Fresh provider queries returned zero memberships and zero user grants. No
password, passkey or verified-contact override was set by this test.

The next visible screen is Choose authentication method (Password/Passkeys).
Full authenticator setup, subsequent sign-in, actual canonical registry proof,
HMS Admin/Global API consumption and independent reviewer acceptance remain
open. This proves actual recipient contact verification through upstream UI,
not the complete A01 provisioning or Access/RBAC journey. The browser is retained
for continuing recipient setup; private evidence/setup and recovery captures are
0600 under the existing authorized QA fixture directory. All changes were local;
production was untouched.

### Access/RBAC private command history — 2026-10-31

Owner commit `9fd472d49b148ff4b1c207da609602b1b1d8d533` is pushed in draft
[PR #22](https://github.com/tesserix/onboarding-service/pull/22). Exact-command
history now returns bounded action/outcome/UTC summaries with current product,
tenant, source and original-initiator checks. Opaque hash-only cursors preserve
15-minute expiry across service instances and exclude later appends until
refresh. Mandatory disclosure-audit failure returns no page or new cursor.
Human viewer authorization remains the Helivanta consumer's responsibility.

Canonical SQL is persisted at `1acc9d687cd161bf77689424fdbb3a1716decd9c`,
chart 0.3.59, SHA-256
`8e2f89da8e6f4771a7d0af3ca075094f2ec622ab6f0ce38475f9b7660018b382`.
Schema-owner PR #1616 passed Repository Tests 41085750101, PR Validation
41085749902 and Helm/template/security 41085750047. Its ephemeral cursor cache
is separate from the four immutable identity ledgers. Actual application-role
tests verified continuation/cleanup, expired-only deletion, no UPDATE/TRUNCATE,
a 1,000-row cleanup limit and skipped locked rows. Narrow UPDATE(token_hash)
privilege permits PostgreSQL row locks; forced RLS rejects actual updates.

Full local owner race/coverage passed (54.3% overall, API 63.8%, store 61.6%),
plus vet/build/lint (zero issues), redacted secret scan, four delivery tests,
shell/diff checks and a Linux/arm64 image build. A rolled-back 100,001-row audit
fixture used the partial index in 0.046 ms; this is query-plan evidence, not an
API SLO. Exact-head CI [41089960472](https://github.com/tesserix/onboarding-service/actions/runs/41089960472)
passed all applicable gates, including the container build/smoke/scan job.

Operator resolution/replacement/conflict/orphan handling, actual recipient
contact delivery/setup, canonical production product registration (owner #12),
Global API/Admin/recipient/independent-reviewer UI, legacy classification and
all remaining A01–A12 lifecycle/cross-module acceptance stay open. No merge,
production rollout or migration occurred. Counts remain 10 partial, 10 audit
pending, 1 research and 0 fully accepted.

### Access/RBAC creation and contact reservation — 2026-10-31

Owner runtime `f7bc168` now exposes creation PUT with immutable input,
server-selected account/nonce, permanent contact reservation, one audited
dispatch and retained creation receipts. Exact/concurrent retries do not create
again; lost responses remain uncertain until matching read-only reconciliation.
Canonical SQL is persisted at schema-owner `2214e7a7` (chart 0.3.58), SHA-256
`676f5c8676e48d9647b3d08e37a6e82ca84c91d22d8d4d343ee9bc9e03fafb63`.
Its repository, Helm/template/security and PR gates passed. Roots retain command
lineage and cannot be rewritten/deleted by the application; historical commands
without roots fail closed rather than receive invented backfill.

Full local owner race tests, vet/build/lint, secret scan, four delivery tests and
the final Linux/arm64 build passed. An explicitly selected actual v4.15.3 provider
test passed owner HTTP creation/replay/observation and verified zero source
memberships/user grants with contact still unverified. It uses fixture registry
proof, not normal Admin/recipient/reviewer UI acceptance. Private account evidence
is retained as 0600 JSON; no password/verified flag was supplied to creation.
Latest owner head `fc5d930` passed all applicable CI gates in
[41076976851](https://github.com/tesserix/onboarding-service/actions/runs/41076976851),
including its container. The earlier runtime run 41076590905 was superseded and
cancelled after its Go/contracts/secret checks; it is not counted as passing.
Subsequent owner/schema commits update documentation only.

Operator recovery/history/root replacement, actual recipient setup/verification,
canonical product registration, Helivanta/Global consumer UI and the remaining
A01–A12 lifecycle/cross-module acceptance remain open. No merge, production
rollout or new live migration occurred. Counts remain unchanged.

### Access/RBAC provider failure acceptance — 2026-10-31

The identity-owner candidate now includes `ec10aa6` in draft
[PR #22](https://github.com/tesserix/onboarding-service/pull/22). Actual isolated
v4.15.3 testing reproduced stale active proof after successful deactivation,
stale binding metadata after replacement, and skipped deactivation hidden by a
later unrelated profile revision. The corrected adapter compares latest
authoritative state/contact/verification events and bounded mutation history;
the same live skipped-deactivation fixture now denies proof. Explicit synthetic
source reactivation restores successful observation. These are provider-only
tests, not Admin/recipient/reviewer UI or membership/employment activation.

Final full owner race/coverage passed (54.6% overall, provider 85.2%), as did
vet/build/lint, secret scan and the final Linux/arm64 image build. Exact-head CI
[41070733440](https://github.com/tesserix/onboarding-service/actions/runs/41070733440)
is in progress; preceding owner CI 41066660408 and Helivanta documentation-head
CI 41067001369 passed. Details and retained limitations are in the
[identity audit](staff-identity-provisioning-audit.md). No merge, production
rollout or new application migration occurred. The full A01–A12 objective and
the counts below remain unchanged.

### Access/RBAC continuation — 2026-10-31

Identity-owner draft [PR #22](https://github.com/tesserix/onboarding-service/pull/22)
now contains committed/pushed `e3dc7e5`: protected command receipts, fresh source
observations, matching-dispatch reconciliation, atomic disclosure audit, replay
cache exclusion and strict source JSON. The full owner race suite with actual
isolated PostgreSQL, vet, build and lint passed again. Exact-head CI 41066660408
was queued at this checkpoint. This candidate remains unmerged/undeployed.

The [identity audit](staff-identity-provisioning-audit.md#owner-receiptobservation-checkpoint--2026-10-31)
records the open provider projection-synchronization gate and corrected explicit
product registration requirement. Creation submission/recovery, real recipient
verification, consumer UI, legacy reconciliation and the remaining A01–A12
lifecycle/cross-module acceptance are still required. Counts remain **10 partial,
10 audit pending, 1 research, 0 fully accepted**; the full Access/RBAC objective
remains open. The production checkpoint below is unchanged.

## Status and completion rules

- **Partial**: implementation and some evidence exist, but required journeys or checks remain open.
- **Audit pending**: implementation exists, but its complete workflow has not been established by this review. This does not mean the feature is absent.
- **Complete**: all applicable server, database, UI, role, failure/retry, persistence, CI and deployed acceptance gates in the ledger pass. There are currently no fully accepted areas.
- Record local positive UI QA separately from production positive or denial QA. A test count, route, screenshot, migration job or published release alone does not establish workflow completion.
- Keep issues open until their actual acceptance criteria pass. Legacy issue references to Keycloak or proposed tables must be reconciled with current Zitadel/IAM/Workforce ownership before implementation.

## Current-source access continuation — 2026-10-31

Source basis: current main `5a618f86`. Scoped leave commands, discovery and the
separate reviewer UI already exist on this source; their existence does not close
the full Access/RBAC objective. Historical results from older checkouts are not
current-source release evidence.

Two current-source UI regressions reproduced: a failed selected-request read
left independently loaded private history visible, and a failed history read
left decision controls enabled. The leave-request panel now hides history when
the selected request cannot be verified and withholds decisions while detail or
history verification is pending, refreshing or failed. Existing successful
filing, review, withdrawal and revocation behavior remains covered. This changes
UI handling only; it grants no role and changes no server authorization, schema
or migration.

Focused red/green evidence: `/tmp/current-staff-read-red.log` and
`/tmp/current-staff-read-green.log` (10 passing tests). Full current-source shell
tests passed 1,266 tests in 245.19 s, plus shell typecheck, lint, formatting and
production build. Build validation supplied `API_URL=http://localhost:8081`,
the repository's development Global API origin; the first build correctly
refused to proceed without an explicit production-build gateway origin. Commands
run with installed Node 24.22.1: `pnpm install --frozen-lockfile`,
`pnpm --filter @helivanta/shell test`, `type-check`, `lint` and `build`,
scoped Prettier, and `git diff --check`.

The eight previously unrun real Zitadel provider tests now all pass, with no
skips, against the existing localhost identity fixture (28.546 s):
`go test -race -json ./internal/modules/iam -run '^TestIntegration_' -count=1`.
The private launcher loaded existing dev credentials into the child environment
without printing or committing them. The mode-0600 evidence file is
`current-main-zitadel-provider-20261031.jsonl` in the existing private QA bundle.
This covers password success/failure indistinguishability, enforced MFA variants,
enrolled-factor handoff, IdP-linked password login, foreign-organization policy
and native TOTP enrollment. It is provider integration evidence, not a completed
Staff provisioning or employment journey.

Fresh browser/CI/merge/deployment acceptance for this candidate remains open.
The next HR-intake inventory confirms 208 Workforce routes, including 23 primary
enrollment-management routes and 10 primary document-management routes. Compound
credential, reminder, leave-standing/report and storage/disclosure consumers
remain part of the required action matrix; adding a template or globally
replacing `enrollmentScope` is insufficient. Continue the
[source-verified plan](../superpowers/plans/2026-10-30-staff-access-and-review-continuation.md)
for scoped HR intake and an independent current-person/employment review basis.
All A01–A12 completion gates below remain applicable. No Staff area is marked
complete and no production rollout, migration or access expansion is claimed.

## Last recorded production release — c20a220, 2026-10-30

The user approved revision `c20a220267cc4e71cc169c47b3ef7220612da1ce` and all four
pending migrations. Kargo promotion `prod.01m659npxcq1ejdjcv41942wbe.06dda82`
succeeded. All six application workloads have two updated/ready/available
replicas on this source; API, Global API and shell are Synced/Healthy. The normal
migration job applied `0002_billing`, `0003_billing`, `0008_medicore` and
`0052_workforce_employment_withdrawal`, with durable `schema_migrations` receipts.
RLS is enabled and forced on all 11 inspected new/related tables.

The [release record](../deployment/releases/2026-10-30-staff-preparation-and-scheduling.md)
and [SQL provenance manifest](../deployment/releases/2026-10-30-staff-preparation-and-scheduling.json)
persist canonical SQL paths, source hashes, registration, application times and
future environment replay instructions. Historical SQL stays in Git and the
embedded owner migration registry; future corrections require new IDs.

Production normal-login QA verified restricted Staff/payroll denial and the
correct payroll heading; Admin preparation/required reason/withdrawal/reload and
immutable history; unchanged inactive Staff; preserved original draft; shift and
roster unsaved-input guards; long-label weekly layout/Remove; and retained shift
and area retirement history. Only designated synthetic records were changed;
no Staff allocation, dated roster, attendance or payroll action was performed.

All required PR CI gates and final image publication passed. Runtime files match
the fully tested PR head, with only the reviewed payroll documentation correction
added at merge. Final-source contractdocs check passed. Full local web checks
passed 30 tasks (1,222 shell and 409 API tests). See the release record for exact
CI references and the distinction between local, production and schema evidence.
The broad acceptance count remains **10 partial, 10 audit pending, 1 research,
0 fully accepted**; the lifecycle, review, payroll/PDF and letter gaps remain open.

## Previous production checkpoint — 22b6241, 2026-10-30

- **Production:** `22b6241ed0ece7180d713b77145104a63d95d343`,
  [employment draft and evidence release](https://github.com/tesserix/helivanta/releases/tag/2026-10-30-staff-employment-evidence),
  [PR #1458](https://github.com/tesserix/helivanta/pull/1458), including draft
  preparation [PR #1457](https://github.com/tesserix/helivanta/pull/1457).
  The user approved this exact rollout and migrations `0050` and `0051`.
- **Deployment verified:** Kargo promotion
  `prod.01m63tbd92xn9gt1ntzzmjb6er.dcd330a` succeeded. All six application
  workloads have two updated, ready and available replicas on the exact source.
  API, Global API and shell are Synced/Healthy. Both migrations are recorded;
  all four new private tables have enabled and forced RLS. CNPG had two healthy
  instances and a successful `2026-10-29T12:00:24Z` backup.
- **Production UI:** fresh synthetic owner login, wrong-tenant no-hospital denial,
  correct-tenant existing manager grant, ongoing draft save, real PNG upload and
  scan, binding to terms revision 1, reasoned withdrawal to binding revision 2,
  retained revisions 1–2 and full reload/reopen. The Staff member stayed inactive.
  Two protected downloads were authorized and audited with 179-byte metadata and
  the expected checksum. **A newly saved production browser file was not captured
  by automation; that artifact check remains unconfirmed.** The earlier local
  downloaded-file SHA-256 check passed.
- **Reviewer:** the user explicitly authorized backend password rotation for the
  synthetic reviewer. Its credential was changed through the existing
  credential-verified password-change backend, the private JSON was updated with
  mode `0600`, and a fresh normal UI login succeeded. Staff hospital access and
  payroll access stayed denied. No role, membership, MFA policy or hospital grant
  was expanded. This is restricted-role QA, not an employment approval.
- **Validation:** [PR CI](https://github.com/tesserix/helivanta/actions/runs/40868299072)
  and [merged-source CI](https://github.com/tesserix/helivanta/actions/runs/40872297255)
  passed, including E2E, Go coverage/latency and web checks. Image publication
  [40872296822](https://github.com/tesserix/helivanta/actions/runs/40872296822)
  passed. The merge tree exactly matches tested PR head
  `ab66664b0ab443011f1e6b2c0280adbd701da8e4`. Final local web gates passed all
  30 tasks (1,195 shell and 408 API tests); Workforce race coverage was 83.6%.
- **Next work:** scoped HR intake administration and a safe current-person basis
  for independent employment submission/review. See the
  [source-verified continuation plan](../superpowers/plans/2026-10-30-staff-access-and-review-continuation.md)
  and [open-issue map](staff-management-open-issues.md). Adding a role label or an
  approval button does not implement transaction-bound authority or safe new-hire
  identity. Effective lifecycle activation remains a separate owner/consumer gate.

No broad Staff area or umbrella issue is newly marked complete. Deployment of
private drafts/evidence does not implement employment approval, access activation,
expiry, suspension, resignation, termination, rehire, payroll runs or letters.
Historical checkpoints retain the state when recorded. The c20a220 release
checkpoint above is the last recorded production state.

## Quantitative checkpoint — 2026-10-30

The 21 tracked areas currently comprise **10 partial (47.6%)**, **10 audit pending
(47.6%)** and **1 research/proposal (4.8%)**. **0/21 are fully accepted (0%)**;
all 21 still have acceptance work. These are area-status percentages, not an
estimate that 38.1% of engineering effort is complete. The remaining scenarios
vary substantially in size, so no defensible overall effort or time percentage
is available yet.

The directory authority and account/version guards are deployed in the production
checkpoint above. Private employment-term draft and protected evidence preparation are deployed
and CI-validated, with production UI evidence and artifact-capture limits above. Scoped HR
role templates, reviewed employment decisions, dated transfers and offboarding
remain open; a directory status or draft term is not proof of operational cutoff.

| Area                            | Status                                      | Verified work / evidence                                                                                                                                                                                                                                                     | Remaining acceptance                                                                                                                                                                                                |
| ------------------------------- | ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Access and RBAC                 | Partial; current priority                   | Scoped proposals/independent decisions, reviewed Staff/login links, projection and membership generations, withdrawal/recovery; full local digital source activation with fresh reviewed link/role, recipient admission and account/source cutoff UI acceptance (2026-11-02) | Identity provisioning and verification; membership recovery; renewal; hospital suspension/resumption; notice, resignation, termination, contract end and rehire; cross-owner cutoff and legacy-grant reconciliation |
| Staff directory and supervision | Partial; profile/supervision slice released | Account/workspace-bound private editing, required fields, persisted revisions, cycle/stale guards and inactive labels; local and production Admin/restricted-reviewer QA in PR #1447                                                                                         | Full employee/consultant onboarding, editing by other authorized roles, dated transfers/postings, offboarding and safe removal                                                                                      |
| HR setup                        | Partial                                     | Broad create/edit/history/retire/reactivate coverage across masters                                                                                                                                                                                                          | Complete dependency, referenced-record, duplicate/concurrency, isolation and deployed acceptance                                                                                                                    |
| Identity and background checks  | Partial; person-review/source UI accepted   | Deployed person/employment reviews; full UI-created digital and non-digital sources, real local scanner, independent normal reviewer approvals, exact evidence bytes and source consumption; production scan/revoke smoke                                                    | Background-provider and photo policy review, purpose-specific verification integrations, photograph/card workflows and all remaining operational owner cutoffs                                                      |
| Photographs and ID cards        | Partial foundations                         | Photograph preparation metadata; protected file scanning/release/recovery foundations                                                                                                                                                                                        | Trusted PNG/JPEG validation, purpose-bound photo review, self-photo request, frozen branded card issue, independent authorization, preview/print, expiry, loss/reissue and offboarding                              |
| Shifts                          | Partial; local workflow audited             | Synthetic overnight create/edit, validation, revision history, retire/reactivate and reload; unsaved navigation/cancel/history protection fixed                                                                                                                              | Referenced retirement, multi-hospital/current-account scope, downstream roster effects and deployed acceptance                                                                                                      |
| Roster setup                    | Partial; local area workflow audited        | Required fields/review, pinned area membership, retirement/history/reload, inactive-reference rejection and unsaved-input guards                                                                                                                                             | Groups/weekly templates, overlaps/effective versions, concurrency, role/isolation and downstream/deployed acceptance                                                                                                |
| Roster policy                   | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Staff/template assignments, conflicts, effective versions and inactive/transferred staff                                                                                                                            |
| Rosters                         | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Populate/allocate, overlaps, credentials/leave, freeze/correct/discard, overnight dates and downstream consequences                                                                                                 |
| Leave setup                     | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Types/calendars/structures, units/rules, effective revisions and retirement                                                                                                                                         |
| Leave balances                  | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Allocation/accrual/carry/expiry, monthly posting, correction/close, ledger and self-only reads                                                                                                                      |
| Leave requests and review       | Partial                                     | Scoped queue/detail/decision flows, self-review denial, cancellation/revocation/replay and current-authority races; local independent UI QA                                                                                                                                  | Complete setup-to-balance-to-request-to-roster journey, all role/state boundaries and deployed positive acceptance                                                                                                  |
| Leave reports                   | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Filters/bounds/totals/pagination, absence/staleness and protected exports                                                                                                                                           |
| Duty exchanges                  | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Proposal/decision, holds/conflicts, cancellation/revocation and roster effects                                                                                                                                      |
| Overtime                        | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Policy/units, eligibility, independent decisions, reversal/history and payroll handoff; approval is not payment                                                                                                     |
| Attendance                      | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Punch/void/correction, independent decision, overnight dates, finalize/reopen, immutable timesheet and payroll handoff                                                                                              |
| Credentials and reminders       | Audit pending                               | Existing register/review/reminder implementation                                                                                                                                                                                                                             | Evidence/expiry/renewal, private disclosure, roster consequences and real worker retry/delivery acceptance; credentials do not confer clinical privilege                                                            |
| Payroll                         | Partial configuration                       | Salary-head create/edit/history, synthetic structure/assignment checks, missing-policy reporting and production restricted-access denial                                                                                                                                     | Reviewed jurisdictional policies; runs/calculation/approval/locking/posting, corrections/reconciliation, final settlement and professional branded Indian payslip PDFs with applicable tax/PF/ESI components        |
| Benefits                        | Audit pending                               | Existing implementation                                                                                                                                                                                                                                                      | Dependent/plan eligibility, independent decisions, overlap/evidence/revoke/expiry and protected exports                                                                                                             |
| Offer and exit documents        | Research/proposal                           | Generated-document research exists                                                                                                                                                                                                                                           | Reviewed templates, hospital logo/branding, versioned data capture, authorization/approval, protected Go PDF generation/download and correction/history                                                             |
| Cross-cutting UI and QA         | Partial                                     | Full-width Staff workspace, responsive layout checks, explicit access denial and latest full Go/frontend validation                                                                                                                                                          | Consistent spacing and accessible forms for every changed workflow; all required fields and edits; no stale subject/tenant data; all remaining journeys through the UI                                              |

## Access and RBAC: ordered completion checklist

IAM owns identity integration, tenant memberships, scoped assignments, generations and sessions. Workforce owns employment, postings and staff/login bindings. The identity provider owns the global login identity. HR changes in one hospital must not disable the same person's other authorized hospitals or tenants. Clinical privilege and physical access have separate owners.

| ID  | Journey                           | Required behavior and proof                                                                                                                                                                                                             | Status / dependencies                                                                                                                                                                                                                                                  |
| --- | --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A01 | Provision or resolve identity     | Authorized admin creates/resolves the correct IdP identity; verify contact state, duplicate/conflict behavior, retry after uncertain provider response and no orphaned success; no credentials in logs or Git                           | Open; reconcile #417/#1053 with current identity owner                                                                                                                                                                                                                 |
| A02 | Enroll tenant membership          | Existing identity, least-privilege staff-member role, subject/tenant pinning, duplicate retry and no business permission from membership alone                                                                                          | Partial; enrollment UI exists; wider recovery/removal acceptance open                                                                                                                                                                                                  |
| A03 | Bind staff to login               | Independent reviewed binding; reject/withdraw/revoke, one current link, self-review denial, immutable history and current actor/subject checks                                                                                          | Partial; local positive evidence; complete all lifecycle dependencies                                                                                                                                                                                                  |
| A04 | Assign scoped access              | Current manager proposes; distinct authorized reviewer approves/rejects; target membership, hospital and validity are rechecked; activation waits for current acknowledged projection; failure stays pending                            | Partial; delivered for leave and identity families, not all product permissions                                                                                                                                                                                        |
| A05 | Renew access                      | New bounded validity and explicit independent review; expired/withdrawn/rejected or old-generation authority never silently reopens; exact retry and future-start boundaries; before/after-expiry recipient UI QA                       | In progress: renewal preparation and local maker/checker/recipient QA implemented; complete CI and merge passed; deployment verified at e0a9e6a; full boundary acceptance remains open; reuse existing proposal/decision lifecycle rather than mutate accepted history |
| A06 | Security disable/recover identity | Old cookies, new login, renewal, private reads/downloads and commands deny when IdP identity is non-active; outage fails closed; restoration requires fresh authentication and current authority                                        | Partial; current-identity local regressions/UI passed; complete route inventory and deployed acceptance                                                                                                                                                                |
| A07 | Suspend hospital employment       | Explicit scope/reason/effective time and required review; affected employment-derived authority denies at cutoff despite stale session/FGA/cache; other hospitals/tenants remain available; resumption restores no historical authority | Open; employment transition/consumer contract required                                                                                                                                                                                                                 |
| A08 | Notice and resignation            | Submission does not itself end access; authorized acceptance/withdrawal, agreed last-working/cutoff time and accountable handover; no privilege growth through stale pending decisions                                                  | Open; employment transition/consumer contract required                                                                                                                                                                                                                 |
| A09 | Termination / contract end        | At effective cutoff deny old employment assignments, links, delegation and cards; retain clinical/payroll/audit history; durable cleanup and visible unresolved acknowledgments                                                         | Open; payroll/clinical/card/physical-access owner integrations required                                                                                                                                                                                                |
| A10 | Rehire / resume                   | New reviewed employment generation, current identity/evidence/link and new scoped decisions; no old grant/card/event replay can restore authority                                                                                       | Open; employment-period and consumer acceptance required                                                                                                                                                                                                               |
| A11 | Membership/access removal         | Distinguish one role, final tenant membership, hospital employment and global identity; final-role removal advances generation; reenrollment requires fresh proposals; preserve other scopes and accountable history                    | Partial backend generation guards; full admin/recipient UI and legacy-grant audit open                                                                                                                                                                                 |
| A12 | Safe deletion/retention           | Offboard first; independently authorized retention/privacy process; no deletion of completed financial/clinical/security history as an access shortcut                                                                                  | Open; policy applicability and owner contracts required                                                                                                                                                                                                                |

Every A-item must cover unauthorized actor, another tenant/hospital, self-review, stale version/generation, duplicate/uncertain retry, dependency outage, lock-wait race, persistence and attributable history where applicable. Record commands actually run and separate each environment.

### Active Access/RBAC completion work — 2026-10-30

PR [#1480](https://github.com/tesserix/helivanta/pull/1480) merged as
`7bbdc7c0c41150a01df0907d6b1f2929fdd7b486` after all five required checks passed
on exact head `4731df96cdfd58ebc1dd34ac7b562b2231e92634`: Go, web, browser E2E,
scripts and secret scan. Repository visibility was verified public before merge.
This is a merged-source checkpoint, not a production deployment or A01–A12
completion. The source admission and transaction-bound security-revocation
boundaries below remain unchanged.

The next local [incarnation graph implementation](../superpowers/specs/2026-10-30-membership-incarnation-graph.md)
now binds graph permission/membership proof to each immutable row, actual subject,
role, tenant and continuity epoch. It rereads source after provider checks and
uses the same adapter for genuine login, discovery, switching and renewal.
Membership events reconcile current source after the event claim commits;
network work no longer holds that claim transaction open. Boot reconciliation
uses the same tuple factory, keeps current incarnation tuples and retires old
ones. Hospital onboarding now uses this path as well.

The absent-membership delayed-grant and re-enrollment-borrowing regressions now
pass locally with real PostgreSQL/OpenFGA, including delayed old revocation that
preserves the replacement. A later local legacy-contract migration below addresses
the remaining raw subject/role DELETE regression.
Real provider/proxy tests reject incomplete, malformed, conflicting and failed
proofs instead of accepting partial authority. Focused signed-session, source
race, boot/prune, HTTP seam and approved permission-matrix checks pass. At `fee984e6`, the
fixed-source affected race/coverage run passed authorization, reconciliation,
architecture, bootstrap and API suites. IAM's only failing test is the retained
unsafe raw-DELETE replay regression. Vet, affected linter (zero issues), API and
Global API builds, and contractdocs generation/check passed. No new source is
merged or deployed and no A-item is newly complete.

The next local [legacy contract migration](../superpowers/specs/2026-10-30-legacy-membership-contract-migration.md)
refuses the unsafe raw DELETE with `409 membership_command_required` instead of
selecting whichever replacement row currently has the same subject/role. A new
real-PostgreSQL test failed with actual 202 versus expected 409 before the change,
then passed with the race detector. It verifies unchanged membership, no new
outbox event or fabricated receipt, absent-target parity and restricted-actor
denial. Existing positive test consumers now use exact-incarnation commands;
the original replacement-survival assertion is retained across actual removal,
recovery, raw DELETE replay and exact removal-command replay. The final affected
race/coverage run passed IAM (81.8%), login client (90.7%), Global API (86.7%),
journeys, architecture and bootstrap (94.1%). These are local results, not full
A01–A12 or release acceptance.

The first run passed Global API and membership journey suites, but IAM did not
compile because of a missing test import. The next full run found a dynamically
constructed concurrent legacy DELETE still in the projection test; it was
migrated to frozen commands without removing the concurrent epoch/history
assertions. The focused group then passed (92.748 seconds), followed by the final
full run (IAM 319.211 seconds). Vet,
affected lint (zero issues), API/Global API builds, contractdocs generation/check
and the canonical staff-access generator check passed. No web code changed in
this increment. Breaking-contract/security/consumer review, the unresolved
external sunset decision, normal-role browser acceptance and production gates
remain open. No production or application database was changed.

Final local commands (from `backend/`; disposable test services only):

```sh
DOCKER_HOST=unix:///Users/samyakrout/.colima/helivanta-staff-withdrawal-20261030/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=15m -p=2 -parallel=4 \
  ./internal/modules/iam/... ./internal/globalapi/... ./internal/journey/... \
  ./internal/archtest/... ./internal/bootstrap/...
mise exec go@1.27.3 -- go vet -p=2 ./internal/modules/iam/... ./internal/globalapi/... ./internal/journey/...
mise exec go@1.27.3 -- golangci-lint run ./internal/modules/iam/... ./internal/globalapi/... ./internal/journey/...
mise exec go@1.27.3 -- go build -p=2 ./cmd/api ./cmd/global-api
mise exec go@1.27.3 -- go run ./cmd/contractdocs
mise exec go@1.27.3 -- go run ./cmd/contractdocs -check
```

From the worktree root, `mise exec node@24 go@1.27.3 -- node
scripts/generate-staff-access.mjs --check`, changed-document Prettier and
`git diff --check` passed. The staged Gitleaks scan found no leaks.
The authorization latency SLO and new-workflow browser acceptance remain
unproven. Existing SQL 0015–0017 remains persisted and unapplied to application
or production databases; the legacy refusal adds no migration.

The next local [identity eligibility follow-up](../superpowers/specs/2026-10-30-membership-identity-eligibility.md)
replaces recovery's active-state-only read with one matching active-human and
verified-email proof. The real PostgreSQL command regression first returned 202
for an active unverified human where 409 was required, then passed after the gate.
Provider HTTP tests cover matching, unverified, machine, missing/inactive,
unknown/ambiguous/malformed evidence, outages, subject mismatch, renamed or
conflicting fields and bounded complete responses. Restoring the old decoder
made the second-document and oversized-valid-prefix assertions fail; restoring
the fix made the full client suite pass. Focused recovery/bootstrap tests pass,
including receipt replay without another provider read or duplicate outbox event.

Read-only execution of the new reader against the existing local synthetic Admin
and reviewer returned eligible for both, without displaying private identities
or credentials. This is client/provider compatibility evidence, not a new user
verification journey or browser acceptance. The final affected race/coverage run
passed IAM (81.8%, 317.769 seconds), login client (91.3%), bootstrap (94.1%),
architecture, API construction (10.6%), Global API (86.7%) and journeys. Vet,
affected lint (zero issues), API/Global API builds, contractdocs generation/check,
canonical staff-access check, formatting/diff and staged secret scan also passed.
No schema, route, event or permission is added by this follow-up. No web code
changed; normal-role browser and deployed acceptance remain pending.

The identity follow-up's final test command (from `backend/`, using the same
isolated Docker environment shown above) was:

```sh
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=15m -p=2 -parallel=4 \
  ./internal/modules/iam/... ./internal/bootstrap/... ./internal/archtest/... \
  ./cmd/api/... ./internal/globalapi/... ./internal/journey/...
```

Its affected vet/lint scopes were `./internal/modules/iam/...`,
`./internal/bootstrap/...` and `./cmd/api/...`; build, contractdocs and canonical
check commands were unchanged. Local runtime discovery confirmed Zitadel
`v4.15.3` through its running image and version command, without changing Docker
context or credentials. Production provider version and provisioning capability
remain unverified.

The next local [first-enrollment candidate](../superpowers/specs/2026-10-30-first-membership-enrollment.md)
adds an account/workspace-pinned, reasoned command for an existing verified human
with no previous tenant membership. It fixes the source role to `staff_member`,
retains trigger-generated immutable receipts and protected history, and refuses
to reopen ended continuity. The Staff form now requires reason/acknowledgement,
freezes uncertain ID/body and shows history; source persistence is distinguished
from graph readiness and operational access. It does not create IdP users or
migrate the legacy privileged POST.

The initial HTTP regression failed 404 versus expected 202 before implementation.
Focused race tests now pass for input/actor/workspace boundaries, exact/conflicting
replays, source history after removal, tenant isolation, direct receipt fabrication,
provider failure, transactional outbox rollback and concurrent exact commands.
A real PostgreSQL lock-wait test observes the writer blocked on the Admin row,
then withdraws that Admin before releasing the lock; the command returns 403 with
no membership, receipt or event. Global API coverage proves projection to ordinary
membership and no business permissions. Client/form/page tests pass (6 API tests,
26 form/page tests). Initial test-fixture DDL was correctly denied to the app role;
the outbox failure now uses the existing owner migration fixture instead.

Append-only migration `0018_iam_membership_enrollments` is persisted; IAM now has
18 migrations and the whole registry 176. Disposable fixture databases only have
been migrated; application/production state remains unchanged. The full affected
race/coverage run passed IAM (81.9%, 386.768 seconds), login client (91.3%),
platform (87.8%), bootstrap (94.1%), API construction (10.6%), Global API (86.7%)
and journeys. Its one failure was pagination architecture metadata for the
single-receipt GET; a specific reasoned entry declares its command-primary-key
lookup, while collection history stays ListRoute. The complete architecture suite
then passed (40.441 seconds), with zero lint issues. New-head CI/browser acceptance
is pending. The full web suite passed
(seven tasks, including API 412, UI 267 and shell 1,255 tests), API/shell/E2E
TypeScript checks, affected web lint, Go vet/lint (zero issues), API/Global API
builds, contractdocs generation/check and the shell production build passed.
The first build correctly rejected an unset production `API_URL`; rerun with the
local Global API origin passed. Type-checking caught an alias collision with the
Workforce-owned `StaffEnrollment` type; the unused IAM alias was removed rather
than redefining Workforce's export. The updated browser
E2E setup creates a random synthetic account only against localhost, stores its
credentials in an ignored mode-0600 JSON outside test artifacts and then uses
normal Admin and recipient UI. Its fixture-set verification flag is not evidence
of a recipient completing contact verification. The [historical-continuity
gate](staff-identity-provisioning-audit.md#historical-continuity-acceptance-gate)
remains open: a missing continuity record cannot prove absence of pre-retention
membership or historical owner authority. No A02/A11 acceptance follows from the
fresh-case tests. All five PR CI jobs passed at `78513985`; that baseline does not
include this uncommitted enrollment work.

Final first-enrollment commands (from `backend/`, same disposable Docker socket):

```sh
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=20m -p=2 -parallel=4 \
  ./internal/modules/iam/... ./internal/platform/... ./internal/bootstrap/... \
  ./internal/archtest/... ./cmd/api/... ./internal/globalapi/... ./internal/journey/...
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=10m -p=2 -parallel=4 ./internal/archtest/...
```

From the root: `pnpm exec turbo run test --concurrency=2`, affected API/shell/E2E
`type-check` and `lint`, `API_URL=http://127.0.0.1:8199 pnpm exec turbo run build
--filter=@helivanta/shell --concurrency=2` (Node 24/Go 1.27.3), canonical staff-access
check, changed-file formatting and diff checks passed. Go vet/build scopes and
contractdocs commands are as above; affected and architecture lint passed.
No current-head browser/deployed acceptance or latency SLO claim follows.

The enrollment candidate was committed and pushed as `549f2f93` in draft
[PR #1489](https://github.com/tesserix/helivanta/pull/1489). Its CI run
`41045911107` failed browser test discovery before executing any scenarios:
`import.meta` in the new fixture is incompatible with Playwright's CommonJS
loading. The exact local discovery command reproduced that syntax error. Using
`resolve(__dirname, ...)`, as the suite's existing environment loader does,
restored discovery of all three Staff access scenarios. E2E typecheck, lint and
format checks passed. The complete browser workflow and remaining live CI jobs
must still be checked; discovery alone is not enrollment acceptance. This
correction changes fixture path resolution only, with no runtime API or SQL
change. The command was `mise exec node@24 -- pnpm --filter e2e exec playwright
test tests/staff-access.spec.ts --list`; package checks used the
`@helivanta/e2e` filter with `type-check`, `lint` and `format:check`.

The [A01 provisioning audit](staff-identity-provisioning-audit.md) records source
and versioned primary provider evidence. The current login client exposes active-state and
presentation-profile reads, without a provisioning/contact-verification workflow.
The local seed script marks synthetic email verified during fixture creation;
it is not acceptable evidence for real user verification. The existing generic
membership POST neither verifies the provider identity nor enforces explicit
recovery after final removal; it also accepts privileged system roles. Those
paths still need the coordinated provisioning/recovery contract and current
actor/source guards before A01/A02/A11 can be accepted.

Migration `0017_iam_membership_projection_reader` grants only SELECT of IAM
continuity metadata to the existing system role for startup reconciliation.
Ordinary permission and role-discovery reads still use normal forced-RLS tenant
transactions. This migration is persisted in the source registry and has run
only in disposable fixtures. The IAM migration-count assertion now pins all 17
IDs. The local application database and production have not been migrated.

The follow-up branch has local membership-command implementation, canonical
Go/Zod models, additive Global API routes, reasoned immutable receipts/history,
fresh ordinary-membership recovery and Staff Admin controls. New SQL
`0015_iam_membership_commands` has run only in disposable test databases, not
the local application database or production. Concurrent-index migration
`0016_iam_membership_candidate_index` has also passed disposable migration/replay
tests; a real forced-RLS query plan uses the new ordering index with 100,000
synthetic neighbouring subjects. Focused real-PostgreSQL race tests
pass for exact-role removal, preserved other roles, final-removal epoch change,
new ordinary membership, old-command replay, source/tenant/role/epoch denials,
inactive/provider-outage recovery refusal and immutable transition evidence.
Focused shell/API tests pass for required reason/acknowledgement, frozen uncertain
retry and recovery from the current epoch. Broader tests and real UI acceptance
are still in progress. At the `e58511e7` checkpoint all three replay/projection
regressions were red; the graph follow-up above now fixes two. This work is not a
release candidate and no A-item is newly complete.

A third real PostgreSQL/OpenFGA regression originally reproduced re-enrollment borrowing
an old graph tuple: the original membership is projected through the registered
consumer, removed, and replaced by a distinct unprojected source row; current
admission incorrectly reported membership. Exact-incarnation graph proof is now
implemented locally and this assertion passes. Generation-bound owner eligibility
remains required; neither this graph proof nor the command/UI work proves
historical Staff-link/legacy-grant cutoffs after recovery.

A same-tenant account-change regression additionally reproduced 202 instead of
409 in the new command before account pinning. All three new routes now require
the verified expected tenant/subject pair, and the normal client sends both.
The current race regression rejects stale, absent or duplicated expected subject
without mutating the membership. The earlier full Go run passed architecture,
bootstrap, API and authz but failed the two known replay regressions and a stale
14-migration count assertion. That assertion was updated for real schema
additions, not removed; the current follow-up pins 17 migration IDs.

The next [membership removal/recovery design](../superpowers/specs/2026-10-30-membership-removal-and-recovery.md)
is source-verified and remains unaccepted. Two disposable local regressions
originally failed: repeating a subject/role DELETE after re-enrollment removes the replacement
membership, and a historical grant delivered through the real registered
consumer recreates graph membership when IAM has no membership. The first used
real PostgreSQL/NATS; the second additionally used real OpenFGA. No production or
local application memberships were changed. Current-source admission separately
denies stale graph authority; the later local incarnation proof fixes the delayed
grant/re-enrollment graph gaps. Command migration and owner recovery/cutoff gates
remain open. A reproduced defect is not acceptance evidence for a completed workflow.
The full objective remains A01–A12.

The user selected full Access/RBAC completion again, including every A01–A12
journey above. The [completion plan](../superpowers/plans/2026-10-30-staff-rbac-completion.md)
keeps that full objective and the owner/consumer dependencies explicit.

The first source-verified defect is stale graph authority after IAM role removal.
A real PostgreSQL/OpenFGA test with an existing signed session reproduced 200
instead of 403 after removal of its administrator role, while ordinary membership
remained. The [current-role admission candidate](../superpowers/specs/2026-10-30-current-tenant-role-authority.md)
now bounds graph permissions by current IAM roles and canonical registry grants,
with the same adapter wired to both production V1 admission gates. Graph and
source outages deny; graph-only or SQL-only authority cannot grant permission.
Final membership removal, late grant replay, another tenant's preserved authority,
authenticated self-service and removal during a delayed graph response have
focused passing tests. This adds no SQL migration or permission.

Local race/coverage passed IAM 82.1%, bootstrap 94.1% and authz 80.5%, including
the IAM login client and API construction tests. Vet, API/Global API builds,
affected lint and contractdocs check passed. A 100,001-row isolated fixture
verified the indexed bounded lookup; the final ordinary-RLS plan used a bitmap
index scan and reported 0.031 ms execution locally. This is not a measured
production latency SLO. The scale-test setup's initial admin-accessor use was
removed after the architecture guard rejected it. The final complete architecture
race suite passed, including every declared role against the independent
permission matrix, unsupported foreign graph authority and stale role removal.

The local candidate is healthy behind the local Global API. The first browser
attempt used an unregistered numeric-loopback callback. Read-only metadata from
the configured local identity provider identified the registered
`http://helivanta.localhost:4399` origin; using it restored the Staff access UI
without changing browser protections or identity-provider permissions. Existing
Admin-session reads pass. The exact clean `e047c56` API build then passed normal
sign-out/password sign-in for the synthetic Admin and existing pharmacist/reviewer
fixture through the local Global API. Admin enrollment/proposal controls remain
available, blank enrollment is rejected, the reviewer cannot administer assignments
or access payroll, and its existing hospital reader grant retains the six-row
read-only directory without edit/enrollment controls. No memberships, roles,
hospital grants or assignments were changed by this UI smoke test. This does not
prove independent employment approval or the unimplemented removal/recovery UI.
This candidate is not production-deployed. No A-item or broad Staff area is
marked complete. Incarnation-bound membership commands/UI, provisioning,
reviewed employment, legacy-grant classification and effective cross-owner
lifecycle cutoffs remain in the full plan. The broad counts remain 10 partial,
10 audit pending, 1 research and 0 fully accepted.

A second A06/A11 source finding concerned explicit global credential revocation:
the old handler trusted a stale graph target binding after that target left the
acting tenant. The [transaction-bound revocation candidate](../superpowers/specs/2026-10-30-current-credential-revocation-scope.md)
now requires current actor/target memberships and a fresh acting credential,
including after real credential-lock waits. Scoped target loss returns 404,
administrative role loss 403 and acting credential revocation 401; source outage
returns 503. The existing workspace precondition rejects stale requests before
external lookup. Real NATS observation verifies the unchanged global envelope;
other-tenant memberships remain intact. A failing real outbox INSERT rolls back
the watermark. Self and reciprocal administrator revocation are tested without
lock upgrades or deadlock. No new production migration is added. The prior
admission commit's five required CI checks passed. The extended candidate's full
IAM race/coverage (82.8%), architecture, bootstrap, API and authz suites passed,
as did affected vet/lint, API/Global API builds and contractdocs generation/check.
Its two membership queries used existing indexes in the 100,001-row forced-RLS
fixture; the locked actor/target query reported 0.080 ms local execution. This is
not a production SLO. The extended source commit's required CI passed before
PR #1480 merged; [post-merge CI](https://github.com/tesserix/helivanta/actions/runs/41014627993)
also passed on `7bbdc7c`. Production rollout, full A01–A12 workflow and deployed
acceptance remain open.

## Delivery evidence and next work

- Identity draft preparation: [PR #1433](https://github.com/tesserix/helivanta/pull/1433), source `935cb62955d592ec10a3f83ef87fd6700ad1a0a0`; [release](https://github.com/tesserix/helivanta/releases/tag/2026-10-29-staff-identity-drafts).
- Exact-tree complete CI: [run 40717063061](https://github.com/tesserix/helivanta/actions/runs/40717063061); full serial local Go race/coverage and all 30 frontend tasks passed. Local UI tested manager preparation, independent reviewer read-only access, all draft kinds, revisions, required fields, reload/fresh-login persistence and grant withdrawal.
- Production independently deployed descendant `d64c6824f954d61a1b63aebcb39e2f552cc3b9ba`, which contains the approved source. Six deployments reached 2/2 updated/ready/available; API, Global API and shell were Synced/Healthy. The succeeded migration job logged “migrations applied”; no per-migration database audit is claimed. Production restricted reviewer saw Staff hospital-scope and payroll denial. Positive draft workflow acceptance was local.
- Current priority: Staff profiles and supervision, following the user’s instruction. A05 renewal preparation uses the existing owner-controlled proposal lifecycle; source/contracts/issues reconciliation for A01–A12 remains open. Hospital employment suspension/exit/rehire must use the reviewed employment-generation design; a cosmetic status dropdown or global IdP disablement cannot substitute for it.

## Update discipline

For every change, update the affected row, exact issue/PR/commit, test commands/results, local UI actors and outcome, migration/release state and deployed verification. Record the next remaining acceptance item. Store private screenshots/credentials/evidence outside the public repository; link only non-sensitive public evidence here. Never promote Partial or Audit pending to Complete because an unrelated PR auto-closes an issue.

## Renewal preparation checkpoint — 2026-10-29

The Staff access page offers renewal only for an activated source assignment, including one past expiry. It creates a new proposal for the same account/hospital/template, requires fresh dates/reason, and includes the prior ID/version as a human reference. This is not a server-enforced predecessor relation or authority. It preserves the previous proposal, uses the existing independent review/current membership/projection checks, and retains the same new proposal ID/payload on uncertain retries. Review-only accounts and rejected/withdrawn/unactivated records have no renewal shortcut.

Validation completed:

- Red: the new renewal tests failed because the action did not exist (21 passed, 2 failed). Green: all 24 Staff access component tests passed, including required dates/reason, new proposal identity, pinned scope, reason bound, exact uncertain retry and role/state boundaries.
- `API_URL=http://127.0.0.1:8199 pnpm turbo lint format:check type-check test build` on Node 24 passed all 30 tasks: 1,125 shell, 404 API, 263 UI, 72 configuration and 9 emergency-access tests. Final affected ESLint, shell type-check, 24-test rerun, Prettier and `git diff --check` passed after the final small reason-validation change. No Go, schema, permission or route changed; no Go test rerun is claimed for this UI-only slice.
- Through the real local UI and Global API, the existing synthetic proposer renewed an expired leave-review assignment. Blank dates/reason failed inline. The old assignment stayed expired at version 5. The new proposal reached independent review at version 2 without proposer approval controls. A separate synthetic approver approved it; history retained versions 1–5 through activated state. A fresh recipient login could discover the synthetic hospital and load the empty review queue. The proposer withdrew the temporary new assignment at version 6, preserving history 1–6 and the independent approval/withdrawal records. A fresh recipient login after withdrawal showed “No hospitals are currently available for leave review”; a full reload retained that denial. No temporary renewal authority remains.

Renewal preparation merged in [PR #1442](https://github.com/tesserix/helivanta/pull/1442) as `4a6c6528d65a4a10a3fefcf1e5783884205c1518` after every CI job passed, including full Go coverage and end-to-end tests ([run 40787060958](https://github.com/tesserix/helivanta/actions/runs/40787060958)). Deployment and production positive renewal acceptance remain open. Wider A01–A12 acceptance remains open, including future validity boundaries, generation changes, dependency failures and employment termination/rehire across consumers. Private screenshots and synthetic credentials remain outside this public repository.

## Membership-removal request guard and provisioning audit — 2026-10-29

A11 audit reproduced removal ignoring the expected-workspace header: six invalid cases returned 202. The owner handler now uses the same optional tenant precondition as enrollment. Real PostgreSQL/NATS HTTP tests verify changed, empty, malformed, zero, repeated and comma-joined values return `409 session_changed` with no-store and preserve the exact membership list. A matching header still allows removal; headerless replay retains the existing 202 tuple-repair contract. Permission, verified tenant and RLS checks remain authoritative. No removal UI, actor/membership-incarnation command, migration or hospital offboarding is added; full A11 remains open.

Validation: focused membership tests passed after the failing regression; full `go test -race -cover -count=1 -timeout=20m ./internal/modules/iam/... ./pkg/authn/...` passed with IAM 81.6%, loginclient 90.7% and authn 97.6%. Affected vet, API/Global API build, contractdocs check, GolangCI-Lint (0 issues), Prettier and whitespace checks passed. The main update added no backend code, so these owner-package results still apply after integrating the merged renewal UI. CI, merge and deployment of this guard are separate outstanding gates until recorded below. These are HTTP/database tests, not production or a new admin removal UI walkthrough.

A01 source reconciliation confirms `memberHandlers.grant` records a supplied subject/role and publishes the existing member event; it does not create a login identity. `NewOnboardingHandler` is workload-authenticated hospital/initial-administrator binding and must not be reused as an interactive Staff identity-creation endpoint. Organization identity proof and current-user state checks are separate capabilities. The joiner workflow still needs the identity owner's published provisioning/resolve/contact-verification contract, uncertain-provider reconciliation and explicit Staff Admin UI. Email verification remains separate from staff identity/background review. Do not expose platform bootstrap credentials or globally deactivate a person as a substitute for one hospital's employment action.

## Released access checkpoint and profiles priority — 2026-10-29

Renewal preparation and the membership workspace guard are deployed as
`e0a9e6af4a56ccb796a82e06914ef654490b030b`:
[release](https://github.com/tesserix/helivanta/releases/tag/2026-10-29-staff-access-renewal),
[complete CI](https://github.com/tesserix/helivanta/actions/runs/40791850824).
The approved Kargo promotion and migration job succeeded, all six deployments
reached 2/2 readiness, and API/Global API/shell were Healthy/Synced. Production
owner Staff/payroll configuration reads and fresh restricted-reviewer denials
passed. Positive maker/checker/recipient renewal acceptance remains local; full
Access/RBAC and A01–A12 are not complete.

The user selected Staff profiles and supervision next. The slice in [PR #1447](https://github.com/tesserix/helivanta/pull/1447)
binds protected enrollment to the opening account/workspace, rejects
older/incomplete API acknowledgments and wrong Staff responses, clears private
forms after account/access loss, and labels inactive supervisor choices. No new
Staff migration, role, endpoint or table is introduced. See the
[scope and remaining gates](../superpowers/specs/2026-10-29-staff-profile-supervision-qa.md).
API context regression and compatibility tests pass; full gates and real local
supervisor reassignment/clear/cycle/stale-version QA are in progress. No new
production approval or area completion is claimed.

Transfers remain a separate unimplemented posting workflow: editing a department
classification is not approval, an effective-dated transfer, retained posting
history, handover or access cutoff. Safe removal retains the employment, clinical,
payroll and audit history; the existing inactive flag does not complete suspension,
resignation, termination or rehire. Those owner/consumer contracts remain open.

## Local profile/supervision QA checkpoint — 2026-10-29

Synthetic UI evidence on the current branch through the local Global API:

- Existing restricted reviewer: directory reads and identity-detail affordance;
  no Add/Edit controls. Synthetic Staff Admin: clear the existing supervisor,
  reload and reopen; assign a new synthetic supervisor, reload and reopen; replace
  with the original supervisor. Self is absent from choices.
- An attempted reciprocal link was rejected as a circular reporting chain; the
  unsaved draft remained visible and reopening confirmed no link was saved.
- The newly created disposable supervisor was marked inactive through the profile
  UI. Its retained directory record and explicit inactive choice label persisted.
  No staff record, global identity or business history was deleted. This is not
  full lifecycle/offboarding acceptance.
- Two tabs edited the disposable profile. The later stale save was refused, its
  exact draft remained visible, and the explicit discard/reload confirmation
  loaded the newer saved value. The temporary preferred-name value was then
  cleared through the UI. The original employee's supervisor was restored.
- New UI against the older e0a9 owner kept protected editing unavailable. A
  loopback-only method-routing fixture then sent the acknowledged GET to the new
  owner and the PUT to e0a9. The strict old decoder rejected the body marker;
  read-only database captures confirmed identical profile, version and write-audit
  count. Temporary old API/proxy processes were stopped and the gateway restored.

Private screenshots, exact synthetic state captures and local credential JSON
remain under the existing private evidence directory, outside Git. No production
personnel changes or new production rollout occurred. Issue #1035 and parent #979
remain open: imports, employee/consultant-specific completeness, full postings,
related-role coverage and safe offboarding/removal require further acceptance.

The account-bound profile slice is tracked in [PR #1447](https://github.com/tesserix/helivanta/pull/1447).
Validation before merge includes the full Workforce/Authn race suite (922 seconds),
final enrollment/master/supervision race regressions (90 seconds), architecture
checks, affected vet/build/GolangCI-Lint (0 issues), generator/contractdocs checks,
script tests and a clean staged secret scan. All 30 frontend tasks passed before
integrating main's later Pharmacy PRs. The integrated run then exceeded the test
runner's five-second budget in the existing three-save enrollment journey; its
assertions passed in isolation. That journey now uses the repository's bounded
15-second long-form test allowance, with all validation/payload/retry assertions
retained. The final integrated `API_URL=http://127.0.0.1:8199 pnpm turbo lint format:check type-check test build --concurrency=2` rerun passed all 30 tasks, including 1,157 shell tests. Final-commit CI remains a merge gate. This checkpoint is not production acceptance or full Staff-area completion.

## Production profile/supervision checkpoint — 2026-10-29

[PR #1447](https://github.com/tesserix/helivanta/pull/1447) is deployed as
`cf8ae1cfb14157fa2cd3a45b7da01151a224177a`. The user approved this named rollout
and inherited migrations. Kargo promotion
`prod.01m62wkctnk5tmp7vaf2n8awfg.d866a0e` succeeded for freight
`d866a0e290e93782f6b4e43c6db5f548c1e6cee5`. All six application deployments
reached two updated/ready/available replicas on that revision; API, Global API
and shell were Synced/Healthy. CNPG retained two ready instances.

The matching migration job succeeded. Read-only production ledger checks confirm
`0014_patientmaster` and `0025_pharmacy` were applied at 03:52:58 UTC; the new
link, lookup-access and registered-patient tables have enabled and forced RLS.
There is no Staff migration in this slice. The image publication retry recovered
a GitHub OIDC network timeout without bypassing signing. Both the
[final PR CI](https://github.com/tesserix/helivanta/actions/runs/40809708956) and
[merged-source CI](https://github.com/tesserix/helivanta/actions/runs/40812672797)
passed every CI job; the tested and merged code trees are identical.

Production UI QA used the designated synthetic hospital and existing accounts:

- A fresh restricted-reviewer session had no hospital Staff access or write controls.
- The Staff Admin's initially selected unrelated tenant had no hospital access;
  explicitly selecting the designated synthetic tenant exposed the authorized directory.
- Blank enrollment refused missing full name and employee code. Two clearly named
  synthetic records were created through the UI. A supervisor assignment persisted
  after full reload; reciprocal supervision was refused and reopening confirmed it
  was not saved. No existing staff record was edited.
- A second tab saved a concurrent profile change. The first tab's stale save was
  refused and its draft remained visible; explicit discard/reload loaded the saved
  revision. Temporary test text and the synthetic display name were restored.
- Changing tenant removed the private form and save controls. Returning and reopening
  confirmed that the unsaved draft had not been persisted.
- Test supervision was cleared and both new records were made inactive through
  the UI. Reload retained both records, and inactive supervisor choices were labeled.
  Read-only DB verification confirmed exact synthetic names/versions, inactive status,
  SQL NULL supervisor IDs, empty temporary profile fields and attributable write audits.

The fixtures have no login grants created by this QA and remain retained for
future verification. Private credentials, screenshots and exact identifiers stay
outside Git. See the [release evidence](../deployment/staff-profile-supervision-release.md).
This completes the released context/supervision slice, not full Staff profiles,
Access/RBAC, transfers, employment lifecycle or safe offboarding acceptance.
The next transfer dependency is the platform department owner in ADR-0044, then
Workforce-owned effective-dated postings and acknowledged access cutoffs.

## Lifecycle continuation checkpoint — 2026-10-29

The safe basic-editing prerequisite now has owner HTTP/database, concurrency,
replay, independent link-review and real two-tab local UI evidence. It binds the
opening account and revision, retains conflicted drafts, and refreshes the form
and table only after confirmed discard. This adds no employment state machine,
new Staff migration or automatic authority restoration.

The owner accepted [ADR-0049](../adr/0049-reviewed-employment-access.md): hospital
operational access requires reviewed Staff/employment links; legacy grants must
be reviewed before enforcement; governance/recovery is separate. This is a new
lifecycle policy decision, distinct from ORG-D6 (tenant-defined permissions).

The [delivery plan](../superpowers/plans/2026-10-29-staff-employment-lifecycle.md)
explicitly includes individual and agency contractors, contract expiry/extension,
suspension/resumption, notice/resignation, termination and rehire, plus dependent
handover, card, leave/attendance/payroll and settlement gates. All remain open
until their real owner/consumer journeys pass. The quantitative area statuses
remain 8 partial, 12 audit pending, 1 research and 0 fully accepted; no overall
coding-effort completion percentage is inferred from these counts.

Safe basic editing and accepted employment-link migration policy merged in
[PR #1454](https://github.com/tesserix/helivanta/pull/1454) as
`1be494b0d6def7341ec28c10d4c3596cbe7afc9b` after all required CI checks passed
([run 40832171906](https://github.com/tesserix/helivanta/actions/runs/40832171906)).
The change has local/CI evidence; it is not a new production rollout.
The [next employment-period design](../superpowers/specs/2026-10-29-reviewed-employment-periods.md)
keeps contractor/agency relationships, immutable renewal history, future cutoff,
no-login workers and governance/recovery boundaries explicit. Those are design
requirements, not newly implemented lifecycle endpoints.

## Employment-term intake continuation — 2026-10-29

The next implementation is a separately gated private draft-preparation slice.
It adds fixed/ongoing terms, individual/agency contractor classification,
contracting party, active hospital sponsor, appointment/contract reference and
attributable immutable revisions. Existing profiles are not silently migrated
or treated as reviewed employment. No login, operational access, payroll, card,
suspension, contract extension or exit is activated by a draft.

Local Admin UI evidence: incomplete intake refused; agency-worker fixed terms,
named contracting party and active named sponsor saved as a draft while the
existing synthetic Staff member remained inactive. Automated owner tests cover
exact replay after edit, stale source/version, required terms, foreign/reader
denial, private immutable history and no directory-state side effect. Client
tests cover opening-workspace headers, scoped receipt validation and the same
command after uncertain save. Full package/build/CI/release and remaining local
UI gates must be recorded before marking this slice accepted.

Independent immutable submission/approval, protected evidence binding, future
cutoffs, consumer inventory/migration and all A07–A10 lifecycle journeys remain
open. The 21-area acceptance count remains unchanged.

## Approved production editing release — 2026-10-29

PR #1456 merged as `5fb03897dd0c2d7afca580c44edc21b987909b52`; its tree is
identical to the fully passing PR CI run 40842867894. Image publication run
40846568679 passed. The user explicitly approved this exact combined revision
and all five inherited migrations (`0001_billing`, `0007_medicore`,
`0015_patientmaster`, `0026_pharmacy`, `0027_pharmacy`).
Kargo promotion `prod.01m63et279fjtcw6kgrff5jvcd.48423ff` succeeded. All six
application deployments reached two updated/ready/available replicas on the
exact revision; API, Global API and shell are Synced/Healthy. The migration job
succeeded, read-only checks found all five migration IDs, and all 12 inspected
new/projection tables had enabled and forced RLS. New employment-draft migration
0050 was explicitly excluded and is absent in production.

Production UI smoke used the existing Staff Admin and designated synthetic
hospital: directory access, protected synthetic profile edit/save/reopen, then
restoration and reopening of the original blank preferred-name value. The Staff
record remained inactive. No production employment, membership, role or login
grant was changed. See the [release record](https://github.com/tesserix/helivanta/releases/tag/2026-10-29-staff-profile-editing).

The separate employment draft slice passed all 30 Node 24 web gates: lint,
formatting, typechecking, tests and builds. Tests included 1,191 shell, 406 API,
263 UI, 72 configuration and nine emergency-access tests. Local UI verified
fixed agency terms, named sponsor, save, full reload persistence, edit to
revision 2 and retention of revision 1. A reproduced JSON-null start-time
validation defect was fixed in the canonical input binding; focused owner
regression tests pass. Full final Go/CI and release gates remain separately
recorded before acceptance.

Final owner validation after the null-time fix: full Workforce race/coverage
passed again (83.8%, 535.017s). The full architecture run required classifying
the two singleton GET resources in its pagination register; collection/history
routes remain cursor-paginated. Contractdocs tests passed (80.7% coverage).

Local concurrency continuation: two Admin tabs opened the same draft. The second
saved revision 3; the first tab's stale save was refused while its text remained
visible. QA then reproduced a missing discard confirmation. The form now uses
the shell navigation guard, with voluntary Staff/hospital changes and hospital
creation routed through that guard. Forced account/access loss still clears
private data immediately. Regression tests verify close/keep-editing/discard and
switching Staff records. The real local UI confirmed the same behavior; saved
revision 3 was retained and only explicitly discarded unsaved text was removed.
All 30 Node 24 web gates passed again after this frontend-only fix. No backend,
SQL, permission or event changed after the final owner/architecture Go checks.

## Protected employment evidence candidate — 2026-10-29

The [implementation plan](../superpowers/plans/2026-10-29-employment-evidence.md)
and [owner contract](../api/workforce.md#protected-evidence-for-employment-terms-revisions)
cover the completed local slice. Database-backed owner HTTP tests verify pending
files, duplicate active files, strict command facts, stale terms/Staff sources,
receipt collisions, stale withdrawal, wrong Staff/hospital/tenant, either missing
permission, forced private RLS, tampering, revoked files and withdrawal during
storage retrieval. The source document upload/scan path uses real PostgreSQL;
external GCS/ClamAV protocols are faked in automated tests only.

Local browser QA used the existing synthetic hospital/contractor. Freshclam
updated stale local definitions from 28166 to 28168 without weakening the
48-hour rejection policy. A UI retry produced a Ready file. Binding revision 1
attached it to terms revision 3; the downloaded 179-byte PNG matched the original
SHA-256. A reasoned withdrawal produced binding revision 2 and removed download
controls, preserving both revisions. A full page reload/reopen retained the withdrawn binding and both history
revisions; private screenshot `local-employment-evidence-withdrawn.jpg` is retained
with the local fixture evidence. No production evidence or reviewer approval
is claimed; all 21 broad acceptance areas remain open. Issues #118 and #1399
remain open, as do related lifecycle, payroll and identity acceptance gates.

Commands run in this candidate include `go test -race -cover -count=1` for
Workforce/architecture/contractdocs; the additional document/evidence race suite;
`go vet`, API/worker builds, `golangci-lint`; contractdocs generation/check;
canonical employment contract generation/check; and
`API_URL=http://127.0.0.1:8199 mise exec node@24 -- pnpm turbo lint format:check type-check test build`.

## Denied payroll page heading — 2026-10-30

Production reviewer QA on release `22b6241` found that a denied payroll page
incorrectly displayed “Staff directory” in the shell header. Permission-filtered
navigation had hidden the payroll entry before the shell selected its title.
The correction selects requested-page metadata from the route catalogue while
retaining permission-filtered navigation and the existing payroll denial.

The regression first failed with expected “Payroll setup”, received “Staff
directory”; after the correction all 50 shell navigation tests passed. Local
browser QA used the existing restricted `pharmacist Seed` fixture through normal
sign-in: the heading was “Payroll setup”, payroll navigation was absent, the
Staff directory link had no current-page marker, and the page denied payroll
access. This local fixture is separate from the production Staff reviewer.
No roles, grants, credentials, backend contracts or migrations changed in this
UI fix. It has not yet been rolled out to production. The broad acceptance counts
above are unchanged; a corrected denial heading does not complete a Staff domain.

## Employment draft withdrawal continuation — 2026-10-30

The denied-page heading fix merged as `6cd135d5ff83a3b26558b58dbe7a92deaba03fc8`
in [PR #1462](https://github.com/tesserix/helivanta/pull/1462), after Go, web,
browser E2E, security and image checks passed. Production remains on `22b6241`;
this merge has not been promoted.

The [withdrawal design](../superpowers/specs/2026-10-30-employment-draft-withdrawal.md)
and [owner contract](../api/workforce.md#withdrawal-of-prepared-employment-terms)
add a reasoned terminal state for employment preparation. A current hospital
manager can close a draft, retain its original terms and history, and recover an
exact command receipt. A closed draft cannot be edited, reopened or receive new
evidence. Legacy reads stay draft-only; the new UI explicitly requests the
withdrawal lifecycle projection. This is not employment termination or approval.

Local UI QA used the existing synthetic contractor and normal administrator
login. It created a separate fixed agency-worker draft with a named contracting
party, active hospital sponsor and required dates/reference/reason, verified the
blank withdrawal reason was blocked, then withdrew it. A full browser reload and
reopening showed terminal revision 2 and both history revisions. The Staff record
remained inactive; the previously existing contractor draft was unchanged. A
separate restricted local login saw “No hospital access yet”, with no employment
terms or withdrawal controls. No role or hospital grant was added for this QA.

Read-only local fixture verification found status `withdrawn`, version 2, two
retained revisions and exactly one attributable withdrawal audit. That
administrative verification is persistence evidence, not a runtime authorization
claim. Real-owner tests separately cover missing permission/foreign scope,
required fields, source/sponsor drift, stale/reused/duplicate commands, immutable
terms and terminal database guards. Evidence integration tests reject attachments
to either old or terminal revisions after closure while retaining protected
historical downloads and denying an unauthorized reader.

Migration `0052_workforce_employment_withdrawal` is implemented locally; it has
not been approved or run in production. Full validation/release evidence is
recorded in [PR #1463](https://github.com/tesserix/helivanta/pull/1463). The initial parallel full backend run encountered
shared Docker dependency readiness/connection failures; the rerun uses bounded
package parallelism and its result is recorded in the PR; no test is skipped or acceptance exception granted.
The initial combined web run found a panel lint violation (corrected) and sign-in
test timeouts. The subsequent complete gate passed all 30 tasks, including
1,200 shell tests and 409 API tests; the final form-control regression also passed.
CI subsequently detected the newly published query-decoding advisory. Its
merged backport from PR #1466 and the additional actual-consumer regression are
documented in the [vulnerability runbook](../runbooks/vulnerability-management.md).
The branch uses the main-line backport and its reviewed version-specific audit
exception; its earlier duplicate ESM adaptation was removed.

CI also reproduced a test-fixture naming defect: a long identity-case test name
plus its iteration suffix exceeded OpenFGA's 64-character store-name limit.
The shared isolation helper now bounds an ASCII-readable prefix while retaining
its per-test stable, per-iteration unique suffix. Long/Unicode names and repeated
iterations have a regression test. This changes test identifiers, not production
role or identity behavior. The current checkout declares Go 1.27.3; full local
validation uses that version and the QA VM's direct Docker socket.

All broad acceptance counts remain 8 partial, 12 audit pending, 1 research and
0 fully accepted. The scoped HR intake role still needs the action/route and
transaction/disclosure guards described in the access/review continuation plan.
Independent employment/identity review, effective lifecycle enforcement, transfers,
photo/card issuance, operational scheduling and payroll/document issuance remain
open. This bounded draft transition does not complete those journeys.

## Shift definition audit and navigation correction — 2026-10-30

Local administrator UI QA in the designated synthetic hospital created an
unallocated night/locum shift, 22:00–06:00 (480 minutes, next day), with break,
grace and overtime configuration. Blank required fields, equal times, a break
as long as the shift and half-day OT above full-day OT were rejected. The code
became immutable after creation. Edit, retirement and reactivation retained
revisions 1–4 after reload. The restricted local fixture had no hospital access
or Add shift action. This is denial evidence, not independent approval.

The audit reproduced silent loss of unsaved text on Staff directory navigation.
The correction reuses the shell navigation guard for route changes, Cancel,
hospital selection and Add/Edit/history replacement; pending saves cannot be
interrupted. Component regressions were observed red before the fix. Eight
focused tests pass, including same-form replacement, focus return and pending
writes. Real local UI verified route and Cancel prompts, Keep editing retained
the exact text, and explicit Discard opened history with the saved revision.
Closing history restored focus to its opener. A final UI save retired the
unallocated synthetic shift as revision 5, without a false discard prompt;
reload retained all five revisions and original timing/rules. No roster,
attendance or payroll action was performed.

Validation: `pnpm turbo lint format:check type-check` passed 21 tasks;
`pnpm turbo test` passed all seven package tasks; `pnpm turbo build` passed both
build tasks; script tests passed 19/19, under Node 24. Private screenshots include
`local-shift-unsaved-navigation-guard.jpg` and
`local-shift-guard-retired-history.jpg`. No API, permission, schema or migration
changed. Related story #1036 and umbrella #118 remain open. Referenced-shift
retirement, multi-hospital/current-account scope, consumer and production QA are
still required. Shifts moves from Audit pending to Partial: **9 partial,
11 audit pending, 1 research, 0 fully accepted**. These are area counts, not an
effort-completion percentage. Earlier checkpoints retain their historical counts.

## Roster setup draft protection — local continuation 2026-10-30

The next UI audit confirmed that the retired synthetic shift was unavailable for
new area membership. Following the in-form shift link after entering an area
name then silently lost that input. No configuration was saved in reproducing
this defect. The correction registers the existing navigation guard and uses
GuardedLink. Voluntary kind/hospital changes, Cancel, Add/Edit/history replacement
now share the same guard; confirmed replacements reset their form session and
pending writes retain their original context.

Regressions for Cancel, kind change, history, same-new-form replacement and the
shift link were observed failing before correction. All 11 roster component
tests then passed, including hospital scope, same-record replacement, pending
save and successful close/focus behavior. Real local UI verified the shift-link
prompt, exact name preservation on Keep editing, retained Roster areas selection
after cancelling a kind change, and explicit Cancel/Discard with opener focus.
Private screenshot: `local-roster-setup-unsaved-guard.jpg`.

Roster setup remains Audit pending. Saved area/group/template composition,
effective revisions, referenced retirement, multi-hospital isolation, downstream
roster behavior and deployed QA are still open. The count remains 9 partial,
11 audit pending, 1 research and 0 fully accepted. No API, schema, permission or
migration changes are introduced; no production or roster/personnel data was
changed by this continuation. Full final web/CI gates are recorded in the PR
before merge.

### Roster area reference and retirement QA — 2026-10-30

Using normal local Staff Admin controls, QA created a separate unallocated day
shift (08:00–16:00, 480 minutes) and roster area effective 2026-10-30. Missing
name and missing review acknowledgement were rejected. The saved area retained
its selected shift after full reload. Retiring that shift as revision 2 preserved
the area's original shift revision/timings, visibly labelled Earlier revision
retained, while removing it from new selections. Changing the area's effective
date to 2026-10-31 was rejected with input preserved. Restoring the original date
and retiring the area saved revision 2. A subsequent attempted reactivation with
the retired shift was rejected; QA explicitly discarded the unsaved attempt.
Reload/history retained both area revisions and pinned 08:00–16:00 membership.
The synthetic day shift and area finish retired; no Staff was allocated.

This provides local area-level evidence, not all roster setup acceptance. Roster
setup now becomes Partial, bringing the current count to **10 partial (47.6%),
10 audit pending (47.6%), 1 research (4.8%), 0 fully accepted**. Group/template
composition, weekly off/overnight overlaps, effective-version concurrency,
role/isolation, downstream and production QA remain open. Historical checkpoints
above keep their earlier counts. Private evidence:
`local-roster-area-retired-reference-history.jpg`.

## Shift groups and weekly-template layout QA — 2026-10-30

Local normal Staff Admin UI continued the same synthetic hospital workflow. An
empty shift group was rejected, then a current day-shift revision was selected
and the group saved. A weekly working day without area/shift selections was
rejected. Selecting the same shift twice on Monday was rejected before any
save. Weekly off/on preserved the entered choices. A corrected Monday-only
pattern saved with Tuesday–Sunday off; full reload/reopen and immutable history
retained its choices. Group and template retirement each saved revision 2;
subsequent history reads retained both revisions and original timings. The
area was reactivated only after replacing its retired reference with a current
shift revision. At the end, group/template are retired at revision 2, area at
revision 4, day shift at revision 4 and night shift at revision 5. No Staff
assignment, dated roster, attendance or payroll action was performed.

The walkthrough exposed an actual layout obstruction with long area/shift
labels: Monday's fieldset expanded into Tuesday's grid track, and a browser
hit test at Remove's center returned Tuesday's fieldset. The functional component
remove/correct/resubmit test passed before the layout fix, so it is supplementary
behavior coverage, not claimed as a failing layout regression. The production
code correction is one line: constrain the weekday fieldset's minimum width.
After loading the updated source, the visible button's center hit Monday's
Remove button, clicking removed only slot two, and the corrected pattern saved.
At 1280 px, weekday cards did not overlap and no button extended outside its
fieldset. At 768 px, cards did not overlap, no control overflowed and the page
fit the viewport. The viewport override was reset.

Private evidence includes `local-weekly-remove-obstructed-before.jpg`,
`local-weekly-remove-layout-fixed.jpg`, `local-weekly-layout-1280.jpg`,
`local-weekly-template-retired-history.jpg` and
`local-shift-group-retired-history.jpg`. The restricted local fixture separately
had no hospital roster configuration or Add controls after fresh normal login;
that is denial evidence, not an independent approval.

The count remains **10 partial, 10 audit pending, 1 research, 0 fully accepted**.
Different-shift overlaps, overnight/weekly-off spill, effective-date concurrency,
full multi-hospital/current-actor isolation, downstream assignment/population and
production QA remain open. No API, role, schema, dependency or migration changes
are introduced by this layout correction. Final package/web/CI evidence belongs
in the associated PR before merge; no additional domain completion is claimed.

### 2026-11-01 operational activation merge and scanner follow-up

- Activation PR #1510 merged as `0e2ca62f4d1c9bab57346be5d7e2e2d8d20b4673` after Go, web, e2e, scripts, secret scan and image checks passed. Activation remains default-off; merging is not production acceptance.
- A fresh Staff Admin UI document upload exposed a mismatch: scanner policy accepts definitions up to 48 hours old, but Workforce's SQL trigger still enforced 24 hours. The real worker rolled back release with SQLSTATE 23514; protected files remained unavailable.
- The regression reproduced the exact error against real PostgreSQL. Forward migration `0067_workforce_document_scanner_freshness` preserves the complete latest recovery trigger and changes only the age bound to the documented 48-hour policy (#1242). Historical migrations remain unchanged. The first focused rerun passed.
- Candidate production scope is now four migrations, ledger 197 → 201. Production rollout requires approval for the exact combined revision and named migrations. No production mutation or activation enablement performed.
- Full affected tests and local browser recovery remain in progress. Overall acceptance remains 10 partial, 10 awaiting audit, 1 research; no full operational activation acceptance claimed.

- Scanner local browser acceptance: fresh upload reached Ready using real ClamAV; normal UI downloads saved by Chrome match all 179 original bytes. Existing local DB upgraded through owner startup to ledger 201. Owner/worker readiness 200, activation false. See [scanner acceptance](2026-11-01-workforce-scanner-freshness.md).
- Scanner validation passed: focused `go test -race -count=1 -run '^TestDocumentProcessor(UsesPublishedScannerFreshnessWindow|RefusesStaleOrFutureScannerDefinitions)$' -timeout=5m ./internal/modules/workforce`, `go build ./...`, scoped `go vet`, scoped `golangci-lint run` (0 issues), contractdocs generation/check, all 23 canonical generators and all 75 script tests under Node 24. The earlier complete suite used superseded test setup and was interrupted; final full Workforce/attachments race rerun is pending. Initial dependency/setup and SQL test-fixture failures are retained in private evidence, not counted as passes.

- Final scanner package verification passed: `go test -race -count=1 -timeout=20m ./internal/modules/workforce ./pkg/attachments` — Workforce 547.734s; attachments 3.191s. The corrected source/test revision completed without failures. The migration and generated registry match the persisted manifest.
- PR #1511 initially failed the web inventory gate because the registered migration changed the pinned `workforce/module.go` hash. A byte-for-byte comparison confirms only the SQL embed and migration registration changed. Ten source pins were refreshed; all 15 inventory tests and normal parity passed. Counts remain 727 surfaces, 17 classified, 710 unclassified, zero complete cutoff acceptances.
- Additional normal-UI source QA created inactive applicant SYN-FULL-UI-20261101, saved fixed-term terms, uploaded separate identity/employment files through the real scanner, bound employment evidence to revision 1 and submitted the person finding. Separate reviewer sign-in and scoped detail opening passed. The reviewer download returned without an application error, but no saved copy was verified; this new finding remains pending and this additional source path is not fully accepted. Browser policy blocked download-history inspection; no workaround was attempted.

- Final scanner recovery acceptance: both earlier failed UI uploads became eligible after the existing 45-minute grace period and recovered via normal Staff Admin Retry scan. Refresh status shows both Ready; read-only Temporal descriptions retain Failed attempt 1 and Completed attempt 2 for both. No manual verdict, state rewrite or workflow reset was used. The separate newly prepared reviewer finding remains pending because saved evidence delivery was not verified; overall operational activation acceptance is still open.

### 2026-11-01 approved production activation/scanner foundation rollout

- Approved source `02b1e2b7` deployed through Kargo freight `f250c430aa344725859fa7270a03681e81aec3e4`; promotion succeeded and Argo is Synced/Healthy. All six deployments are 2/2; all twelve application pods match signed digests with zero application restarts.
- The Argo migration hook added exactly the four persisted migrations: ledger 197 → 201, no removals. All six new tables force RLS; scanner provenance is 48 hours with the existing retry guard retained. Activation remains off and employment generations remain zero.
- Production browser QA passed fresh reviewer and owner login, intended owner tenant selection, reviewer review/directory/payroll restrictions, and a real production Staff Admin upload → scanner Ready → exact 179-byte UI download → audited revocation on an existing synthetic Staff record. No new account, Staff fact or role grant was created.
- The earlier local reviewer saved artifact is also present and byte-identical after download completion. Its initial missing-file observation was premature; file delivery is now verified. Independent approval of that newly UI-created source case is the next local check.
- [Release evidence](../deployment/releases/2026-11-01-staff-activation-scanner.md) and the machine manifest preserve exact source, promotion, migration IDs/hashes and verification limits. Whole Staff and operational activation remain incomplete; no umbrella acceptance claim is made.

- Full normal-UI source acceptance now passed for SYN-FULL-UI-20261101: applicant creation, fixed-term draft, separate identity/employment uploads through the real scanner, exact revision evidence binding, person submission/independent approval, employment submission/independent approval, and reopened approved receipts. Both reviewer downloads match all 179 original bytes. Empty decision reasons are blocked. Staff remains Inactive after both approvals; no login or assignment was provisioned. No signed API business-source fixture was used for this case. Fresh-source activation and digital/lifecycle gates remain open.
- Published release: [Staff activation foundation and scanner recovery](https://github.com/tesserix/helivanta/releases/tag/2026-11-01-staff-activation-scanner), tagged at the exact deployed source `02b1e2b7`. The release explicitly retains default-off activation and does not claim complete Staff Management.

- Fresh fully UI-created non-digital activation also passed locally. A hospital-scoped `workforce.employment_activator.v1` fixture was separately proposed and independently approved through IAM, then withdrawn after the test. Business sources and the activation command used normal UI throughout. Missing activation reason/examination were blocked; valid activation returned a saved receipt; reopening reported already recorded with no second activation form. Staff changed to Active only after that explicit command and remained Active after owner restart with the feature restored off. Read-only persistence checks confirm Staff v2 / employment epoch 2, exactly one generation, zero login links, zero IAM employment bases and zero assignment bases for this non-digital case. Production activation remained off throughout. This closes the local fresh-source non-digital journey; digital recipient, governance/race and consumer/lifecycle gates remain open.

### 2026-11-04 leave separation of duties and scoped posting release record

- Local QA on tree `d465d104` (identical to merge `091f2287`) passed both leave separation-of-duties rules through the normal UI and the bound session API. The owner who filed a request got 403 on its approval while the independent leave reviewer approved it. A request filed on the reviewer's own Staff record never reached that reviewer's queue, returned 404 on review detail and 403 on both approve and reject, and stayed pending at version 1.
- Setup exposed a Leave balances gap: **Correct balance** is hidden until a staff member's first ledger entry, so an opening balance for a newly assigned structure needed the correction API. Filed as [#1537](https://github.com/tesserix/helivanta/issues/1537).
- [Release record](../deployment/releases/2026-11-04-scoped-posting-actors.md) for freight `a03f45e8` and its five migrations. The user approved the promotion on 2026-11-04; Promotion `prod.01m6gxvcqvx37wcqnavf92aq27.a03f45e` Succeeded, the ledger gained exactly those five IDs and every pod runs `091f2287`. Staff self-service filing (#1132) and reporting-line routing (#1536) remain open.

### 2026-11-04 scoped posting inbox candidate — not yet released

The reviewer can now discover public posting proposals by approved hospital via
the candidate owner API/SDK, without entering Staff UUIDs. The eight-item queue
returns canonical Staff code/name, public proposal metadata and an original-
submitter display hint. It excludes private rationales and identity evidence;
opening protected context still requires current independent review authority.

The local protection journey passed before the final RLS policy-composition fix;
complete corrected-migration suites remain in progress. SDK 633/typecheck/lint/
format, Go build/scoped vet/lint, full platform race and 15 inventory tests pass.
The first full owner run caught a real extra-permissive-policy error; it is fixed
in candidate 0076 and awaits complete rerun. Candidate 0077 persists its concurrent
queue index. No production migration or rollout occurred. See the [delivery
record](../superpowers/plans/2026-11-03-independent-staff-posting-review.md).

Reviewer UI/browser acceptance, effective supervision/approval, transfers,
contractor expiry, suspension, exit/rehire and departmental/Pharma consumers remain
open. Overall counts remain **10 partial, 10 awaiting audit, 1 research, 0 fully
accepted**. This is progress within a partial area, not a completion percentage.

Candidate validation update: corrected-migration Global API race 472.140s,
architecture race 65.636s and focused owner security 11.579s pass. The full owner
rerun passed in 679.419s after the private test-wrapper interruption. Read-only reviewer UI
now has four positive/refusal/account-switch component checks, and the UI library
passes 279 tests plus types/lint. The shell passes types/lint and a webpack build;
full shell tests and real-account browser acceptance remain pending. Shared local
ports 4399/8399 belong to another Leave candidate and were preserved. An isolated
posting runtime is still required. No rollout, migration or production activation
was performed; the overall 21-area counts are unchanged.

Full shell verification now passes **1,435 tests /177 files** (321.34s). This
covers source/component behavior only; real-account browser decisions and
operational/lifecycle acceptance remain open.

The complete corrected-migration Workforce race suite passed in **679.419s**;
four exact-session-owned fixtures were removed and no fallback SSH forwards were
needed. The representative inbox query-plan/index probe is the final pending
checkpoint check. Production and broad Staff acceptance remain unchanged.

The final real gateway protection/index-plan journey passed in **100.443s**.
The representative full-projection first-page `EXPLAIN (ANALYZE, BUFFERS)` uses
`workforce_posting_proposal_inbox`, returns three synthetic rows under a Limit
and records 2.124ms execution/5.003ms planning on this small fixture. Index
registration/column order are verified. This does not prove continuation plans,
production scale or an SLO. The full plan and test logs are retained privately in
`~/.helivanta/staff-management-20261027/posting-inbox-20261104/`.
All checkpoint checks now pass. Normal decision UI, an isolated real-account
browser journey and the wider supervision/operational/lifecycle work remain open;
this candidate remains draft and is not rolled out.

### 2026-11-04 reviewer terminal decisions — candidate only

Implemented independent rejection and original-submitter withdrawal in the normal
posting reviewer page, with current source observation, mandatory rationale/
attestation, retained-command recovery/exact retry, navigation protection and
private-view clearing on revoked admission. A missing receipt rechecks context
without discarding the command. Nine reviewer checks, full shell 1440 and SDK 633
pass alongside types/lint/format and a webpack build. The backend/contract hashes
match the already race-tested candidate; no new migration or operating approval
was added.

A separate local DB restores 1131 tenant-scoped QA rows over 151 required tables,
with FK/RLS checks, fresh audit history and independent NATS/OpenFGA. It preserves
the existing Leave runtime. A dedicated Temporal namespace and API readiness are
still being completed before real-account browser acceptance. Positive approval,
supervision, transfers, contractors, suspension, exit/rehire and departmental/Pharma
operating consumers remain open. Overall counts remain 10 partial, 10 awaiting
audit, 1 research, 0 fully accepted; the candidate stays draft and undeployed.
