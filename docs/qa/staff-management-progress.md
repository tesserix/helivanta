# Staff Management delivery tracker

## Access/RBAC checkpoint — 2026-10-31: Staff document account binding

The source/phase audit now records all **33 HR intake primary-permission routes**
(23 enrollment, 10 document), secondary evidence permissions, shared helper
consumers, storage authorization phases and the historical template migration
boundary in [the inventory](staff-intake-access-inventory.md). Scoped HR intake,
compound command authority and immutable employment review remain to implement.

A real PostgreSQL/NATS file regression returned private synthetic metadata with
200 despite a different expected account. The common document parameter helper
now reuses the shared Staff principal precondition. All ten document routes
reject empty/invalid/repeated/joined/mismatching tenant/account assertions with
no-store `409 session_changed`; actual foreign scope stays 404. This explicitly
corrects the old mismatching-tenant 404 response. Real released-file checks cover
100 invalid-header/route combinations, unchanged file/classification/audit/outbox,
zero released-byte reads on denial, matching/headerless downloads and foreign scope.

Separate client regressions were RED for missing subject headers and same-tenant
account cache collisions. The Staff document UI and credential-evidence selector
now inherit the original verified Staff account; SDK headers/cache keys and
classification form keys bind that account. Tests cover aborted file preparation
without PUT after account change, no uncertain classification command carried
across accounts, and hidden private state plus parent context invalidation on
session_changed. Network uncertainty retains exact retry within the original account.

Focused backend race and 29 related UI tests pass. Full Workforce race/coverage
passed in 449.125 seconds: 83.8% statement coverage, 521 passing test/subtest
events, zero failures and zero skips. Full Go vet/build/lint and 21 frontend
lint/format/typecheck tasks pass. API suite: 413 tests in 60 files passed.
The five remaining frontend test tasks also pass. Complete shell tests pass:
1,258 tests in 160 files. Production shell build and Expo web/iOS/Android exports
pass. The first build correctly refused an absent API_URL; the successful rerun
supplied the canonical local Global API origin `http://localhost:8081`, without
changing the configuration guard. Existing Vite/jsdom navigation and Next
middleware-deprecation warnings were reported; no test or guard was suppressed. An initial
client typecheck identified the credential-evidence selector as another consumer;
it now inherits the account too. No failing assertion was removed or skipped.

Commands actually run (Go 1.27.3; Node 24.22.1 for final frontend checks):

- RED/GREEN: `go -C backend test ./internal/modules/workforce -run TestStaffDocumentRequestsRejectChangedExpectedAccount -count=1` (200 before the guard; 409 after).
- `go -C backend test -race ./internal/modules/workforce -run 'TestStaffDocumentRequestsRejectChangedExpectedAccount|TestDocumentRequestsCannotCrossAnExpectedTenantBoundary' -count=1` (passed).
- `go -C backend test -json -race -cover -timeout=20m ./internal/modules/workforce -count=1` (passed, results above; disposable Colima PostgreSQL/NATS).
- `go -C backend vet ./...`, `go -C backend build ./...`, and from backend `golangci-lint run ./...` (passed).
- `go -C backend run ./cmd/contractdocs -check`, canonical staff-access check and Go formatting/diff checks (passed).
- `pnpm turbo lint format:check type-check` (21 successful tasks).
- `pnpm --filter @helivanta/api exec vitest run --maxWorkers=2` (413 passed).
- `pnpm --filter @helivanta/shell exec vitest run --maxWorkers=2` (1,258 passed).
- `pnpm turbo test --filter='!@helivanta/api' --filter='!@helivanta/shell' --concurrency=2` (remaining five test tasks passed).
- `API_URL=http://localhost:8081 pnpm turbo build` (both build tasks passed).

Membership guard source `a12216d1` is pushed to [draft PR #1489](https://github.com/tesserix/helivanta/pull/1489),
with exact-head CI [41136443845](https://github.com/tesserix/helivanta/actions/runs/41136443845)
still running at the latest check. This continuation adds no migration, SQL/FGA
change, permission, endpoint, production account or rollout. Browser handoffs,
console CI billing gate, production registration, legacy grant migration, all
employment lifecycles and cross-module cutoff remain open. Counts stay 10 partial,
10 audit pending, 1 research, 0 fully accepted; A01–A12 remains the full objective.

## Access/RBAC checkpoint — 2026-10-31: ended-membership admission

The current legacy POST regression reproduced implicit restoration after final
membership removal: expected 409, actual 202. Candidate migration
`0019_iam_membership_recovery_admission` now rejects zero-to-positive continuity
without explicit command/removal-basis context, using the already serialized
continuity row. The handler maps only the named integrity violation to no-store
`409 membership_recovery_required`. SQL is persisted beside its embedded module
migration and recorded in the generated contract inventory; no live migration
has been run.

Real PostgreSQL/NATS HTTP checks pass for ordinary/clinical/admin-role legacy
restoration refusal, unchanged epoch/membership/audit/outbox, legitimate explicit
recovery, duplicate repair retaining the recovered ID, direct runtime-role
inserts with absent/partial contexts, forged non-admin context refusal, tenant
isolation and an observed final-removal lock wait followed by refusal. These
are synthetic API/database checks, not normal browser authentication evidence.

Validation checkpoint: full build, full vet, full Go lint (0 issues), contract
generation/check, contractdocs tests and focused IAM membership journey passed.
The complete architecture suite and IAM loginclient/contract race suites also
passed. Full IAM runs exposed historical raw/legacy POST reenrollment in two
projection fixtures plus the obsolete migration count; both positive fixtures
now use public removal/recovery commands while preserving stale-projection,
pending-approval and generation assertions. Targeted leave/identity/projection
race checks pass. The final full IAM race/coverage run passed in 300.193 seconds
with 82.0% statement coverage, 288 passing test/subtest events, zero failures
and eight pre-existing conditional skips. Eight real-Zitadel integration tests skip
without their separate dev credential environment; this is unrun acceptance,
not a passing real-provider journey. No new skips were introduced. One full
architecture invocation omitted the required Docker socket environment and
failed fixture startup; its corrected full rerun passed without test changes.


Commands run from `backend/` with the established disposable Colima socket for
container-backed suites:

- RED: `go test ./internal/modules/iam -run TestLegacyMembershipGrantCannotReopenEndedMembership -count=1` (202 instead of 409).
- Final: `go test -json -race -cover ./internal/modules/iam -count=1` (passed; conditional skips above).
- `go test -race ./internal/modules/iam/loginclient ./internal/modules/iam/contract -count=1` (passed).
- `go test ./internal/archtest -count=1` (passed).
- `go test ./internal/journey -run TestIAMGrantMembership -count=1` (passed, in the combined focused architecture/journey invocation).
- `go test ./cmd/contractdocs -count=1`, `go run ./cmd/contractdocs`, `go run ./cmd/contractdocs -check` (passed).
- `go vet ./...`, `go build ./...`, `golangci-lint run ./...` (passed; scoped lint rerun after fixture changes also passed).
- `gofmt -l internal/modules/iam`, `node ../scripts/generate-staff-access.mjs --check`, `git diff --check` and staged redacted Gitleaks scan (passed).

Full A01–A12 remains open: first legacy enrollment/privilege expansion and
external migration, provisioning/recovery browser acceptance, employment
suspension/resumption, resignation/termination/contract expiry/rehire and
cross-module cutoff. Counts remain 10 partial, 10 audit pending, 1 research,
0 fully accepted. Console CI 41132417027 was rechecked: GitHub's annotation still
says jobs did not start because payments/spending limits need owner attention.
No merge, production migration, rollout, release tag or issue closure occurred.

## Access/RBAC checkpoint — 2026-10-31: operator recovery UI candidate

Console [draft PR #669](https://github.com/tesserix/tesserix-home/pull/669)
contains implementation `d9117af4a21526ebd9f642cb1eb96ee7723f18de` and QA-gate
notes at `209c2aa`. It adds exact retained command/request inspection, timestamped
source observations, required proposal evidence, independent review decisions and
matching-account reconciliation through the existing same-origin BFF. Current
reader/writer capabilities, server-held credentials, source owner reauthorization,
no-store, CSRF, exact route/method scope, query refusal and strict UTF-8 stay enforced.
Uncertain requests/decisions freeze their UUIDv7/body/key; session-scoped browser
storage retains identifiers/action/digest without private reasons or credentials.
Reload and retained-outcome recovery preserve the same successor. Confirmed
operator denial refreshes current reads and hides revoked private evidence;
authorization-source outages are reported separately as unavailable.

Canonical Go-derived models are published and byte-verified at owner
`f75bb0e5479446c3a36015516a2bf661aee357a2`; exact owner CI 41131295353 passed.
Source/schema ownership is unchanged: schema PR #1616, chart 0.3.62 / `325012b`.
No new migration, SQL/FGA change, production account/grant or rollout occurred.

Local console acceptance: **5,572 tests in 295 files passed with no skips** using
`--maxWorkers=2`; typecheck, lint, production build, immutable artifact/provenance,
staged redacted secret scan and diff checks pass. One earlier parallel rerun hit
10-second setup hooks in six existing database files; no tests/timeouts were
changed and bounded complete reruns passed. Read the console's
`docs/qa/staff-recovery-console.md` for exact commands and evidence boundaries.

Real local sign-in used separate synthetic provider/client/operator and PostgreSQL
fixtures with auth bypass disabled. It reached the console callback, which Chrome
blocked with `ERR_BLOCKED_BY_CLIENT`; human handoff is requested. No warning was
bypassed, callback code committed or human session manufactured. Separately,
GitHub refused to start console CI 41131982143, secret scan 41131982554 and mobile
check 41131982158 because failed account payments/spending limits require owner
attention. No remote tests ran. Billing/capacity handoff is requested; no billing,
visibility or runner changes were made. New-head checks must pass before merge.

**Access/RBAC A01–A12 remains incomplete.** Normal console/HMS Admin → recipient →
independent reviewer acceptance, source/route coverage, production registration
(owner #12), profile correction/activity, legacy membership/link/grant handling,
renewal, all employment/contract lifecycles and cross-module cutoff acceptance
remain open. Staff Management counts stay 10 partial, 10 audit pending, 1 research,
0 fully accepted. No merge, production deployment, issue closure or release tag.


## Access/RBAC checkpoint — 2026-10-31: reconciliation and independent decisions

Follow-up source-integrated HTTP evidence is pushed at `17e3dc90323a3d2c6ac410488b3c1295d792fc4d`
in owner PR #22. The tagged local PostgreSQL/Zitadel race test passes through real
organization/project/grant proof, independent absent-account approval, one late
original creation, matching orphan reconciliation, immutable retry, no successor
dispatch or reopened original dispatch, and actual grant withdrawal/restoration.
Tagged API vet/lint and redacted secret/diff checks pass. Console tokens remain
asserted fixtures; real login and normal HMS UI acceptance are not proven.
Implementation-head CI run 41125307163 is now **passed**; the follow-up head's CI
has not yet been observed at this update. No merge or deployment occurred.

Identity-owner draft [PR #22](https://github.com/tesserix/onboarding-service/pull/22)
now contains pushed `293aa359a97ba7a3d853d102b12253914f913ab2`.
Fresh console write authority guards creation reconciliation and immutable independent
review decisions. Matching source accounts retain original creation facts and block
replacement; removed accounts retain historical birth. Approval serializes exact
birth/absence checks with receipt admission, preventing a stale absence decision
after a receipt arrives. Replacement atomically prepares a fresh bound successor
without dispatching an account or granting membership. Entire recovery route family
bypasses cached response replay. Authority-withdrawal tests cover before commit,
after commit without disclosure, and authorized recovery of retained history.

Local evidence: full PostgreSQL Go race/coverage suite, vet/build, default and
provider-fixture lint, four delivery Python tests, redacted Gitleaks and diff checks
passed. Decision/reconciliation HTTP tests use callback source proofs; actual-provider
adapter tests from the preceding candidate remain separate evidence. New CI run
[41125307163](https://github.com/tesserix/onboarding-service/actions/runs/41125307163)
is queued at this checkpoint; previous `a3eab89` run 41120222174 passed.
No new migration/schema change, merge, production rollout, tag or issue closure.

**Access/RBAC remains partial.** Next gates include full real-source owner HTTP,
consumer UI and Admin → recipient → independent reviewer acceptance, followed by
membership recovery/removal, renewal, all employment/contract lifecycles and
cross-module cutoff acceptance. No whole-area completion percentage is claimed;
Staff Management counts remain 10 partial, 10 audit pending, 1 research, 0 accepted.
The earlier checkpoint sections below describe their own evidence dates.


Updated: 2026-10-31. Owner: Helivanta Staff Management delivery. This is the summary tracker for the full user-requested scope. The [workflow acceptance ledger](staff-management-acceptance.md) holds detailed scenarios and evidence; this document does not replace it.

### Exact-account proof and fresh reviewer source view — 2026-10-31

Owner `a3eab89` implements exact frozen-account classification and a freshly
authorized reviewer source-view POST. Absence requires two actual missing reads
and stable empty authoritative history. Matching requires retained birth/contact
and sealed command binding. Removal additionally requires terminal removal, actual
missing reads and bounded ownership history proving one human birth and the latest
causal nonce. Unknown, contradictory, stale or saturated proof fails closed.
No universal contact availability, verified contact or operational access is claimed.

The source view derives all input from the exact private dispatched command,
validates returned scope/freshness, rechecks live authority and canonical registry,
then commits mandatory disclosure audit. It exposes only disposition and observed/
birth time. A replay regression hid changed facts; explicit cache exclusion now
requires fresh reads and denies revoked authority. It creates no account, retained
creation receipt, decision, closure or successor. Approval must obtain new proof.

Actual isolated v4.15.3 race tests passed absent/matching/removed proof and the
previous operator checks. Email delivery advanced source history during immediate
matching proof; a later fresh proof passed with creation dispatched once. Final
matching used two proof attempts within the fixture's three-attempt bound.
An actual projection fault captured the synthetic target's parent/child rows
privately, produced real GetUser 404 with retained creation history, and verified
that recovery denied absence. Restore initially failed on a generated notification
column; the correction recomputes it, verifies every captured row, and restores
matching proof. Source event history stayed retained; all captured target projection rows are restored.

Full owner PostgreSQL race/coverage passed: 56.0% overall, API 66.0%, store 66.7%,
provider 87.8%; classifier 93.6%, removal ownership 95.8%, cross-package source-view
service 85.7%. Vet/build, normal and fixture-tag lint (zero issues), four delivery
tests, secret/diff checks and both actual-provider race fixtures passed. These
figures are test coverage, not completion. Configuration/time-budget documentation
was corrected to actual console environment names and the unchanged two-second
creation budget. No schema/production change, migration, merge or release occurred.

Database/HTTP source-view tests use callbacks; actual source-protocol/fault proof
is separate. Real-source full owner HTTP and normal HMS Admin/recipient/independent-
reviewer UI remain unproven. New-head CI is tracked separately. Next: matching-
orphan receipt reconciliation and fresh independent decision service/API, explicit
conflict handling, activity, prepared-command/profile correction, consumer UI,
recipient setup/sign-in, canonical registration (#12), legacy grants and remaining
A01–A12 lifecycle/cross-module cutoff acceptance. Counts stay 10 partial,
10 audit pending, 1 research, 0 fully accepted. Full Access/RBAC stays active.

### Exact request review checkpoint at 2c61d2a — 2026-10-31

Owner `2c61d2a` adds current-human exact-request GET for reviewer evidence:
maker, requested action, reason/evidence reference, optional proposed successor
initiator and retained decision/successor command ID. Original source account,
nonce, contact/profile and machine bindings are omitted. Two live operator/source
checks surround mandatory audited inspection, followed by a fresh transactional
read. Request, decision and lineage use one statement snapshot; a decision
committed during reverification appears in the final view. No approval authority
or membership eligibility is inferred from a request read.

Tests pass for undecided/final rejection/complete replacement snapshots, incomplete
approval denial, foreign or missing requests, malformed input, audit rollback,
source outage, revoked authority/entitlement and canonical rotation. A new regression
reproduced proposal/decision commits paired with `denied` audit outcomes; both
stores now reject those outcomes and retain no business action. Full final
actual-PostgreSQL race suite, vet/build/lint (zero issues), four delivery tests,
redacted secret scan and diff checks passed. Coverage: API 65.6%, store 66.7%;
request store read 90.0%, HTTP handler 94.1%, cross-package service 89.7%.
These are coverage figures, not a Staff completion percentage.

Pinned v4.15.3 primary-source research now records why empty user projection or
search checkpoint cannot prove absence: rows/checkpoint are read separately, and
projection progress is not payload conformance. Next source proof must classify
the exact frozen account and reconcile matching orphans; replacement preparation
must not claim universal contact availability or operational access. The source
checkout was preserved, and findings were checked with `git show` at the pinned
tag. Owner canonical-registration issue #12 was rechecked and remains OPEN.

New HTTP/database tests use source callbacks. Real-source full HTTP and normal
HMS Admin/recipient/independent-reviewer UI remain unproven. Parent 74abd3d CI
41111788806 passed; new-head CI is tracked separately. No schema/production change,
migration, merge, deployment, release or completion tag is claimed. Next: exact-
object source conflict/orphan proof, independent decision service/API, activity,
prepared-command/profile correction, consumer UI and remaining A01–A12 lifecycles/
cross-module cutoff acceptance. Counts stay 10 partial, 10 audit pending,
1 research, 0 fully accepted. Full Access/RBAC remains active.

### Recovery read/proposal checkpoint at 74abd3d — 2026-10-31

Owner `74abd3d` connects current human console proof to an audited exact-command
recovery read and immutable proposal PUT by UUIDv7 request ID. Source organization/
project and canonical active tenant/product/entitlement are checked under the real
operator principal. Original initiator/machine/provider facts survive canonical
machine rotation. Recovery proposals derive maker from verified identity and
require reason/evidence; they create no decision, closure, replacement, user,
membership or dispatch. Independent review remains required.

A test reproduced generic cached success after maker revocation. Explicit route
exclusion now forces fresh authorization on every retry, with no generic cached
response. Tests cover rejected forged authority/credentials and malformed inputs,
read-only operator denial for proposals, live proof withdrawal, source/registry
changes during I/O, mandatory audit binding/rollback and committed proposal
retention without disclosure after revocation, followed by exact retry recovery.

Full actual-PostgreSQL owner race/coverage passed (55.4% overall; API 64.8%, store
65.6%). Vet/build/lint (zero issues), four delivery checks, redacted secret scan
and diff checks passed. Cross-package integration coverage confirms service
execution: receipt 94.1%, scope 79.5%, proposal 80.6%, validation 100%. These are
test coverage figures, not a completion percentage. Callback source proofs drive
the new HTTP/database tests; previous real v4.15.3 adapter proof is separate.
Full real-source HTTP and normal HMS Admin/recipient/independent-reviewer UI are
unproven. New-head CI is tracked separately. No schema/production change, migration,
merge, deployment, release or completion tag is claimed.

Next: minimized recovery request detail/activity, source conflict/orphan checks,
independent decision service/API, prepared-command/profile correction and consumer
UI; recipient setup/sign-in, canonical registration (#12), legacy grants and all
remaining A01–A12 lifecycle/cross-module cutoff acceptance. Counts remain 10 partial,
10 audit pending, 1 research, 0 fully accepted. Full Access/RBAC stays active.

### Recovery-operator authority checkpoint at 2e68596 — 2026-10-31

Owner `2e68596` adds a bounded source verifier for active/verified human,
configured platform organization, direct console-project grant, authoritative
organization/project/grant state and current role definitions/keys. Token-time
roles and grant projections alone cannot authorize recovery. Relevant source
heads must remain unchanged across snapshot reads; stale, malformed, ambiguous,
changing or saturated proof fails closed. No source roles/memberships are changed.

HTTP authentication now retains verified console-token origin; registered
machines remain products. A regression reproduced the lost origin before the
fix. Recovery policy rejects legacy-origin admins, product machines and customer
credentials; operators can only read, administrators can propose/decide subject
to later live proof and target checks. These new actions/source proof are not yet
wired to operator service endpoints.

An explicitly selected actual isolated Zitadel v4.15.3 test passed current proof,
grant deactivation denial/reactivation, removed-role-definition write denial with
read preservation and disabled-human denial. Backend-created synthetic operator
authority is a protocol fixture, not human-login or normal HMS Admin/recipient/
independent-reviewer UI acceptance. TLS tests additionally cover foreign scope,
stale contact/state, missing/malformed history, churn/saturation, redirects,
ambient cookies and provider errors.

Full actual-PostgreSQL owner race/coverage passed: 56.4% overall, API 63.8%,
authz 95.2%, store 64.9%, provider 86.7%, operator verifier 94.0%. Vet/build/lint
(zero issues), four delivery tests and redacted secret/diff checks passed.
No schema changed. New-head CI is tracked separately from local evidence;
no merge, production migration, rollout, release or completion tag is claimed.

Next: audited service integration using the actual operator principal and fresh
canonical registry/source scope; conflict/orphan proof; authenticated recovery
endpoints/activity and prepared-command/profile correction; normal operator/HMS
Admin/recipient/reviewer UI; recipient authenticator setup, canonical production
registration (#12), legacy grants and all remaining A01–A12 lifecycle/cross-module
cutoff acceptance. Counts remain 10 partial, 10 audit pending, 1 research,
0 fully accepted. Full Access/RBAC remains the active objective.

### Atomic reviewed recovery and historical self-review — 2026-10-31

Owner `a99551f7ee120e4890e9d342dd626aedd2a279c9` is pushed in draft PR #22.
Independent decision, prepared successor, permanent root/parent lineage and audit
commit atomically; approval itself performs no provider dispatch. Exact retry
retains reviewer/reason/successor ID and original server-selected provider ID/nonce.
Final rejection, one competing replacement winner, audit rollback and bare-approval
failure are verified. Generation-linked FKs and unique parent/request/successor
prevent branching/cycles with indexed point reads. Historical root/intermediate
creators and accounts remain excluded from later-generation review; actual tests
reproduced that bypass before the fix and now reject it.

Normal creation admits the reviewed successor and blocks closed/superseded parents.
Receipts preserve late/historical creation facts and add only recovery state and
successor command ID. Observation/creation deny authority before/during-source
closure; no private operator reason/identity or child provider binding is exposed.
Actual application-role decision/retry and lineage mutation denial pass. These
are store/contract/race checks, not verified current operator authorization or
normal operator/HMS Admin/recipient/independent-reviewer UI acceptance.

Canonical schema `325012bc6fbaf199413dbce6ed58406be4ca66e5`, chart 0.3.62,
SHA-256 `aa89167dad24dcfb3fd71e305cad29f573d1604ffc99ee75f9e8f459b4880756`,
is published, hash-verified and pinned for owner CI. Actual local catalog checks
confirm six new indexes valid/ready, seven lineage FKs valid and onboarding
SELECT/INSERT with UPDATE/DELETE/TRUNCATE denied. Bootstrap ON_ERROR_STOP=0 means
Job success is not migration proof; actual state validation remains a release gate.

Fifteen added tests (11 transaction/lineage, four API scenarios) plus fourteen
previous recovery tests pass. Full actual-PostgreSQL race/coverage: 55.1% overall,
API 63.8%, store 64.9%, decision 83.6%, successor/root helpers 83.3%, shared scope
100%. Vet/build/lint (zero issues), four delivery tests, redacted secrets and
shell/diff checks pass. Schema local Helm lint/template and seven selected tests
pass. Exact owner head passed CI 41102254872. Exact schema head passed Repository Tests
41101963585, PR Validation 41101963451 and Helm 41101963412. These current-head
passes do not prove operator/source authorization or full normal-role UI acceptance.

Next: verified-current operator role/source proof, conflict/orphan handling,
authenticated operator endpoints and minimized activity; prepared-command/profile
correction; normal operator/HMS Admin/recipient/reviewer UI; recipient authenticator
setup/sign-in; canonical production registration (#12); legacy staff/link/grant
classification; remaining A01–A12 lifecycle and cross-module cutoff acceptance.
No operator write endpoint is exposed and no merge, production migration, rollout,
release, issue closure or completion tag occurred. Full Access/RBAC remains open;
Staff Management area counts are unchanged.

### Recovery requests and independent decision records — 2026-10-31

Owner `713076bd9ef832730ca69088637c03261e9170fa` (runtime unchanged from
`fd65aca6e37faa95dafeeb8e8fdf1e378b5711b5`) is pushed in draft PR #22.
Its new store operation retains exact recovery proposal input and mandatory audit
atomically. Current canonical machine/entitlement/source scope is rechecked;
original command/initiator/provider-user facts survive machine-credential rotation.
Changed requester/action/reason/evidence conflicts. Audit actor/type/product/tenant/
command/action must match the request. Failed new/replay audit releases no private
result and retains no new request. No operator endpoint is exposed yet.

Canonical schema `8fa241651362392d8c90322e97947066688bb832`, chart 0.3.60,
SHA-256 `b9bfb2a34e7e8ff0708ce4bccaf52121e8a64557c7ff21a160189332e4a0b14c`,
is persisted in schema-owner PR #1616; owner CI pins it and the published immutable
source hash was verified. New append-only request/decision ledgers bind an actual
dispatch and exact source/actor facts. Maker, original initiator, original source
account and replacement initiator cannot review. Rejected requests remain final;
one replacement approval wins concurrently. Closing unresolved investigation
permits later reviewed replacement and preserves the original dispatch/root.
An approved row alone authorizes no dispatch, linking or operational membership.

Fourteen new actual-PostgreSQL tests passed, including self/target review,
forged/NULL bindings, mandatory evidence, final rejection, closure/replacement,
twelve concurrent replacement approvals with one winner, actual application-role
mutation denial, machine rotation, withdrawal/scope drift, immutable retry and
audit rollback/binding. Actual application-role proposal and exact retry also
passed. Full owner race/coverage passed: 54.5% overall, API 63.8%,
store 62.7%, proposal operation 87.0%. Vet/build/lint (zero issues), four delivery
checks, redacted secrets and shell/diff checks passed. Schema local Helm
lint/template and seven selected repository tests passed. Latest owner CI 41095777733 is pending. Schema head passed PR validation
41094188936, repository 41094188917 and Helm 41094188898. Earlier owner passes
are historical, not substitutes for the new owner head.

Still required: atomic reviewed decision execution/successor/root lineage,
current verified operator authority and owner/consumer endpoints, conflict/orphan
handling, full Admin/recipient/independent-reviewer UI, recipient authenticator
setup/sign-in, canonical production registration (owner #12), legacy
staff/link/grant classification and all remaining A01–A12 lifecycle/cutoff gates.
No merge, migration, rollout or release occurred; area counts stay unchanged.

### Actual recipient contact UI verification — 2026-10-31

A fresh browser visit reproduced a missing fixture dependency: the isolated
v4.15.3 provider redirected to Login V2, but that UI was absent. Added a separate
loopback-only Login V2 v4.15.3 container, pinned to upstream image digest
`sha256:906081d8c5b8760321fa811d670e680247f768e5c4bdd4edb73d3ea942800385`.
Its dedicated IAM_LOGIN_CLIENT machine has a private, two-day credential; it is
not a Staff or operational access grant. Original instance-feature configuration
was saved privately before changing only this isolated fixture's Login V2 base
URI. Added pinned Mailpit local capture on the fixture network; its UI is
loopback-only and no external email delivery was configured.

Normal browser actions used the existing role-free synthetic account: enter
login name, request a fresh invitation, read the delivered email and submit its
code. The provider changed to active/verified; the unchanged owner adapter then
observed active plus email_verified=true and the exact retained creation time.
Fresh provider queries returned zero memberships and zero user grants. No
password, passkey or verified-contact override was set by this test.

The next visible screen is Choose authentication method (Password/Passkeys).
Full authenticator setup, subsequent sign-in, actual canonical registry proof,
HMS Admin/Global API consumption and independent reviewer acceptance remain
open. This proves actual recipient contact verification through upstream UI,
not the complete A01 provisioning or Access/RBAC journey. The browser is retained
for continuing recipient setup; private evidence/setup and recovery captures are
0600 under the existing authorized QA fixture directory. All changes were local;
production was untouched.

### Access/RBAC private command history — 2026-10-31

Owner commit `9fd472d49b148ff4b1c207da609602b1b1d8d533` is pushed in draft
[PR #22](https://github.com/tesserix/onboarding-service/pull/22). Exact-command
history now returns bounded action/outcome/UTC summaries with current product,
tenant, source and original-initiator checks. Opaque hash-only cursors preserve
15-minute expiry across service instances and exclude later appends until
refresh. Mandatory disclosure-audit failure returns no page or new cursor.
Human viewer authorization remains the Helivanta consumer's responsibility.

Canonical SQL is persisted at `1acc9d687cd161bf77689424fdbb3a1716decd9c`,
chart 0.3.59, SHA-256
`8e2f89da8e6f4771a7d0af3ca075094f2ec622ab6f0ce38475f9b7660018b382`.
Schema-owner PR #1616 passed Repository Tests 41085750101, PR Validation
41085749902 and Helm/template/security 41085750047. Its ephemeral cursor cache
is separate from the four immutable identity ledgers. Actual application-role
tests verified continuation/cleanup, expired-only deletion, no UPDATE/TRUNCATE,
a 1,000-row cleanup limit and skipped locked rows. Narrow UPDATE(token_hash)
privilege permits PostgreSQL row locks; forced RLS rejects actual updates.

Full local owner race/coverage passed (54.3% overall, API 63.8%, store 61.6%),
plus vet/build/lint (zero issues), redacted secret scan, four delivery tests,
shell/diff checks and a Linux/arm64 image build. A rolled-back 100,001-row audit
fixture used the partial index in 0.046 ms; this is query-plan evidence, not an
API SLO. Exact-head CI [41089960472](https://github.com/tesserix/onboarding-service/actions/runs/41089960472)
passed all applicable gates, including the container build/smoke/scan job.

Operator resolution/replacement/conflict/orphan handling, actual recipient
contact delivery/setup, canonical production product registration (owner #12),
Global API/Admin/recipient/independent-reviewer UI, legacy classification and
all remaining A01–A12 lifecycle/cross-module acceptance stay open. No merge,
production rollout or migration occurred. Counts remain 10 partial, 10 audit
pending, 1 research and 0 fully accepted.

### Access/RBAC creation and contact reservation — 2026-10-31

Owner runtime `f7bc168` now exposes creation PUT with immutable input,
server-selected account/nonce, permanent contact reservation, one audited
dispatch and retained creation receipts. Exact/concurrent retries do not create
again; lost responses remain uncertain until matching read-only reconciliation.
Canonical SQL is persisted at schema-owner `2214e7a7` (chart 0.3.58), SHA-256
`676f5c8676e48d9647b3d08e37a6e82ca84c91d22d8d4d343ee9bc9e03fafb63`.
Its repository, Helm/template/security and PR gates passed. Roots retain command
lineage and cannot be rewritten/deleted by the application; historical commands
without roots fail closed rather than receive invented backfill.

Full local owner race tests, vet/build/lint, secret scan, four delivery tests and
the final Linux/arm64 build passed. An explicitly selected actual v4.15.3 provider
test passed owner HTTP creation/replay/observation and verified zero source
memberships/user grants with contact still unverified. It uses fixture registry
proof, not normal Admin/recipient/reviewer UI acceptance. Private account evidence
is retained as 0600 JSON; no password/verified flag was supplied to creation.
Latest owner head `fc5d930` passed all applicable CI gates in
[41076976851](https://github.com/tesserix/onboarding-service/actions/runs/41076976851),
including its container. The earlier runtime run 41076590905 was superseded and
cancelled after its Go/contracts/secret checks; it is not counted as passing.
Subsequent owner/schema commits update documentation only.

Operator recovery/history/root replacement, actual recipient setup/verification,
canonical product registration, Helivanta/Global consumer UI and the remaining
A01–A12 lifecycle/cross-module acceptance remain open. No merge, production
rollout or new live migration occurred. Counts remain unchanged.

### Access/RBAC provider failure acceptance — 2026-10-31

The identity-owner candidate now includes `ec10aa6` in draft
[PR #22](https://github.com/tesserix/onboarding-service/pull/22). Actual isolated
v4.15.3 testing reproduced stale active proof after successful deactivation,
stale binding metadata after replacement, and skipped deactivation hidden by a
later unrelated profile revision. The corrected adapter compares latest
authoritative state/contact/verification events and bounded mutation history;
the same live skipped-deactivation fixture now denies proof. Explicit synthetic
source reactivation restores successful observation. These are provider-only
tests, not Admin/recipient/reviewer UI or membership/employment activation.

Final full owner race/coverage passed (54.6% overall, provider 85.2%), as did
vet/build/lint, secret scan and the final Linux/arm64 image build. Exact-head CI
[41070733440](https://github.com/tesserix/onboarding-service/actions/runs/41070733440)
is in progress; preceding owner CI 41066660408 and Helivanta documentation-head
CI 41067001369 passed. Details and retained limitations are in the
[identity audit](staff-identity-provisioning-audit.md). No merge, production
rollout or new application migration occurred. The full A01–A12 objective and
the counts below remain unchanged.

### Access/RBAC continuation — 2026-10-31

Identity-owner draft [PR #22](https://github.com/tesserix/onboarding-service/pull/22)
now contains committed/pushed `e3dc7e5`: protected command receipts, fresh source
observations, matching-dispatch reconciliation, atomic disclosure audit, replay
cache exclusion and strict source JSON. The full owner race suite with actual
isolated PostgreSQL, vet, build and lint passed again. Exact-head CI 41066660408
was queued at this checkpoint. This candidate remains unmerged/undeployed.

The [identity audit](staff-identity-provisioning-audit.md#owner-receiptobservation-checkpoint--2026-10-31)
records the open provider projection-synchronization gate and corrected explicit
product registration requirement. Creation submission/recovery, real recipient
verification, consumer UI, legacy reconciliation and the remaining A01–A12
lifecycle/cross-module acceptance are still required. Counts remain **10 partial,
10 audit pending, 1 research, 0 fully accepted**; the full Access/RBAC objective
remains open. The production checkpoint below is unchanged.

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
preserves the replacement. A later local legacy-contract migration below addresses
the remaining raw subject/role DELETE regression.
Real provider/proxy tests reject incomplete, malformed, conflicting and failed
proofs instead of accepting partial authority. Focused signed-session, source
race, boot/prune, HTTP seam and approved permission-matrix checks pass. At `fee984e6`, the
fixed-source affected race/coverage run passed authorization, reconciliation,
architecture, bootstrap and API suites. IAM's only failing test is the retained
unsafe raw-DELETE replay regression. Vet, affected linter (zero issues), API and
Global API builds, and contractdocs generation/check passed. No new source is
merged or deployed and no A-item is newly complete.

The next local [legacy contract migration](../superpowers/specs/2026-10-30-legacy-membership-contract-migration.md)
refuses the unsafe raw DELETE with `409 membership_command_required` instead of
selecting whichever replacement row currently has the same subject/role. A new
real-PostgreSQL test failed with actual 202 versus expected 409 before the change,
then passed with the race detector. It verifies unchanged membership, no new
outbox event or fabricated receipt, absent-target parity and restricted-actor
denial. Existing positive test consumers now use exact-incarnation commands;
the original replacement-survival assertion is retained across actual removal,
recovery, raw DELETE replay and exact removal-command replay. The final affected
race/coverage run passed IAM (81.8%), login client (90.7%), Global API (86.7%),
journeys, architecture and bootstrap (94.1%). These are local results, not full
A01–A12 or release acceptance.

The first run passed Global API and membership journey suites, but IAM did not
compile because of a missing test import. The next full run found a dynamically
constructed concurrent legacy DELETE still in the projection test; it was
migrated to frozen commands without removing the concurrent epoch/history
assertions. The focused group then passed (92.748 seconds), followed by the final
full run (IAM 319.211 seconds). Vet,
affected lint (zero issues), API/Global API builds, contractdocs generation/check
and the canonical staff-access generator check passed. No web code changed in
this increment. Breaking-contract/security/consumer review, the unresolved
external sunset decision, normal-role browser acceptance and production gates
remain open. No production or application database was changed.

Final local commands (from `backend/`; disposable test services only):

```sh
DOCKER_HOST=unix:///Users/samyakrout/.colima/helivanta-staff-withdrawal-20261030/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=15m -p=2 -parallel=4 \
  ./internal/modules/iam/... ./internal/globalapi/... ./internal/journey/... \
  ./internal/archtest/... ./internal/bootstrap/...
mise exec go@1.27.3 -- go vet -p=2 ./internal/modules/iam/... ./internal/globalapi/... ./internal/journey/...
mise exec go@1.27.3 -- golangci-lint run ./internal/modules/iam/... ./internal/globalapi/... ./internal/journey/...
mise exec go@1.27.3 -- go build -p=2 ./cmd/api ./cmd/global-api
mise exec go@1.27.3 -- go run ./cmd/contractdocs
mise exec go@1.27.3 -- go run ./cmd/contractdocs -check
```

From the worktree root, `mise exec node@24 go@1.27.3 -- node
scripts/generate-staff-access.mjs --check`, changed-document Prettier and
`git diff --check` passed. The staged Gitleaks scan found no leaks.
The authorization latency SLO and new-workflow browser acceptance remain
unproven. Existing SQL 0015–0017 remains persisted and unapplied to application
or production databases; the legacy refusal adds no migration.

The next local [identity eligibility follow-up](../superpowers/specs/2026-10-30-membership-identity-eligibility.md)
replaces recovery's active-state-only read with one matching active-human and
verified-email proof. The real PostgreSQL command regression first returned 202
for an active unverified human where 409 was required, then passed after the gate.
Provider HTTP tests cover matching, unverified, machine, missing/inactive,
unknown/ambiguous/malformed evidence, outages, subject mismatch, renamed or
conflicting fields and bounded complete responses. Restoring the old decoder
made the second-document and oversized-valid-prefix assertions fail; restoring
the fix made the full client suite pass. Focused recovery/bootstrap tests pass,
including receipt replay without another provider read or duplicate outbox event.

Read-only execution of the new reader against the existing local synthetic Admin
and reviewer returned eligible for both, without displaying private identities
or credentials. This is client/provider compatibility evidence, not a new user
verification journey or browser acceptance. The final affected race/coverage run
passed IAM (81.8%, 317.769 seconds), login client (91.3%), bootstrap (94.1%),
architecture, API construction (10.6%), Global API (86.7%) and journeys. Vet,
affected lint (zero issues), API/Global API builds, contractdocs generation/check,
canonical staff-access check, formatting/diff and staged secret scan also passed.
No schema, route, event or permission is added by this follow-up. No web code
changed; normal-role browser and deployed acceptance remain pending.

The identity follow-up's final test command (from `backend/`, using the same
isolated Docker environment shown above) was:

```sh
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=15m -p=2 -parallel=4 \
  ./internal/modules/iam/... ./internal/bootstrap/... ./internal/archtest/... \
  ./cmd/api/... ./internal/globalapi/... ./internal/journey/...
```

Its affected vet/lint scopes were `./internal/modules/iam/...`,
`./internal/bootstrap/...` and `./cmd/api/...`; build, contractdocs and canonical
check commands were unchanged. Local runtime discovery confirmed Zitadel
`v4.15.3` through its running image and version command, without changing Docker
context or credentials. Production provider version and provisioning capability
remain unverified.

The next local [first-enrollment candidate](../superpowers/specs/2026-10-30-first-membership-enrollment.md)
adds an account/workspace-pinned, reasoned command for an existing verified human
with no previous tenant membership. It fixes the source role to `staff_member`,
retains trigger-generated immutable receipts and protected history, and refuses
to reopen ended continuity. The Staff form now requires reason/acknowledgement,
freezes uncertain ID/body and shows history; source persistence is distinguished
from graph readiness and operational access. It does not create IdP users or
migrate the legacy privileged POST.

The initial HTTP regression failed 404 versus expected 202 before implementation.
Focused race tests now pass for input/actor/workspace boundaries, exact/conflicting
replays, source history after removal, tenant isolation, direct receipt fabrication,
provider failure, transactional outbox rollback and concurrent exact commands.
A real PostgreSQL lock-wait test observes the writer blocked on the Admin row,
then withdraws that Admin before releasing the lock; the command returns 403 with
no membership, receipt or event. Global API coverage proves projection to ordinary
membership and no business permissions. Client/form/page tests pass (6 API tests,
26 form/page tests). Initial test-fixture DDL was correctly denied to the app role;
the outbox failure now uses the existing owner migration fixture instead.

Append-only migration `0018_iam_membership_enrollments` is persisted; IAM now has
18 migrations and the whole registry 176. Disposable fixture databases only have
been migrated; application/production state remains unchanged. The full affected
race/coverage run passed IAM (81.9%, 386.768 seconds), login client (91.3%),
platform (87.8%), bootstrap (94.1%), API construction (10.6%), Global API (86.7%)
and journeys. Its one failure was pagination architecture metadata for the
single-receipt GET; a specific reasoned entry declares its command-primary-key
lookup, while collection history stays ListRoute. The complete architecture suite
then passed (40.441 seconds), with zero lint issues. New-head CI/browser acceptance
is pending. The full web suite passed
(seven tasks, including API 412, UI 267 and shell 1,255 tests), API/shell/E2E
TypeScript checks, affected web lint, Go vet/lint (zero issues), API/Global API
builds, contractdocs generation/check and the shell production build passed.
The first build correctly rejected an unset production `API_URL`; rerun with the
local Global API origin passed. Type-checking caught an alias collision with the
Workforce-owned `StaffEnrollment` type; the unused IAM alias was removed rather
than redefining Workforce's export. The updated browser
E2E setup creates a random synthetic account only against localhost, stores its
credentials in an ignored mode-0600 JSON outside test artifacts and then uses
normal Admin and recipient UI. Its fixture-set verification flag is not evidence
of a recipient completing contact verification. The [historical-continuity
gate](staff-identity-provisioning-audit.md#historical-continuity-acceptance-gate)
remains open: a missing continuity record cannot prove absence of pre-retention
membership or historical owner authority. No A02/A11 acceptance follows from the
fresh-case tests. All five PR CI jobs passed at `78513985`; that baseline does not
include this uncommitted enrollment work.

Final first-enrollment commands (from `backend/`, same disposable Docker socket):

```sh
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=20m -p=2 -parallel=4 \
  ./internal/modules/iam/... ./internal/platform/... ./internal/bootstrap/... \
  ./internal/archtest/... ./cmd/api/... ./internal/globalapi/... ./internal/journey/...
mise exec go@1.27.3 -- go test -race -cover -count=1 -timeout=10m -p=2 -parallel=4 ./internal/archtest/...
```

From the root: `pnpm exec turbo run test --concurrency=2`, affected API/shell/E2E
`type-check` and `lint`, `API_URL=http://127.0.0.1:8199 pnpm exec turbo run build
--filter=@helivanta/shell --concurrency=2` (Node 24/Go 1.27.3), canonical staff-access
check, changed-file formatting and diff checks passed. Go vet/build scopes and
contractdocs commands are as above; affected and architecture lint passed.
No current-head browser/deployed acceptance or latency SLO claim follows.

The enrollment candidate was committed and pushed as `549f2f93` in draft
[PR #1489](https://github.com/tesserix/helivanta/pull/1489). Its CI run
`41045911107` failed browser test discovery before executing any scenarios:
`import.meta` in the new fixture is incompatible with Playwright's CommonJS
loading. The exact local discovery command reproduced that syntax error. Using
`resolve(__dirname, ...)`, as the suite's existing environment loader does,
restored discovery of all three Staff access scenarios. E2E typecheck, lint and
format checks passed. The complete browser workflow and remaining live CI jobs
must still be checked; discovery alone is not enrollment acceptance. This
correction changes fixture path resolution only, with no runtime API or SQL
change. The command was `mise exec node@24 -- pnpm --filter e2e exec playwright
test tests/staff-access.spec.ts --list`; package checks used the
`@helivanta/e2e` filter with `type-check`, `lint` and `format:check`.

The [A01 provisioning audit](staff-identity-provisioning-audit.md) records source
and versioned primary provider evidence. The current login client exposes active-state and
presentation-profile reads, without a provisioning/contact-verification workflow.
The local seed script marks synthetic email verified during fixture creation;
it is not acceptable evidence for real user verification. The existing generic
membership POST neither verifies the provider identity nor enforces explicit
recovery after final removal; it also accepts privileged system roles. Those
paths still need the coordinated provisioning/recovery contract and current
actor/source guards before A01/A02/A11 can be accepted.

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
