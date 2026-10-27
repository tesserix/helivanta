# Staff Management workflow acceptance

Current audit: 2026-10-27. Scope is every Staff navigation entry and its complete
business workflows, including account provisioning/verification/offboarding,
staff identity/background verification, protected photographs and identity-card
issuance, supervision, review and payroll. This checklist records evidence, not readiness
inferred from a visible screen or passing schema test.

Work one menu area at a time. Finish **Staff access** first, then continue through
the order below. A dependency in another area can be developed/tested as part of
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

| Order | Area / route                                           | Workflow coverage required                                                                                                                                                                                                                                                                                                        | Current evidence / gaps                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ----- | ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | Staff access `/workforce/access`                       | Identity creation and credential verification; tenant membership; reviewed staff/login binding; scoped assignment proposal, independent approve/reject, withdrawal, activation, expiry, revoke/renew; offboarding, reactivation, access removal and recovery                                                                      | **In progress.** PRs #1381/#1382/#1385 merged and deployed. Synthetic identity created via IdP UI; `staff_member` membership recorded through the live Staff Admin UI. Read-only verification confirms no hospital grant. Reviewer password-change handoff remains pending. Scoped Workforce consumer, usable reviewer queue, independent administrator provisioning/bootstrap and full negative/lifecycle QA remain open. #417/#1053/#1132/#434                                                                                                      |
| 2     | Staff directory `/workforce`                           | Employee/consultant creation; proper staff/employee IDs; identity/background checks; protected photo and ID-card issuance; profile, contacts, qualifications, experience, credentials and private files; supervisory relationships; status changes, transfer, exit, reactivation; account linking, import/export and safe removal | Not yet fully verified in this audit. Preserve Workforce ownership and distinguish staff records from logins and clinical practitioners. #1035/#417/#617/#1105/#1107                                                                                                                                                                                                                                                                                                                                                                                  |
| 3     | HR setup `/workforce/setup`                            | Every master type, code/name uniqueness, dependent references, versions/history, effective dates, activation/inactivation and referenced-record removal                                                                                                                                                                           | Not yet fully verified. Include organization/department ownership and posting/supervision dependencies; do not create a second department master. #1035/#22/#287/#616/#429                                                                                                                                                                                                                                                                                                                                                                            |
| 4     | Shifts `/workforce/shifts`                             | Day/night/overnight definitions, duration/break/grace/locum rules, changes/history, referenced shift inactivation and invalid timing                                                                                                                                                                                              | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 5     | Roster setup `/workforce/roster-setup`                 | Areas, groups, weekly templates, composition, applicability, version/effective-date rules and referenced-record removal                                                                                                                                                                                                           | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 6     | Roster policy `/workforce/roster-policy`               | Settings, staff/template assignments, effective dates, conflicts, history and inactive/transferred staff behavior                                                                                                                                                                                                                 | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 7     | Rosters `/workforce/rosters`                           | Draft creation/population, allocations, overnight/DST windows, locum/additional duty, clashes, credential/leave checks, freeze, correction and discard                                                                                                                                                                            | Not yet fully verified. #1036                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 8     | Leave setup `/workforce/leave-setup`                   | Leave types, calendars and structures; all rules, units, effective revisions, duplicates, inactivation and referenced definitions                                                                                                                                                                                                 | Not yet fully verified. #1037                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 9     | Leave balances `/workforce/leave-balances`             | Year settings, staff structures, allocation/accrual/carry/expiry, monthly posting, corrections, closing, ledger, balances and self-only reads                                                                                                                                                                                     | Not yet fully verified. #1037/#1132                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 10    | Leave requests `/workforce/leave-requests`             | Filing and self-service, eligibility/probation, holiday/half-day/short leave, balance/overlap, independent approve/reject, cancellation/revocation, exact replay and roster/ledger consequences                                                                                                                                   | Scoped-only approval/replay, own-leave denial, link revocation, post-input withdrawal and target-bound RLS regressions pass in development. Post-probation eligibility is checked without enrollment endpoint access. Command-boundary guard and endpoint rollback regressions now pass locally; combined real-IAM Workforce integration and withdrawal/expiry lock races pass; reviewer scope discovery passes locally; queue/detail/UI remain open; this consumer is not deployed. Full employee/reviewer UI journey remains open. #1037/#1132/#434 |
| 11    | Leave reports `/workforce/leave-reports`               | Monthly/status reports, filters, date/year bounds, paging, totals, permission/purpose, absence/staleness and export privacy                                                                                                                                                                                                       | Not yet fully verified. #1037/#1018/#589                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 12    | Duty exchanges `/workforce/duty-exchanges`             | Propose, independent decision, holds/conflicts, cancellation/revocation, roster effects, duplicate/stale/concurrent requests                                                                                                                                                                                                      | Not yet fully verified. #1038                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 13    | Overtime `/workforce/overtime`                         | Policy/settings, filing, eligibility and units, approval/rejection, cancellation/reversal, history and payroll handoff                                                                                                                                                                                                            | Not yet fully verified; approved overtime is not payment. #1038/#1040                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| 14    | Attendance `/workforce/attendance`                     | Punch/void, missing-punch corrections, independent decisions, local/overnight dates, period finalization/reopen, immutable timesheet revisions and payroll handoff                                                                                                                                                                | Not yet fully verified. #1039/#1040                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 15    | Credential register `/workforce/credentials`           | Evidence, validity, independent review, expiry, rejection/revocation, renewal, private disclosure and roster consequences                                                                                                                                                                                                         | Not yet fully verified. Credential evidence never grants clinical privilege. #1108                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| 16    | Credential reminders `/workforce/credential-reminders` | Reviewed reminder policy, durable scheduling, threshold delivery, duplicate/retry handling, acknowledgment and changed/expired credentials                                                                                                                                                                                        | Not yet fully verified against deployed workers and dependencies. #1108                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| 17    | Payroll setup `/workforce/payroll-setup`               | Every head/structure, salary assignment, bank/tax identifiers and reveals, statutory/tax policies, declarations/evidence, independent decisions; attendance/OT inputs; payroll preparation, lock/process/correct, payslips, reports, exports and payment/ERP reconciliation                                                       | Not yet fully verified. Reconcile implemented configuration with missing payroll execution/appraisal/exit workflows; previews are not a posted payroll ledger. Activate jurisdictional policy only with current qualified review. #1040/#1041/#1042/#1107                                                                                                                                                                                                                                                                                             |
| 18    | Benefits `/workforce/benefits`                         | Dependents, relationship/plan applicability, effective coverage, independent proposal decisions, overlap, evidence quarantine/release, expiry/revocation and protected exports                                                                                                                                                    | Not yet fully verified. Dependent enrollment must not create a patient identity automatically. #1106                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 19    | Review leave `/workforce/leave-review`                 | Current scope discovery; bounded pending queue; protected detail; approve/reject; reject reasons; exact uncertain retry; stale/access/workspace states                                                                                                                                                                            | Implemented locally. Real gateway/RLS and nine UI scenarios pass. Browser, independent administrator setup, current-main integration and live two-person release acceptance remain open. #434/#1132/#1053                                                                                                                                                                                                                                                                                                                                             |

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
credentialing concern. The focused issue search returned no dedicated identity-card
story; reconcile the new requirement with these trackers before implementation.

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
changed in this privacy fix. The last pushed head's Go CI did fail the existing
Practitioner concurrent policy-replay test (one 409 where replay requires 200);
that is a separate unresolved release blocker, not a passed backend check.
