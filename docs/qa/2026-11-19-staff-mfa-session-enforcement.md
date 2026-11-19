# Staff MFA recovery session enforcement candidate

For #1703 / #1749. This is not a released reset or enrollment journey.

The application session signer now takes an explicit recovery generation argument.
Every production mint caller and test fixture was updated deliberately; no
variadic/default argument can silently discard a generation. The signed claim is
an optional canonical UUIDv7. Empty means unadmitted, not an exemption from recovery.
The verifier checks its shape and carries it through the private principal and
expired-cookie renewal evidence. It never becomes native signed-session proof.
Ordinary principal JSON omits both recovery generation and native session ID.

The private identity credential projection now opts into current account-wide
recovery evidence. Login, renewal and protected request admission fail closed if
that evidence is missing or invalid. Explicit `none` requires no admitted
generation; `completed` requires the exact matching signed generation. Pending,
uncertain and enrollment-required states refuse access. Existing password cutoff,
global revocation, membership, cookie and idle controls remain in place. Renewal,
activity and tenant switching preserve the generation already verified.

Existing positive fixtures now explicitly declare no recovery, or the matching
completed generation when testing preservation. Their original assertions remain.
New negative cases cover missing evidence, malformed evidence, pending/enrollment
states and stale/legacy cookies. Neither production adapter supplies a default
`none` when the owner cannot answer. Both hosted owner-schema fixture pins now
match infrastructure `7bd817e6fdfec553ee95499faf6076f155a4907d` and SHA256
`844877964546ef1c936298837123c6f528543d8ca609effb4aa7fe566ab6b0a2`.
No live migration has been applied.

## Evidence and source review

Observed red before green: missing signer/claim support, missing principal mapping,
renewal/activity dropping the generation, and renewal returning 200 for pending,
missing or stale recovery evidence. New HTTP projection tests first failed on
missing recovery support. These assertions now pass with actual signed application
cookies and actual handlers; native/owner HTTP dependencies are simulated locally.
A correctly signed `null` recovery claim was also observed being accepted, then
fixed with strict scalar decoding; absence remains distinct from malformed input.

Local commands executed:

- Full race suites: `go test -race ./pkg/session ./pkg/authn ./internal/modules/iam/loginclient -count=1`.
- Scoped IAM race tests: renewal, generation-preserving activity, credential freshness,
  password-recovery replay and password-only refusal for retained MFA recovery.
- Scoped customer-onboarding race tests: exact identity projection, recovery evidence
  and minimal assignment-candidate purpose.
- `go test ./... -run '^$'` compiled every backend test package without running
  database/native integration. It is not runtime test evidence.
- `go vet ./...`, `golangci-lint run ./...`, `go build ./...` and
  `go run ./cmd/contractdocs -check` passed.
- The operational inventory checker and its 15 tests, shellcheck of both fixture
  scripts and the three identity-owner fixture-control tests passed.
- The canonical remote fixture schema was fetched and its pinned digest checked.

Re-reviewed the changed authentication principal, renewal, activity and private
projection diffs before refreshing seven source pins across five existing review
records. Registration, authority classifications and outstanding acceptance
obligations are unchanged. Credential-only recovery opt-in does not expand the
assignment-candidate/profile/membership purposes or disclose employer source data.
The regenerated ledger remains source classification, not runtime acceptance.

New real database tests for generation-preserving tenant switch and protected
admission in two legacy tenant contexts run in hosted CI, not locally. No local
application, PostgreSQL or container was started. No live identity or factor was
changed. The new JWT claim and source checks are not proof of a deployed feature.

## Current candidate verification

At application revision `3d00c4a8`, native recovered-login admission is wired in
`backend/internal/modules/iam/login_recovery.go` through the onboarding owner's
`AdmitStaffMFASession` contract. It requires a native session ID, validates the
returned generation, then reads current credential state again so a concurrent
reset or password change wins. This supersedes the earlier missing-admission
status; it does not establish native runtime or deployed acceptance.

Workforce exposes reviewed Staff reset, read and cancellation operations through
`backend/internal/modules/workforce/staff_mfa_recovery.go`. The candidate includes
the IAM retained intent/fence and the shared Staff recovery UI. The hospital and
independent-pharmacy native cases are authored but require a successful hosted run.

Additional local commands passed on 19 November:

- `pnpm --filter @helivanta/api test`: 133 files, 1,492 tests.
- `pnpm --filter @helivanta/e2e type-check` and
  `pnpm --filter @helivanta/e2e lint`.
- `pnpm exec prettier --check e2e/tests/support/staff-mfa-native.ts`.
- From `backend/`, `go build ./...`, `go vet ./internal/integration`, and
  `go test ./internal/integration -run '^TestStaffMFAFixtureTenantsReachTheCanonicalOwnerClient$' -count=1`.
- From `backend/`, `go test -race ./internal/customeronboarding ./internal/modules/iam/loginclient ./pkg/session ./pkg/organizationidentity ./pkg/authn -run 'MFA|Recovery|Credential' -count=1`.

The focused integration-package command tests the owner HTTP client fixture only;
it does not run PostgreSQL or native recovery. Node reported version 26.13.1 against
the repository's required Node 24 range; hosted Node 24 results remain required.
No local application, database or container was started. `go build ./...` emitted
no application binaries into the worktree.

The earlier hosted run `44488449145` failed an independent Staff setup assertion:
creation returned the documented 201 while the test expected 200. The correction
preserves the API and now checks 201 creation followed by 200 identical replay for
both proposals and decisions. Hosted run `44508111060` tests the pushed correction;
its results must be recorded after completion. Earlier employer-setup and lockout
failures are not considered resolved without those runtime results.

## Native acceptance follow-up

Hosted run `44512746850` at `1992a0c0` passed the Staff MFA integration step,
including exact proposal/decision replay receipts. Its browser suite reported
104 passed, two failed, one skipped and two not run. Lockout guidance passed.
Both hospital and independent-pharmacy reset submissions returned 503; their
bounded warnings identify `prepare_owner`. The independent form now accepts its
canonical pharmacy Staff context. Native reset completion and recovered sign-in
are still unproven.

The next candidate checks native password presence and the selected target's
provider organization-member proof before factor setup. Diagnostic assertion
messages contain only result counts, sequence/count metadata and a timestamp
presence flag. They do not serialize accounts, credentials, factors or tokens.

Playwright now schedules these three native cases as dependencies of `specs`;
`bulk` still depends on `specs`. The main invocation and report retain all cases.
The executable registration test checks every main-stack spec file is present,
every case is unique and the three native cases are selected through dependency
resolution. Its assertion failed before the scheduling change and passed after.
A native dependency failure prevents dependent cases from running; it never
establishes a passing full-suite result.

## Empty native member-proof correction

The recovery-first run `44517575218` at `714c8e8f` isolated the owner-preparation
failure before factor setup: both native member searches returned no records,
omitted count/sequence fields, and a view timestamp. Its three recovery cases
reported one passed and two failed; the other 106 cases did not run because their
project dependency failed. This is not full-suite acceptance.

Zitadel v4.15.3 represents these counters as protobuf `uint64` fields, so ProtoJSON
can omit their zero defaults. Owner candidate `4b2a96a` accepts the empty default
only with a valid non-future positive timestamp and no member records. Explicit
nulls, malformed/recased metadata, contradictory counts and privileged member
records remain refused. Its new regression cases failed before the parser fix;
the full native adapter race suite, vet, lint and owner Go build passed locally.
Owner CI run `44519826627` passed all checks. Candidate `37e6f801` pinned both
consumers to `4b2a96a0aef2d9d1926dbe7d416b95f7f6671855`; native acceptance
remained required.

The browser assertion now checks this same native response shape. It verifies
empty result type/count and valid timestamp evidence when counters are omitted or
zero; it does not invent a positive projection sequence. The owner's authenticated
query and guarded recovery command still authorize preparation and dispatch.

## Current-main integration

The owner branches now retain main's multi-account email lookup: prerequisite
`eab2adb` and Staff recovery `c67ff09` passed all hosted owner checks, including
PostgreSQL Go quality, contracts, images and security. Both HMS consumers pin
`c67ff0981bfab93e275bca591d1ce45e8de568c7`.

The parent HMS resolution `e7b9c1d4` preserves the validated native organization
lookup and multi-account email dispatch. The inherited multiple-account test
failed when that dispatch was absent, then passed after restoration. Unscoped
**Forgot password?** opens the existing mailbox account finder before scoped
recovery; its new regression failed before the change and passed after it. A
browser case covers the same transition and awaits hosted execution.

Main already allocated `0053_iam_login_account_choice`. The unshipped MFA fence
migration therefore becomes `0054_iam_staff_mfa_fences`; the published migration
remains unchanged. The registration regression failed with only 53 phases and
passed after retaining both phases in order. This is source-level evidence, not
an applied live migration.

Run `44521873765` at `37e6f801` passed web, scripts and secret scanning. Its E2E
job failed during Ubuntu browser-dependency installation before application
startup. No native recovery case executed in that job. Its Go coverage job was
still running when the tested current-main integration was prepared. Advancing
the necessary integration through normal PR concurrency supersedes this older
run; no passing Go gate or full acceptance is claimed for it.

Parent run `44528471426`, E2E job `135575206691`, passed at `e7b9c1d4`:
106 main-stack cases passed, one live security-key case skipped, and both the
idle-timeout and renewal phases passed. The actual `specs`/`bulk` selection
includes `password-recovery-native.spec.ts` and the new unscoped account-finder
case. Native password recovery has no skip path: missing fixture opt-in fails
the test. The only conditional skip in this selection is
`login-security-key-live.spec.ts`.

That hosted native case verifies mailbox delivery/verification, prepared password
replacement, no resend after verification, old-access 401, old-password 401 and
new-password sign-in followed by authorized access 200. This proves the isolated
password journey at the parent revision, not Staff MFA recovery, a passing full
Go gate or any live deployment.

GitGuardian incident `40266894` was classified in the provider as **Not a secret
(false positive)** after source review. Its normal PR check passed at `5cad384c`.
This resolves the reviewed finding without disabling repository scanning; fresh
required checks remain necessary for each subsequent application revision.

Local validation of the combined candidate used Node 24.11.1: 133 API files /
1,498 tests and 240 shell files / 2,165 tests passed. API/shell/E2E typechecks and
lint passed; lint retains the existing signup effect-dependency warning. The
Node 24 shell production build passed with `API_URL=http://127.0.0.1:8081`, and
its generated `.next` directory was removed afterward. No local application,
database or container was started.

Backend validation passed `go build -p=2 ./...`, the loginclient, session, authn
and organizationidentity race suites, focused customer-onboarding recovery and
IAM MFA/registration race tests, and affected-package vet. Contractdocs
regeneration/check, release-capability generation/check, source-inventory
generation/check, E2E typecheck/lint, formatting and all 16 browser-registration /
inventory tests passed. The full database and native browser suites remain hosted
CI obligations; these local results do not establish deployed acceptance.

## Remaining release gates

Require full hosted database and native-browser acceptance for both employer
models, password-reset-to-login, lockout guidance and forced replacement MFA
before enabling recovery. Require normal passing security checks at the accepted
release revision as well as the resolved provider finding recorded above.

Follow [the schema-first rollout and acceptance plan](../deployment/2026-11-19-staff-recovery-rollout.md)
for approved live migrations, owner/application Kargo releases, named QA factor
actions and deployed Chrome/API verification. Keep #1703 open until every issue
acceptance criterion and live release gate is evidenced.

## Native token-lifetime diagnosis

Staff run `44533809734` at `d6a8430a` passed web, scripts, image and secret
checks, but native E2E reported one pass, two failures and 107 cases not run.
Both reset requests returned 503 at `prepare_owner`; this contradicts native
recovery acceptance despite the earlier empty-member parser correction.

The pinned Zitadel v4.15.3 `cmd/defaults.yaml` sets both
`OIDC.DefaultAccessTokenLifetime` and
`DefaultInstance.OIDCSettings.AccessTokenLifetime` to 12h. The disposable Compose
fixture previously overrode neither. The product credential client accepts at
most one hour. A public-client regression using the Compose settings and pinned
defaults failed for both 12h configuration paths with
`organisation identity unavailable`. Setting both fixture lifetimes to 1h makes
the full organizationidentity race suite pass. This reproduces a configuration
incompatibility; the hosted native test must still prove it caused the 503.
The client lifetime guard remains unchanged. Existing persistent dev instances
retain their initialized policy; these settings apply to the clean CI instance.

Read-only discovery found the active operator/project/context unchanged and
`zitadel/zitadel` at three Ready replicas. Its `zitadel-config-yaml` ConfigMap
already sets `DefaultInstance.OIDCSettings.AccessTokenLifetime` to 1h. No live
Zitadel policy was changed or inferred from the disposable fixture.

Owner `a4be89b8d97aedcbe4ff4c2cbd441f6ec4c4ea88` adds fixed-stage unavailable
warnings and a JSON fixture logger; both consumers now pin this revision. The
consumer failure diagnostic accepts only known stage enums and the exact
normalized prepare-route HTTP status. It rejects raw identity paths, unknown or
malformed records, private fields and oversized input, and bounds output to 100
records. No owner log body is dumped. Four sanitizer/CLI tests and the existing
fixture/registration suites pass. These diagnostics change no authorization,
contract or native operation.

Local validation for this dependency/configuration update passed the full
organizationidentity, loginclient, session and authn race suites, focused
customeronboarding recovery/credential races, affected vet and organizationidentity
lint (zero issues). `go build -p=2 ./...` and `contractdocs -check` passed.
The API typecheck/lint and all 1,498 tests passed. The Node 24 shell production
build passed with `API_URL=http://127.0.0.1:8081`; its generated `.next` was
removed. The 19 source-inventory/diagnostic tests and nine owner-fixture/native
registration/diagnostic tests passed. No local app, database or container ran.

## Native reset succeeds; admission remains unverified

Run `44541450632` at `960ca294` records owner preparation HTTP 200 for both
real employer models. Both browser cases then assert native reset acknowledgment,
absence of the original TOTP, old access and renewal 401, forced replacement
enrollment with a different secret and no application access before confirmation.
They fail at line 355 waiting for dashboard navigation after confirmation: the
callback returns unavailable current-credential evidence. The run still reports
one pass, two failures and 107 cases not run. The token-lifetime configuration
fix resolves the earlier prepare failure; it does not establish full recovery.

Both consumers now pin owner `f76433d6bcfb521e4d0e361794476cc0e2afd57c`. It
adds fixed unavailable stages to admission and native enrollment/session proof
reads, retaining all existing authentication/cutoff decisions. Regression tests
for logging and the consumer allowlist failed before implementation, then passed.
No native session, account, generation, timing value, factor or provider payload
is exposed by these diagnostics.

Parent Go run `44528471426` at `e7b9c1d4` failed four inherited account-choice
tests: their successful native-session fixture lacked the primary password proof
required at finalization. The three database-free cases reproduced the 400 locally
and passed after adding the synthetic proof. Parent `75dcc5c8` contains this
fixture-only fix; the database-backed two-account selection case remains a hosted
obligation. The Staff candidate additionally requires explicit current owner
credential/recovery evidence for managed native organizations. Its same three
cases reproduced 503, then passed with a scope-checked synthetic owner projection
that declares the actual fixture password and no pending recovery. Production
proof checks are unchanged.

The parent fixture update passed affected Go build, vet and IAM lint; the Staff
fixture update passed the same local checks. No local application, database or
container was started for these validations. An initial command including the
database-backed case was interrupted when its startup requirement was identified;
it provides no passing evidence. That case remains a hosted CI obligation.

For the new owner diagnostic pin, local `go build -p=2 ./...`, complete
organizationidentity/loginclient/authn/session race suites, `contractdocs -check`,
source-inventory generation/check and all 25 inventory/diagnostic/fixture tests
passed. API canonical-generator checks and TypeScript typecheck passed. The owner
contracts directory is unchanged between `a4be89b` and `f76433d`; this pin adds
diagnostics rather than a wire-contract change. Native acceptance is still pending.
