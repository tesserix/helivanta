# Posting proposal candidate source review

2026-11-04. Source classification only; independent review and production
acceptance remain open. See the [feature plan](../superpowers/plans/2026-11-03-independent-staff-posting-review.md).

Compared the generated registry against catalog release `e9ea7281`: every
existing Workforce and IAM route registration and permission declaration remains
unchanged. Workforce adds ten scoped submission/decision/source/receipt/list/
history/discovery/protected-context routes and three posting proposal permissions. IAM migration
0025 is already merged/deployed as the prerequisite, not a new migration for this
API candidate. No prior migration record or SQL was replaced. Candidate Workforce
0075 remains additive and has only been applied to disposable test databases.

The three new permissions are declared without legacy staff-role grants. The
implicit tenant-admin catalog entries do not authorize the scoped routes:
current hospital assignment, membership/credential epochs, exact original account
headers and reviewed digital Staff actor remain owner-enforced. The independent
reviewer template grants no preparation, clinical or Pharma authority. A proposal
has `grants_access=false`. The decision route supports rejection and withdrawal;
positive approval returns `409 posting_supervision_required` because reviewed
supervision and effective-posting policy remain unimplemented. Its registry
permission is `authz.Public` to dispatch two different command permissions, not
to authorize anonymous access: the handler requires current owner providers,
verified tenant/subject, exact hospital-scoped submit authority for withdrawal or
review authority for rejection, and the current reviewed digital Staff actor.
Missing owner providers return 503 before request parsing.

`postingTransaction` preserves its legacy owner path. Its prior scoped path is
extracted into `scopedPostingTransaction`, with explicit action permission and a
transaction-local permission setting. Existing draft callers select the same read
or prepare permission and mode. New proposal callers select separate proposal
read or submit permissions; no legacy route opts into proposal authority. IAM
guards, supervision-before-actor order, actor source guard and owner contexts
remain required through outer commit.

`readProtectedGenerationState` retains its original signature and supplies the
same Staff identity as target and actor to the extracted owner-private helper.
The latter permits a separate target only for the new posting source consumer;
it retains exact generation loading, identity-epoch/person/employment/evidence
checks and restoration of all previous purpose settings. No existing actor call
changes its source or gains permission. The target-specific public test verifies
non-digital admission and changed/restored target refusal; a targeted epoch bypass
changes required 409 into 200 and fails that assertion. An HR-purpose-ID mutation
survived and is explicitly not claimed as an independently tested boundary.

Reviewed source pins for existing employment review, IAM and legacy draft
surfaces are refreshed only for these compared changes. Their authority categories
and unresolved runtime obligations remain unchanged. The ten new proposal
surfaces remain explicitly **unclassified** in the operational ledger until their
own full source/lifecycle review; ledger parity does not complete those reviews.
No department-scoped operating access, supervisor policy, effective posting,
production migration or clinical/regulatory acceptance is established here.


## Terminal decision review and outstanding evidence

### Protected context addition

The exact-head context GET uses explicit review/submission purpose and current
scoped actor authority. It loads only public head metadata before invoking the
same SQL decider as a command; rationale is loaded only after that predicate
passes. The extracted Go decision-context/decider helpers retain the existing
settings, generation read purpose, SQL call and denied/error behavior. The new
read uses the existing write gate before actor rows, avoiding a lock upgrade and
holding the same source/contributor writers through outer commit. No prior SQL,
RLS policy, migration, permission or catalog changed.

Reasons come from the selected immutable draft, version-1 submission and exact
current terminal revision. Later draft edits cannot substitute a different
rationale. The response excludes actor identities, hashes and identity dossiers;
current-source/Organization eligibility views remain separate. Exact version
audit and Staff-source-use evidence commit together; audit failure withholds
the view and rolls source-use evidence back. `sources_examined` remains an
attributable actor attestation, not a claim that a browser examined every source.

Real public tests first failed with absent-route 404, then passed for current
review and original-submitter context. The strengthened real gateway run passed
in 98.276s. Bypassing only the protected-view decider returned the contributor's
private context with 200 instead of 403, failing in 88.239s; original source was
restored and hash-verified. SDK630/typecheck/lint/format, Go build/scoped vet/lint,
contract checks and 15 inventory tests passed. The complete restored gateway
race/profile suite subsequently passed in 454.878s, including real OpenFGA outage
withholding. The current complete Workforce race/profile suite passed in 700.117s
and the complete architecture race suite in 49.919s. Combined owner/gateway source
coverage is 12,204/14,672 statements (83.179%); Go's report independently confirms
83.2% rounded. The rebase onto the merged coverage correction retains all 1,418
tested runtime/contract source hashes exactly and preserves upstream Leave QA
documentation. Rebased contract/inventory checks and shell lint passed; exact-head
CI remains required. The registry adds exactly
one authenticated/membership-required GET with explicit bounded-object rationale.
The ledger is 794 registered/771 unclassified; all ten proposal surfaces remain
unclassified, with normal reviewer UI and production acceptance still open.

`posting_decisions.go` compares the exact pending version/revision and recovers
same-key receipts only for the original normalized intent and subject. SQL permits
pending-to-rejected/withdrawn transitions, preserves original source pins, and
requires the original submitter for withdrawal. Independent review excludes the
target, draft contributors and historical linked-account associations. These
historical-alias exclusions still require stronger public-interface test proof;
source inspection alone is not acceptance.

The subsequent legacy-alias regression supplies public-interface proof for the
reviewer-Staff/contributor association branch. It imports a revoked historical
link while preserving public-workflow current generations, links and assignments.
The decision returns 403 with unchanged pending state/history and no receipt;
original-submitter withdrawal still works. A mutation removing only this branch
returned 200/rejected version 2 and failed the assertion (83.673s). Original SQL
was restored and hash-verified, then the focused real PostgreSQL/OpenFGA race run
passed (105.106s). This does not establish modern account-rebinding UI acceptance
or separately prove the target's historical-association branch.

The decision read purpose extends the existing login-link read policy while
preserving its write check and adding a restrictive delete guard. Full migration
and RLS suites must pass again before release. Decision heads, retained revisions,
actor-use evidence, audit and outbox share the root transaction. The fault fixture
uses the canonical event subject constant and asserts a positive pre-fault event
count; a wrong duplicated subject was caught and corrected in the test fixture.

The latest focused Global API run failed before assertions: the disposable
OpenFGA process started, but host HTTP readiness timed out. Concurrent coverage
work in another worktree was using the same small Docker VM; this establishes
contention, not a proved transport root cause. No product assertion passed in that
run. The task-owned fixture was captured and removed without pruning unrelated
containers. Full affected suites and authenticated reviewer UI remain pending.


The final pin reconciliation compared Workforce routes/grants against current
main and confirmed that all existing registrations remain unchanged; the eight
new proposal routes and additive 0075 migration remain separate. Module-file pins
changed for 16 previously reviewed surfaces. The 12 draft-surface source pins
changed because shared reason validation now rejects all Unicode control
characters before persistence, matching the generated SDK validator. No draft
read, source owner, grant, purpose, command identity or history semantics changed.
The reason regression passed with race detection; this reconciliation does not
classify or accept the eight new proposal surfaces.

The new earlier-contributor assertion passed in the 109.950s focused run. Removing
only the draft-history contributor union made that exact public decision return
200/rejected instead of 403, failing at the new assertion. The mutated run failed
in 96.847s; all original SQL bytes were restored and SHA256 verified. A full suite
on the restored source is required before merge. Historical login-alias exclusions
remain a distinct unproved obligation.


The new review-scope discovery route is also an authenticated/membership-checked
`authz.Public` registration with owner-enforced current proposal-read and
Staff-actor-use authority. Its provider checks precede candidate discovery, so
unavailable owners cannot produce a verified empty page. It reuses the existing
bounded reviewed-link candidate scan and exact-actor source transaction. Each
capability is independently checked and audited through that transaction; the
DTO flags do not authorize a later command. No Staff/proposal RLS, catalog or
migration changed for discovery. Existing reviewer catalogs already include
draft reading and exclude editing; the initial contrary fixture expectation was
corrected against the canonical snapshot, not by changing that snapshot.

The discovery addition changes only the module route registration and its
reviewed file pins; all previously registered routes/grants remain unchanged.
The canonical generated DTO has exactly hospital ID/name and required boolean
submit/review observations, with no private Staff fields.


The latest-main worktree preserves main's catalog review rationales and all 19
previous classifications. All 1,498 tracked backend/API/contract/script files and
the 27 changed source hashes matched the earlier tested worktree before the
expiry fixture-only change. The full Workforce race suite passed in 622.627s and
the full architecture race suite in 56.617s. The full API package subsequently
exposed a pre-existing clock-domain assertion and cancelled-context cleanup bug;
these are not counted as a pass. The fixture now uses either actual owner clock
without a cutoff grace period and retains its exact denial/rollback assertions.
Moving only the shared source timer one hour late failed with 503 versus expected
403, demonstrating the corrected test still detects that protocol regression.
The original timer bytes were restored and verified. Production runtime code and
source cutoff semantics were not changed by the fixture correction.


The restored full Global API race suite subsequently passed in 411.298s. The
candidate is committed in draft PR #1544 (`80dbc610`). A rebase preserved main's
separate Leave change 96a47d0b; only six inherited Leave implementation/test/
contract files differ from the pre-rebase runtime source, and posting source
bytes are unchanged. Current-head SDK625, typecheck/lint, Go build, scoped vet/
lint, contractdocs check and committed secret scan passed. Complete-package
pre-rebase results are not represented as complete-package runs of those new
Leave changes. The current combined implementation's complete Workforce race
suite subsequently passed in 576.956s. CI at formatting commit `0cf52a1a` passed
web/scripts/secrets/E2E/image checks and all Go behavioral suites, but failed the
Workforce per-test-binary coverage floor (69.2% versus 70%). A separate source-owner
coverage-accounting correction retains the floor and missing-owner-test safeguards;
its real Go fixture regressions passed, while full CI remains required. The API
candidate still has no production or normal reviewer UI acceptance.

The additional full Workforce race/profile run passed in 685.470s at the
historical-association candidate. The owner binary again reported 69.2%. The
source-owner parser in separate CI PR #1545 counts 11,775 / 14,611 statements
(80.590%) when combining that full owner profile with the restored focused real
gateway profile. Its seven dependency-free real Go fixture regressions and CI
lint passed. The complete current Global API race run subsequently passed in
565.391s, including the new alias case, with three owned fixtures removed and no
fallback SSH forwards. Combining the two complete-suite profiles with that parser
gives 12,147 / 14,611 Workforce statements (83.136%). Full backend CI remains in
progress; these complete affected-package runs are not a substitute for that gate.

### Hospital inbox source delta

The inbox adds one scoped GET and additive candidate migrations 0076/0077.
Comparison against published `b5cf5631` preserves every existing Workforce/IAM
route registration and grant. The only changed previously reviewed source hash
is Workforce `module.go`: additive embed/migrations/route registration, with no
legacy route or grant changes. Existing 23 review classifications and rationales
are preserved. Generated inventory is now 795 registered / 772 unclassified;
all eleven new proposal surfaces remain unclassified.

`postingActorForHospital` extracts the existing exact active-link discovery
without changing its query, and adds nil-provider/zero-actor refusal. The new
inbox still obtains current authority inside `scopedPostingTransaction`.
`postingProposalAuditRecord` preserves existing proof fields and merges trusted
inbox status/count before immutable source proof. Existing audit callers pass
no additional details. The list helper retains old wrappers/cursor encoding;
only the new scoped-query wrapper opts into subject/hospital/filter binding and
an eight-item default/max with handler-overrun refusal.

The SQL read purpose broadens SELECT only. The first full owner run rejected a
second permissive policy; the migration now composes the existing permissive
predicate and retains its original UPDATE/DELETE eligibility with restrictive
guards and its unchanged WITH CHECK predicate. Existing statement-level destructive
operation guards continue to reject revision updates and both table deletes;
proposal-head UPDATE under the read purpose affects zero rows. The fixture
assertions were corrected to distinguish those two existing protection modes.
No rejected statement is treated as a successful zero-row command.


Corrected-migration validation: full Global API race 472.140s, Workforce
race 679.419s, architecture race 65.636s and platform race 12.850s pass. The RLS
invariant failure from the first owner run is resolved. Contractdocs, the
canonical generators and 15 inventory tests pass; the additional full-projection
queue-plan/index probe is being recorded separately. No scale or production
acceptance follows from these small synthetic fixtures.


The final gateway protection/index-plan run passed in 100.443s. The representative
full-projection first-page plan uses the registered inbox index and returns three
rows under Limit; execution 2.124ms/planning 5.003ms on the small synthetic fixture.
The exact index columns are asserted. The plan is captured privately; continuation
and production-scale/SLO evidence remain open. No runtime code changed after the
complete suite runs; this final addition is test-only plan/index evidence.

### Approval diagnostic source delta — 2026-11-14

Review of 89605942..90950803 confirms the transfer's shared
`recordPostingDecision` implementation is byte-identical. The separate decision
handler only tracks constant phases and elapsed milliseconds and enriches the
existing error mapper on failure. The successful receipt branch is equivalent: it
returns the same audit error before marking guarded completion. No permission,
source predicate, actor eligibility, persistence, time budget, route or transfer
call path changes. Race tests cover unchanged 403/500/503 responses and absence
of raw errors/private proof in logs and HTTP bodies.

The source hash for the classified transfer surface is refreshed after this
review; its authority classification and remaining obligations are preserved.
At 89605942, browser CI passed all three phases (99 passes/1 skip in the main
suite); supplier Staff lifecycle still failed child approval at
`workforce_posting_chain_test.go:135` with 503 instead of 200. Diagnostics and
this source review do not establish a repair, live rollout or full acceptance.

### Shared persistence substep attribution — 2026-11-14

Supplier43597336249 at 90950803 reproduced the child-approval 503, now located at
`decision_persistence` after 5017ms overall. The earlier file-level-only delta
did not repair it. The next candidate adds failure-only operation/elapsed/total
metadata to the shared persistence method and enriches both handlers' existing
failure logs. Its result types and all raw SQL literals match 90950803; guards,
success effects and transaction boundaries are unchanged. The chain-review
else branch is equivalent. A private error wrapper retains its cause through
Unwrap, preserving errors.Is/As and PostgreSQL classification without exposing
raw error content. No timeout or status assertion changes.

Re-review updates the two affected source hashes and additionally pins the small
diagnostic helper. This is source review of diagnostic error propagation, not
runtime proof of the method or an acceptance of the unresolved deadline.
