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
