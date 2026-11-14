# Staff initiation, reviewed linking and pharmacy-access acceptance

Status: incomplete candidate; no merge or live rollout is established here.
This checkpoint preserves the full goal across the hospital-owned pharmacy and
independent pharmacy/supplier models. Source inspection, component checks,
service integration, browser testing and live acceptance are distinct evidence.

## Candidate and authoritative evidence

- HMS PR: [#1669](https://github.com/tesserix/helivanta/pull/1669).
  Native UI head: `89605942`; approval diagnostic head: `90950803`.
  Source-review correction `0c590133` records the file-level review; the later
  substep candidate extends that review without changing authority.
- Identity owner: [onboarding-service #39](https://github.com/tesserix/onboarding-service/pull/39),
  pinned source `20d240693ac04d779fec504720803a989ca6f5ce`.
  Its private assignment-candidate purpose is a dependency, not a deployed API claim.
- [CI43595306491](https://github.com/tesserix/helivanta/actions/runs/43595306491)
  at `89605942`: web/scripts/secret checks passed; browser job `132448498978`
  passed all three phases. Main browser suite: 99 passed and one skipped;
  subsequent workflow phases: one pass each. The two native operating-page
  cases cover bounded employer discovery and denial without private requests;
  they do not exercise a complete native Staff-to-duty browser journey.
  The Go job was cancelled by the subsequent push; it did not pass.
- [Supplier43595306385](https://github.com/tesserix/helivanta/actions/runs/43595306385)
  at `89605942`, job `132448415183`: all stages through supplier grant projection
  isolation passed, including real owner allocation/recovery, financial
  acquisition and reviewed receiving corrections. The final current-hospital
  Staff lifecycle stage failed in `activation_posting_chain_child` at
  `workforce_posting_chain_test.go:135`: expected 200, received 503
  `posting_source_unavailable`. This is not a successful complete supplier run.
- `90950803` diagnostics passed local race-tested privacy/status cases, backend
  build, affected vet/lint, compile-only owner/gateway tests, contractdocs check
  and redacted secret scan. No authority budget, source predicate or expected
  lifecycle response was relaxed. Its web job found the source-review hash
  mismatch corrected in `0c590133`; this is separate from the posting failure.
- The live environment was inspected read-only on 2026-11-14 UTC: project
  `tesseracthub-480811`, cluster `tesseract-prod-in-gke` in `asia-south1`,
  application namespace `helivanta`, Kargo namespace/stage
  `kargo-helivanta/prod`. Kargo was Healthy/Verified, Helivanta Argo applications
  were Synced/Healthy, and workloads still used `0a1fad14`. Candidate runtime
  behavior cannot be inferred from the health of that earlier deployment.

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
      transfer persistence failure. Remove the temporary one-shot plan observer
      and verify the ordinary cold path. Re-run the entire original current-Staff
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

## Current next action

The first WF0117 CI attempt at `5b2bb8a5` did not establish a lifecycle result:
the staged legacy-upgrade fixture applied WF0117 in its early batch before its
deliberately deferred WF0080/WF0081 prerequisites. Later WF0080 failed because
the replacement function already existed. Both fixture partitions now defer and
apply WF0117 with the complete posting batch in module order. WF0117 also fails
explicitly when its existing guard prerequisites are absent. Production module
order remains unchanged; original test assertions and authority budgets remain.
The next CI run must execute the complete lifecycle before any repair claim.

Supplier43614580558 at `4470fb2a` passed child approvals and coordinated transfer,
then failed a cutoff/approval race with expected 200/actual 503. The aggregate
function sample showed root approval context 54 calls, 8463ms self/21925ms total;
chain approval context 36 calls, 5633ms self/14781ms total; proposal decider 210
calls, 2369ms self/45584ms total. The head trigger's own time was only 6.86ms across
58 calls. This identifies root/chain guard work as material execution cost; it is
not a per-request timing attribution.

WF0117 is a controlled planning candidate for those two context functions. It
uses stable invoker PL/pgSQL, fast-denies an absent matching private purpose and
preserves each complete original authorization SELECT byte-for-byte for the
valid-purpose path. Prepared plans can be reused; authority results are never
cached. Current source/tenant/actor/version/time checks and five-second command
budgets remain intact. PostgreSQL references and migration/source review are in
the posting source-review document. The function timing option/helpers are
removed so the next full lifecycle run is ordinary cold acceptance, including
all original transfer faults and concurrency expectations. No live migration is
authorized or applied by this change; scratch migration/runtime behavior is CI-only.

At `4470fb2a`, both native browser branches reached every assertion through
acknowledged activation and HTTP/UI revocation, but the job failed after success
because closing a context disposed an in-flight background route. Local commit
`2ae3319b` makes cleanup await in-flight handlers with `unrouteAll({behavior:
"wait"})` and does not suppress errors. The next CI exit must be clean before
this is counted as a passing journey. Standard E2E passed that head.

The private hospital-selection helper still has no owner query, route, SDK or
UI: those gates remain open. Deployed identity/login, actual product-operation
access, source cutoffs and full receiving/financial acceptance remain required.
No main merge or live rollout occurred.

### Diagnostic correction after Supplier43605854860

At `9def90fd`, Supplier43605854860/job132480212302 failed at the first
current-destination approval in the coordinated transfer scenario, after the
initial child approval. It reached `chain_evidence` at 5016ms overall (2392ms
persistence, 923ms current operation). There were no observer records: the
observer matched string bindings only, while canonical proposal IDs are passed
as `uuid.UUID`. A pure binding regression now covers both representations and
rejects unrelated IDs. This corrects test observation only; it does not repair
posting performance. The observer remains at the initial child approval and
still must be removed before accepting a cold workflow result. Web and browser
jobs passed at `9def90fd`; these do not close the lifecycle or selector gates.

### Native browser acceptance extension — pending CI

The existing real PostgreSQL/OpenFGA native-governance fixture now invokes
`scripts/verify-native-operating-owner.mjs` when CI supplies the built shell URL.
The web job explicitly runs both pharmacy and supplier fixture branches with that
URL; ordinary backend-only runs retain their existing API assertions. Browser
requests pass through Global API to the same real IAM, Workforce and product
owners. The identity boundary remains the fixture's explicitly synthetic identity
reader; this does not establish deployed Zitadel or normal signup behavior.

The browser prepares as the employer-scoped auditor and independently reviews as
the second owner, using the normal employer, reviewed Staff, location and template
controls. It deliberately drops committed proposal/approval responses, asserts
navigation freezes, and requires GET receipt recovery without another PUT. It
checks exact selected identity/location/version bindings, requires acknowledged
activation through the real projection worker and revokes the synthetic duty
afterwards. Legacy catalog, account-link and operating-duty route calls fail
the browser assertion. The browser now refreshes through the normal control until the acknowledged
active state appears, rather than requiring a transient pending state that can
race the real worker. Receipt recovery still checks the original immutable
approval; revocation uses the activated version. Product-operation access, source
cutoff, hospital selectors and the complete receiving/financial journey remain
separate gates. Runtime results remain pending.

Local checks for this test increment: compile-only bootstrap tests, bootstrap vet
and golangci-lint, JavaScript syntax, Prettier, actionlint, diff whitespace and all
15 inventory tests/check passed. No local server, browser or database was started.

### Browser runtime evidence at `4470fb2a`

CI43614580588/web job132506828260 reached every native journey assertion for
both pharmacy and supplier: bounded employer/Staff/location selection, proposal,
independent review, GET recovery after lost responses, acknowledged active state,
HTTP-confirmed revocation/version 4 and revoked UI state. Both branches printed
their success line. The job nevertheless failed when closing browser contexts:
a background Staff-choice route was still awaiting `route.fetch`, producing
`Request context disposed` after success. This is not a passing test exit.

The cleanup correction awaits `context.unrouteAll({behavior: "wait"})` before
closing each context. It waits for handlers and does not use `ignoreErrors`,
suppress route assertions or change business expectations. Syntax/format checks
pass locally; clean CI completion remains required. This fixture continues to use
synthetic identity boundaries and does not prove deployed login or product-operation
access. Hospital preparation and full live acceptance gates remain open.

### Existing-browser management-source cutoff — pending CI

The native-governance fixture now adds a late browser check for each independent
model, after its existing replacement-source/receipt tests. It opens the current
Staff workspace as the delegate, starts an unsent duty draft, and uses a separate
owner request context to revoke that replacement governance grant through the
normal Global API. The signed-in delegate refreshes Staff choices: the test
requires 404, no Staff/account details, no proposal control and no duty mutation.
It does not substitute logout or removal of ordinary membership for source denial.

The owner request uses an isolated context without the delegate's cookie. The
fixture uses the subtest lifetime for long browser/DB bookkeeping instead of its
short setup context; business request/authority deadlines are unchanged. Real
identity/login and operating product-access cutoff remain separate requirements.
Compile-only/bootstrap vet/lint, script syntax/format and clean CI execution are
the relevant checks; no local browser, application or DB is started.

### Checkpoint at `87e605b9` and department-selection candidate

CI43619827672 web, E2E, scripts and secret scan passed. In particular, both
independent native browser journeys now exit cleanly after acknowledged active
state and revocation; their synthetic external identity boundary still excludes
normal deployed login and product operations. The Go job has not returned a
passing result at this checkpoint.

Supplier43619827649 failed the original complete Staff lifecycle at
`workforce_department_transfer_test.go:49` (initial destination approval): expected
200, actual 503. Failure diagnostics show 5018ms overall, 1512ms persistence and
315ms in chain evidence; about 3506ms was spent before persistence. WF0117 has not
resolved the full cold-path deadline. No authority deadline or expected response
was widened.

The candidate now includes Organization's qualified department-choice owner API,
private read-only purpose, canonical-model SDK and explicit real-owner CI test.
Local SDK suite: 126 files/1403 assertions passed; Go race-tested input boundaries,
compile-only bootstrap, affected vet/lint and backend build passed. Contractdocs
caught and prevented a route-registration panic from a missing domain filter;
the corrected registration binds the fixed active status and rejects other status
values. Real database acceptance and native target/account preparation UI remain
open. No live migration, main merge or deployment is asserted.
