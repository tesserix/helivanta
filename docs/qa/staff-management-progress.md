# Staff Management delivery tracker

Updated: 2026-10-30. Owner: Helivanta Staff Management delivery. This is the summary tracker for the full user-requested scope. The [workflow acceptance ledger](staff-management-acceptance.md) holds detailed scenarios and evidence; this document does not replace it.

## Status and completion rules

- **Partial**: implementation and some evidence exist, but required journeys or checks remain open.
- **Audit pending**: implementation exists, but its complete workflow has not been established by this review. This does not mean the feature is absent.
- **Complete**: all applicable server, database, UI, role, failure/retry, persistence, CI and deployed acceptance gates in the ledger pass. There are currently no fully accepted areas.
- Record local positive UI QA separately from production positive or denial QA. A test count, route, screenshot, migration job or published release alone does not establish workflow completion.
- Keep issues open until their actual acceptance criteria pass. Legacy issue references to Keycloak or proposed tables must be reconciled with current Zitadel/IAM/Workforce ownership before implementation.

## Current production release — c20a220, 2026-10-30

The user approved revision `c20a220267cc4e71cc169c47b3ef7220612da1ce` and all four
pending migrations. Kargo promotion `prod.01m659npxcq1ejdjcv41942wbe.06dda82`
succeeded. All six application workloads have two updated/ready/available
replicas on this source; API, Global API and shell are Synced/Healthy. The normal
migration job applied `0002_billing`, `0003_billing`, `0008_medicore` and
`0052_workforce_employment_withdrawal`, with durable `schema_migrations` receipts.
RLS is enabled and forced on all 11 inspected new/related tables.

The [release record](../deployment/releases/2026-10-30-staff-preparation-and-scheduling.md)
and [SQL provenance manifest](../deployment/releases/2026-10-30-staff-preparation-and-scheduling.json)
persist canonical SQL paths, source hashes, registration, application times and
future environment replay instructions. Historical SQL stays in Git and the
embedded owner migration registry; future corrections require new IDs.

Production normal-login QA verified restricted Staff/payroll denial and the
correct payroll heading; Admin preparation/required reason/withdrawal/reload and
immutable history; unchanged inactive Staff; preserved original draft; shift and
roster unsaved-input guards; long-label weekly layout/Remove; and retained shift
and area retirement history. Only designated synthetic records were changed;
no Staff allocation, dated roster, attendance or payroll action was performed.

All required PR CI gates and final image publication passed. Runtime files match
the fully tested PR head, with only the reviewed payroll documentation correction
added at merge. Final-source contractdocs check passed. Full local web checks
passed 30 tasks (1,222 shell and 409 API tests). See the release record for exact
CI references and the distinction between local, production and schema evidence.
The broad acceptance count remains **10 partial, 10 audit pending, 1 research,
0 fully accepted**; the lifecycle, review, payroll/PDF and letter gaps remain open.

## Previous production checkpoint — 22b6241, 2026-10-30

- **Production:** `22b6241ed0ece7180d713b77145104a63d95d343`,
  [employment draft and evidence release](https://github.com/tesserix/helivanta/releases/tag/2026-10-30-staff-employment-evidence),
  [PR #1458](https://github.com/tesserix/helivanta/pull/1458), including draft
  preparation [PR #1457](https://github.com/tesserix/helivanta/pull/1457).
  The user approved this exact rollout and migrations `0050` and `0051`.
- **Deployment verified:** Kargo promotion
  `prod.01m63tbd92xn9gt1ntzzmjb6er.dcd330a` succeeded. All six application
  workloads have two updated, ready and available replicas on the exact source.
  API, Global API and shell are Synced/Healthy. Both migrations are recorded;
  all four new private tables have enabled and forced RLS. CNPG had two healthy
  instances and a successful `2026-10-29T12:00:24Z` backup.
- **Production UI:** fresh synthetic owner login, wrong-tenant no-hospital denial,
  correct-tenant existing manager grant, ongoing draft save, real PNG upload and
  scan, binding to terms revision 1, reasoned withdrawal to binding revision 2,
  retained revisions 1–2 and full reload/reopen. The Staff member stayed inactive.
  Two protected downloads were authorized and audited with 179-byte metadata and
  the expected checksum. **A newly saved production browser file was not captured
  by automation; that artifact check remains unconfirmed.** The earlier local
  downloaded-file SHA-256 check passed.
- **Reviewer:** the user explicitly authorized backend password rotation for the
  synthetic reviewer. Its credential was changed through the existing
  credential-verified password-change backend, the private JSON was updated with
  mode `0600`, and a fresh normal UI login succeeded. Staff hospital access and
  payroll access stayed denied. No role, membership, MFA policy or hospital grant
  was expanded. This is restricted-role QA, not an employment approval.
- **Validation:** [PR CI](https://github.com/tesserix/helivanta/actions/runs/40868299072)
  and [merged-source CI](https://github.com/tesserix/helivanta/actions/runs/40872297255)
  passed, including E2E, Go coverage/latency and web checks. Image publication
  [40872296822](https://github.com/tesserix/helivanta/actions/runs/40872296822)
  passed. The merge tree exactly matches tested PR head
  `ab66664b0ab443011f1e6b2c0280adbd701da8e4`. Final local web gates passed all
  30 tasks (1,195 shell and 408 API tests); Workforce race coverage was 83.6%.
- **Next work:** scoped HR intake administration and a safe current-person basis
  for independent employment submission/review. See the
  [source-verified continuation plan](../superpowers/plans/2026-10-30-staff-access-and-review-continuation.md)
  and [open-issue map](staff-management-open-issues.md). Adding a role label or an
  approval button does not implement transaction-bound authority or safe new-hire
  identity. Effective lifecycle activation remains a separate owner/consumer gate.

No broad Staff area or umbrella issue is newly marked complete. Deployment of
private drafts/evidence does not implement employment approval, access activation,
expiry, suspension, resignation, termination, rehire, payroll runs or letters.
Historical checkpoints retain the state when recorded. The c20a220 release
checkpoint above is the current production state.

## Quantitative checkpoint — 2026-10-30

The 21 tracked areas currently comprise **10 partial (47.6%)**, **10 audit pending
(47.6%)** and **1 research/proposal (4.8%)**. **0/21 are fully accepted (0%)**;
all 21 still have acceptance work. These are area-status percentages, not an
estimate that 38.1% of engineering effort is complete. The remaining scenarios
vary substantially in size, so no defensible overall effort or time percentage
is available yet.

The directory authority and account/version guards are deployed in the production
checkpoint above. Private employment-term draft and protected evidence preparation are deployed
and CI-validated, with production UI evidence and artifact-capture limits above. Scoped HR
role templates, reviewed employment decisions, dated transfers and offboarding
remain open; a directory status or draft term is not proof of operational cutoff.

| Area                            | Status                                      | Verified work / evidence                                                                                                                                                                                        | Remaining acceptance                                                                                                                                                                                                |
| ------------------------------- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Access and RBAC                 | Partial; current priority                   | Scoped proposals, independent decisions, reviewed staff/login links, projection/membership generations, withdrawal, current-identity denial and fresh-login recovery; local synthetic maker/checker/reviewer QA | Identity provisioning and verification; membership recovery; renewal; hospital suspension/resumption; notice, resignation, termination, contract end and rehire; cross-owner cutoff and legacy-grant reconciliation |
| Staff directory and supervision | Partial; profile/supervision slice released | Account/workspace-bound private editing, required fields, persisted revisions, cycle/stale guards and inactive labels; local and production Admin/restricted-reviewer QA in PR #1447                            | Full employee/consultant onboarding, editing by other authorized roles, dated transfers/postings, offboarding and safe removal                                                                                      |
| HR setup                        | Partial                                     | Broad create/edit/history/retire/reactivate coverage across masters                                                                                                                                             | Complete dependency, referenced-record, duplicate/concurrency, isolation and deployed acceptance                                                                                                                    |
| Identity and background checks  | Partial; draft preparation released         | All three unreviewed draft types, create/edit/current/history, immutable snapshots, required fields, replay and scope/actor/source-generation guards                                                            | Protected evidence binding, immutable submission, independent approve/reject/revoke, effective validity, source changes and historical self-review denial                                                           |
| Photographs and ID cards        | Partial foundations                         | Photograph preparation metadata; protected file scanning/release/recovery foundations                                                                                                                           | Trusted PNG/JPEG validation, purpose-bound photo review, self-photo request, frozen branded card issue, independent authorization, preview/print, expiry, loss/reissue and offboarding                              |
| Shifts                          | Partial; local workflow audited             | Synthetic overnight create/edit, validation, revision history, retire/reactivate and reload; unsaved navigation/cancel/history protection fixed                                                                 | Referenced retirement, multi-hospital/current-account scope, downstream roster effects and deployed acceptance                                                                                                      |
| Roster setup                    | Partial; local area workflow audited        | Required fields/review, pinned area membership, retirement/history/reload, inactive-reference rejection and unsaved-input guards                                                                                | Groups/weekly templates, overlaps/effective versions, concurrency, role/isolation and downstream/deployed acceptance                                                                                                |
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

### Active Access/RBAC completion work — 2026-10-30

PR [#1480](https://github.com/tesserix/helivanta/pull/1480) merged as
`7bbdc7c0c41150a01df0907d6b1f2929fdd7b486` after all five required checks passed
on exact head `4731df96cdfd58ebc1dd34ac7b562b2231e92634`: Go, web, browser E2E,
scripts and secret scan. Repository visibility was verified public before merge.
This is a merged-source checkpoint, not a production deployment or A01–A12
completion. The source admission and transaction-bound security-revocation
boundaries below remain unchanged.

The next local [incarnation graph implementation](../superpowers/specs/2026-10-30-membership-incarnation-graph.md)
now binds graph permission/membership proof to each immutable row, actual subject,
role, tenant and continuity epoch. It rereads source after provider checks and
uses the same adapter for genuine login, discovery, switching and renewal.
Membership events reconcile current source after the event claim commits;
network work no longer holds that claim transaction open. Boot reconciliation
uses the same tuple factory, keeps current incarnation tuples and retires old
ones. Hospital onboarding now uses this path as well.

The absent-membership delayed-grant and re-enrollment-borrowing regressions now
pass locally with real PostgreSQL/OpenFGA, including delayed old revocation that
preserves the replacement. The raw subject/role DELETE regression remains red.
Real provider/proxy tests reject incomplete, malformed, conflicting and failed
proofs instead of accepting partial authority. Focused signed-session, source
race, boot/prune, HTTP seam and approved permission-matrix checks pass. The
fixed-source affected race/coverage run passed authorization, reconciliation,
architecture, bootstrap and API suites. IAM's only failing test is the retained
unsafe raw-DELETE replay regression. Vet, affected linter (zero issues), API and
Global API builds, and contractdocs generation/check passed. No new source is
merged or deployed and no A-item is newly complete.

Migration `0017_iam_membership_projection_reader` grants only SELECT of IAM
continuity metadata to the existing system role for startup reconciliation.
Ordinary permission and role-discovery reads still use normal forced-RLS tenant
transactions. This migration is persisted in the source registry and has run
only in disposable fixtures. The IAM migration-count assertion now pins all 17
IDs. The local application database and production have not been migrated.

The follow-up branch has local membership-command implementation, canonical
Go/Zod models, additive Global API routes, reasoned immutable receipts/history,
fresh ordinary-membership recovery and Staff Admin controls. New SQL
`0015_iam_membership_commands` has run only in disposable test databases, not
the local application database or production. Concurrent-index migration
`0016_iam_membership_candidate_index` has also passed disposable migration/replay
tests; a real forced-RLS query plan uses the new ordering index with 100,000
synthetic neighbouring subjects. Focused real-PostgreSQL race tests
pass for exact-role removal, preserved other roles, final-removal epoch change,
new ordinary membership, old-command replay, source/tenant/role/epoch denials,
inactive/provider-outage recovery refusal and immutable transition evidence.
Focused shell/API tests pass for required reason/acknowledgement, frozen uncertain
retry and recovery from the current epoch. Broader tests and real UI acceptance
are still in progress. At the `e58511e7` checkpoint all three replay/projection
regressions were red; the graph follow-up above now fixes two. This work is not a
release candidate and no A-item is newly complete.

A third real PostgreSQL/OpenFGA regression originally reproduced re-enrollment borrowing
an old graph tuple: the original membership is projected through the registered
consumer, removed, and replaced by a distinct unprojected source row; current
admission incorrectly reported membership. Exact-incarnation graph proof is now
implemented locally and this assertion passes. Generation-bound owner eligibility
remains required; neither this graph proof nor the command/UI work proves
historical Staff-link/legacy-grant cutoffs after recovery.

A same-tenant account-change regression additionally reproduced 202 instead of
409 in the new command before account pinning. All three new routes now require
the verified expected tenant/subject pair, and the normal client sends both.
The current race regression rejects stale, absent or duplicated expected subject
without mutating the membership. The earlier full Go run passed architecture,
bootstrap, API and authz but failed the two known replay regressions and a stale
14-migration count assertion. That assertion was updated for real schema
additions, not removed; the current follow-up pins 17 migration IDs.

The next [membership removal/recovery design](../superpowers/specs/2026-10-30-membership-removal-and-recovery.md)
is source-verified and remains unaccepted. Two disposable local regressions
originally failed: repeating a subject/role DELETE after re-enrollment removes the replacement
membership, and a historical grant delivered through the real registered
consumer recreates graph membership when IAM has no membership. The first used
real PostgreSQL/NATS; the second additionally used real OpenFGA. No production or
local application memberships were changed. Current-source admission separately
denies stale graph authority; the later local incarnation proof fixes the delayed
grant/re-enrollment graph gaps. Command migration and owner recovery/cutoff gates
remain open. A reproduced defect is not acceptance evidence for a completed workflow.
The full objective remains A01–A12.

The user selected full Access/RBAC completion again, including every A01–A12
journey above. The [completion plan](../superpowers/plans/2026-10-30-staff-rbac-completion.md)
keeps that full objective and the owner/consumer dependencies explicit.

The first source-verified defect is stale graph authority after IAM role removal.
A real PostgreSQL/OpenFGA test with an existing signed session reproduced 200
instead of 403 after removal of its administrator role, while ordinary membership
remained. The [current-role admission candidate](../superpowers/specs/2026-10-30-current-tenant-role-authority.md)
now bounds graph permissions by current IAM roles and canonical registry grants,
with the same adapter wired to both production V1 admission gates. Graph and
source outages deny; graph-only or SQL-only authority cannot grant permission.
Final membership removal, late grant replay, another tenant's preserved authority,
authenticated self-service and removal during a delayed graph response have
focused passing tests. This adds no SQL migration or permission.

Local race/coverage passed IAM 82.1%, bootstrap 94.1% and authz 80.5%, including
the IAM login client and API construction tests. Vet, API/Global API builds,
affected lint and contractdocs check passed. A 100,001-row isolated fixture
verified the indexed bounded lookup; the final ordinary-RLS plan used a bitmap
index scan and reported 0.031 ms execution locally. This is not a measured
production latency SLO. The scale-test setup's initial admin-accessor use was
removed after the architecture guard rejected it. The final complete architecture
race suite passed, including every declared role against the independent
permission matrix, unsupported foreign graph authority and stale role removal.

The local candidate is healthy behind the local Global API. The first browser
attempt used an unregistered numeric-loopback callback. Read-only metadata from
the configured local identity provider identified the registered
`http://helivanta.localhost:4399` origin; using it restored the Staff access UI
without changing browser protections or identity-provider permissions. Existing
Admin-session reads pass. The exact clean `e047c56` API build then passed normal
sign-out/password sign-in for the synthetic Admin and existing pharmacist/reviewer
fixture through the local Global API. Admin enrollment/proposal controls remain
available, blank enrollment is rejected, the reviewer cannot administer assignments
or access payroll, and its existing hospital reader grant retains the six-row
read-only directory without edit/enrollment controls. No memberships, roles,
hospital grants or assignments were changed by this UI smoke test. This does not
prove independent employment approval or the unimplemented removal/recovery UI.
This candidate is not production-deployed. No A-item or broad Staff area is
marked complete. Incarnation-bound membership commands/UI, provisioning,
reviewed employment, legacy-grant classification and effective cross-owner
lifecycle cutoffs remain in the full plan. The broad counts remain 10 partial,
10 audit pending, 1 research and 0 fully accepted.

A second A06/A11 source finding concerned explicit global credential revocation:
the old handler trusted a stale graph target binding after that target left the
acting tenant. The [transaction-bound revocation candidate](../superpowers/specs/2026-10-30-current-credential-revocation-scope.md)
now requires current actor/target memberships and a fresh acting credential,
including after real credential-lock waits. Scoped target loss returns 404,
administrative role loss 403 and acting credential revocation 401; source outage
returns 503. The existing workspace precondition rejects stale requests before
external lookup. Real NATS observation verifies the unchanged global envelope;
other-tenant memberships remain intact. A failing real outbox INSERT rolls back
the watermark. Self and reciprocal administrator revocation are tested without
lock upgrades or deadlock. No new production migration is added. The prior
admission commit's five required CI checks passed. The extended candidate's full
IAM race/coverage (82.8%), architecture, bootstrap, API and authz suites passed,
as did affected vet/lint, API/Global API builds and contractdocs generation/check.
Its two membership queries used existing indexes in the 100,001-row forced-RLS
fixture; the locked actor/target query reported 0.080 ms local execution. This is
not a production SLO. The extended source commit's required CI passed before
PR #1480 merged; [post-merge CI](https://github.com/tesserix/helivanta/actions/runs/41014627993)
also passed on `7bbdc7c`. Production rollout, full A01–A12 workflow and deployed
acceptance remain open.

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

## Denied payroll page heading — 2026-10-30

Production reviewer QA on release `22b6241` found that a denied payroll page
incorrectly displayed “Staff directory” in the shell header. Permission-filtered
navigation had hidden the payroll entry before the shell selected its title.
The correction selects requested-page metadata from the route catalogue while
retaining permission-filtered navigation and the existing payroll denial.

The regression first failed with expected “Payroll setup”, received “Staff
directory”; after the correction all 50 shell navigation tests passed. Local
browser QA used the existing restricted `pharmacist Seed` fixture through normal
sign-in: the heading was “Payroll setup”, payroll navigation was absent, the
Staff directory link had no current-page marker, and the page denied payroll
access. This local fixture is separate from the production Staff reviewer.
No roles, grants, credentials, backend contracts or migrations changed in this
UI fix. It has not yet been rolled out to production. The broad acceptance counts
above are unchanged; a corrected denial heading does not complete a Staff domain.

## Employment draft withdrawal continuation — 2026-10-30

The denied-page heading fix merged as `6cd135d5ff83a3b26558b58dbe7a92deaba03fc8`
in [PR #1462](https://github.com/tesserix/helivanta/pull/1462), after Go, web,
browser E2E, security and image checks passed. Production remains on `22b6241`;
this merge has not been promoted.

The [withdrawal design](../superpowers/specs/2026-10-30-employment-draft-withdrawal.md)
and [owner contract](../api/workforce.md#withdrawal-of-prepared-employment-terms)
add a reasoned terminal state for employment preparation. A current hospital
manager can close a draft, retain its original terms and history, and recover an
exact command receipt. A closed draft cannot be edited, reopened or receive new
evidence. Legacy reads stay draft-only; the new UI explicitly requests the
withdrawal lifecycle projection. This is not employment termination or approval.

Local UI QA used the existing synthetic contractor and normal administrator
login. It created a separate fixed agency-worker draft with a named contracting
party, active hospital sponsor and required dates/reference/reason, verified the
blank withdrawal reason was blocked, then withdrew it. A full browser reload and
reopening showed terminal revision 2 and both history revisions. The Staff record
remained inactive; the previously existing contractor draft was unchanged. A
separate restricted local login saw “No hospital access yet”, with no employment
terms or withdrawal controls. No role or hospital grant was added for this QA.

Read-only local fixture verification found status `withdrawn`, version 2, two
retained revisions and exactly one attributable withdrawal audit. That
administrative verification is persistence evidence, not a runtime authorization
claim. Real-owner tests separately cover missing permission/foreign scope,
required fields, source/sponsor drift, stale/reused/duplicate commands, immutable
terms and terminal database guards. Evidence integration tests reject attachments
to either old or terminal revisions after closure while retaining protected
historical downloads and denying an unauthorized reader.

Migration `0052_workforce_employment_withdrawal` is implemented locally; it has
not been approved or run in production. Full validation/release evidence is
recorded in [PR #1463](https://github.com/tesserix/helivanta/pull/1463). The initial parallel full backend run encountered
shared Docker dependency readiness/connection failures; the rerun uses bounded
package parallelism and its result is recorded in the PR; no test is skipped or acceptance exception granted.
The initial combined web run found a panel lint violation (corrected) and sign-in
test timeouts. The subsequent complete gate passed all 30 tasks, including
1,200 shell tests and 409 API tests; the final form-control regression also passed.
CI subsequently detected the newly published query-decoding advisory. Its
merged backport from PR #1466 and the additional actual-consumer regression are
documented in the [vulnerability runbook](../runbooks/vulnerability-management.md).
The branch uses the main-line backport and its reviewed version-specific audit
exception; its earlier duplicate ESM adaptation was removed.

CI also reproduced a test-fixture naming defect: a long identity-case test name
plus its iteration suffix exceeded OpenFGA's 64-character store-name limit.
The shared isolation helper now bounds an ASCII-readable prefix while retaining
its per-test stable, per-iteration unique suffix. Long/Unicode names and repeated
iterations have a regression test. This changes test identifiers, not production
role or identity behavior. The current checkout declares Go 1.27.3; full local
validation uses that version and the QA VM's direct Docker socket.

All broad acceptance counts remain 8 partial, 12 audit pending, 1 research and
0 fully accepted. The scoped HR intake role still needs the action/route and
transaction/disclosure guards described in the access/review continuation plan.
Independent employment/identity review, effective lifecycle enforcement, transfers,
photo/card issuance, operational scheduling and payroll/document issuance remain
open. This bounded draft transition does not complete those journeys.

## Shift definition audit and navigation correction — 2026-10-30

Local administrator UI QA in the designated synthetic hospital created an
unallocated night/locum shift, 22:00–06:00 (480 minutes, next day), with break,
grace and overtime configuration. Blank required fields, equal times, a break
as long as the shift and half-day OT above full-day OT were rejected. The code
became immutable after creation. Edit, retirement and reactivation retained
revisions 1–4 after reload. The restricted local fixture had no hospital access
or Add shift action. This is denial evidence, not independent approval.

The audit reproduced silent loss of unsaved text on Staff directory navigation.
The correction reuses the shell navigation guard for route changes, Cancel,
hospital selection and Add/Edit/history replacement; pending saves cannot be
interrupted. Component regressions were observed red before the fix. Eight
focused tests pass, including same-form replacement, focus return and pending
writes. Real local UI verified route and Cancel prompts, Keep editing retained
the exact text, and explicit Discard opened history with the saved revision.
Closing history restored focus to its opener. A final UI save retired the
unallocated synthetic shift as revision 5, without a false discard prompt;
reload retained all five revisions and original timing/rules. No roster,
attendance or payroll action was performed.

Validation: `pnpm turbo lint format:check type-check` passed 21 tasks;
`pnpm turbo test` passed all seven package tasks; `pnpm turbo build` passed both
build tasks; script tests passed 19/19, under Node 24. Private screenshots include
`local-shift-unsaved-navigation-guard.jpg` and
`local-shift-guard-retired-history.jpg`. No API, permission, schema or migration
changed. Related story #1036 and umbrella #118 remain open. Referenced-shift
retirement, multi-hospital/current-account scope, consumer and production QA are
still required. Shifts moves from Audit pending to Partial: **9 partial,
11 audit pending, 1 research, 0 fully accepted**. These are area counts, not an
effort-completion percentage. Earlier checkpoints retain their historical counts.

## Roster setup draft protection — local continuation 2026-10-30

The next UI audit confirmed that the retired synthetic shift was unavailable for
new area membership. Following the in-form shift link after entering an area
name then silently lost that input. No configuration was saved in reproducing
this defect. The correction registers the existing navigation guard and uses
GuardedLink. Voluntary kind/hospital changes, Cancel, Add/Edit/history replacement
now share the same guard; confirmed replacements reset their form session and
pending writes retain their original context.

Regressions for Cancel, kind change, history, same-new-form replacement and the
shift link were observed failing before correction. All 11 roster component
tests then passed, including hospital scope, same-record replacement, pending
save and successful close/focus behavior. Real local UI verified the shift-link
prompt, exact name preservation on Keep editing, retained Roster areas selection
after cancelling a kind change, and explicit Cancel/Discard with opener focus.
Private screenshot: `local-roster-setup-unsaved-guard.jpg`.

Roster setup remains Audit pending. Saved area/group/template composition,
effective revisions, referenced retirement, multi-hospital isolation, downstream
roster behavior and deployed QA are still open. The count remains 9 partial,
11 audit pending, 1 research and 0 fully accepted. No API, schema, permission or
migration changes are introduced; no production or roster/personnel data was
changed by this continuation. Full final web/CI gates are recorded in the PR
before merge.

### Roster area reference and retirement QA — 2026-10-30

Using normal local Staff Admin controls, QA created a separate unallocated day
shift (08:00–16:00, 480 minutes) and roster area effective 2026-10-30. Missing
name and missing review acknowledgement were rejected. The saved area retained
its selected shift after full reload. Retiring that shift as revision 2 preserved
the area's original shift revision/timings, visibly labelled Earlier revision
retained, while removing it from new selections. Changing the area's effective
date to 2026-10-31 was rejected with input preserved. Restoring the original date
and retiring the area saved revision 2. A subsequent attempted reactivation with
the retired shift was rejected; QA explicitly discarded the unsaved attempt.
Reload/history retained both area revisions and pinned 08:00–16:00 membership.
The synthetic day shift and area finish retired; no Staff was allocated.

This provides local area-level evidence, not all roster setup acceptance. Roster
setup now becomes Partial, bringing the current count to **10 partial (47.6%),
10 audit pending (47.6%), 1 research (4.8%), 0 fully accepted**. Group/template
composition, weekly off/overnight overlaps, effective-version concurrency,
role/isolation, downstream and production QA remain open. Historical checkpoints
above keep their earlier counts. Private evidence:
`local-roster-area-retired-reference-history.jpg`.

## Shift groups and weekly-template layout QA — 2026-10-30

Local normal Staff Admin UI continued the same synthetic hospital workflow. An
empty shift group was rejected, then a current day-shift revision was selected
and the group saved. A weekly working day without area/shift selections was
rejected. Selecting the same shift twice on Monday was rejected before any
save. Weekly off/on preserved the entered choices. A corrected Monday-only
pattern saved with Tuesday–Sunday off; full reload/reopen and immutable history
retained its choices. Group and template retirement each saved revision 2;
subsequent history reads retained both revisions and original timings. The
area was reactivated only after replacing its retired reference with a current
shift revision. At the end, group/template are retired at revision 2, area at
revision 4, day shift at revision 4 and night shift at revision 5. No Staff
assignment, dated roster, attendance or payroll action was performed.

The walkthrough exposed an actual layout obstruction with long area/shift
labels: Monday's fieldset expanded into Tuesday's grid track, and a browser
hit test at Remove's center returned Tuesday's fieldset. The functional component
remove/correct/resubmit test passed before the layout fix, so it is supplementary
behavior coverage, not claimed as a failing layout regression. The production
code correction is one line: constrain the weekday fieldset's minimum width.
After loading the updated source, the visible button's center hit Monday's
Remove button, clicking removed only slot two, and the corrected pattern saved.
At 1280 px, weekday cards did not overlap and no button extended outside its
fieldset. At 768 px, cards did not overlap, no control overflowed and the page
fit the viewport. The viewport override was reset.

Private evidence includes `local-weekly-remove-obstructed-before.jpg`,
`local-weekly-remove-layout-fixed.jpg`, `local-weekly-layout-1280.jpg`,
`local-weekly-template-retired-history.jpg` and
`local-shift-group-retired-history.jpg`. The restricted local fixture separately
had no hospital roster configuration or Add controls after fresh normal login;
that is denial evidence, not an independent approval.

The count remains **10 partial, 10 audit pending, 1 research, 0 fully accepted**.
Different-shift overlaps, overnight/weekly-off spill, effective-date concurrency,
full multi-hospital/current-actor isolation, downstream assignment/population and
production QA remain open. No API, role, schema, dependency or migration changes
are introduced by this layout correction. Final package/web/CI evidence belongs
in the associated PR before merge; no additional domain completion is claimed.
