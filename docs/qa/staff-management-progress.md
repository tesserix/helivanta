# Staff Management delivery tracker

Updated: 2026-10-29. Owner: Helivanta Staff Management delivery. This is the summary tracker for the full user-requested scope. The [workflow acceptance ledger](staff-management-acceptance.md) holds detailed scenarios and evidence; this document does not replace it.

## Status and completion rules

- **Partial**: implementation and some evidence exist, but required journeys or checks remain open.
- **Audit pending**: implementation exists, but its complete workflow has not been established by this review. This does not mean the feature is absent.
- **Complete**: all applicable server, database, UI, role, failure/retry, persistence, CI and deployed acceptance gates in the ledger pass. There are currently no fully accepted areas.
- Record local positive UI QA separately from production positive or denial QA. A test count, route, screenshot, migration job or published release alone does not establish workflow completion.
- Keep issues open until their actual acceptance criteria pass. Legacy issue references to Keycloak or proposed tables must be reconciled with current Zitadel/IAM/Workforce ownership before implementation.

## Current delivery checkpoint — 2026-10-29

- **Production:** `5fb03897dd0c2d7afca580c44edc21b987909b52`, released as
  [Staff profile editing and integration recovery](https://github.com/tesserix/helivanta/releases/tag/2026-10-29-staff-profile-editing).
  All five approved migrations were verified applied; six application workloads
  had two ready/available replicas and API/Global API/shell were Synced/Healthy.
  Production Admin edit/save/reopen/restoration passed on retained synthetic Staff.
- **Merged, not yet deployed:** [PR #1457](https://github.com/tesserix/helivanta/pull/1457),
  `334a6117090ccec4a2b69ba69af0c056413ff4da`. Its tree matches the fully passing
  final PR head `1894bac7d2a33572c8999ecb901a7ecc0b590a1f`.
  [CI 40852324440](https://github.com/tesserix/helivanta/actions/runs/40852324440)
  passed web, end-to-end, scripts, secret scan and full Go/latency checks.
  [Image publication 40856363955](https://github.com/tesserix/helivanta/actions/runs/40856363955)
  succeeded. Production approval of this exact revision and
  `0050_workforce_employment_period_drafts` is still pending; the earlier approval
  for five inherited migrations does not include this new migration.
- **Local acceptance for the draft slice:** required fixed/ongoing terms,
  contractor/agency party and named active sponsor, create/edit/reload, immutable
  revisions, two-tab stale refusal, exact uncertain retry, source/version/scope
  denial, and explicit keep-editing/discard behavior. Final Node 24 web gates
  passed all 30 tasks, including 1,193 shell and 406 API tests. Full Workforce
  race/coverage passed at 83.8%; full architecture and contractdocs checks passed.
- **Protected employment evidence — locally verified candidate:** scanned-file
  binding to an exact immutable terms revision, terminal reasoned withdrawal,
  retained receipts/history, combined fresh enrollment/document permissions and
  current hospital manager scope. No new approval, access grant or employment
  activation is introduced. Migration `0051_workforce_employment_evidence` is
  additive and has only been applied locally; production approval is pending.
- **Validation:** full affected Go race/coverage passed (Workforce 83.6%,
  contractdocs 80.7%, architecture passed); additional evidence/document race
  checks passed. Final Node 24 web gates passed all 30 tasks, including 1,194
  shell and 408 API tests. Real local Staff Admin UI verified required fields,
  upload, scanner freshness failure/recovery, ready-file selection, navigation
  keep-editing, attach, checksum-matching download and reasoned withdrawal with
  both immutable revisions. Synthetic contractor remained inactive.
- **Next slice:** immutable employment submission and independent review, with
  source-change/concurrency and downstream access activation gates. Evidence
  binding records scanned bytes; it does not verify the employment claim.

No full Staff area or broad GitHub issue is newly marked complete. Contract
expiry, suspension, resignation, termination, rehire, transfers, cards, payroll
execution and generated letters still need their complete owner/consumer tests.

## Quantitative checkpoint — 2026-10-29

The 21 tracked areas currently comprise **8 partial (38.1%)**, **12 audit pending
(57.1%)** and **1 research/proposal (4.8%)**. **0/21 are fully accepted (0%)**;
all 21 still have acceptance work. These are area-status percentages, not an
estimate that 38.1% of engineering effort is complete. The remaining scenarios
vary substantially in size, so no defensible overall effort or time percentage
is available yet.

The directory authority and account/version guards are deployed in the production
checkpoint above. Private employment-term draft preparation is merged and fully
CI-validated, with its production migration still awaiting approval. Scoped HR
role templates, reviewed employment decisions, dated transfers and offboarding
remain open; a directory status or draft term is not proof of operational cutoff.

| Area                            | Status                                      | Verified work / evidence                                                                                                                                                                                        | Remaining acceptance                                                                                                                                                                                                |
| ------------------------------- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Access and RBAC                 | Partial; current priority                   | Scoped proposals, independent decisions, reviewed staff/login links, projection/membership generations, withdrawal, current-identity denial and fresh-login recovery; local synthetic maker/checker/reviewer QA | Identity provisioning and verification; membership recovery; renewal; hospital suspension/resumption; notice, resignation, termination, contract end and rehire; cross-owner cutoff and legacy-grant reconciliation |
| Staff directory and supervision | Partial; profile/supervision slice released | Account/workspace-bound private editing, required fields, persisted revisions, cycle/stale guards and inactive labels; local and production Admin/restricted-reviewer QA in PR #1447                            | Full employee/consultant onboarding, editing by other authorized roles, dated transfers/postings, offboarding and safe removal                                                                                      |
| HR setup                        | Partial                                     | Broad create/edit/history/retire/reactivate coverage across masters                                                                                                                                             | Complete dependency, referenced-record, duplicate/concurrency, isolation and deployed acceptance                                                                                                                    |
| Identity and background checks  | Partial; draft preparation released         | All three unreviewed draft types, create/edit/current/history, immutable snapshots, required fields, replay and scope/actor/source-generation guards                                                            | Protected evidence binding, immutable submission, independent approve/reject/revoke, effective validity, source changes and historical self-review denial                                                           |
| Photographs and ID cards        | Partial foundations                         | Photograph preparation metadata; protected file scanning/release/recovery foundations                                                                                                                           | Trusted PNG/JPEG validation, purpose-bound photo review, self-photo request, frozen branded card issue, independent authorization, preview/print, expiry, loss/reissue and offboarding                              |
| Shifts                          | Audit pending                               | Existing implementation                                                                                                                                                                                         | Day/night/overnight rules, breaks/grace/locum, edits/history, invalid timing and referenced retirement                                                                                                              |
| Roster setup                    | Audit pending                               | Existing implementation                                                                                                                                                                                         | Areas/groups/templates, composition/applicability, effective versions and referenced retirement                                                                                                                     |
| Roster policy                   | Audit pending                               | Existing implementation                                                                                                                                                                                         | Staff/template assignments, conflicts, effective versions and inactive/transferred staff                                                                                                                            |
| Rosters                         | Audit pending                               | Existing implementation                                                                                                                                                                                         | Populate/allocate, overlaps, credentials/leave, freeze/correct/discard, overnight dates and downstream consequences                                                                                                 |
| Leave setup                     | Audit pending                               | Existing implementation                                                                                                                                                                                         | Types/calendars/structures, units/rules, effective revisions and retirement                                                                                                                                         |
| Leave balances                  | Audit pending                               | Existing implementation                                                                                                                                                                                         | Allocation/accrual/carry/expiry, monthly posting, correction/close, ledger and self-only reads                                                                                                                      |
| Leave requests and review       | Partial                                     | Scoped queue/detail/decision flows, self-review denial, cancellation/revocation/replay and current-authority races; local independent UI QA                                                                     | Complete setup-to-balance-to-request-to-roster journey, all role/state boundaries and deployed positive acceptance                                                                                                  |
| Leave reports                   | Audit pending                               | Existing implementation                                                                                                                                                                                         | Filters/bounds/totals/pagination, absence/staleness and protected exports                                                                                                                                           |
| Duty exchanges                  | Audit pending                               | Existing implementation                                                                                                                                                                                         | Proposal/decision, holds/conflicts, cancellation/revocation and roster effects                                                                                                                                      |
| Overtime                        | Audit pending                               | Existing implementation                                                                                                                                                                                         | Policy/units, eligibility, independent decisions, reversal/history and payroll handoff; approval is not payment                                                                                                     |
| Attendance                      | Audit pending                               | Existing implementation                                                                                                                                                                                         | Punch/void/correction, independent decision, overnight dates, finalize/reopen, immutable timesheet and payroll handoff                                                                                              |
| Credentials and reminders       | Audit pending                               | Existing register/review/reminder implementation                                                                                                                                                                | Evidence/expiry/renewal, private disclosure, roster consequences and real worker retry/delivery acceptance; credentials do not confer clinical privilege                                                            |
| Payroll                         | Partial configuration                       | Salary-head create/edit/history, synthetic structure/assignment checks, missing-policy reporting and production restricted-access denial                                                                        | Reviewed jurisdictional policies; runs/calculation/approval/locking/posting, corrections/reconciliation, final settlement and professional branded Indian payslip PDFs with applicable tax/PF/ESI components        |
| Benefits                        | Audit pending                               | Existing implementation                                                                                                                                                                                         | Dependent/plan eligibility, independent decisions, overlap/evidence/revoke/expiry and protected exports                                                                                                             |
| Offer and exit documents        | Research/proposal                           | Generated-document research exists                                                                                                                                                                              | Reviewed templates, hospital logo/branding, versioned data capture, authorization/approval, protected Go PDF generation/download and correction/history                                                             |
| Cross-cutting UI and QA         | Partial                                     | Full-width Staff workspace, responsive layout checks, explicit access denial and latest full Go/frontend validation                                                                                             | Consistent spacing and accessible forms for every changed workflow; all required fields and edits; no stale subject/tenant data; all remaining journeys through the UI                                              |

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
