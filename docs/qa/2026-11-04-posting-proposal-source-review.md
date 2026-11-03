# Posting proposal candidate source review

2026-11-04. Source classification only; independent review and production
acceptance remain open. See the [feature plan](../superpowers/plans/2026-11-03-independent-staff-posting-review.md).

Compared the generated registry against catalog release `e9ea7281`: every
existing Workforce and IAM route registration and permission declaration remains
unchanged. Workforce adds nine scoped submission/decision/source/receipt/list/
history/discovery routes and three posting proposal permissions. IAM migration
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
and unresolved runtime obligations remain unchanged. The nine new proposal
surfaces remain explicitly **unclassified** in the operational ledger until their
own full source/lifecycle review; ledger parity does not complete those reviews.
No department-scoped operating access, supervisor policy, effective posting,
production migration or clinical/regulatory acceptance is established here.


## Terminal decision review and outstanding evidence

`posting_decisions.go` compares the exact pending version/revision and recovers
same-key receipts only for the original normalized intent and subject. SQL permits
pending-to-rejected/withdrawn transitions, preserves original source pins, and
requires the original submitter for withdrawal. Independent review excludes the
target, draft contributors and historical linked-account associations. These
historical-alias exclusions still require stronger public-interface test proof;
source inspection alone is not acceptance.

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
Leave changes. Current-head Workforce and combined CI validation remain pending.
The API candidate still has no production or normal reviewer UI acceptance.
