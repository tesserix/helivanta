# Staff Management workflow acceptance

Current audit: 2026-10-28. Summary status and the ordered Access/RBAC checklist are maintained in [the delivery tracker](staff-management-progress.md), updated 2026-10-29. Scope is every Staff navigation entry and its complete
business workflows, including account provisioning/verification/offboarding,
staff identity/background verification, protected photographs and identity-card
issuance, supervision, review and payroll. This checklist records evidence, not readiness
inferred from a visible screen or passing schema test.

Work one menu area at a time. The user selected **Staff profiles and supervision**
next on 2026-10-29; Access/RBAC remains incomplete and tracked separately. Continue
through the ledger without treating that priority change as acceptance of prior gaps. A dependency in another area can be developed/tested as part of
the current journey, but is not a substitute for that area's own acceptance.
Every open item remains part of the full Staff Management goal.

## Acceptance gate for every workflow

- Identify its actual issue acceptance criteria, canonical contract, data owner,
  UI route, backend handler, migration/RLS policy, events and downstream consumers.
- Exercise every supported create/read/edit/verify/approve/reject/withdraw/
  revoke/renew/inactivate/reactivate/delete/import/export action. Mark unsupported
  required actions as gaps; do not invent a successful equivalent.
- Use synthetic data through the UI as the actual Staff Admin and a separate
  reviewer/employee. Keep local test fixtures and live evidence distinct.
- Verify tenant, hospital, department/target and subject isolation on the server.
  Check permitted actors, read-only actors, another tenant/hospital, linked self,
  original filer, revoked/expired authority and inactive employment.
- Check required fields, invalid and duplicate values, effective dates, stale
  versions, simultaneous changes, identical replay, uncertain outcomes and
  dependency failures. Verify persistence, audit/history and downstream effects.
- Verify loading, empty, failure, pagination/filter boundaries, keyboard labels,
  validation focus, retries and accessible success feedback.
- For deletion, establish the owner's actual retention and deletion contract.
  Distinguish login disablement, access revocation, staff inactivation and permanent
  deletion. Verify that offboarding terminates access without erasing required
  business history. Permanent live deletion needs its named approval.
- Run relevant real-DB, API, client, component and browser tests, builds and lints.
  Release through the owning GitOps path and verify the deployed behavior.
- Close/label a GitHub issue as end-to-end verified only when its entire acceptance
  scope has this evidence. No broad issue is complete from one successful form.

## Ordered area ledger

All areas below remain incomplete in this audit. Earlier implementation or CI
evidence is a starting point, not proof of the full live workflow.

| Order | Area / route                                           | Workflow coverage required                                                                                                                                                                                                                                                                                                        | Current evidence / gaps                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ----- | ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | Staff access `/workforce/access`                       | Identity creation and credential verification; tenant membership; reviewed staff/login binding; scoped assignment proposal, independent approve/reject, withdrawal, activation, expiry, revoke/renew; offboarding, reactivation, access removal and recovery                                                                      | Current deployed access checkpoint is e0a9e6a, including renewal preparation and membership workspace guards. Migration job and six deployments succeeded; owner Staff/payroll reads and restricted reviewer denials passed. Positive maker/checker/recipient renewal and scoped-review acceptance are local. Local ordinary-request identity disablement, old-cookie denial, reactivation/fresh login and reviewer isolation are verified (see current-identity checkpoint). Wider account/lifecycle coverage and the production reviewer password handoff remain open. #417/#1053/#1132/#434                                                                                                                                                                                                                         |
| 2     | Staff directory `/workforce`                           | Employee/consultant creation; proper staff/employee IDs; identity/background checks; protected photo and ID-card issuance; profile, contacts, qualifications, experience, credentials and private files; supervisory relationships; status changes, transfer, exit, reactivation; account linking, import/export and safe removal | Local admin profile editing, conditional validation, reload persistence, optional qualification removal and stale-tab recovery verified. Supervision database/concurrency guards are included in deployed e0a9e6a. Context protection in PR #1447 passed local admin clear/assign/replace/reload, circular-chain refusal, two-tab draft/reload handling, inactive-choice labels, restricted reviewer denial and old/mixed-owner rejection; exact persisted profile/version/write-audit state was retained after the mixed-version refusal. Identity/background/photo/cards (#1399), full lifecycle, related-role edit coverage and safe staff/account removal remain open. #1035/#417/#617/#1105/#1107                                                                                                                 |
| 3     | HR setup `/workforce/setup`                            | Every master type, code/name uniqueness, dependent references, versions/history, effective dates, activation/inactivation and referenced-record removal                                                                                                                                                                           | Local department checks plus all eight other categories now cover creation, immutable code, retirement/reactivation and retained history. Designation/grade assignment and retirement preserve pinned enrollment revisions through unrelated edit and reload. Language, exit-reason and contract-type consumers merged in #1403; repeatable qualifications merged in #1405. Emergency relationships merged in #1407 after local schema/API/UI compatibility QA and all exact-head CI; deployed QA remains pending; document-category sidecar metadata merged in #1411 after every exact-head CI check and local owner/reviewer UI QA; deployed acceptance remains pending; legacy fallback, scoped-role negatives, further duplicate/concurrency/isolation cases and deployed QA remain open. #1035/#22/#287/#616/#429 |
| 4     | Shifts `/workforce/shifts`                             | Day/night/overnight definitions, duration/break/grace/locum rules, changes/history, referenced shift inactivation and invalid timing                                                                                                                                                                                              | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 5     | Roster setup `/workforce/roster-setup`                 | Areas, groups, weekly templates, composition, applicability, version/effective-date rules and referenced-record removal                                                                                                                                                                                                           | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 6     | Roster policy `/workforce/roster-policy`               | Settings, staff/template assignments, effective dates, conflicts, history and inactive/transferred staff behavior                                                                                                                                                                                                                 | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 7     | Rosters `/workforce/rosters`                           | Draft creation/population, allocations, overnight/DST windows, locum/additional duty, clashes, credential/leave checks, freeze, correction and discard                                                                                                                                                                            | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 8     | Leave setup `/workforce/leave-setup`                   | Leave types, calendars and structures; all rules, units, effective revisions, duplicates, inactivation and referenced definitions                                                                                                                                                                                                 | Not yet fully verified. #1037                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 9     | Leave balances `/workforce/leave-balances`             | Year settings, staff structures, allocation/accrual/carry/expiry, monthly posting, corrections, closing, ledger, balances and self-only reads                                                                                                                                                                                     | Not yet fully verified. #1037/#1132                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| 10    | Leave requests `/workforce/leave-requests`             | Filing and self-service, eligibility/probation, holiday/half-day/short leave, balance/overlap, independent approve/reject, cancellation/revocation, exact replay and roster/ledger consequences                                                                                                                                   | Scoped-only approval/replay, own-leave denial, link revocation, post-input withdrawal and target-bound RLS regressions pass in development. Post-probation eligibility is checked without enrollment endpoint access. Command-boundary guard and endpoint rollback regressions now pass locally; combined real-IAM Workforce integration and withdrawal/expiry lock races pass; reviewer scope discovery passes locally; queue/detail/UI now have merged #1393 and local maker/checker/reviewer evidence; this consumer is not deployed. Wider leave eligibility/cancellation/revocation cases and deployed employee/reviewer coverage remain open. #1037/#1132/#434                                                                                                                                                   |
| 11    | Leave reports `/workforce/leave-reports`               | Monthly/status reports, filters, date/year bounds, paging, totals, permission/purpose, absence/staleness and export privacy                                                                                                                                                                                                       | Not yet fully verified. #1037/#1018/#589                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 12    | Duty exchanges `/workforce/duty-exchanges`             | Propose, independent decision, holds/conflicts, cancellation/revocation, roster effects, duplicate/stale/concurrent requests                                                                                                                                                                                                      | Not yet fully verified. #1038                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 13    | Overtime `/workforce/overtime`                         | Policy/settings, filing, eligibility and units, approval/rejection, cancellation/reversal, history and payroll handoff                                                                                                                                                                                                            | Not yet fully verified; approved overtime is not payment. #1038/#1040                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 14    | Attendance `/workforce/attendance`                     | Punch/void, missing-punch corrections, independent decisions, local/overnight dates, period finalization/reopen, immutable timesheet revisions and payroll handoff                                                                                                                                                                | Not yet fully verified. #1039/#1040                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| 15    | Credential register `/workforce/credentials`           | Evidence, validity, independent review, expiry, rejection/revocation, renewal, private disclosure and roster consequences                                                                                                                                                                                                         | Not yet fully verified. Credential evidence never grants clinical privilege. #1108                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 16    | Credential reminders `/workforce/credential-reminders` | Reviewed reminder policy, durable scheduling, threshold delivery, duplicate/retry handling, acknowledgment and changed/expired credentials                                                                                                                                                                                        | Not yet fully verified against deployed workers and dependencies. #1108                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| 17    | Payroll setup `/workforce/payroll-setup`               | Every head/structure, salary assignment, bank/tax identifiers and reveals, statutory/tax policies, declarations/evidence, independent decisions; attendance/OT inputs; payroll preparation, lock/process/correct, payslips, reports, exports and payment/ERP reconciliation                                                       | Not yet fully verified. Reconcile implemented configuration with missing payroll execution/appraisal/exit workflows; previews are not a posted payroll ledger. Activate jurisdictional policy only with current qualified review. #1040/#1041/#1042/#1107                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 18    | Benefits `/workforce/benefits`                         | Dependents, relationship/plan applicability, effective coverage, independent proposal decisions, overlap, evidence quarantine/release, expiry/revocation and protected exports                                                                                                                                                    | Not yet fully verified. Dependent enrollment must not create a patient identity automatically. #1106                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 19    | Review leave `/workforce/leave-review`                 | Current scope discovery; bounded pending queue; protected detail; approve/reject; reject reasons; exact uncertain retry; stale/access/workspace states                                                                                                                                                                            | Merged in #1393 after full CI. Local independent maker/checker/reviewer browser flow, gateway/RLS tests and retained-history plans passed. Production migration rollout and fresh deployed UI acceptance remain open. #434/#1132/#1053                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |

## Disablement, suspension, resignation, termination and rehire

The product owner explicitly requires the complete lifecycle and its capability
boundaries. Use the [researched lifecycle design](../superpowers/specs/2026-10-27-staff-lifecycle-rnd.md)
for the actor/state matrix, owners, primary references, current implementation
evidence and synthetic acceptance scenarios.

- [ ] Distinct UI commands and authority for global security disablement, tenant
      suspension, hospital employment suspension, notice, resignation, termination,
      contract end, transfer, archive/privacy deletion, restoration and rehire.
- [ ] Existing cookies/bearer tokens, renewals, scope switches, cached graph
      decisions, private downloads/exports and queued approvals obey the effective
      deny boundary, including missed broadcasts and unavailable dependencies.
- [ ] Independent approval, effective dates/timezones, reasons, immutable history,
      stable command IDs, exact retry and concurrent transition rules are verified.
- [ ] Supervisors receive accountable pending-work/clinical handovers; future
      schedules, leave, duty/OT and attendance are reconciled without deleting history.
- [ ] Cards/property, identity/credential obligations, letters, final payroll and
      downstream acknowledgments have explicit owners and completion evidence.
- [ ] Leaving one hospital preserves independently authorized other scopes.
      Reactivation/rehire never restores old grants, cards or stale queued events.
- [ ] Former-staff documents, where permitted, require a separate self-only
      disclosure purpose and current identity authority. Security-disabled accounts
      receive no implicit self-service exception.

## Staff identity, background verification and identity cards

These are explicit Staff directory/onboarding requirements added by the product
owner. Existing employee codes, reviewed login links, credential evidence and
private document uploads do not establish completion of these workflows.

- [ ] A stable Workforce staff ID and properly scoped employee code identify the
      person. Verify uniqueness, allocation/edit policy, transfer, rehire and card
      reissue behavior without introducing a second staff master.
- [ ] Identity verification distinguishes submitted evidence, pending review,
      verified/not verified, expired and revoked results. Record the actual checking
      method/authority, evidence revision, reviewer, reason and dates. Email/account
      verification is distinct from verification of the person's staff identity.
- [ ] Background verification covers the check types actually configured by the
      organization, with applicable consent/purpose, authorized evidence/provider
      handling, review, disputes/corrections, expiry/reverification and failure states.
      Provider unavailability is not a successful check. Human reviewers make any
      consequential employment decision; do not infer automatic rejection rules.
- [ ] Staff can supply a protected photograph through the UI, with file type,
      byte/dimension limits, quarantine/scanning, authorized review, replacement and
      audited disclosure. The photo must be associated with the correct staff record
      and verified revision; a public asset URL is not access control.
- [ ] Identity-card issuance uses the authorized current staff identity, employee
      code and approved photograph. Verify preview, approval/issuance, card identifier
      and version, validity, reissue/replacement, loss, withdrawal/revocation and
      offboarding. Preserve issue history and distinguish card validity from login,
      hospital authority and clinical privilege.
- [ ] Test the whole journey with synthetic people and evidence through Staff
      Admin and an independent reviewer, including wrong-person evidence, duplicate
      identifiers, cross-hospital access, stale photos, rejected/expired checks,
      concurrent issue/revoke and unavailable dependencies.

Related existing trackers are #1035 (staff registry), #417 (staff accounts),
#1053 (onboarding/access reconciliation) and #1105 (private evidence). #298 covers
contractor background checks; it does not by itself cover verification of general
hospital staff. Doctor-registration verification is also a separate clinical
credentialing concern. The initial focused search found no dedicated identity-card story;
[#1399](https://github.com/tesserix/helivanta/issues/1399) now records the
photo/card workflow and its related identity-review preconditions. The identity
and background-review requirements above remain part of the directory scope.

Initial source search found employee-code handling and the existing private-file
and credential foundations, but no dedicated Staff identity/background/photo/card
workflow in the affected Workforce contracts and UI. Confirm each source gap and
the relevant issue acceptance criteria during the Staff directory slice. Do not
make a particular government identifier, background check or validity period
mandatory without the applicable organization/country policy and qualified review.

## Release and private evidence

Latest completed release for this audit: `a8af648722cc723e83ea63b87c92c0315155bbce`
(PR #1385), promoted after explicit approval of `0001_drugmaster` and
`0002_drugmaster`. Argo reported three applications Synced/Healthy; six workloads
had two ready replicas each, with the exact release tag and zero restarts. The
migration job succeeded; both migrations were verified applied. No catalogue data
was imported. Production auto-promotion remained disabled.

The synthetic reviewer is a tenant member only. Identity login and application
access are not yet verified. Account subjects, credential JSON, screenshots,
runtime evidence and release packets stay in the private `~/.helivanta/`
directory. Never put credentials, session tokens or personal/clinical data in
this public checklist or a GitHub issue.

Use [HR capability coverage](../hr-capability-coverage.md), the
[Workforce API](../api/workforce.md), [organization knowledge](../domain/organization/README.md)
and actual GitHub issue criteria to expand each row into concrete assertions.
Update this ledger with the observed result and remaining gaps after each area.

## Local command-guard verification — 2026-10-27

The scoped leave decision now joins the IAM guard's transaction. A failed command
rolls back the decision, ledger consumption and success audit. IAM verifies fresh
credential revocation, current assignment/membership/epoch state and expiry while
holding owner locks through product commit. This does not establish complete
session disablement or employment offboarding.

Commands run from `backend/`:

- `go test -race ./pkg/tenantdb ./pkg/authz ./internal/modules/iam ./internal/modules/workforce`
  passed: tenantdb cached, authz 7.085s, IAM 235.770s, Workforce 313.608s.
- `go vet ./pkg/authz ./pkg/tenantdb ./internal/modules/iam ./internal/modules/workforce`
  and `go build ./cmd/api` passed.
- `golangci-lint run ./pkg/tenantdb/... ./pkg/authz/... ./internal/modules/iam/... ./internal/modules/workforce/...`
  passed with zero issues.
- `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check` passed.
- `git diff --check` passed.

Failing-first evidence covers missing IAM guard, revoked credentials entering the
command, request-local legacy fallback after guard failure, a Workforce command
reporting success despite a failed guard, and assignment expiry returning 500.
Private logs retain those failures and passing reruns. The combined real-IAM
Workforce gateway test now verifies withdrawal/expiry lock races and credential
cutoff even when middleware misses invalidation. Reviewer discovery/queue/UI and
live two-person QA remain open.
No new commit, merge, migration or production rollout is established by these tests.

## Real scoped reviewer integration — 2026-10-27

`TestScopedLeaveThroughGlobalAPI` now drives signed sessions through Global API,
real IAM/Workforce handlers, PostgreSQL, NATS and OpenFGA permission/membership
resolution. All setup and business commands use HTTP APIs against disposable
synthetic infrastructure; direct SQL only observes evidence or controls locks to
reproduce concurrency. This is integration evidence, not live UI acceptance.

Verified: independent account-link and assignment approvals; no tenant-wide
reviewer permissions or hospital grant; exact replay creates one consumption and
one attributed success audit; self-decision and foreign hospital/staff denial;
empty unauthorized directory pages; protected enrollment/payroll; reviewed-link
revocation and fresh rebind; credential cutoff despite intentionally missed
middleware invalidation; fresh authentication without restoring an obsolete grant;
withdrawal before a waiting guard; and expiry while the decision waits for the
leave-ledger lock. Both races preserve pending state/version with no consumption
or success audit. Expiry first returned 500; the guard now preserves its
cancellation cause and returns 403.

Final commands from `backend/`:

- `go test -race ./internal/globalapi`: passed in 30.364s after the final fixture change.
- `go test -race ./internal/globalapi ./internal/modules/iam`: passed; IAM 212.148s.
- `go test -race ./internal/archtest ./cmd/contractdocs`: passed in 36.452s / 5.362s.
- `golangci-lint run ./internal/globalapi/... ./internal/modules/iam/...`: zero issues.
- `go vet ./internal/globalapi ./internal/modules/iam`: passed.
- `go build -o /tmp/helivanta-scoped-leave-api ./cmd/api`: passed.
- `go run ./cmd/contractdocs -check` and `git diff --check`: passed.

Reviewer discovery, bounded queue/detail contracts, UI, independent administrator
bootstrap and live two-person acceptance remain open. No issue is marked E2E
complete, and no new migration or production rollout was performed.

## Reviewer scope discovery — 2026-10-27

The membership-gated scope-discovery API is implemented locally with canonical
Go/Zod leave models. It requires current IAM authority plus locked active
employment and a reviewed account link. Discovery alone grants no business access.
The gateway journey verifies that a link without an assignment returns no scope;
an active reviewed assignment/link returns the hospital; and link revocation,
credential cutoff, assignment withdrawal and expiry remove or deny disclosure.
Dependency failure and a failed guard do not return a partial scope page.

The shared filtered paginator preserves continuation through empty denied pages,
binds cursors to caller/route/tenant, and rejects oversized candidate/filter pages.
Migration `0037_workforce_leave_review_discovery_index` uses the existing concurrent
index runner. Both it and `0036_workforce_scoped_leave` remain unreleased. Large
retained-history query plans and latency measurements remain release gates.

Commands/results from `backend/`:

- `go test -race ./internal/modules/workforce ./internal/globalapi ./internal/platform ./pkg/pagination ./internal/archtest ./cmd/contractdocs ./internal/testutil`
  passed: 340.676s / 36.095s / 16.525s / 6.315s / 50.732s / 10.634s / 14.216s.
- `go vet ./internal/modules/workforce ./internal/globalapi ./internal/platform ./pkg/pagination ./internal/testutil ./cmd/api`
  and `go build -o /tmp/helivanta-scoped-leave-api ./cmd/api` passed.
- Scoped `golangci-lint run` over the changed packages and API bootstrap: zero issues.
- `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check` passed.

With Node 24, `pnpm --filter @helivanta/api type-check` and `test` passed, including
all canonical generator checks and 379 API tests. Package lint and format checks also passed. `git diff --check` passed.

Pending-request queue/detail, decision UI, independent administrative bootstrap,
live Staff Admin/reviewer acceptance and the wider lifecycle matrix remain open.
Current main advanced to a MediCore change after the last recorded production
release; integrate and revalidate it before preparing the next release packet.
Nothing in this record establishes a new commit, merge or deployment.

## Reviewer queue/detail and UI — 2026-10-27

The new Staff navigation entry makes the scoped reviewer workflow reachable.
Queue rows exclude reasons/filer identifiers; protected detail includes counted
days and real roster clashes. Forced RLS denies a valid decision update attempted
with the read context, private enrollment/ledger reads and unrelated staff reads.
Failed guarded reads roll back their audits. Real gateway tests cover pending
queue/detail, self/foreign/completed target denial, revoked links/credentials and
assignment expiry. Shared route tests cover membership, hospital binding,
pagination declarations and changed-workspace rejection before scope lookup.

Backend race results: Workforce 284.627s, Global API 34.851s, platform 16.398s,
authz 11.336s and contractdocs 7.582s passed. The initial architecture run identified
the single-target detail as an unclassified non-paged route. Its existing bounded
resource shape was explicitly documented; the full architecture rerun passed in
32.336s. The final real-gateway run with the concurrent queue index passed in
26.822s. Scoped Go lint reports zero issues; vet/build and contract checks passed.

With Node 24 and the existing public local OIDC client/Global API build settings,
`pnpm turbo lint type-check test build format:check` passed all 30 tasks. Results:
1,008 shell tests, 379 API tests, 234 UI tests and 62 configuration tests passed;
the production Next.js build includes the new reviewer page. The first build
stopped on missing `API_URL`; no validation was removed. Nine new reviewer UI
scenarios and 47 zone-navigation tests pass. Browser acceptance remains unproven.

No broad issue is tagged E2E-complete. Independent administrator bootstrap,
reviewer credential handoff/login, real Staff Admin → reviewer browser QA, scale
query plans, integration of current main, and release remain open. The complete
staff lifecycle/identity/background/photo/card and wider Staff workflows remain
active requirements.

## Local UI evidence, 2026-10-28

Continued on PR #1393's scoped worktree. The user explicitly approved a local-only
hospital and two administrator-grant setup fixture after the empty environment
blocked UI QA. All staff, employment edits, account-link proposals/decisions,
access-assignment proposals/decisions, leave configuration, assignment, posting
and filing/approval records below were created through the actual UI. Existing
synthetic identities signed in normally; no injected session cookies were used.
Credentials and IDs remain in the private local QA JSON, not this public repo.

- Created reviewer and employee enrollment; saved and reread the employee's
  supervisor reference. An administrator edited the probation end date and saved
  it through enrollment. This proves this admin edit path, not every edit story.
- Empty staff identity, link proposal/review reason, assignment hospital/subject/
  reason, leave structure/review and leave type were rejected. Conditional filing
  gates rejected a missing calendar and missing probation evidence with entered
  values retained. Supplied those prerequisites through UI and retried the filing.
- Separate administrators proposed/approved the login binding and scoped leave
  authority. The proposer had no self-approval action. Assignment reached active
  version 5 after owner verification/projection; OpenFGA's immutable assignment
  subject tuple was read back. Reviewer has zero legacy hospital grants. Its
  pre-existing pharmacist role remains: this live persona is not role-free.
- Saved effective-dated annual leave, a ten-day allowance structure, January leave
  year, employee binding and January allocation. Filed a one-day request; signed
  in as the separate reviewer, discovered the hospital/queue, read protected
  detail and approved it. Rejection without a note was blocked. PostgreSQL
  verification found approved version 2, one approval audit, allocation +20 and
  consumption -2 half-days. The refreshed pending queue was empty.
- Reviewer could not reload the staff directory/employee/edit controls. Direct
  payroll setup navigation returned to the inaccessible directory; no payroll
  data was disclosed. These checks do not establish every object/tenant denial.
- Administrator inactivated the reviewer through UI. The login binding became
  revoked version 3; approved request/ledger history remained. On ordinary fresh
  reviewer login, leave discovery returned no hospitals even though IAM's old
  assignment record remained active. This is the guarded consumer's current-state
  denial, not completion of global offboarding or removal of every assignment.
- UI QA exposed promise-returning scroll effects crashing Staff forms with
  `destroy is not a function`. A regression reproduced it. Updating the test
  browser's scrolling behavior exposed eight failures in six Staff test files;
  fixing the affected effects made all 241 tests in 31 Staff files pass.

Still open: complete edit/correction coverage by role, retained-history query-plan
measurements, production release approval/migrations, fresh deployed UI QA, pure
staff-member live persona, full lifecycle/handover/settlement, identity/background/
photo/card workflows, and every remaining menu's full acceptance. A previously
open admin tab retained its rendered directory until refresh after an account
switch; record and resolve that cross-tab privacy behavior before claiming full
session/offboarding coverage. No broad issue is closed by this evidence.

The requested branded payslip/offer/exit-letter research is in
[Staff generated documents R&D](../superpowers/specs/2026-10-28-staff-generated-documents-rnd.md).
It specifies renderer evaluation, required sources, versioned approval/correction
and protected PDF delivery. These document-generation workflows are proposals.

### Cross-tab privacy and reactivation follow-up

The cross-tab rendered-directory finding above is fixed in development. With two
real local browser tabs, an administrator's enrollment form and staff rows were
open in one tab; signing out in the other removed both and returned the first to
sign-in. A separate successful reviewer login also cleared the old administrator
view, while the new login remained usable. Counts of old enrollment fields and
staff rows were zero. A public signup page stayed open during Staff sign-out;
component regressions additionally prove synthetic public draft preservation.
Private browser-history restoration has a separate regression. See the lifecycle
research's cross-tab update for transport limits and the unchanged server boundary.

Administrator editing also reactivated the synthetic reviewer. Read-only SQL
verified active employment with the historical link still revoked at version 3;
the earlier leave remained approved at version 2. Ordinary reviewer login still
showed no hospitals for leave review. This proves reactivation alone does not
restore the link; it does not prove full rehire/incarnation or exit-clearance
implementation. Fresh binding/assignment policy and the other open gates remain.

Validation for the cross-tab follow-up: `pnpm turbo lint type-check test build
format:check` passed all 30 tasks using the local Global API/OIDC build variables.
The initial public-form regression failed before the route-aware correction;
restored-history private content likewise failed before the pageshow guard.
The root-layout CSP test now mocks the application-owned client provider boundary
and retains its nonce assertions. No business API, migration or OpenFGA model
changed in this privacy fix. An earlier Go CI run failed a Practitioner concurrent
policy-replay test (one 409 where replay requires 200). The subsequent diagnostic
test preserves that replay contract, synchronizes concurrent starts and includes
response bodies in failures; no speculative production fix was made. Pushed head
`fb24806` passed CI run `40524062964`, including Go, web, E2E and secret scanning.
This green run does not establish the cause of the earlier intermittent failure.

### Retained-history query gate — 2026-10-28

The explicit `workforce_perf` probe passed against 100,000 retained cancelled
requests and 1,000 withdrawn account-link proposals with RLS and constraints
active. Actual endpoint SQL used the discovery and pending-queue indexes;
first/tail queue execution measured 6.241/3.255 ms and discovery 0.150 ms locally.
See the [consumer specification](../superpowers/specs/2026-10-27-scoped-leave-consumer.md#retained-history-owner-query-measurement--2026-10-28)
for fixture, command, row/buffer bounds and measurement limits. This completes
the local retained-history query-plan gate, not a production performance claim.
Current-main integration, release review, named migration/rollout approval and
deployed UI QA remain open, along with the wider Staff acceptance matrix.

### Current-main integration validation — 2026-10-28

Integrated `d138416` in the isolated Staff worktree, preserving unrelated changes
in the original checkout. The combined web command below passed 30/30 tasks:
1,031 shell tests, 385 API tests, 235 UI tests and 62 configuration tests.

```sh
API_URL=http://localhost:8199 \
NEXT_PUBLIC_ZITADEL_ISSUER_URL=http://auth.tesserix.localhost:20080 \
NEXT_PUBLIC_ZITADEL_CLIENT_ID=local-build-validation \
mise exec node@24 -- pnpm turbo lint type-check test build format:check
```

From `backend/`, with the existing disposable Docker test engine:

- `go test -race ./internal/modules/workforce ./internal/globalapi ./internal/modules/pharmacy ./internal/archtest ./cmd/contractdocs`: Workforce (350.089s), Global API (63.406s), Pharmacy (142.593s) and contractdocs (16.567s) passed. The architecture package failed on the new performance fixture's administrative statistics setup; no application behavior failed in these suites.
- Removed that `WithAdmin` call; `go test -race ./internal/archtest` passed (36.192s). No allowlist exemption was added. The tagged retained-history repeat passed (397.02s).
- `go vet ./internal/modules/workforce ./internal/modules/pharmacy ./internal/globalapi ./internal/archtest ./cmd/contractdocs` passed.
- `golangci-lint run ./internal/modules/workforce/... ./internal/modules/pharmacy/... ./internal/globalapi/... ./internal/archtest/... ./cmd/contractdocs/...` passed with zero issues.
- `go build -o /tmp/helivanta-staff-integrated-api ./cmd/api`, `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check` passed.
- Tagged Workforce `go vet` and `golangci-lint` passed after the fixture correction.
- Node 24 `generate-pharmacy-contract.mjs --check` and `generate-workforce-leave.mjs --check` passed. `git diff --check` and staged redacted gitleaks passed.

This is local integration evidence. The latest remote CI, merge, deployment and
fresh deployed browser QA remain separate gates. Read-only release discovery
still found application images at `a8af648`, healthy application replicas and a
healthy two-instance CNPG cluster. The PostgreSQL Argo application separately
reports degraded/out-of-sync seed jobs; that discrepancy is recorded for release
review. No cloud mutation, migration or new production rollout was performed.

## Directory and department UI follow-up — 2026-10-28

Using the existing synthetic administrator through ordinary local login:

- Added an address, emergency contact, qualification and experience. Blank
  conditional fields were rejected; an invalid international phone and an end
  date before the start date were rejected without losing input. Valid synthetic
  values saved and were read back after a page reload.
- Two tabs edited the same enrollment. The newer designation saved; the stale
  editor received the version conflict and retained its draft. Reload required
  explicit discard and showed the saved value. Removing the synthetic
  qualification persisted; address/contact/experience remained. Read-only SQL
  confirmed those row counts and the saved designation/version.
- Created a department, rejected a duplicate code while preserving input, edited
  its name and retired it at revision 2. History retained active revision 1.
  Reactivation produced revision 3; selected it in enrollment and saved. Retiring
  it at revision 4 preserved the employee's pinned revision 3. An unrelated
  enrollment edit still saved with the historical department reference intact.
- Staff documents correctly reported unavailable local storage and disabled
  upload. This is a dependency gap, not successful file/scanner/release QA.
- An ordinary reviewer login after the API update showed no hospital directory
  access or enrollment controls. This persona still has its pre-existing
  pharmacist membership; it is not a pure Staff-member persona.

### Supervision defect and local fix

The UI accepted A reporting to B and then B reporting to A. Read-only SQL
confirmed the cycle; it was repaired through the UI before local migration.
The [supervision design](../superpowers/specs/2026-10-28-staff-supervision-cycles.md)
records the database constraint, hospital-local serialization, snapshot-mode
boundary, migration/rollback limits and failing-first evidence. No supervision
link grants clinical or IAM authority.

The first backend regression expected 409 and received 200. The first capacity
probe also failed at 48 seconds; the actual owner plan showed repeated hospital
scans. The bounded per-hop refinement passed without changing the budgets.
An additional repeatable-read race initially committed both opposite links;
unsupported snapshot writes now fail closed. Tests retain both regressions.

Against the updated local API through Global API, the same UI cycle attempt
returned a safe conflict, kept its draft editable, and left the saved reviewer
supervisor empty at its existing version. Clearing the refused draft saved
normally. The local fixture's unreleased function was refined from the canonical
SQL in an owner transaction, without business-data SQL edits or disabling RLS.
This development-only schema refinement does not establish a production rollout.

Validation from `backend/`:

- `go test -race ./internal/modules/workforce ./internal/globalapi ./internal/archtest ./cmd/contractdocs`: passed after the final isolation fix; Workforce 339.228s, Global API 41.551s, architecture 51.732s and contractdocs 8.943s.
- Focused eight supervision cases passed, including indirect/concurrent/direct SQL, existing-cycle repair, lock contention, hospital isolation and stale repeatable-read snapshots (32.558s).
- `go test -tags workforce_perf -race ./internal/modules/workforce -run '^TestSupervisionLongChainPlans$' -count=1 -v -timeout 10m`: passed. The final 2,000-person fixture measured owner SELECT 37.510 ms (9,995 shared hits, zero reads), guarded UPDATE 40.381 ms (39.837 ms trigger), and HTTP cycle refusal 98.091 ms. Full staff-key lookup and one-row-per-hop assertions passed under RLS. These are local evaluation measurements, not production percentiles.
- `go vet -tags workforce_perf ./internal/modules/workforce`, `golangci-lint run --build-tags workforce_perf ./internal/modules/workforce/...` and the API build passed. Contractdocs generation/check passed after regenerating the new errors/migration. The canonical enrollment generator check passed after installing the new worktree's frozen dependencies offline.
- Frontend source is unchanged; the parent merge passed all 30 web tasks and remote E2E. Actual browser checks above exercise the existing editor against the changed API. Final remote CI and a named production migration/rollout remain open.

No broad Staff or organization story is closed by these cases. The other setup
categories and remaining 19-area acceptance gaps stay open.

### Supervision review follow-up — 2026-10-28

Review found and reproduced a deferred-constraint authority-loss bypass in
#1397. Four initial variants committed the cycle; the guard now locks current
hospital manager authority and checks the canonical enrollment policy with
three-valued SQL logic failing closed. Missing enrollment/tenant/hospital
context, grant deletion and downgrade to reader all refuse and roll back. The
updated focused suite passed (48.694s); the explicit directory-only supervisor
compatibility test was also run. The capacity probe still passed (7.909s):
40.285 ms owner SELECT, 34.656 ms guarded UPDATE and 125.567 ms HTTP refusal.

The combined backend race command passed Workforce (335.198s), architecture
(55.111s) and contractdocs (8.698s), but failed Global API's existing real-clock
expiry-lock observation. The original failed log is retained. Without changing
product code or weakening assertions, the exact integration test passed three
repetitions (68.680s), and the full Global API package then passed (30.751s).
This does not establish the intermittent failure's cause.

CI `40548628104` for the earlier `a1252ea` head passed Go/web/scripts/secret
checks but failed the cross-zone navigation E2E: Emergency was clicked while
Pharmacy's resolved scope was being added to the address, and the page stayed in
Pharmacy. The trace and error context are retained privately; investigation of
its automatic address synchronization is separate from the supervision guard.
No navigation assertion was relaxed and no broad issue is tagged complete.
Remote CI and production release remain open gates.

## HR category and assignment UI evidence — 2026-10-28

All business changes below were made through the local Staff Admin UI against
the existing synthetic hospital. No direct SQL/API writes created this evidence.
Visible-history captures and screenshots remain in the private evidence directory.

- Created designation, grade, qualification, document category, exit reason,
  relationship, language and contract-type records. Each completed active v1 →
  retired v2 → active v3; all three revisions were visible in history. Code was
  disabled in each editor. Designation name changed without changing its code.
- Blank designation name and then missing code produced inline validation.
  Contract-type retirement persisted after reload.
- Assigned designation/grade v3 to the synthetic employee, then retired both
  masters to v4. Department v3 was already pinned to a retired master. All three
  displayed “recorded revision” when the employee was reopened.
- Changed only the employee's preferred name and saved. A full reload preserved
  that change and all three historical master assignments.
- Consumer integration remains incomplete: preferred language, exit reason and
  emergency relationship are still text fields. Qualification, document-category
  and contract-type consumers need separate verification. `masterField` deliberately
  falls back to legacy text when there is no active choice and no recorded revision.
  The unassigned reviewer's editor exhibited that fallback after retirement.
  Governing new assignments while preserving legacy records remains #1035 work.
- Signed out the administrator and signed in as the independent reviewer. HR
  setup showed the hospital-access denial message and no hospital selector,
  master table or add-record button (all three observed counts were zero). This
  covers this UI persona, not every server-side scope or role combination.
- HR master configuration has no independent approval stage in its current
  contract. These checks do not replace maker/checker approval of access, payroll,
  credentials or employment documents. No employment status, role, reviewed login
  binding or clinical authority changed in these operations.

The saved eight-category visible-history capture was checked for revisions 1, 2
and 3 and both active/retired states. This is local browser evidence. Further
duplicate normalization, concurrency, scoped-role/isolation, pagination and
consumer connections remain open; HR setup is not marked complete.

## Merge and release status — 2026-10-28

Supervision #1397 merged as `81dc18b4` after full CI. Navigation #1398 merged as
`ec1a4280` after Go coverage, web, scripts, E2E, image builds and secret checks
passed. The original chrome-preservation E2E assertions remain unchanged. Main
CI/image workflows are separate release gates. Photo/card issue #1399 records
the previously untracked explicit requirement.

Read-only production discovery still found three application deployments at
`a8af648`, each two ready replicas, and a healthy two-instance CNPG cluster.
A bounded read-only audit of three enrollment profiles found no supervision
cycles or unresolved depth. Ten migrations across Drugmaster, MediCore, Pharmacy
and Workforce remain unapplied. PostgreSQL Argo remains OutOfSync/Degraded due
to seed jobs, including a privacy-seed DeadlineExceeded on October 25. No cloud
mutation, migration or rollout was performed. The combined release, existing
Argo discrepancy, recovery scope and fresh deployed UI QA remain subject to
release review and named production approval.

## Profile master connection checkpoint — 2026-10-28

The isolated #1035 feature connects preferred language, exit reason and contract
type to immutable HR revisions through the existing protected enrollment
contract. Contract type remains separate from engagement and is selection-only.
Real-DB/HTTP tests cover canonical labels, retired/history retention, current
selection, exact retry, old-client omission, explicit clearing, null compatibility,
wrong-kind/hospital/tenant denial and refusal of older writers deleting new pins.
The retry event assertion proves a single metadata-only outbox event.

Affected backend race suites, full backend vet/lint/build, canonical generation
and registry checks passed. All 30 web tasks passed (1,037 shell tests); the six
Node script tests passed. The detailed [implementation checkpoint](../superpowers/plans/2026-10-28-staff-profile-master-connections.md#validation-checkpoint--2026-10-28)
records commands and initial validation failures. Local migrations 0040/0041 and
the feature API/configured shell are running; no production mutation occurred.

Staff Admin UI QA saved all three selections, verified full-reload persistence,
retired each selected master to v4 and retained the recorded v3 choices through
an unrelated edit and another reload. Read-only SQL confirmed staff version 11
and all three pins; all business changes used the UI. Restoring the fixture's
exact client and hostname resolved initial sign-in configuration errors.
Independent reviewer login confirmed denied directory/setup access, zero
add/edit staff controls and zero master tables/add controls. The full backend
coverage retry remains pending; no merge, deployment or broad Staff completion
claim follows yet. Relationship, repeatable qualification,
private-document categories, identity/background/photo/cards, lifecycle and
payroll/issued documents remain in the ordered acceptance scope.

Main CI 40573508434 for navigation merge
`361a32de56785962f44e19ae4a0ef8805294ea38` completed successfully; image workflow
40573507886 succeeded. Repository visibility was rechecked as PUBLIC. These
release gates do not establish that production has moved from its last observed
`a8af648` baseline or that the pending schemas have been applied there.

### Profile response compatibility follow-up

The default enrollment response retains the original profile shape; updated
editors opt into `profile_masters=v1` and require its response marker. Actual
HTTP tests reproduce older strict-client shape rejection and invalid-query writes
before the correction, then prove legacy amendments/retries retain all pins.

Local browser QA also exercised the real older UI against the new API: its
unrelated amendment saved and reloaded, while all new references remained v3.
The new UI against the old API blocked editing explicitly; retry after restoring
the new API recovered the recorded references. The final UI and API are restored.
Read-only SQL verified employee version 13 and all three pins, with masters still
retired at v4. All business writes used UI. Full 30-task web and static backend
checks passed; the post-projection full backend race/coverage gate passed all
55 tested packages with no coverage-floor failures. See the feature
plan for exact commands, initial failures and the separate baseline full pass.

The independent clock-fixture prerequisite #1402 merged as `3238d2b8` after all
CI checks. This is not a Staff feature deployment or a broad issue closure.

The final-build independent reviewer repeat again showed no hospital directory
access, no add/edit staff controls and no HR setup table/add control. This
confirms the current fixture's denial, not every related role or a payroll/letter
approval. Profile feature merge, deployment and full Staff acceptance remain open.

## Research reconciliation and next implementation boundaries — 2026-10-28

Reviewed the lifecycle and generated-document research, the October14 legacy HR
policy note, organization authority/access/structure/workflow references and
current issue criteria. These sources have different evidentiary roles: legacy
observations establish fields/menu coverage; accepted ADRs establish design
choices; source and tests establish implementation; actual synthetic UI evidence
establishes only the exercised workflow. None activates statutory policy.

The active candidate is #1403 at `36e60eba87a14c6889aa6ab4f0eb36ad3e307143`.
Its local full backend race/coverage and thirty-task web gates passed, including
actual old-editor/new-API and new-editor/old-API compatibility journeys. Remote
web, E2E, security and image checks passed; remote Go coverage remains pending
at this checkpoint. #1402 main CI and image publication both passed. This entry
is a checkpoint, not approval to merge a failing/pending candidate or deploy it.

### Remaining master consumers

| Consumer                       | Verified source constraint                                                                                                                                                                                                                  | Required implementation/acceptance boundary                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Qualifications                 | Canonical qualification text is at most 150 characters; master names are at most 80. Private evidence resolves a qualification by its staff-scoped row ID in `documents.go`. Enrollment replacement currently deletes/reinserts child rows. | Add an optional typed revision pin through canonical generation, preserve existing child IDs and evidence anchors, derive selected labels on the server and retain historical pins. Older-editor omission must preserve pins by existing row ID; an omitted row is a separate removal with existing evidence-release consequences. Prove edit/removal/retry/foreign-row behavior and old/new editor compatibility before release. |
| Emergency-contact relationship | Canonical field, Go validation and SQL CHECK are limited to 60 characters, while HR master names allow 80.                                                                                                                                  | Resolve the contract mismatch explicitly before connecting selections. Do not truncate or silently widen v1. Any constrained-selection proposal must explain what happens to valid 61–80-character master labels and test those boundaries. Existing historical free text must remain readable.                                                                                                                                   |
| Document category              | `documentCategoryBindings` and `documentInput` use fixed evidence-purpose values, including qualification and identity evidence.                                                                                                            | Preserve the purpose enum and scanning/release authorization. Any additional configurable HR classification needs a separate contract and scope/version validation; it cannot replace the existing security category.                                                                                                                                                                                                             |

These are proposed requirements, not implemented child references. Profile
`profile_masters=v1` currently covers profile fields only; it does not negotiate
new child fields. A child extension needs an explicit compatible response shape,
server capability check and its own older-writer guard. Reusing the existing
profile support marker would allow an intermediate binary to erase unknown pins.

### Payroll, documents and lifecycle dependencies

The generated-document research confirms that salary configuration and calculation
previews do not establish payroll preparation/finalisation/correction (#1040).
Payslip policy (#1041), offer/exit letters (#1107), and protected photo/card issue
(#1399) remain open. Finalized immutable payroll inputs and independently reviewed
results precede issued payslips. Hospital print branding needs an approved legal
identity/address/signatory and immutable logo/template snapshot; tenant login
branding is not that owner contract. Evaluate the Go renderer against rendered
PDF pages, rupee glyphs, Indian-script shaping, long values, multi-page tables and
extracted totals before selecting it. No renderer library or statutory default is
selected by this checkpoint.

The lifecycle research continues to distinguish security disablement, tenant
membership, hospital employment, supervisory accountability, physical cards and
clinical authority. Inactivating staff and revoking reviewed login links does not
prove universal old-session/legacy-grant cutoff or final settlement. Rehire must
not restore old grants/cards. ORG-D2, ORG-D5 and ORG-D6 remain open domain decisions;
a feature must not settle them implicitly.

### Release scope corrected from source and live discovery

Read-only production checks found all six application Deployments ready 2/2 at
`a8af648`: API, Global API, shell, document worker, emergency worker and
supply-evidence worker. Kargo updates three Argo applications; the API chart owns
all three workers and they share its image tag. Release approval and verification
must therefore name six affected Deployments. ClamAV and OpenFGA are separate
pinned dependencies. No production mutation occurred.

The read-only ledger showed all twelve expected pending migrations unapplied:
Drugmaster0003, MediCore0006, Pharmacy0012–0015 and Workforce0036–0041 (the last
two depend on #1403). CNPG was healthy with two ready instances; the latest
observed scheduled backup completed on October27 at18:00:25Z. The documented
October13 restore does not verify that newer backup. PostgreSQL Argo warnings
correspond to three obsolete seed Jobs requiring pruning; current desired seed
Jobs succeeded. Keep any targeted cleanup separate from rollout approval and
never enable broad database pruning to clear this warning.

The exact release SHA, main CI/images, migration scope, authority and recovery
conditions must be refreshed after the candidate merges. Production rollout and
migrations need approval naming that concrete target under the repository's cloud
rules. The prior #1385 approval does not approve this combined release. Private
release history and current packet remain under the restricted evidence directory.
No issue was closed or labelled end-to-end verified by this research review.

### Profile feature merged

#1403 merged after every CI check passed at the exact tested head `36e60eba`.
The merge commit is `9d1d6d355ba5b96cc3e0bd4d1ca7c072b0d45ed7`; repository
visibility was verified PUBLIC immediately before merge. Main CI and published
images for this merge remain release gates. The earlier pending-check entries
retain their checkpoint context. No production schema/application change or
full Staff completion follows from the merge.

## Qualification master connection — local acceptance

The qualification candidate adds optional `qualification_revision_id` through the
canonical enrollment generator. Existing hospital-manager/enrollment permissions
remain authoritative. Labels resolve on the server within the same verified
tenant, hospital and category; new assignments require the active current master.
Historical pins remain usable. Omission preserves a pin by existing child ID,
explicit empty clears it, and null/malformed references fail. Duplicate
qualification/institution/year combinations are revalidated after canonical
resolution, before writes, audit or event publication.

The default response retains the old qualification shape. Independent
`qualification_masters=v1` negotiation returns the reference and its response
marker; it may be combined with `profile_masters=v1`. The editor requires both
markers and keeps a distinct cache identity. Migrations `0042_workforce_qualification_masters` /
`0043_workforce_qualification_master_index` add the typed
reference, constant category, concurrent index and separate intermediate-writer
guard. The local migration runner applied both; read-only inspection confirmed
the validated foreign key, ready/valid index and enabled guard. They have **not**
been applied to production and are excluded from the `9d1d6d3` rollout request.

Actual local browser evidence through Global API:

- Admin selected the existing synthetic qualification's active revision 3, added
  institution/country/year, saved the employee and reloaded the page.
- Admin retired that master to revision 4, retained recorded revision 3 and saved
  an unrelated employee amendment. Read-only SQL confirmed the same qualification
  row ID/revision and all prior profile pins, with a single staff-version advance.
- The feature UI against the actual previous profile-aware API showed an
  unavailable view and zero Save controls. Restoring the API and using Try again
  recovered the recorded pin without a write.
- The actual previous editor loaded its compatible response and saved an
  amendment against the feature API. Final feature UI reload and read-only SQL
  confirmed the same row ID, qualification revision and prior profile pins at
  staff version 16.
- Independent reviewer sign-in had no hospital access, no Add/Edit controls in
  the staff directory and no HR setup controls. This account retains its existing
  pharmacist membership; this is a negative scope test, not a pure employee-persona
  acceptance or evidence of an approval stage for profile/master edits.

Red-to-green tests exposed unsupported input, noncanonical retry comparison,
omission clearing, null clearing, an intermediate writer erasing a pinned row,
canonical duplicate acceptance, missing reference index and a missing UI selector.
Final gates passed: full backend race/coverage (55 tested packages; Workforce
393.498s/83.6%, Practitioner 130.723s/84.6%, Global API 44.395s/86.7%), Go vet,
golangci-lint and builds; all 30 web tasks (1040 shell, 391 API, 236 UI, 62 config,
9 emergency-access tests); 16 Node tests; canonical generation/check and
contractdocs generation/check. The first broad web run lacked its required
Global API URL; the configured run passed. Private logs, schema/database snapshots
and browser screenshots are in the restricted evidence directory.

This connects a qualification classification. It does not verify a real academic
award, grant clinical privileges, implement identity/background review or prove
private-file upload/scanning/release. Those workflows and the other nineteen-area
gates remain open. Emergency relationships and configurable document
classification retain their recorded contract/security decisions.

## Emergency-contact relationship masters — local QA, 2026-10-28

The optional `relationship_revision_id` uses its own `contact_masters=v1`
projection and response marker. The existing emergency relationship limit remains
60 code points; valid HR names of 61–80 code points remain visible but disabled
for this consumer. The server rejects an over-limit canonical label before writes.
Free text remains supported. Historical references belong to the same contact row,
tenant and hospital; omission preserves a recorded reference, explicit empty clears
it and null is invalid. Migrations 0044/0045 add the typed foreign key, previous
writer guard and concurrent reference index. No permission, route or event changed.

Actual local UI QA used the synthetic hospital and employee through the Global
API. All business changes below were made in the UI; bounded read-only SQL
verified persistence. Schema checks confirmed the validated FK, enabled guard and
ready/valid index.

- Admin created a 61-character relationship master in HR setup. Enrollment showed
  its complete label as unavailable with the 60-character explanation.
- Admin selected active revision 3 of the bounded relationship master, saved and
  reloaded it (staff version 17). Clearing the selection and entering free text
  persisted a null reference at version 18. Selecting it again restored the same
  reference at version 19.
- Admin retired the master to revision 4. An unrelated amendment retained contact
  row `691f05c9-b204-4824-ac8e-7426f102d63a` and recorded revision
  `40aee186-bec6-4fcd-b280-9a7c6b44c6bf`. A QA input mistake had appended the
  preferred name to the full-name field; both synthetic names were corrected
  through exact-label UI controls, retaining the reference at version 21.
- The new UI against the actual previous qualification-aware API displayed the
  unavailable view with zero Save controls. Restoring the feature API and using
  Try again recovered the editor and recorded reference without a write.
- The actual previous qualification-aware UI saved an unrelated preferred-name
  amendment against the feature API. Version 22 retained the same contact row,
  relationship revision, qualification reference and profile pins. Restored feature
  UI displayed the recorded retired relationship again.
- Independent reviewer sign-in had no hospital staff access and no directory
  Add/Edit controls or HR setup mutation controls. This account retains its prior
  pharmacist membership: this is a negative scope check, not pure employee-persona
  acceptance or an approval stage for enrollment/master edits.

Final implementation gates passed: backend race/coverage across 55 packages
(Workforce 384.011s/83.6%, Practitioner 132.469s/84.6%, Global API 37.468s/86.7%),
Go vet, golangci-lint (zero issues), Go builds; all 30 web tasks (1042 shell,
394 API, 236 UI, 62 config and 9 emergency-access tests); 16 Node tests;
canonical generator/check and contractdocs generator/check. Real-DB suites cover
scope/category mismatches, retired history, child-row identity, omission/null/clear,
old-writer protection, bounded labels and projection combinations. The initial
web run failed on an unsupported test-only matcher option; the corrected full run
passed. Logs, screenshots and database snapshots remain in the restricted local
evidence directory.

This classification establishes neither kin consent nor authority for clinical
or administrative decisions. Production rollout, document classification,
identity/background/photo/cards and the wider Staff Management gates remain open.

## Document classification — development checkpoint, 2026-10-28

Contact relationship PR #1407 merged as `aab7d0ded61a4c23816dd52319daeb05fdeb1924`
after all exact-head Go, web, scripts, browser E2E, secret and image/security checks
passed. Main validation remains separate from production acceptance.

A separate classification slice is in progress, based on that merged main and
concurrent pharmacy PR #1408. The fixed evidence-purpose enum and shared file
pipeline are preserved. Local source now has canonical models, current/history
routes and migration 0046, scoped owner checks, versioned history, protected read
and write audits and identifier-only change events. The metadata editor is now
implemented; no production migration is approved by this checkpoint.

Failing-first tests reproduced the missing route, a shared-audit integration
mistake, an oversized-body acceptance and lost response scope IDs. Those were
corrected. Focused real-DB/race tests now cover edit/clear, exact retry, stale
versions, retired-reference retention, independent/foreign/reader denial, owner-ID
substitution, unchanged strict upload responses, unavailable content, concurrent
retry deduplication, forced RLS, immutable history, version skips and minimized
outbox payloads. Six client tests cover strict/consistent models, Global API
routing and tenant headers, owner mismatch, exact uncertain retry, receipt checks
and bounded history. Current-main focused backend/architecture race checks,
Go vet, API lint/typecheck and contractdocs checks passed. Earlier failed logs
remain in the restricted evidence directory; one test run failed only because
its local test-container socket setting was mistyped.

Boundary/dependency-failure, pagination, initial-version guards, grant downgrade,
foreign masters, excluded benefit/tax files, atomic outbox failure and UTC wire
time checks passed with real-DB/race tests. The outbox-failure trigger is installed
through the isolated migration fixture; no `WithAdmin` allowlist was widened.
The editor has eight component tests; combined document panels have seventeen.
Full web validation passed all 30 tasks. The first backend gate found two
architecture-test integration omissions, now fixed; a concurrent-load IAM timeout
passed its focused rerun. The fresh full backend race/coverage gate passed
all packages, including Workforce 456.913s/83.6%, IAM 282.241s/80.3%,
Pharmacy 130.953s/84.5% and Global API 31.134s/86.7%. Go vet/build/lint,
canonical generation, contractdocs, script tests and staged secret scan passed.
After concurrent #1409 fixture/document renames, 60 tests across the five affected
Go packages and nine API tests passed. #1410 fixed the unrelated pharmacy test
fixture and merged after every check passed. The candidate base tree matches
that merged main tree exactly.

Actual owner UI QA used only synthetic local records: uploaded a PNG through the
previous UI, created a document category, assigned/reloaded it, explicitly cleared
it, reassigned it, then retired/renamed its master. The original recorded revision
remained visible, was absent from new choices, and saving it unchanged produced
no duplicate history. History showed versions 3/2/1 with assigned/clear/assigned
values. New UI against the previous API showed an explicit unavailable state
without Save; restoring the feature API recovered the pin without a write.
Read-only snapshots confirmed metadata version 3/history 3, unchanged file facts
and staff version 22. Evidence is restricted under the local QA evidence directory.
The file remains `scan_pending`; this is not scanner/release acceptance.

Independent reviewer login showed no hospital access in Staff directory and HR
setup, with no Add/Edit/Documents or master mutation controls. This account
retains its existing pharmacist membership; it is not a pure employee persona
and this check is denial acceptance, not a classification approval workflow.
After signing out the reviewer and signing in the owner again, the protected
file and recorded retired classification reloaded correctly without a write.

PR #1411 merged as `19819f37a9121cd4b78cc2bd19a118faf071ac23` after every exact-head check passed. Main CI `40622181635` and images `40622181456` passed. Remaining before release: separately approved migration rollout and deployed acceptance.
This checkpoint completes neither private-file release nor identity/background,
photo/cards, payroll, letters or the wider Staff Management scope.

## 2026-10-28 stalled private-file processing recovery checkpoint

While preparing real scanner/worker UI QA for #1105/#1399, the synthetic file
remained `scan_pending` after its Temporal execution timed out without a worker.
The worker subsequently became ready, but the old UI/API had no recovery path.
The previous schema was reproduced with an isolated real-DB regression: expected
202 versus actual 409. The fixture explicitly scopes and asserts its one aged
row; no business QA row was backdated or patched.

The candidate keeps current file and parent authorization, then accepts only the
next attempt of an error or a pending scan whose database-owned 45-minute waiting
window elapsed. The migration guards that clock and attempt, preserving RLS and
scan/release provenance. Real-DB/race checks passed for duplicate/concurrent
commands, early/foreign denial, forged clock/attempt denial, atomic outbox failure
and rollback, superseded workers and an old worker's in-flight storage copy.
Staff UI and the shared proposal evidence UI tests failed before the change and
passed after it; recent pending files and retained/frozen proposal evidence do not
receive the recovery control.

Native loopback ClamAV 1.5.4 has verified official definitions 28166, built
2026-10-27T06:24:12Z. Its protocol runs in UTC; a harmless standard EICAR control
returned `Eicar-Test-Signature FOUND`. The document worker uses the existing
restricted local runtime role, local storage emulator and local Temporal namespace;
no credential, grant, startup service or production setting changed. Migration
0047 was applied only to the explicitly authorized loopback QA fixture.

The first full backend run caught a real candidate migration regression: the
replacement copied the earlier worker guard instead of the later tax-evidence
guard, dropping the `record_frozen` settlement exception. The current canonical
guard is now preserved; all benefit/tax evidence and recovery regressions passed
a focused race rerun (40.463s). The rollback test restores that current prior
guard, rather than the obsolete worker-only guard. The corrected canonical SQL
was reapplied only to the local fixture; business rows and migration ledger were
preserved. The full backend also exceeded Go's default 10-minute suite deadline
under concurrent web load. Web checks caught a missing build API_URL and test
locator typing, both corrected; the combined run then had unrelated five-second
timeouts. Gates are being rerun sequentially without widening timeouts.

The initial sequential gates passed. Final source review then reproduced a frozen
pending-row attempt change and an uncertain UI retry disappearing after refresh
showed the committed attempt. Frozen guards now reject attempt changes while
retaining existing tax/benefit `not_pending` reasons; UI uncertain retries keep
the original attempt after refresh. The additional regression suites passed.
Final web validation passed all 30 tasks and 1,060 shell tests. The final affected
backend race/coverage rerun passed: Workforce 503.489s (83.7%), attachments
4.030s (82.1%) and architecture checks 49.998s.

Actual Admin UI retry recovered the same timed-out file as attempt 2. Temporal
completed, ClamAV 1.5.4/definitions 28166 committed a matching release and the UI
showed Ready. The native browser Save dialog saved the download into the private
QA folder; all 179 bytes and SHA-256 matched the original upload. Exactly one
download audit was recorded. Admin UI revocation persisted and removed Download
and Retry controls while retaining the file. A fresh second PNG upload reached
Ready on attempt 1 through the real scanner/storage/worker path. The independent
reviewer's fresh session had no hospital access and no staff-file controls; its
existing pharmacist membership was unchanged. Staff enrollment remained version
22 throughout. All business writes above used the UI; verification SQL was
bounded and read only.

After reviewer logout and a fresh Admin login, Ready/Revoked states persisted.
The classification pin and its three history entries remained unchanged at
metadata version 3, and staff enrollment remained version 22. A rebuilt API
containing the corrected canonical guard is serving the local fixture.

A second browser-saved download of the fresh Ready upload also matched all 179
original bytes and SHA-256. The final local UI still shows the expected Ready and
Revoked records, with Download available only on Ready.

Review/merge and a separately approved production rollout remain open. This is file-processing recovery, not identity/background acceptance
or card issuance.

## Current identity checkpoint — 2026-10-28 (local, release pending)

[ADR-0047](../adr/0047-current-identity-on-protected-requests.md) records the
mandatory IAM admission guard after rate limiting and before permission resolution.
Current non-active/deleted identities commit an attributed credential watermark
and outbox invalidation before 401. Fresh IAM reads cover missed broadcasts and
revocation during the upstream read. Unknown/unreadable state and failed persistence
deny with 503. Only self sign-out skips the upstream read; administrative revocation
must not be available to a disabled or unreadable identity. Migration
`0013_iam_identity_guard` adds the distinct revocation reason without copying identity
data or changing OpenFGA permissions.

Browser QA used the local Staff Admin and separate pharmacist/staff-member reviewer
from the protected local fixture JSON. Ordinary Staff navigation/Refresh access
with existing cookies was denied after each synthetic IdP account was deactivated.
Both accounts were restored to active. Fresh UI sign-in succeeded afterward; the
Admin retained the same two synthetic staff records and the reviewer retained no
hospital-directory or leave-approval authority. No membership/role was expanded.
The local watermark and standard `auth.identity.inactive` audit event were verified.

Real PostgreSQL/NATS and signed-session regressions cover durable dispatch, outbox
rollback, missed broadcast, concurrent revocation, later fresh authentication, real
HTTP provider responses, known locked/suspended/deleted/initial states, unknown or
unavailable state, missing configuration, database outage after a warm cache, and
disabled-admin/outage denial for administrative revocation.

Commands run from backend with the existing local Testcontainers context:

```sh
go test -race -cover -p 1 -timeout 20m ./internal/modules/iam/... ./internal/bootstrap ./internal/archtest
go vet ./internal/modules/iam/... ./internal/bootstrap/... ./internal/archtest/... ./cmd/api/...
golangci-lint run ./internal/modules/iam/... ./internal/bootstrap/... ./internal/archtest/... ./cmd/api/...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
go build -o /tmp/helivanta-staff-session-identity-api ./cmd/api
```

The full affected race/coverage run passed: IAM 217.460s (81.1%), identity client
1.766s (90.5%), bootstrap 14.299s (93.8%) and architecture 38.075s. PR CI, a named
production rollout and deployed UI verification remain open. Current main's
Practitioner concurrent-policy replay failure is being fixed separately; no broad
Staff, account-lifecycle or Practitioner issue is closed by this checkpoint.

### Identity-source preparation checkpoint — 2026-10-28

The next bounded slice adds shared HR assignment templates and a guarded current
identity-source read, including database-owned employment generations. Local
maker → independent approver → restricted recipient UI checks and source security
evidence are recorded in [the preparation report](staff-identity-source-preparation.md).
This does not complete identity/background/photo cases, cards, employment lifecycle,
payroll runs, payslips, offers or exit letters. The area ledger and #1399 remain open.

### Production release checkpoint — 2026-10-28

PR #1423 merged as `01154b69c9419361d34f7d35070f1266f256e3d8`.
The explicitly approved Kargo promotion
`prod.01m611h1bvf2nk4b90xhrdck97.67fb839` succeeded. API, Global API,
shell and the document, emergency and supply-evidence workers each report two
updated, ready and available replicas at that release. Migrations
`0013_iam_identity_guard`, `0014_iam_staff_templates`, `0019_pharmacy` and
`0048_workforce_identity_source` are recorded as applied.

Production owner and restricted reviewer sign-in/protected UI loading were
verified. The owner can load the identity assignment family and both manager and
reviewer templates, but has no hospital-manager grant and receives the explicit
no-access message instead of a selectable hospital. No production grant was
created. The positive independent assignment/link/revocation flows remain local
synthetic UI evidence; this checkpoint does not claim production positive identity
source access. The full local Linux Go race/coverage run and PR/main CI passed.

Published release:
[Staff identity-source preparation](https://github.com/tesserix/helivanta/releases/tag/2026-10-28-staff-identity-preparation).
This supersedes earlier pending-release statements for those changes only. Full
Staff workflow acceptance and the gaps above remain open.

### Workspace layout checkpoint — 2026-10-28

Staff and Dashboard use the shared full-width zone policy (maximum 1760px),
removing the inherited 1152px content cap. Staff subroutes share that policy;
individual fields and explanatory text retain their readable widths. Department
shortcuts use available-content breakpoints for one, two or three columns.
Analytics panels automatically fill the grid when permissions expose only one
service, rather than reserving a second empty column.

Local browser QA with synthetic staff verified the directory at 1728px with both
expanded and collapsed navigation. The content widths were 1216px and 1344px,
respectively, with no document overflow. At 390px the document remained 390px;
the 546px table scrolled inside its 340px panel. Dashboard shortcuts stacked in
one 342px column without page overflow. Normal viewport settings were restored.
Width-policy regression tests first failed against the capped implementation.
This is layout evidence, not completion of the Staff business workflows.

Issue #1399 was found closed by the document-recovery PR #1415 even though that
PR explicitly does not implement its photo/card acceptance criteria. It was
reopened so its incomplete workflow remains visible.

Validation command: `pnpm turbo lint format:check type-check test build` using
Node 24 and the local Global API URL. All 30 tasks passed, including 1,085 shell
tests and 236 shared UI tests. `pnpm exec prettier --check docs/qa/staff-management-acceptance.md` and `git diff --check` also passed.

### Payroll configuration and route-access checkpoint — 2026-10-28

Local synthetic Staff Admin UI QA rejected an empty salary head with field-level
errors for code, name, payslip label and review confirmation. A synthetic earning
head was created, edited to version 2 and reloaded; history retained versions 1
and 2. A salary structure with an explicitly synthetic INR 10,000 monthly amount
was created after incomplete-structure validation was exercised.

The Admin proposed that structure for the synthetic employee. The proposer could
withdraw but had no approve action. A separate existing approver signed in,
reviewed and approved the proposal. History recorded distinct proposer and
reviewer accounts; the approved version survived page reload. A 31-day preview
returned the synthetic earning and explicitly reported missing PF, ESI,
professional-tax and labour-welfare rules plus the missing income-tax election.
No statutory policy, payroll run, payment or payslip was created. These observations
cover configuration only, not financial execution or statutory correctness.

A restricted reviewer had no Payroll navigation entry. Direct navigation exposed
no payroll records because the server denied reads, but showed generic retry
errors. The workspace now checks `workforce.payroll.view` before mounting hospital
and payroll queries, with a clear denial message for missing permission. Configure
permission alone does not substitute for view permission. Existing API permission,
tenant/hospital scope and object checks remain the security boundary; no new access
is granted by this UI change. Browser QA confirmed the denial after a fresh load.

The two denial regressions failed before the guard; the corrected parameterized
fixtures explicitly model empty permissions and configure-only permissions. The
11-test payroll setup suite passed, retaining authorized read-only and manager
coverage. After updating the isolated branch to merged main, all 30 tasks in
`pnpm turbo lint format:check type-check test build` passed on Node 24, including
1,091 shell tests and 236 shared UI tests. No API, schema or permission contract
was changed. Full payroll execution and the remaining Staff acceptance areas stay
open.

### Approved production releases — 2026-10-29

PR #1426 merged as `bb59caa3bdf362f13cde5a20fe6e04fabd967ba4` and was
promoted through Kargo successfully. The published
[workspace layout release](https://github.com/tesserix/helivanta/releases/tag/2026-10-28-workspace-layout)
also contains the approved Pharmacy daily sales report from #1424. Production
browser QA confirmed the expanded Staff width and unauthorized identity-source
denial; no production identity grant was created.

PR #1427 merged as `2fede3e55470b25576e19f800fc3f95eb6b97dda` and was
promoted successfully. The published
[payroll access release](https://github.com/tesserix/helivanta/releases/tag/2026-10-28-staff-payroll-access)
was verified with a fresh restricted-reviewer login and direct payroll setup
navigation: explicit denial, no payroll records or retry controls. API, Global
API, shell and three Go workers reported two updated/ready/available replicas.
Both owning repositories remained public.

One production browser reload returned Cloudflare 520 at
`2026-10-28T12:17:54Z`. The next controlled reload recovered; shell was
Healthy/Synced with zero pod restarts and subsequent fresh-login QA passed.
The underlying cause was not established. This observation does not justify a
claim that its cause was fixed. Private rollout/preflight evidence retains it.

### Identity draft preparation — 2026-10-29 (local)

The bounded draft slice adds create/edit/current/list/history for identity,
background and photograph preparations. Current hospital manager authority is
required to write; managers or reviewers may read, with current independently
reviewed actor binding and fail-closed IAM transaction guard. Every new case
request pins both verified tenant and account through mandatory precondition
headers forwarded by Global API. Drafts are explicitly unreviewed.

Canonical input/receipt and source-projection contracts, migration
`0049_workforce_identity_cases`, immutable snapshots, forced RLS, scoped indexes,
metadata-only event/audit records and exact replay receipts are implemented.
Migration 0049 is applied only to the synthetic local QA database. No production
rollout or migration approval is implied by this checkpoint.

Local browser QA through the Global API verified reviewer-only empty drafts
without prepare/edit controls. An existing owner proposed a two-hour manager
assignment for the linked synthetic actor in the synthetic QA hospital; a
separate existing approver reviewed it and activation reached version 5. The
manager UI rejected blank policy/check/validity fields, saved a synthetic identity
draft, edited it to version 2, displayed both immutable revisions and retained
version 2 after a full page reload. The proposer then withdrew the temporary manager grant (version 6). After a fresh
reviewer login, the UI retained version 2 and both history snapshots without
Prepare or Edit controls.
No real identity evidence, government identifier, verification decision, card,
production role or credential was created.

Validation completed: `pnpm turbo lint format:check type-check test build` on Node
24 (30 tasks; 1,095 shell, 404 API and 236 UI tests); `go test -race -cover` for
Workforce (83.7%), Global API (86.7%) and bootstrap (93.8%). The initial architecture
run correctly required a documented pagination exemption for a singular case GET;
after adding the bounded single-record rationale, its complete race suite passed.
`golangci-lint run` for Workforce/Global API/architecture reported zero issues;
`go vet` for affected packages, `go build ./...`, identity generator check,
contractdocs generation/check, document formatting and diff whitespace checks
also passed. Earlier red/green regressions cover command replay, scope and current
authority, guard rollback, source/epoch changes, required fields, UTC dates,
immutable history, pagination, header forwarding and private-cache removal.

Evidence binding/upload, immutable submissions, independent check decisions and
revocation, protected photograph processing/self-service, identity cards and
employment lifecycle remain open. Payroll execution, branded payslips and
offer/exit documents also remain open. Neither this draft slice nor the approved
layout/access releases complete Staff Management or issue #1399.

### Staff scope tracker and renewal preparation — 2026-10-29

The [delivery tracker](staff-management-progress.md) consolidates the complete requested Staff scope, current evidence, remaining acceptance and the ordered A01–A12 Access/RBAC journeys. Its renewal checkpoint records new-proposal preparation, local independent maker/checker activation, recipient access, withdrawal/history preservation and fresh-login denial. All 30 frontend tasks passed. This does not complete the Access area or employment suspension/exit/rehire, and no production rollout of this UI slice is claimed.

### Membership workspace precondition checkpoint — 2026-10-29

Renewal preparation #1442 merged after full CI/E2E as `4a6c6528d65a4a10a3fefcf1e5783884205c1518`; production acceptance remains open. The subsequent Access audit reproduced removal ignoring changed/ambiguous expected-tenant headers. A one-line reuse of the enrollment guard now rejects them before writes; the [delivery tracker](staff-management-progress.md#membership-removal-request-guard-and-provisioning-audit--2026-10-29) records red/green and full local IAM/authentication race evidence, compatibility limits and pending delivery gates. This is not a membership-removal UI or complete provisioning/offboarding.
