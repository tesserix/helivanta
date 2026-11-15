# Staff initiation, reviewed linking and pharmacy-access acceptance

Status: incomplete candidate; no merge or live rollout is established here.
This checkpoint preserves the full goal across the hospital-owned pharmacy and
independent pharmacy/supplier models. Source inspection, component checks,
service integration, browser testing and live acceptance are distinct evidence.

## Full coverage reproduced the race; generic-plan follow-up pending

At `6ed88933`, supplier43678325835 passed the unchanged full lifecycle in1,068.68s.
The web job passed all three native hospital/pharmacy/supplier Staff workflows;
E2E passed100 main-stack cases plus idle-timeout and degraded-dependency scenarios.
One existing live-issuer security-key case was skipped outside the live issuer.

Full Go race/coverage job132703350587 nevertheless reproduced the original child
chain cutoff/approval race. Both requests queued by270ms; the first completed200
at4.123s and the second returned503 at5.255s. Ten Staff statements consumed2,874ms,
with a longest389ms. The supervision gate's longest wait was3,864ms. The standalone
pass therefore does not establish a stable repair. No merge or deployment occurred.

Forward-only Workforce0121 configures only the two predicate functions introduced
by0120 to use generic internal prepared plans. The hypothesis is that parameterized
custom plans still require repeated planning during a cold command. Predicate
bodies, policies and current-context evaluation remain unchanged; no authorization
result is cached. Both full integrity definitions retain every condition and update
only the two helper fingerprints, including their function configuration.

The real-database regression now compares the119 prefix,120 custom-plan barrier
and121 generic-plan configuration; it retains owner/foreign rows1/0, write metadata,
forced RLS, lint and tamper/restoration assertions. Generic-plan timing improvement
and the unchanged full race/coverage gates remain pending. Original request budgets
and assertions are unchanged.

## Staff predicate planning migration candidate; validation pending

The paired plan job at `d80ead99` disproved the JIT hypothesis. With JIT on and off,
PostgreSQL compiled zero JIT functions. Owner planning took268.532/270.468ms and
execution12.981/12.488ms; foreign planning271.442/269.614ms and
execution11.588/11.123ms. Both modes retained one owner row and zero foreign rows.
The transaction-local JIT setting has been removed from the candidate.

Forward-only Workforce0120 now retains the exact prior canonical Staff read and
locking-read predicates inside two typed invoker functions, allowing their internal
query plans to be reused across the command's bounded reads. It keeps the outer
canonical tenant-visibility guard, every existing write check, policy role/command
and forced-RLS setting. Both earlier integrity functions retain their complete
checks and gain the new helper fingerprints. No older migration is edited, and no
business data is changed.

The paired real-database test first creates an actual Staff row using the ordinary
hospital/access/Staff APIs against the complete prefix schema, then captures owner
and foreign plans. It applies0120, verifies unchanged write metadata and clean RLS
lint, compares cold/warm plans and row admission, and tests fail-closed tampering and
exact restoration of both new helpers. The unchanged full original lifecycle,
including the observed term-expiry failure, still must pass.

At `adb76682`, plan job132698264151 passed. Owner planning/execution changed from
377.030/13.258ms to132.134/146.825ms, with warm129.858/141.005ms. Foreign
planning/execution changed from376.762/12.445ms to128.162/4.260ms. Row admission
remained1/0; write metadata, startup RLS lint, forced RLS and both helper tamper and
restoration checks passed. The combined owner time improved in this fixture;
this does not establish the full lifecycle request budget or live readiness.

That same head failed the bootstrap governance test: an allowed employment
preparation search returned503. Five new governance schema checks still inspected
only direct policy text after0120 moved it into helpers. The follow-up resolves
those definitions through a shared catalog projection only after both complete
selection integrity checks pass. It keeps the original checks on unwrapped
schemas and leaves write expressions unchanged. API regressions alter each helper,
require owner and delegate searches to return503 without Staff data, restore exact
definitions and require positive search again. Corrected-head runtime validation
remains pending; the previous positive regression must pass unchanged.

## Earlier JIT hypothesis and measured failure

At `ed832b58`, [Supplier43671446518](https://github.com/tesserix/helivanta/actions/runs/43671446518)
reproduced the original lifecycle failure. Both queued commands returned503 at
5.067s and5.179s. Safe phase timing recorded1,618ms in scope authorization,
1,409ms in supervision-source validation and1,949ms in decision persistence.
Across that race eight Staff statements consumed2,483ms, with a longest statement
of464ms; separate source/chain/review statements each consumed384–480ms. Earlier
Staff statements reached469ms and summed to3,563ms. There was also an unchanged
term-expiry acceptance failure: its request did not reach the expected audit lock
boundary. Both failures remain required gates.

That earlier experiment turned JIT off transaction-locally to test compilation
cost. Its paired `EXPLAIN (ANALYZE, BUFFERS)` job disproved the hypothesis, as
recorded above, and the setting was removed. The paired check logged only
operator/count/cost/timing metadata and preserved owning-tenant row admission and
foreign-tenant denial. Authority budgets and original lifecycle assertions stayed
unchanged.

At that preceding head, web and E2E jobs passed, including native hospital,
pharmacy and supplier Staff browser workflows. The full Go coverage job was still
active at last inspection. Local validation of the execution-setting candidate:
Workforce package compile-only, full Go vet/build and GolangCI-Lint0issues passed;
workflow Actionlint and whitespace checks passed. No local application, database,
container or browser stack was started.

## Passing original cold suite; intermittent timeout still unresolved

At `ab00b02c`, [Supplier43669596367](https://github.com/tesserix/helivanta/actions/runs/43669596367)
passed every step, including the original full Staff lifecycle fixture in996.604s.
This head changes observation only. It is not a production concurrency repair and
does not invalidate the earlier503 failures. The successful run suppressed its
Go diagnostic logs because the command lacked `-v`; the workflow now retains
verbose output without changing the fixture, assertions or timeouts. The observer
registers callbacks before harness workers start and toggles collection atomically,
so collection does not mutate callbacks while background workers execute queries.

[CI43669596281](https://github.com/tesserix/helivanta/actions/runs/43669596281)
passed scripts and secrets, and its workspace lint/tests/build step passed; native
Staff browser runtime, E2E and the full Go coverage gate were still running at the
latest inspection. No overall CI success or stable lifecycle repair is claimed.

The [live acceptance run sheet](2026-11-15-live-staff-acceptance.md) is prepared
from current native UI labels. Its live results are pending; private test mailbox
mappings and credentials are excluded from this public repository.

## Latest native browser acceptance and unchanged lifecycle failure

At `1c488f79`, [CI43665142567](https://github.com/tesserix/helivanta/actions/runs/43665142567)
passed the web job, including the real native hospital, independent pharmacy and
supplier Staff browser workflows. Its E2E job also passed. The Go job was still
running its coverage gate at the latest inspection; no overall green CI is
claimed.

[Supplier43665142621](https://github.com/tesserix/helivanta/actions/runs/43665142621)
failed the original `activation_posting_chain_child` lifecycle race. After both
commands queued, approval completed200 at4.143s and the cutoff returned503 at5.142s.
The fixture now observes statement timings only during these original races to
separate query time from locking and transaction completion. Output contains only
fixed operation classes, counts, failure flags and durations; it contains no SQL,
parameters, row values or credentials. Production code, the five-second deadline,
full fixture and expected200/409 responses remain unchanged. This is diagnostic
instrumentation, not a claimed concurrency fix.

The separate identity owner [PR42](https://github.com/tesserix/onboarding-service/pull/42)
was merged as `748ac8a571ad8cd35bc41819a41119bcb886cca9` after all checks passed and
released through Kargo. Both onboarding replicas were ready on digest
`sha256:2dab467c85db92af1c764c1721a622ee61770f2d4c188b33484643c44660ff70`, with Argo
Synced/Healthy. Public API checks against the existing Yes Hospital owner passed:
fresh challenge202, code verification200, account choices200 twice with exactly
one Yes Hospital account, and no-proof lookup404. Scoped OIDC authorization reached
the password page, and the password in the approved file was accepted200 with
the expected TOTP requirement. Full session verification awaits the owner's
current authenticator code; full browser login remains unverified. No account,
credential, tenant data or grant was changed. The Helivanta candidate remains
unmerged and undeployed.

## Successful qualified API checks; remaining release gates

At `88974167`, [CI43661273089](https://github.com/tesserix/helivanta/actions/runs/43661273089)
passed the complete Staff governance input-boundary step, including healthy empty
choices, positive reviewed Staff/account qualification, foreign membership denial,
source change/revocation checks and the actual-owner shared-lock/mutation-refusal
regression. Its E2E job passed. Go later failed the old IAM migration-count test
(still50 despite the registered0051/0052 additions). That test now expects52 and
explicitly checks both new migrations, preserving all prior migration hashes.
The exact pure test reproduced red locally before correction and passed green.

The web job passed workspace quality/tests/build and the original independent
owner browser scenarios. Its new hospital browser fixture failed before navigation
because it configured an unknown Global API destination `organization`.
The fixture now supplies only the registered `platform` and `workforce`
destinations required by its real handlers. It keeps the Global API routing,
source authorization, response-loss replay and independent review assertions.
Actual hospital browser runtime acceptance was outstanding at that checkpoint;
see the later exact-head evidence above.

[Supplier43661273193](https://github.com/tesserix/helivanta/actions/runs/43661273193)
failed the unchanged original posting-cutoff race with `posting_source_unavailable`
(503 where200 is required) after both commands reached their PostgreSQL waits.
One observed lifecycle command completed200 in1.47s. The five-second public
budget is unchanged; this intermittent cutoff failure remains unresolved and
must not be hidden by a timeout increase, skipped test or one previous cold pass.
There is no HMS merge/rollout or complete fulfilment/Staff acceptance claim.

The existing-owner sign-in was attempted normally through email discovery. Code
verification succeeded, followed by account-choices503. Read-only checks using
the existing onboarding workload credential and owner database permissions found
one active verified native account, active matching org/tenant metadata, project
and current grant, and matching canonical store records. No credential value,
private identity ID, proof, cookie or email code was emitted or committed. No
identity, grant, tenant, data or credential was changed. At that checkpoint sign-in remained under investigation and the password step
had not been reached. See the identity-owner release and live API evidence above
for the subsequent recovery; no password value is retained here.

## Current acceptance target and successful healthy empty reads

The user directed acceptance against the existing **Yes Hospital** tenant and
its already-approved owner account. Do not create a replacement hospital tenant,
reset the owner credential, delete retained data or infer that a legacy identity
mapping is valid. Normal sign-in and live authorization still need verification.
The earlier fresh-tenant run sheet is historical; it is not authorization to
replace this selected acceptance target. Credentials stay in the approved local
secret file and are never copied into documentation or logs.

Candidate `c81eb52e` passed the healthy empty chooser request (200), the
app-role capture-privilege failure (503), and recovery after revoke (200).
[Go job132647316697](https://github.com/tesserix/helivanta/actions/runs/43660115536/job/132647316697)
then failed the independent foreign-member check: expected hidden-scope404,
actual403 `not a member of this tenant`. Its IAM membership row existed, but the
fixture's real OpenFGA checker had no matching TenantB role/member relation.
The fixture now grants only TenantB's ordinary Staff role/member relation,
matching its existing IAM row. No foreign hospital, department, employment,
login-link or operating access is granted. The foreign assertion remains404;
the separate stale-header assertion remains409. These negative checks run in an
independent subtest so a refusal mismatch cannot suppress later positive tests.
Positive linked qualification and hospital browser acceptance remain unproven
until corrected-head CI reaches them. No deployment or full-flow acceptance is
claimed from these successful empty reads.

## Current-main integration checkpoint

Main advanced to `7f85a42e` (pharmacy tax-command replay, PR #1717) while
`4bd9151e` was queued. GitHub reported a merge conflict, so PR CI did not start
for that head; only image builds started. Integration preserves both Pharmacy
`0047_pharmacy_operating_location_choices` and main's
`0048_pharmacy_tax_command_identity` in migration order. Both conflicted
inventories were regenerated from canonical source; no main business change was
replaced or omitted.

After integration, local workspace quality checks passed 21/21 tasks; workspace
unit tests passed 7/7 tasks; the shell production Next.js build and AmbulanceConnect Expo export passed
2/2 build tasks (the unchanged Expo task used its cache). Explicit CI-only
API/identity settings were used for the shell build. Generated shell `.next`
and AmbulanceConnect `dist` outputs were removed. The shell suite passed
2,023 tests across 229 files. Focused IAM and
Workforce race tests, compile-only bootstrap, full Go vet/build/lint (0 issues),
contractdocs generation/check, canonical Staff generator and inventory checks/tests
passed. No local app, DB, container or browser test stack was started. The
existing Chrome login was inspected read-only through native accessibility;
email-based discovery is present and no credentials or tenant data were changed.
The integrated-head runtime checks remain required before merge or HMS rollout.

## Qualified locking-read correction

Candidate `c4bfef80` passed exact restoration checks and Workforce schema support.
[Go job132639901854](https://github.com/tesserix/helivanta/actions/runs/43657664728/job/132639901854)
then reached candidate qualification and returned404 (previously503) at the first
empty-page assertion. The read owner logged `phase=candidates`, without an
authorization-unavailable or guard mismatch/closure flag. No successful public
chooser response is claimed yet.

Source inspection identified the canonical `partner_candidate_update_guard` on
Staff/login links: PostgreSQL applies UPDATE `USING` policies to `FOR SHARE`.
The adjacent operating chooser explicitly admits its narrow read predicate there;
assignment selection had not. Workforce0119 now adds the same bounded selection
read predicate to exactly those two existing guards, retaining their explicit
prior `WITH CHECK` and rejecting missing guards/checks during migration. The
selection's restrictive `WITH CHECK=false` for business records and independent
DELETE guard remain unchanged. All old behavior is retained outside this purpose.

A new real-database regression obtains the actual current IAM owner source and
Workforce topology, installs the derived short-lived selection proof, locks the
reviewed Staff/link rows, and attempts updates to each. Both attempts must fail;
savepoint recovery confirms the original versions and restores purpose settings.
It adds no grant, membership, employment or stock seed and does not impersonate
the target. Canonical source ownership, public five-second deadlines and foreign
scope assertions are unchanged. SQL/PLpgSQL grammar passes (14 statements,
8 bodies); the real locking-read and mutation-refusal result still requires CI.

## Reversible missing-policy fixture repair

Candidate `fc073aa5` identified one changed object in
[Go job132636773703](https://github.com/tesserix/helivanta/actions/runs/43656653249/job/132636773703):
`workforce_staff/employment_governance_purpose`. The first failing restoration was
`missing_private_purpose_denies_before_disclosure`: dropping and reconstructing
that policy from its deparsed expressions changed its canonical fingerprint from
`b5524ac6499281fa4265d55c23d12890` to `11120fac27c0a76460951cf5a3e78ab3`.
Later trigger-cleanup comparisons inherited that same single difference. No
production policy defect or exact expression-level cause is inferred from the
hash comparison.

The missing-policy metadata test now temporarily renames the original policy,
asserts that the required canonical name is absent, and still requires HTTP 503.
It then renames the same original object back and checks every original policy
and function fingerprint. This tests absence of the required named policy while
preserving its OID, command, roles and predicates across cleanup; it does not
physically remove the renamed restrictive predicate during the request. The
application's policy-existence/integrity checks, public assertions and deadlines
are unchanged. This avoids reconstructing trusted metadata and never recaptures
an altered schema as trusted. Failure diagnostics now compare one schema key at
a time rather than printing the entire snapshot. Corrected-head runtime evidence
is still required for the chooser and hospital browser journey.

## Exact Workforce schema-support diagnosis

Candidate `87e25e99` reached the same empty chooser failure. Its owner diagnostic
in [Go job132633393419](https://github.com/tesserix/helivanta/actions/runs/43655573636/job/132633393419)
identified `phase=schema_support`, `authorization_unavailable=true`, with no guard
scope mismatch/closure. Startup support was true. This narrows investigation to
schema support after the fixture's policy/trigger fault injections; it does not
prove that qualification or identity lookup has run successfully.

The real-service fixture now snapshots canonical Workforce policy and function
hashes after migration, checks exact restoration after each policy/trigger
restoring cleanup, and checks again before the chooser. Failure output contains
schema object names and hashes, without database row contents, identity or secrets.
The support/fingerprint checks are unchanged. Corrected-head CI must identify the
remaining difference before any production policy change is considered.

## Identity owner release and remaining chooser failure

Identity PR #39 merged as `620d05531794e89b59ca9e5b57e00c3f8dbc6b30` after
current-main candidate `cd51b3d` passed all PR checks. Main
[CI43654647655](https://github.com/tesserix/onboarding-service/actions/runs/43654647655)
passed tests, image publication/signing and deploy advancement. Read-only GKE
inspection on 2026-11-14 UTC confirmed both `onboarding-api` replicas ready on
that exact release, digest
`sha256:fd220eb8086f40aaa752176106653e23a56cddbec11c1c85194b84a3a4bee8ed`.
Account `unidevidp@gmail.com`, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `onboarding`.
No database migration or imperative workload mutation was used. Deployment
health does not prove candidate account disclosure through the HMS consumer.

HMS candidate `684c3588` passed the corrected foreign-membership fixture and
reached the first empty chooser assertion, but returned 503 instead of 200.
[Go job132630120444](https://github.com/tesserix/helivanta/actions/runs/43654525148/job/132630120444)
logged `phase=workforce_candidates`, 27ms, no SQLSTATE/deadline/cancellation.
The failure is still unresolved. The candidate adds fixed owner read phases
and separates candidate-shape validation; it never logs raw errors, Staff/account
values, proof, SQL, cookies or credentials. A pure diagnostic regression failed
before the logging change. Further corrected-head runtime acceptance is required.

## Current checkpoint — 2026-11-14 23:41 UTC

Candidate `866c3eb7` includes current main `07af22ab`. Local full workspace
quality checks (21 tasks), tests (7 tasks; shell 2,020 and API 1,437 tests),
both production app builds, Go build/vet/lint and generator checks passed.
Generated app build outputs were removed; no local app, DB, container or browser
was started. Both candidate image builds passed.

[CI43653934690](https://github.com/tesserix/helivanta/actions/runs/43653934690)
reproduced a fixture setup error at `staff_governance_person_test.go:96`: a
TenantB membership was inserted in a TenantA transaction, correctly rejected by
forced RLS with SQLSTATE 42501. The fixture now uses separate exact-tenant
transactions; production policy and foreign-tenant HTTP denial assertions are
unchanged. The existing real-service test is the red reproduction; corrected-head
runtime acceptance remains required. E2E, web and the full supplier workflow are
still running. No positive canonical chooser qualification or hospital browser
acceptance is claimed from this run.

The identity dependency now includes current owner main `918378d`, preserving
its restored strict tenant-owned identity boundary. Candidate `cd51b3d` passed
local pure tests, compile checks, build/vet/lint, contract and delivery tests;
[CI43654282927](https://github.com/tesserix/onboarding-service/actions/runs/43654282927)
is running. It remains draft/unmerged/undeployed. Older checkpoint entries below
record historical evidence, not current completion.

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

### Preserve full audit fingerprint across migration staging

CI43647206769 Go job132607715822 at `d2439bc9` confirms
`IAMIntegrity=false` with Workforce, prior assignment and prior operating
integrity all true, and no failing table-level support flags. Static source
verification identifies Workforce document-worker and credential-reminder
migrations changing `audit_events` after IAM0052's initial capture. The correction
preserves all policy coverage, including the canonical audit policy, through an
IAM-owned schema capture function. IAM invokes it after refreshing prior
integrity; Workforce0119 invokes it after the legitimate shared audit changes.
Both prior governance integrity checks must be true before capture.

The capture function runs with invoker rights and PUBLIC EXECUTE is revoked.
Runtime selector support also requires that its application role cannot execute
capture. The real-owner fixture requires this protection, grants EXECUTE to the
test application role and expects qualification to fail closed, restores the ACL
and expects normal discovery again. This is metadata migration staging, with no
business rows or authority cache. Normal bootstrap places Workforce after the
other registered audit-policy writers; future writers must maintain that contract.

Local grammar checks pass (IAM13 statements/8 PLpgSQL bodies; Workforce14/8).
Bootstrap compiles and the selector race tests, backend vet/build/lint and
contractdocs are rerun. Corrected-head database and browser CI are still required;
the complete workflow is not accepted or ready for main/deployment.

### Cold supplier pass and concrete chooser scan regression

At `3450485a`, supplier43648210751/job132610698980 passed the entire
workflow, including effective consent, real-owner allocation/recovery, fulfilment,
receiving corrections, financial acquisition and the complete original Staff
lifecycle. The unchanged Staff recipient/membership-cutoff step ran
22:32:51–22:49:42 UTC on 2026-11-14. Function tracking remains removed; original
deadlines, fault/concurrency cases and status assertions are unchanged. This is
one current cold pass, not a deterministic repair claim for the earlier
intermittent cutoff failure. Web job132610787023 also passed, including the
independent pharmacy/supplier native browser journeys.

CI43648210819 Go job132610786937 now passes every startup integrity/support
assertion, then the initial chooser request returns 503. E2E job132610786820
passed97 cases but failed its new empty chooser case with the same503. A pure
GORM schema regression reproduced a concrete failure before any SQL: scanning
`AssignmentTargetCandidate` treats its private `Reference` as an invalid relation.
The corrected scan uses only canonical Staff ID/creation columns, then attaches
qualification evidence in the owner port. The regression failed before the fix
and passes afterward with `-race`; no local database was started.

The existing exact-decision UI regression also exposed an enabled Close form
after an uncertain response. Its new assertion failed before navigation/close
guarding and passed afterward. All94 affected UI tests, shell TypeScript and the
production build passed; generated outputs were removed, preserving cache.
New selector failure logs expose only fixed phase, elapsed time, deadline flags
and SQLSTATE. A red/green pure test verifies private DB message/detail never
appears in logs or public responses. Backend compile/vet/build/lint pass.

The foreign-tenant fixture now supplies the foreign account's real expected-tenant
header and initial membership; stale expected-tenant rejection remains a separate
409 assertion. Actual foreign resource access still must return404. No assertion
was broadened. The identity lookup counter is atomic for concurrent browser reads.

A new real-owner hospital browser helper runs only under the existing CI browser
environment. Starting after normal reviewed linking, it exercises qualified Staff
selection, committed/lost proposal and decision responses, exact retry,
independent owner review, canonical activation and native history through Global
API. The existing pharmacy/supplier browser command remains unchanged; the same
CI step additionally runs the original real-owner person fixture. Its identity
boundary remains synthetic and it does not establish deployed login acceptance.
The browser helper is authored/syntax checked, not run locally. Corrected-head
runtime acceptance is required before this gate closes.

Source verification of the canonical protected-generation reader also found that
the private selection purpose named a nonexistent evidence-draft relation and
omitted the document creation-provenance rows required by person/employment
state checks. It now permits the actual evidence bindings/history only at the
selected consumed binding/version/revision and creation-captured contributors
only for selected evidence files. Unused audit-history reads are denied;
qualification audit insertion remains separate. This preserves the canonical
generation checks rather than replacing them with a weaker local eligibility
rule. SQL grammar passes; real qualified-target acceptance remains CI-only.
