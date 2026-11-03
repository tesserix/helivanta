# Independent Staff posting review delivery

Follow the [candidate specification](../specs/2026-11-03-independent-staff-posting-review.md)
and preserve the complete [parent plan](2026-11-02-effective-staff-postings.md).
This is one vertical feature branch, not permission to ship separate scaffolding.

- [x] Revalidate scoped preparation branch/CI and isolate the next feature from it.
- [x] Pin owners, actors, command paths, retry/disclosure/failure behavior and
      current-source/transaction boundaries in the candidate spec.
- [x] Observe public submission/decision scope assertions fail on absent routes.
- [ ] Pin the complete canonical submission/decision/source/receipt models and
      managerial supervision/bootstrap rules before implementing positive approval.
- [ ] Observe a positive real-IAM/PostgreSQL submission and independent review
      fail through Global API; keep real reviewed target/actor sources and exact
      independent assignments, without mocking owned repositories or eligibility.
- [ ] Implement append-only Workforce proposal/decision/command persistence,
      restrictive purposes, owner invariants and atomic audit/outbox.
- [ ] Compose current actor, target/generation, department and supervisor checks
      in the reviewed lock order through root commit; withhold response on failure.
- [x] Add closed versioned IAM submitter/reviewer catalogs and projection tests;
      old preparer/reader catalogs and historical SQL remain unchanged.
- [ ] Implement source/readiness, bounded inbox/history and original-key recovery
      in the canonical SDK and normal Staff UI on this same branch.
- [ ] Verify independent decision, contributor/subject exclusions, versions,
      duplicate/conflicting commands, foreign scope, contractor/interval bounds,
      supervisor cycles/cutoffs, changed/restored source and dependency failures.
- [ ] Prove both command/source orders and rollback with deterministic barriers;
      mutation-test critical predicates, restore exact bytes, run complete suites.
- [ ] Complete normal local Admin/submitter/reviewer/target/unrelated browser
      journeys through Global API with read-only persistence and protected evidence.
- [ ] Reconcile exact forward migrations against main, generated contracts and
      source-inventory evidence; complete types, lint, race suites and builds.
- [ ] Commit/push with origin-derived identity, complete exact-head CI/review,
      verify public visibility, and request the specific production rollout only
      after all code/migration/browser evidence is concrete and reviewable.
- [ ] Verify GitOps/schema/runtime and production synthetic QA after named approval.

## 2026-11-03 development evidence

Worktree: `helivanta-effective-posting-review-20261103`, based on scoped preparation
`0b27e927`. No application, production or migration change is made by the initial
spec/test work. `TestPostingReviewCommandsFailClosedWithoutCurrentOwners` exercises
the actual owner HTTP router for submission and decision commands. Both currently
return the absent-route 404 instead of 503 for unavailable current owners. The
owner-only harness deliberately composes no scope/structural/source provider;
403 would incorrectly imply the owners were available and denied authority.
This is RED evidence of missing endpoints, not an authorization bypass: neither
request disclosed a resource or performed a business write.

Command from `backend/`:

```sh
python3 /tmp/helivanta-posting-review-test-runner.py ./internal/modules/workforce -run '^TestPostingReviewCommandsFailClosedWithoutCurrentOwners$'
```

The wrapper runs Go with race detection and real disposable PostgreSQL/NATS,
uses existing cached images, captures exact fixtures and removes only its two
owned containers/anonymous volumes afterward. Corrected owner-unavailability test exit 1, package 4.751s;
private log `/tmp/helivanta-posting-review-workforce-verified.log`. No assertion
was weakened and no incomplete command handler was added to make this green.

The earlier evidence-download admission observation was separately rerun five
times, using the unchanged scoped-preparation source. All five four-transfer
admission cases passed; total package time 140.672s, zero fallback forwards,
three owned fixtures removed. This narrows reproducibility but establishes no
root cause or fix for the prior isolated 503. Private log:
`/tmp/helivanta-posting-admission-globalapi-verified.log`.

### Real-source public submission RED and closed IAM catalog

The additional `activation_posting_submission` case creates a real reviewed,
activated digital Staff source and independently approved reader/preparer
assignments through the existing public Global API fixture. It then attempts
submission before a submitter assignment. The route is absent: expected current
assignment refusal 403, observed 404. Parent package RED took 94.290s, zero fallback
forwards and three owned fixtures removed. The subsequent positive assertions
cover pending submission, immutable same-key recovery/history and unchanged active
Staff; they have not yet run past the missing route. The fixture now bounds its
draft to the reviewed generation cutoff and retires its temporary department in
cleanup; these are fixture corrections, not product fixes. Private log:
`/tmp/helivanta-posting-review-positive-globalapi-verified.log`.

The new IAM catalog test first failed on unsupported discovery (400). Canonical
input now appends IAM `0025_iam_staff_posting_review_templates`, generates the new
Go/Zod/template outputs and installs the two separate role bundles. The original
`staff-postings-v1` remains exactly eight definitions; the new catalog has those
same eight plus submitter/reviewer. No historical SQL changed. The generator
continues enforcing exact role bundles before writing any artifact. Its 51 tests
pass, including missing source authority and cross-role expansion rejection.

The first combined projection run exposed a fixture selector still requesting
`staff-postings-v1` for the new roles. Corrected only the two new keys to request
`staff-posting-reviews-v1`; existing keys retain their old closed catalog. The combined old/new catalog and real-PostgreSQL/OpenFGA projection tests pass
in 117.945s, with zero fallback forwards and three exact owned fixtures removed. This catalog component
is part of the full feature branch; no backend-only acceptance, deployed role or
working submission/review UI is claimed. The owner/public command tests remain
RED until real persistence and handlers are implemented.

Targeted IAM `go vet` and `golangci-lint` pass (0 issues). Contractdocs regeneration
and check, canonical Staff-access generator check and whitespace checks pass.
The generated inventory records the additive IAM migration. All five historical
IAM template migration SQL files remain byte-identical to the branch base.
The API SDK suite passes all 76 files / 609 tests, and API `tsc --noEmit` passes.
The package test invocation's trailing `--` ran the full SDK suite, not just its
two named files; the reported scope is the actual result.

Local dependency setup used Node 24.11.1. Full and filtered offline installs
stopped on missing cached tarballs, with zero packages downloaded. Reused the
already installed dependency trees using filesystem clones and rebound the seven
local workspace package links to this new worktree. Verified the shell resolves
this worktree's `packages/api/src/index.ts` before testing; shared registry
packages remain the existing installed versions. Partial-install directories are
retained privately under `/tmp/helivanta-posting-review-partial-dependencies-*`.
No new dependency/lockfile change or network package download was needed.

## Submission implementation checkpoint — 2026-11-04

Implemented in the current worktree: canonical proposal/source/decision models,
Workforce `0075_workforce_posting_proposals`, scoped pending submission and source
readiness, immutable command lookup, bounded head/history reads, source-bound
SDK submission/recovery and receipt validation. Proposal/revision/source pins,
private reason, audit and identifier-only event commit atomically. SQL currently
refuses every approval/head update until independent supervision policy exists.
No placeholder decision handler, duplicate Staff master or operating grant exists.

The real signed-session/PostgreSQL/OpenFGA fixed-term journey passes submission,
source readiness, required/false/unknown-field refusal, stale source refusal,
changed command intent, duplicate live proposal refusal and exact retained retry/
history. Audit and outbox failures roll back proposal state and actor source-use
evidence, then the original command succeeds with exactly one event. The earlier
complete Global API checkpoint passed in 391.040s; that finite-end implementation
was superseded by the ongoing-employment correction below, so it is not the final
current-source validation result. Private archived log:
`/tmp/helivanta-posting-review-bounded-submission-full-pass.log`.

The first fault fixture attempted DDL with the application role and failed with
42501 before injections ran. It now uses the existing disposable test migration
owner, preserving the application role's ownership restrictions. The earlier
premature progress statement about those injections was corrected. The real
fixed-term flow's passing retry/fault assertions are retained, without changing
the expected errors or weakening the database role.

Other development failures preserved privately: the staged pre-provenance test
initially applied 0075 before generation schema 0066; it now defers 0075 to the
existing later source stage. A missing generation cutoff fixture produced an
unbounded fixed-term draft and was refused. Department cleanup reused an immutable
command ID and was corrected to a new revision. Isolating only activation cases
omitted earlier source-policy/role setup; focused runs now include all literal
parent setup cases. These are fixture corrections, not product fixes. A deferred
integrity trigger's record-field selection was changed from a CASE expression to
shape-specific IF statements before the first successful commit journey.

### Ongoing employment correction

A new actual public `activation_posting_ongoing` journey first failed with 409
instead of 200 (focused parent 71.111s). The SDK separately rejected the open-ended
selected draft. This exposed a product restriction: reviewed-source expiry had
been incorrectly treated as a mandatory physical placement end. Canonical models,
SQL, handler and SDK now retain nullable placement ends for ongoing employment;
fixed appointments still refuse null or outlasting ends. The source cutoff stays
explicit and finite, the start must precede it, and operating eligibility still
requires all current owner checks. The earlier negative SDK case of a cutoff
before the nominal placement end was corrected to a cutoff at the placement
start, which is the actual invalid boundary under this contract.

Focused real fixed/ongoing journeys and their parent setup now pass in 89.620s,
zero fallback forwards, three owned fixtures removed. The focused SDK tests pass
14 assertions. Logs: `/tmp/helivanta-posting-review-ongoing-red.log` and
`/tmp/helivanta-posting-review-ongoing-focused-pass.log`. The later full Global API and
SDK/type/lint/build/contract validation is recorded below after this semantic correction.
The previous 615-test SDK pass is superseded by that run. Legacy posting bootstrap
owner tests passed in 42.058s; the event/owner architecture checks passed in 11.394s.

### Required next proof

- Separate digital actor/non-digital target submission and changed/restored
  identity are verified below. Target cutoff races remain separate requirements;
  self-target source tests alone cannot prove them.
- Prove new submission versus target/source ending in both orders, including
  changed/restored target identity with current fresh draft concurrency pins.
- Implement and prove independent decisions, retained contributor/target exclusions,
  supervisory roots/chains/cycles and lifecycle before any positive approval.
- Complete the normal Staff UI and local browser role matrix on this branch,
  then exact-head CI, named migrations/rollout and production acceptance.
- Keep `TestPostingReviewCommandsFailClosedWithoutCurrentOwners/decisions` RED
  until the actual decision implementation exists. It is not a passing Workforce
  package claim and must not be deleted, skipped or hidden to report green.

Only the earlier IAM catalog component is committed/pushed at `268e7d99`.
Current proposal work remains in progress; no new merge, migration rollout,
release or complete Staff/Pharma acceptance is claimed.

### Different non-digital target proof — 2026-11-04

`activation_posting_nondigital` uses a separately reviewed digital submitter from
the preceding ongoing-appointment case. All target business sources are created
through public commands with separate person/employment review and explicit
activation; its person finding is non-digital and no login is provisioned.
The target-specific helper verifies current source, exact generation/department,
pending submission/retry/history and unchanged account intent, then changes and
restores identity through normal HR enrollment. A fresh draft uses current Staff
pins but cannot submit against the old generation (409); its absent proposal is
404, the earlier receipt remains readable, and login links remain empty.

Focused real-parent run passed in 62.224s. Initial fixture setup exceeded the
40-character employee-code limit and returned 400; shortening the scenario/code
fixed that fixture without changing validation. Private evidence:
`/tmp/helivanta-posting-review-target-code-fixture-failure.log` and
`/tmp/helivanta-posting-review-nondigital-target-pass.log`.

An actor-purpose mutation survived, demonstrating that this test does not prove
that HR-purpose setting is an independent boundary. That result is retained in
`/tmp/helivanta-posting-review-nondigital-actor-mutation.log`; it is not classified
as a product defect without a reproduced unauthorized outcome.
The actual target-epoch mutation forced `identity_changed` to effective only when
actor and target differ. The test then observed unauthorized pending submission
200 rather than 409, failing at `workforce_posting_target_test.go`. It did not
stop at a readiness-label assertion. Exact owner source was restored and verified
by SHA-256. Private log:
`/tmp/helivanta-posting-review-nondigital-epoch-mutation.log`.

The unmutated ongoing-correction full Global API suite passed in 410.914s, archived
as `/tmp/helivanta-posting-review-ongoing-full-pass.log`. SDK validation passes
616 tests / 77 files, all canonical checks/typecheck and lint. Go build, scoped
Workforce/Global API vet and golangci-lint (0 issues), contractdocs check and
whitespace checks pass. The subsequent full Global API race run including the
non-digital target passed in 448.467s, zero fallback forwards, three exact owned
fixtures removed. Archived log:
`/tmp/helivanta-posting-review-nondigital-full-pass.log`.

GitHub read-only verification reports parent preparation PR #1532 merged as
`091f2287` with required checks passing. Refreshed main contains Workforce 0074
and IAM 0024 as their latest migrations, so candidate 0075/0025 have no collision
at this checkpoint. Recheck before release. No production rollout or persistent
QA schema mutation was performed.

### Registry and architecture integration correction

The full architecture suite reproduced missing Workforce permission declarations
for proposal read/submit and missing singleton classification for source,
proposal and command receipt GETs. These are actual integration findings, separate
from the initial invocation's missing Docker endpoint. The owner now declares
proposal read/submit/review without assigning them to legacy staff roles; the
independently authored legacy permission matrix records only implicit tenant-admin
catalog entries, while the public scoped owner paths still require current
independent assignments and source. Three exact singleton exceptions describe
their one-record responses; proposal/history collections remain paginated.

The full architecture race suite then passed in 71.684s through the configured
disposable-fixture runner (real PostgreSQL/OpenFGA, zero fallback forwards, three
exact owned fixtures removed). Go build, affected vet/lint (0 issues), regenerated
contractdocs/check and whitespace checks passed after this correction. Public
fixed/ongoing/non-digital submission and parent setup cases passed after the
declaration change in 79.689s, zero fallback forwards, three owned fixtures removed. The owner decision case remains intentionally RED and full Workforce
acceptance is not asserted.

Operational inventory first rejected changed authority hashes. The detailed
[source review](../../qa/2026-11-04-posting-proposal-source-review.md) verified
unchanged existing Workforce/IAM route/grant registrations against main and
preserved legacy helper behavior. Nineteen affected existing reviews now pin the
reviewed bytes, without changing authority category or runtime obligations.
Ledger parity passes: 790 surfaces, 23 source-classified, 767 unclassified.
All 15 inventory tests pass; six new proposal surfaces remain unclassified.


## Reviewer workspace admission gap — source review 2026-11-04

The current preparation UI discovers hospitals and targets using
`workforce.posting.read`; the independently deployed reviewer catalog includes
draft/proposal reading, review and current Staff actor use, without draft
editing. The existing preparation discovery reports only `can_prepare`, so it
cannot identify review versus submission capabilities for the new workspace.
The first new fixture incorrectly assumed reviewers lacked draft reading;
the canonical immutable catalog disproved that assumption. Correct the fixture
to preserve read-only discovery while explicitly refusing draft edits.

Before adding reviewer UI, extend the canonical posting contract with a separate
bounded review-scope projection and hospital proposal inbox. Preserve preparation
routes, existing catalog snapshots and their purpose checks. Review discovery
must consume current proposal-read and Staff-actor authority at the verified
hospital, with independently checked submit/review capabilities. A scope flag is
presentation, never command authorization. Follow the existing bounded candidate
scan/opaque cursor rules, including empty filtered pages with continuation.

The hospital inbox must expose only canonical Staff summary identifiers/labels
and public proposal/department/version/status fields, with no protected reasons,
identity hashes, employment evidence or contributor subjects. New hospital-read
RLS must be read-only, exact tenant/hospital/actor/proposal purpose, backed by the
owner's current scoped command and reviewed digital actor. It must not broaden
existing exact-target proposal writes or login-link write/delete purposes.
Historical/current independence must be derived by Workforce; the UI cannot
construct it from displayed subjects. Reauthorize every decision against exact
pending revision and current actor sources. Record sensitive inbox reads, and
withhold results if audit or current authorization fails.

Required RED/green proof before exposure: reviewer without draft-edit can find
its eligible hospital and bounded proposal summaries; tenant-admin/membership
alone cannot; wrong tenant/hospital and arbitrary Staff reads remain denied;
expired/revoked actor or assignment clears results; cursor scopes and audit
failure are enforced. Then exercise normal submitter/reviewer UI, including
retained command recovery, contributor refusal, rejection, withdrawal and reload.
Positive approval remains a separate unfinished supervision policy obligation.
This section records an implementation gap and next design work, not deployed
routes or accepted UI behavior.


The existing public proposal DTO intentionally omits the retained rationale and
private source evidence. The `sources_examined` input is an attributable actor
attestation, not proof that the browser displayed those private fields. Before
claiming normal reviewer UI acceptance, define and implement the auditable,
current-authority release of the exact proposal's protected review context, with
independence checked before disclosure. Do not make a checkbox stand in for a
missing protected review view. The original submitter needs the matching retained
context for withdrawal/recovery without acquiring unrelated reviewer authority.
Keep any source release bound to exact proposal/draft revisions and current
actor/tenant/hospital; later commands must recheck those pins through commit.

Protected-context candidate now adds the exact-head/purpose GET and canonical
Go/Zod/SDK response described in the specification. Real gateway and SDK tests
first failed on the absent route/helper, then passed for current independent
review and original-submitter views. The strengthened gateway case covers
retained rationale after a later draft edit, terminal rationale, contributor and
historical-association denial, stale/hidden targets and audit withholding with
actor-use rollback. Current-source and Organization verification remain separate;
no checkbox or retained rationale is represented as operational eligibility.
The decider-bypass mutation failed with an unauthorized 200 instead of 403.
Restored complete gateway (454.878s), Workforce (700.117s) and architecture
(49.919s) race suites passed, alongside SDK630/typecheck/lint/format and contract
checks. Exact-head CI and the hospital inbox/normal UI remain required before
acceptance. No new SQL or migration is introduced by this view.
