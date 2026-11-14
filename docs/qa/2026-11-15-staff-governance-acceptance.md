# Staff initiation, reviewed linking and pharmacy-access acceptance

Status: incomplete candidate; no merge or live rollout is established here.
This checkpoint preserves the full goal across the hospital-owned pharmacy and
independent pharmacy/supplier models. Source inspection, component checks,
service integration, browser testing and live acceptance are distinct evidence.

## Candidate and authoritative evidence

- HMS PR: [#1669](https://github.com/tesserix/helivanta/pull/1669), candidate
  `e5e016c772ef7f59b3643d22fc59fe41c31d3305`.
- Identity owner: [onboarding-service #39](https://github.com/tesserix/onboarding-service/pull/39),
  pinned source `20d240693ac04d779fec504720803a989ca6f5ce`, unmerged/undeployed at
  this checkpoint. Its private assignment-candidate projection is a dependency.
- Exact-head [CI43624573111](https://github.com/tesserix/helivanta/actions/runs/43624573111)
  and [Supplier43624573143](https://github.com/tesserix/helivanta/actions/runs/43624573143)
  are not passing yet. The Go job failed the new department integration fixture's
  initial grant at `assignment_department_choices_test.go:102`: expected 202,
  actual 400. The fixture sent nanosecond timestamps; the canonical governance
  contract requires microsecond precision. The fixture is corrected locally with
  `Truncate(time.Microsecond)`; production validation and expectations are unchanged.
  Browser/E2E and the full supplier lifecycle are still running.
- At `87e605b9`, [CI43619827672](https://github.com/tesserix/helivanta/actions/runs/43619827672)
  web, E2E, scripts and secret scan passed. Both native independent browser branches
  exited cleanly through acknowledged activation and HTTP/UI revocation.
- [Supplier43619827649](https://github.com/tesserix/helivanta/actions/runs/43619827649)
  at `87e605b9` failed the original complete Staff lifecycle at
  `workforce_department_transfer_test.go:49`, initial destination approval:
  expected 200, actual 503. Overall 5018ms; persistence 1512ms; chain evidence
  315ms. This is not successful transfer or complete supplier acceptance.
- Last read-only live evidence, 2026-11-14 UTC: project `tesseracthub-480811`,
  cluster `tesseract-prod-in-gke`/`asia-south1`, namespace `helivanta`, Kargo
  `kargo-helivanta/prod`. Kargo was Healthy/Verified, Argo applications were
  Synced/Healthy and workloads still used `0a1fad14`. Older deployment health
  establishes no candidate behavior.

## Completion gates

Every unchecked item remains required; a passing neighboring row does not close it.
Use only authorized synthetic people, establishments and stock. Keep retained
legacy identity/history untouched as specified by the fresh-tenant plan.

- [ ] Merge and deploy the identity-owner dependency through its owning delivery
      path, then verify the workload-only candidate projection and refusal/audit
      behavior against the deployed owner.
- [ ] Complete a fresh normal tenant signup with a distinct native organization,
      verified address, owner password/MFA and correct hospital or independent
      pharmacy ownership. Use the user-driven identity steps in the
      [fresh-tenant plan](2026-11-14-pharmacy-acceptance-fresh-tenant.md).
- [ ] Complete Staff initiation without requiring operational Staff authority
      from the first owner: bounded native employer discovery, canonical Staff
      and person data, employment preparation and protected evidence.
- [ ] Independently review the person/account link and first digital employment
      activation. Verify wrong organization, nonhuman/unverified account, stale
      identity/profile/link, missing membership, duplicate command, source loss
      and audit/persistence failures. Membership alone grants no business role.
- [ ] Finish the native hospital assignment preparation UI with qualified
      existing-account and Organization-owned department selection. Current
      proposal/account presentation and hospital discovery do not complete the
      missing target selectors; raw subject/department IDs and broad legacy
      directory permissions do not qualify as completion.
- [ ] Propose, independently review, activate, withdraw and revoke hospital and
      department assignments with real current sources, exact tenant/account
      context, stale/duplicate handling, dependency failure and receipt recovery.
- [ ] Resolve the supervised child-posting deadline and the earlier coordinated
      transfer persistence failure. Temporary plan/function observers are removed;
      verify the ordinary cold path. Re-run the entire original current-Staff
      lifecycle test; do not substitute a smaller scenario or change its expected
      200/500 responses to accept unavailable sources.
- [ ] Verify department staff, heads, supervisors, chains and transfers through
      canonical owner workflows, including source cutoff and rollback. Job title
      or department ownership does not confer IAM, clinical or statutory authority.
- [ ] Execute the complete native independent employer -> reviewed Staff ->
      product-owned outlet/premise -> operating proposal -> independent decision
      browser journey for both pharmacy and supplier owners. Include pending
      command scope retention, lost response/GET recovery, stale source and
      unauthorized access. Current discovery-only browser cases are insufficient.
- [ ] Verify effective allowed and denied pharmacy access for current hospital
      and independent Staff, then remove/revoke/change each relevant source and
      verify access cutoff. Check another tenant, employer, department and outlet
      and unavailable dependencies; UI visibility alone is insufficient.
- [ ] Verify the requested collaboration journey on deployed synthetic fixtures:
      effective supplier grant, each stock owner's fulfilment, hospital receiving,
      discrepancy/correction, release and financial integration. The passing
      supplier stages above do not establish this complete live journey.
- [ ] Pass required checks on the actual merge candidate, keep the repository
      public, merge the candidate, execute the approved migrations/rollouts through
      the owning GitOps/Kargo path, and verify the resulting freight, Argo sync,
      workloads, image digests and health. No imperative bypass or private seeding.
- [ ] Test the deployed web login and both Staff/pharmacy journeys with the
      authorized distinct users. Record attributable owner/API/browser evidence
      and all exclusions. Preserve professional/licence/trading boundaries; do
      not use real patients, real licence assertions or real payments as fixtures.

## Current implementation and next verification

### Independent native browser journeys

The CI fixture uses actual Global API, PostgreSQL, OpenFGA, IAM, Workforce and
product owners. The shell selects a qualified employer, reviewed Staff account
and product-owned outlet/premise; a scoped delegate proposes and a separate owner
reviews. Deliberately lost committed proposal/approval responses require GET
recovery without repeating PUT. Navigation freezes while uncertain; the real
projection worker must acknowledge active state. The preparer cannot self-approve.
HTTP-confirmed revocation requires version 4 and the revoked UI state. Legacy
catalog/account-link/operating-route fallback is rejected.

At `4470fb2a`, both branches passed business assertions but failed at context
cleanup. `2ae3319b` made cleanup await `unrouteAll({behavior: "wait"})` before
closing without suppressing errors. Web passed at `5b2bb8a5` and `87e605b9`.
The fixture uses a synthetic external identity reader and lacks profile/session
services. It does not prove normal deployed Zitadel/login or allowed/denied
product operations.

The candidate extends both branches after existing replacement-source/receipt
checks: an authenticated delegate opens current Staff and starts an unsent draft;
a separate empty owner request context revokes its replacement governance grant
through Global API. Refresh must return 404, hide Staff/account details and proposal
controls, show an alert and produce zero duty mutations. It does not substitute
logout or ordinary-membership removal for management-source cutoff. Exact-head
runtime acceptance of this extension remains pending.

### Hospital department choices

Organization's candidate owner route, read-only private purpose and canonical
`DepartmentOutput` SDK are implemented. The route holds current owner/exact
hospital reviewer authority and the restructuring gate, returns only active heads
from a verified registration, uses sealed pages of at most 20, and commits its
read audit before disclosure. All master writes and unrelated history are denied
under this purpose. Runtime support checks fail closed on policy/RLS/index drift.

Contractdocs generation caught a startup panic from missing domain-filter binding;
the corrected registration binds active status and rejects other statuses. A real
Global API/IAM/Workforce/Organization test is explicitly in CI: owner/delegate
reads, foreign/sibling scope, paging/cursor replay, audit rollback, revoked source,
missing registration, RLS/policy tampering and master-write denial. Compile-only
checks do not establish that it passes. Qualified Staff/account composition and
native preparation UI remain open.

### Full Staff lifecycle performance

The original full lifecycle remains the acceptance test, including all its injected
faults, coordinated transfer, cutoff races and original 200/500 expectations. No
five-second authority deadline or source predicate has been relaxed.

Earlier aggregate profiling at `4470fb2a` found root context 54 calls/8463ms self/
21925ms total, chain context 36 calls/5633ms self/14781ms total and decider 210
calls/2369ms self/45584ms total. These are aggregate fixture counters, not
per-request timing attribution. JIT-off did not repair transfer and was reverted.
Temporary EXPLAIN/function observers and their opt-ins are removed.

WF0117 uses stable invoker PL/pgSQL, fast-denies an absent private purpose, and
retains the complete original authorization SELECT for each valid purpose.
Prepared plans may be reused; authority results are never cached. The initial
WF0117 run exposed fixture migration ordering; both staged partitions now defer
it with its prerequisites and the SQL explicitly checks those prerequisites.
`87e605b9` got past ordering but failed initial transfer approval as recorded above.

The current candidate records fixed-label stage durations on the existing error
path to distinguish scope authorization, proposal reads, decider, source collection
and persistence. A deterministic regression verifies exact durations, preserved
503 status and no diagnostic fields in client responses. The next full CI result
must identify the slow stage before any repair is claimed.

### Local validation of this increment

Commands actually run and passed:

- API package `vitest run`: 126 files/1403 tests, including 11 new department SDK
  tests; the focused department/governance SDK run passed 37 tests.
- New SDK ESLint and `tsc --noEmit`.
- Organization input and posting diagnostics `go test -race`; compile-only
  bootstrap; affected `go vet` and `golangci-lint` (0 issues).
- Backend `go build ./...` and shell production `next build`. Generated shell
  output was removed; existing cache was retained.
- Contractdocs generation/check; inventory generation/check and all 15 inventory
  tests; JavaScript syntax, Prettier, actionlint and diff whitespace.
- Exact staged-diff redacted secret scan: no leaks. A broader SDK directory scan
  reported pre-existing fixture strings; it is not recorded as a clean scan.

No local app, browser, DB or container was started. Scratch migration, real-owner
and browser tests run in CI. No main merge, live migration, rollout, cleanup or
identity rewrite occurred. The broader completion gates above remain required.

## Terminal results for `e5e016c7`

The main E2E, scripts, secret scan and API client jobs passed. Go failed the
new grant fixture's nanosecond precision (corrected in `da18ae96`). Workspace
lint/typecheck/unit tests/builds and emergency browser checks passed, but web
failed both added management-cutoff branches at the receipt assertion: the test
incorrectly read `status` from `GovernanceGrantDecisionReceipt`. The canonical
command receipt contains `input.decision` and `result_version`; current status is
on the grant head. The test now verifies exact command/grant/tenant/actor/version,
then independently GETs the revoked version-3 grant before checking browser denial.
Its cutoff assertions are unchanged and still require a new passing CI result.

Supplier43624573143/job132537451883 failed the same original initial transfer
approval (expected 200, actual 503). Exact request timings: scope authorization
1052ms, proposal basis 17ms, decider 356ms, supervision read 10ms, supervision
sources 1163ms, persistence 2414ms; total 5013ms. The failed persistence operation
was proposal update (536ms). Cost spans multiple stages; no single-query cause,
repair, successful full lifecycle, merge or deployment is claimed.

Read-only live recheck at 2026-11-14 18:16 UTC confirmed account
`unidevidp@gmail.com`, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`.
All Helivanta Argo applications were Synced/Healthy; Kargo was Healthy and the
application workloads remained ready on `0a1fad14`. No live mutation occurred.

### Conditional full-lifecycle database diagnosis

A separate CI job now runs only when the ordinary supplier job's original full
Staff lifecycle step fails. It reruns that entire unchanged test with explicit
PostgreSQL function tracking; the ordinary acceptance job keeps tracking disabled.
The diagnostic emits at most 30 function counters sorted by self time, admits only
function names declared by the canonical Workforce migration sources, and rejects
unknown names, negative/unsafe counts and nonfinite/invalid durations. It emits no
SQL, bind values, proof, person labels or row contents. Other testinfra settings
and production configuration are unchanged.

These counters are aggregate fixture measurements, not per-request attribution.
Tracking can affect execution cost; a diagnostic pass cannot replace the ordinary
cold lifecycle gate. Both use the same original assertions, fault/race scenarios,
source predicates and five-second authority budget. The diagnostic job is temporary
investigation code to remove once the source of the measured cost is repaired.

### Qualified department owner runtime acceptance at `8b622a0b`

CI43627091553 Go job132545330831 passed the full Staff governance boundary
step at 18:40:52 UTC and the dedicated department-selection integration step at
18:41:08 UTC. The corrected real Global API/IAM/Workforce/Organization test now
passes all its original positive, scope/cursor, audit, revocation, registration,
RLS/policy-support and master-write-denial assertions. This closes the backend
runtime test for that endpoint, not native hospital preparation UI, full Go/CI,
identity-owner deployment or full Staff/operating acceptance. Other jobs were
still running when the subsequent diagnostic commit was prepared.

## Retained-duty management and WF0118 candidate

At `5f6f70cc`, CI43628051355 web job132548386037 passed both native
pharmacy/supplier browser journeys, including management-source cutoff. E2E,
scripts and secret scan also passed. Go job132548386488 failed the department
master-write-denial fixture: two `clock_timestamp()` calls could produce different
instants, triggering chronology validation before the expected RLS denial. Both
fixture timestamps now use `statement_timestamp()`; the exact 42501 assertion is
retained. This correction requires a new runtime result.

Supplier43628051315/job132548349130 failed the original full Staff lifecycle.
Conditional diagnostic job132555275876 also failed (at the injected root-failure
expectation). At 19:19:38 UTC, its aggregate function counters were:

| Function                              | Calls | Total ms |  Self ms |
| ------------------------------------- | ----: | -------: | -------: |
| workforce_operating_selection_link    |  1803 | 46232.78 | 45657.68 |
| workforce_employment_review_write     |    57 | 22794.18 | 21925.96 |
| workforce_lock_supervision            |  2146 | 14134.75 | 14134.75 |
| workforce_employment_review_complete  |    47 | 15487.27 | 12213.96 |
| workforce_posting_proposal_decider    |   143 | 50102.60 |  2364.74 |
| workforce_operating_selection_context |  1803 |   575.08 |   575.08 |

These are aggregate measurements, not exact request attribution. WF0118 is a
candidate response to the dominant selection-link self time: it returns false
before planning the Staff subquery when the private operating-selection purpose
is absent. When present, it retains the entire original authorization SELECT.
There is no authority-result cache, policy change, budget increase or weakened
lifecycle assertion. Before replacement, the migration requires the previous
integrity result to be exactly true; it then refreshes the identical policy and
function coverage. Pure regression tests and backend build/vet/lint passed.
The ordinary cold full lifecycle with tracking disabled remains mandatory.

The same candidate adds IAM's bounded, employer-scoped retained native-duty inbox,
SDK and management UI. Current employer management authority is required on every
read; current Staff eligibility is not a prerequisite to revoking a retained
record. The UI supports revocation and maker withdrawal, hides cached details on
source cutoff, and recovers an unresolved command before permitting navigation.
New real-owner runtime assertions cover paging, scope replay, missing authority,
audit failure and retained-duty revocation after Staff revision changes. Browser
coverage adds retained-duty navigation after revocation. Those runtime additions
remain pending CI; local SDK/UI tests and production shell build passed.

Hospital target/account selection and preparation UI, effective product-access
cutoffs, identity-owner deployment and real tenant/login journeys remain open.
No merge, live migration or deployment is claimed by this checkpoint.

Validation correction: the final broad local Workforce regex also selected five
existing database tests. They failed immediately because rootless Docker is
unavailable; no database/container started. This was an incorrect local selection,
not a successful runtime check. Those tests remain CI-only. The new pure
selection-plan regression is run separately with its exact test name.

### `0a255186` early runtime finding

The complete local shell unit suite passed: 228 files / 2011 tests. Exact-head
CI43635567911 Go job132571491664 failed the new retained-inbox setup in both
independent models. Creating its extra pending duty reused the current duty's
employer/location/subject/template tuple, so the existing
`iam_operating_duties_one_live_template` constraint correctly returned 409.
The fixture now explicitly asserts that duplicate conflict, then prepares its
paging record at the already-created second product-owned location using its
current version. All paging, scope, audit, cutoff and retained-revocation
assertions remain intact. Runtime acceptance of that corrected fixture is pending.

Clarification of the prior diagnostic: the failure reported in
`workforce_posting_root_failure_test.go:56` was the strict concurrent approval
success check after fault tests, not an expected injected-failure response.

CI43635567911 E2E passed. Its web job132571491930 completed workspace
lint/format/typecheck/unit tests/builds and emergency browser checks, then ran both
native operating journeys. At 20:21:21 and 20:21:53 UTC respectively, pharmacy and
supplier each passed proposal, independent review, lost-response GET recovery,
worker-acknowledged activation, revocation and the new retained-duty navigation.
The encompassing web step then failed on the same duplicate pending-duty fixture
at operating_inbox_test.go:41. This is positive browser evidence for retained
navigation, not a successful full job, corrected paging/audit/cutoff acceptance or
deployed identity/login acceptance. The ordinary full lifecycle in
Supplier43635567961 remains running with tracking disabled; its outcome must be
collected before pushing the test-only correction, to preserve that measurement.

### Ordinary cold Staff lifecycle passed at `0a255186`

Supplier43635567961/job132571491647 completed successfully. Its original
`TestPreEmploymentPersonReviewPreservesInactiveJoiner` step passed with function
tracking disabled; the failure-only diagnostic job was skipped. The same run
passed supplier grants, owner stock allocation/recovery, current Staff source
views, financial acquisition evidence/recovery, receiving corrections/custody
rollback and wholesale product identity checks. Original full lifecycle, injected
faults, transfer/concurrency assertions and five-second authority budgets remain
unchanged. This closes the previously failing ordinary cold lifecycle gate on
that production-code head. It does not close corrected inbox paging/audit/cutoff
acceptance, hospital native preparation, identity-owner deployment or live tests.
The queued correction changes only test fixtures and this evidence record.

### `4ca8fd59` hidden-scope error classification

CI43637629648 Go job132577837958 progressed past the corrected extra-duty
preparation and first inbox page, then failed the foreign-tenant plain read in
both employer models at operating_inbox_test.go:68: expected404, actual503.
`ErrGovernanceScopeHidden` was missing from the new inbox-only error mapper,
although the operating location chooser already classifies it as hidden404.
The correction adds that sentinel with `errors.Is`; source/dependency and audit
failures remain503. New direct and wrapped-sentinel regression cases first failed
locally with expected404/actual503, then passed with the mapper correction.
Pure IAM race tests, vet, lint, backend build, contractdocs check and inventory
check passed. The original foreign-tenant assertions remain unchanged and need a
new runtime result. No authority scope, identity rule or query was broadened.

Qualified hospital chooser contract/SDK work is in progress separately and is not
part of this error-mapping correction. It is not an implemented server endpoint
or a complete preparation flow.

### `d648852b` retained activation ordering

CI43638575226 Go job132580923254 passed inbox paging, account/tenant/kind replay,
missing authority, read-audit failure and source cutoff, then failed the final
retained-duty status assertion in both models. The fixture changed Staff revision
immediately after approval, while activation was still queued. The real projector
correctly revoked the duty when its acknowledgement detected the changed basis.
This is expected production compensation, not permission to accept a weaker
retained-duty test. The fixture now waits through the owner GET for actual active
acknowledgement before changing Staff. Its final inbox assertion is tightened to
exactly active, followed by the original owner revoke and exact version+1 result.
No projector is paused, replaced or privately seeded. No application timeout or
authority deadline changes; the new ten-second fixture wait establishes the
physical ordering being tested. Compile/vet/lint/backend build passed; corrected
runtime acceptance remains pending.

### Second ordinary cold pass and profiling retirement

Supplier43638575206 completed successfully at `d648852b`; supplier and API-client
jobs passed and conditional diagnostics were skipped. The unmodified original
full Staff lifecycle step ran20:43:36–20:59:48 UTC and passed with function tracking
disabled. Together with43635567961 at0a255186, this supplies two cold production-code
head passes without relaxed authority deadlines or lifecycle assertions.
Temporary function-stat collection, its opt-in test-server flag and failure-only
workflow job are now removed. Only the diagnostic defer is removed from the
original supervised-approval helper; its actual calls/assertions remain intact.
The normal lifecycle workflow command is unchanged. Pure testinfra checks and
compile/vet/lint/build are rerun; database runtime checks remain CI-only.

### Latest head reopens the lifecycle performance gate

At `9b8766ba`, supplier run 43640694304/job 132587399962 failed the
original full Staff lifecycle approval-first cutoff race. Approval returned 200
after 4.110 seconds; the queued cutoff returned 503 (`posting_source_unavailable`)
after 5.128 seconds. The preceding two cold passes do not establish a stable
repair. The original deadline and expected statuses remain unchanged; this
performance gate is open again. Evidence: `/tmp/helivanta-9b-supplier.log`.

CI run 43640694314 also failed Go/web retained-duty setup because the new
activation poll appended `?staff=` to the native duty GET. That route correctly
rejects filters with 400. The corrected fixture polls the canonical bare GET,
uses collecting assertions in the eventual callback and still requires the exact
duty to become active before changing Staff. Bootstrap compile-only checks,
vet, lint and backend build passed after that correction; runtime CI remains
required. This is a test correction, not a production route change.

### Qualified hospital Staff/account selector candidate

IAM0052 and Workforce0119 are now registered and composed through the production
bootstrap and test harness. The candidate native IAM chooser uses actual current
governance source, canonical reviewed link/digital generation and IAM employment
basis, identity-owner presentation outside transactions, and a final locked
recheck. Canonical wire fields contain only hospital, Staff, subject, name and
email; query/paging and SDK checks bind tenant, account and hospital. Neither
selection nor employment activation confers an operating role.

The existing real-owner person/employment fixture now continues through normal
login-link proposal and independent approval after its original zero-link
assertion. New cases exercise pending-link exclusion, exact eligible presentation,
hospital/tenant isolation, unauthorized discovery, identity failure/ineligibility,
IAM/Workforce read-audit rejection and recovery, and normal link revocation
during identity I/O. The last case also checks that identity I/O holds no target
lock. These database cases are authored and compile, but remain **unrun locally**
under the no-local-stack constraint; CI must establish their runtime result.

Local validation: 19 selector SDK tests and the complete API suite (128 files,
1,436 tests) passed; API TypeScript and focused ESLint passed. The focused IAM
and Workforce selector tests passed with `-race`; bootstrap compile-only,
`go vet ./...`, `go build ./...` and `golangci-lint run ./...` passed (0 issues).
Contractdocs generation/check, canonical Staff model check and inventory
generation/check passed: **1,138 registered / 1,004 unclassified**, no acceptance
promotion. Existing module and fixture review pins were rechecked for the additive
composition and tests; the new route remains unclassified. Native preparation
UI, full lifecycle performance, identity-owner release and deployed acceptance
are still open. No main merge, live migration or rollout is recorded here.

### IAM policy-count correction

CI43644396375 E2E job132598904759 failed migration lint at `76143cec`:
`iam_employment_bases` had a second permissive policy. The selector migration
now alters the one canonical policy under an explicit selection-purpose CASE,
preserving its prior expressions when inactive; selection writes remain false.
The real-owner bootstrap fixture explicitly calls `LintRLS` after migration and
requires no findings. This failure was reproduced by CI before the correction;
SQL/PLpgSQL grammar passes locally (11 statements / 8 bodies), but runtime
migration acceptance still requires corrected-head CI.

### Operating integrity refresh and native preparation candidate

At `513c1927`, CI43644988816 migrated successfully but Go job132600716846
returned 503 from retained operating-duty setup and the first chooser read. The
IAM migration contained the assignment integrity refresh twice and omitted the
operating refresh, leaving prior operating policy hashes stale after new
restrictive policies. The correction retains one exact original-coverage refresh
for each. The bootstrap fixture now explicitly requires both prior integrity
functions and both IAM/Workforce selection support functions to return true.
These failures remain open until corrected-head runtime CI passes.

Native hospital preparation now selects named qualified Staff/account and current
department choices and uses canonical hospital proposal commands. Both owners
and current delegates can enter it; private account details disappear on source
loss. Submission freezes the exact request and navigation. A red regression
showed that validating future expiry again prevented an exact uncertain retry;
new-form validation retains the future-expiry rule, while immutable command
serialization preserves interval/reason/structure checks without a clock check.
Retry retains the ID and payload even after expiry; the server remains authority
for current validation and recorded-command replay.

Local evidence: focused UI94 tests passed, then the complete shell suite passed
**2,018 tests**. The complete API suite passed **1,437 tests**; TypeScript and
affected ESLint checks passed. Next.js production build passed with explicit
CI-only API/identity build settings after the first attempt correctly rejected
missing `API_URL`. Generated `.next` outputs were removed, retaining only cache.
No local app/server/database/browser was started. A Playwright case creates a
synthetic hospital through its API and checks the real qualified chooser's empty
state and absence of raw IDs within preparation; it type-checks but is CI-only
and has not run locally. Browser testing with a fully qualified Staff target,
deployed acceptance and lifecycle performance remain open.

### Exact-head selector support remains unavailable

CI43646235878 Go job132604666624 at `96f85d64` reports only the real-owner
person fixture as failing in the governance input-boundary command: the previous
assignment/operating integrity assertions pass, but IAM selector support is false
before business setup. This does not establish complete Go/runtime acceptance.
The fixture now distinguishes the selector's own integrity checks from its schema
guards and emits failing table-level RLS/force/TRUNCATE/purpose flags only when
support fails. Diagnostics contain schema names and booleans, not row data,
credentials or SQL payloads. No support guard, deadline or status assertion is
relaxed. `/tmp/helivanta-96f8-go.log` retains the failure evidence.
