# Distributor structure and scoped access — verification

Issue [#1137](https://github.com/tesserix/helivanta/issues/1137), under epic [#1133](https://github.com/tesserix/helivanta/issues/1133). See the [architecture and editable diagram](../architecture/distributor-scoped-access.md), [design](../superpowers/specs/2026-10-17-distributor-structure-and-access.md), and [Mednet comparison](../requirements/pharmaconnect-legacy-procurement.md).

The implementation was merged in [#1199](https://github.com/tesserix/helivanta/pull/1199)
and released as `51a91d7a6c6f862f90e08cd042499222df707468` after explicit approval
for the rollout and migrations `0007_supply` and `0008_supply`. The story remains
open while production API/UI acceptance and the policy-aware form follow-up are
completed. Deployment health alone is not completion evidence.

## Behavior and evidence

| Requirement                                                                 | Verification                                                                                                                                                                                                                                                                                                                                                   |
| --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Corporate root, subsidiary, registrations, branch, warehouse/depot and bins | Real PostgreSQL tests enforce ownership, two levels, country boundaries and immutable versions. Genuine OIDC acceptance links IN/PK companies and proves no inherited access. Existing legal/registration masters remain canonical.                                                                                                                            |
| Territories and sales assignments                                           | Current effective sales authority is required; assigned users see their coverage and their own assignment only. PostgreSQL, UI and authenticated API acceptance cover replay, revocation and inaccessible history.                                                                                                                                             |
| Role and tenant separation                                                  | The independent permission matrix, forced-RLS tests and actual sessions distinguish finance, inventory, sales, drivers, auditors/regulators and Pharmacy roles. Location-only `operating_roles` never become private-profile roles.                                                                                                                            |
| Current grants, expiry and recovery anchor                                  | Tests cover unknown/foreign users, policy failures, scheduled/expired/revoked grants, delegation horizons, legacy replay/revocation and the permanent administrator anchor. A finite grant cannot replace the last permanent administrator.                                                                                                                    |
| Operational access limits                                                   | Ten real PostgreSQL/NATS race tests cover versioned settings, exact retry, concurrency, rollback, direct SQL constraints, expiry, limits on new/extended access and pending support approval. Existing grants retain their recorded expiry and require explicit revocation.                                                                                    |
| Assigned deliveries and archive                                             | Fourteen operating/delivery race tests cover minimal driver DTOs, assignment generations, stale commands, replay, revocation, tenant/location denial and inactive-origin mutation guards. Global administrators/auditors retain visibly read-only archived delivery history; scoped users and mutations remain denied.                                         |
| Independently approved support                                              | Support tests and real sessions exercise separate requester/approver/beneficiary, narrow audited counts, current approver capability, revocation and fail-closed identity checks. UI tests hide counts on expiry/denial and prohibit blanket profile access.                                                                                                   |
| No duplicated stock, financial or clinical effects                          | A bootstrap integration test creates nine nonempty Pharmacy tables and four API read models, then assigns/starts/delivers a Supply task with retries. The tables/read models remain unchanged and exactly three minimized delivery events are published. A temporary extra receipt made this assertion fail; it was removed before the final passing race run. |

The authenticated acceptance uses real Zitadel password/PKCE and Helivanta
sessions, including a fresh-session persistence check. It was run first against
the API and then through the Next.js production standalone proxy. Tests respect
`Retry-After`; the browser's actual retry also recovered from rate limiting during
concurrent scripted activity. No quotas or authentication protections were
disabled.

## Local environment

The task uses a separate scratch PostgreSQL database, non-superuser/non-BYPASSRLS
owner and app roles, a separate OpenFGA store and a separate NATS server. Only
configured synthetic IAM memberships were copied; no legacy customer, staff,
patient, banking or clinical rows were imported. A separate synthetic approver was
created without changing existing credentials. Secrets and session cookies stay
outside Git in private local files.

The UI runs the generated standalone artifact on Node 24 at
`http://helivanta.localhost:4398`, with static/public assets copied as in
`Dockerfile.shell`, against the task API on port 8096. Only the previous task's
verified API/Next processes were replaced. User development servers and other
worktrees were preserved.

Both migrations applied to the scratch database. Subsequent unreleased function
corrections were reapplied transactionally only there; fresh database test
harnesses exercise the final embedded migrations. Production migrations were
subsequently applied by the approved GitOps PreSync hook, as recorded below.

## Commands and limits

The integrated backend passes `GOTOOLCHAIN=go1.27.2 GOFLAGS=-p=1
./scripts/coverage-gate.sh` against the isolated PostgreSQL/NATS environment.
Supply coverage is **81.1%** and every gated package exceeds 70%. Build, vet and
golangci-lint pass with zero lint issues. `govulncheck ./...` reports no reachable
or imported-package vulnerabilities and one uncalled required-module advisory.
Supply and Pharmacy contract-drift checks pass; the final staged secret scan is
clean.

The preceding full run found an obsolete scoped-access assertion and two
unchanged IAM cache-test timeouts. The assertion now checks separate operating
roles while private access stays denied. Both isolated cache tests and the
complete integrated race/coverage gate pass without skipped tests or timeout
changes.

The full serial workspace run passes **402 shell tests / 59 files**, **227 API
tests / 32 files**, **93 shared UI tests / 13 files**, and two script tests. The
integrated source passes all **87 Supply tests / 12 files**. All 15 static tasks
and the production standalone build pass. An earlier parallel build/test run
exceeded unrelated payroll tests' unchanged five-second limits; their isolated
rerun and the subsequent complete serial suite pass.

The final integrated API passes all eight grouped synthetic acceptance scenarios
through the production Next standalone proxy. Rebuilt desktop/mobile browser
checks verify profile focus below the sticky header (heading y128, header end
y65), mount-time numeric focus, retained reason-field typing and keyboard order.
At 390px, numeric fields increased from 126px to 200px, Save fits its form and the
document width remains 390px. No policy/grant changes were saved in the browser.
A final text-only clarification explains the permanent administrator rule;
14 affected tests, 15 static tasks, a fresh production build and rendered browser
verification pass after that copy change.

`make lint-shell` could not bind-mount the worktree into the remote Docker VM.
The same 11 discovered scripts and `.shellcheckrc` were copied through Docker's
API and checked with the repository's identical pinned ShellCheck image; this
passed without changing source or exclusions.

`EXPLAIN (ANALYZE, BUFFERS)` under the ordinary app role used
`supply_territories_page` and `supply_access_grants_page` for scoped cursor reads;
the small delivery fixture used its scoped driver index and a small sort.
Observed execution times were approximately 0.54 ms, 0.09 ms and 8.06 ms
respectively. These are synthetic fixture measurements, not a capacity benchmark
or evidence for the planning p99 target.

## Production release

The main image workflow [39019887790](https://github.com/tesserix/helivanta/actions/runs/39019887790)
passed builds, scans, SBOM generation, signing and deploy advancement. The main
repository checks [39019887948](https://github.com/tesserix/helivanta/actions/runs/39019887948)
also passed. Kargo promotion `prod.01m54j6dmp8z331me2kfhcqhmk.48bbb07` succeeded
at 09:14:41 UTC on 2026-10-17 through the normal image/deploy/Kargo/Argo pipeline.

Read-only verification in GCP project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`,
confirmed:

- All four deployments reached 2/2 desired, updated and available replicas;
  all eight application pods were Ready with zero restarts on the exact release.
- Argo applications were Healthy/Synced, and the migration hook succeeded at
  09:14:01 UTC. The Supply ledger recorded `0007_supply` at 09:13:53.427503 UTC
  and `0008_supply` at 09:13:53.715493 UTC.
- All 14 new Supply tables enabled both RLS and FORCE RLS. CNPG remained 2/2
  healthy, continuous archiving succeeded and the latest backup completed at
  06:00:19 UTC.

No imperative SQL, deployment, scaling or quota edits were used. Temporary
startup/quota warnings resolved through normal reconciliation. The repository
remains public. A separate read-only acceptance preflight pinned the release,
image digests, all replicas, migration hook and schema before fixture writes.

A bounded follow-up at 09:28:17 UTC confirmed all eight pods still Ready with
zero restarts. Sampling the preceding ten minutes (at most 500 lines per pod)
found no warning/error/panic/fatal/SQLSTATE or logged 5xx entries. The 34 events
then emitted for the two synthetic acceptance organisations were all published;
the whole outbox had no unpublished rows, and the Workforce/Supply scanner
consumers had no pending, unacknowledged or redelivered messages. This is a
point-in-time operational check, not a claim about unsampled logs.

## Policy-aware form follow-up

Production acceptance identified a client validation gap: the backend enforced
organisation access limits, but grant/support forms did not consistently load
and display those limits. The follow-up shares a tenant/subject/organisation
scoped policy query, applies the configured maximum to new or extended grants,
support requests and approvals, and preserves administrator exemptions and
unchanged/shortened historical grants. Access-expanding actions wait for valid
policy data; revocation and rejection remain available if that query fails.

The local Supply suite passed 104 tests across 13 files, including 17 focused
policy regressions. The full workspace gate passed 421 shell, 229 API, 93 shared
UI and two script tests. Static checks (15 tasks) and the production build passed.
Commands run for the follow-up were:

```sh
pnpm exec turbo run test --concurrency=1 -- --maxWorkers=1
pnpm exec turbo run lint format:check type-check --concurrency=1
pnpm build
gitleaks git --pre-commit --staged --redact --no-banner --no-color
git diff --cached --check
```

The new standalone build runs on the registered local origin with a synthetic
one-hour policy. Authenticated HTTP checks through that UI proxy returned 200
for Supply entry, directory, the exact synthetic organisation and its one-hour
policy; a two-hour grant returned 400. This does not substitute for browser
acceptance. Historical unchanged/shortened access semantics were covered by
component regressions; no suitable local historical grant was mutated for the
HTTP smoke. Local visual acceptance subsequently passed through native Chrome
controls on the running standalone build: both forms displayed the one-hour
policy, rejected two-hour entries inline, cleared the error after correction to
one hour and preserved the other inputs. The grant's expiry started blank;
keyboard member selection, cancel actions, policy/grant revision histories and
navigation expansion were exercised. The invalid grant stayed in the editor
after submission; no valid grant or support request was submitted. Desktop
screenshots showed aligned fields and readable policy guidance. This patch has
not yet been released; production visual acceptance remains the release gate.

## Ordered release and recovery

1. The migration-free legacy grant security hotfix [#1197](https://github.com/tesserix/helivanta/pull/1197) was merged as `e6eb72d` after all CI passed and released through the normal image/deploy/Kargo/Argo pipeline. All four deployments reached 2/2 and CNPG stayed 2/2; the Supply ledger remained `0001`–`0006`. Live permission acceptance passed with a dedicated synthetic test user; see the [security release evidence](https://github.com/tesserix/helivanta/pull/1197#issuecomment-6231743272).
   Review the separate security release evidence before proceeding.
   Verify every API replica runs that exact safe image before introducing timed
   grants. The vulnerable pre-hotfix image must not reappear through promotion or
   rollback.
2. Explicit approval was obtained for the concrete `0007_supply`/`0008_supply`
   migration and feature release. Both migrations are additive and bounded by
   lock/statement timeouts. They retain existing identities, grants and history.
3. Use the owning GitOps/Kargo/Argo pipeline. Verify migration completion, exact
   image tags/digests, API/shell/worker readiness and the two-instance CNPG state.
   Do not apply imperative production grants, SQL, scaling or deployment edits.
4. Repeat genuine production acceptance for company/territory ownership,
   tenant/location/private-role denial, grant/driver/support revocation,
   configured limits, archived history, persistence and anonymous denial. Use
   synthetic records only. Check logs and outbox/consumer health.
5. Add the exact release/test evidence to #1137, update its acceptance checkboxes
   and close it only after every criterion works. Broader procurement,
   fulfilment, commercial approval and finance stories retain their own scope.

The migrations are intentionally forward-only: deleting audit/revision rows is
not a down migration. Retain the expanded schema on failure, stop promotion and
use a reviewed forward fix. Any rollback requires explicit approval and a safe
image at least as new as the security hotfix; the new operational UI may be
unavailable on that compatibility image. No unauthorised data-access fallback is
part of recovery.

This work provides tested controls; it does not establish SOC 2/HIPAA
certification or legal approval of country/category trading rules.
