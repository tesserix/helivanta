# Distributor structure and scoped access — verification

Issue [#1137](https://github.com/tesserix/helivanta/issues/1137), under epic [#1133](https://github.com/tesserix/helivanta/issues/1133). See the [architecture and editable diagram](../architecture/distributor-scoped-access.md), [design](../superpowers/specs/2026-10-17-distributor-structure-and-access.md), and [Mednet comparison](../requirements/pharmaconnect-legacy-procurement.md).

This records the current local implementation. Migrations `0007_supply` and
`0008_supply` have not been approved or released to production. The story remains
open until the complete local/CI gates and approved production acceptance pass.

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
harnesses exercise the final embedded migrations. There is no production SQL
application in this evidence.

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

## Ordered release and recovery

1. The migration-free legacy grant security hotfix [#1197](https://github.com/tesserix/helivanta/pull/1197) was merged as `e6eb72d` after all CI passed and released through the normal image/deploy/Kargo/Argo pipeline. All four deployments reached 2/2 and CNPG stayed 2/2; the Supply ledger remained `0001`–`0006`. Live permission acceptance is being completed with a dedicated synthetic test user.
   Review the separate security release evidence before proceeding.
   Verify every API replica runs that exact safe image before introducing timed
   grants. The vulnerable pre-hotfix image must not reappear through promotion or
   rollback.
2. Obtain explicit approval for the concrete `0007_supply`/`0008_supply`
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
