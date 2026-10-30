# Staff identity provisioning audit

## Staff document account binding — 2026-10-31

The source audit reproduced ignored expected-account assertions and unbound
client document caches. The current correction covers the ten owner routes,
Staff UI, SDK, classification retry state and credential-evidence selector.
See the [progress/evidence checkpoint](staff-management-progress.md#accessrbac-checkpoint--2026-10-31-staff-document-account-binding)
and [33-route intake inventory](staff-intake-access-inventory.md). This is an
undeployed access correction; full A01–A12 acceptance remains open.

## Access/RBAC checkpoint — 2026-10-31: ended-membership admission

Candidate IAM migration `0019_iam_membership_recovery_admission` blocks legacy
restoration after retained final removal. Explicit recovery and duplicate repair
remain supported; raw/legacy projection fixtures now exercise public recovery
without weakening stale-authority assertions. See the
[current evidence and outstanding gates](staff-management-progress.md#accessrbac-checkpoint--2026-10-31-ended-membership-admission)
for the reproduced RED, validation results and real-provider/UI limitations.
This remains an undeployed A02/A11 correction. Full A01–A12 and all 21 Staff
area acceptance gates remain open; no production migration or rollout occurred.


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


## Atomic reviewed recovery and historical self-review — 2026-10-31

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

## Recovery requests and independent decision records — 2026-10-31

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

## Actual recipient contact UI verification — 2026-10-31

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

## Private command history — 2026-10-31

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

## Creation command and contact roots — 2026-10-31

Owner runtime `f7bc16899dcfe81cc8c0b3a9a4f003bc6da419d0` adds actual creation PUT
to the protected receipt/observation candidate. The service freezes server-selected
identity/nonce and exact request fields, reserves the case-insensitive contact,
reauthorizes source/registry scope before dispatch and after provider I/O, and
commits one dispatch claim before creation. The URI command is authoritative;
generic response-cache keys cannot disclose receipts after withdrawal or bypass
changed input. Known creation yields 200; uncertain source outcomes retain 202
dispatched state without fabricated account/verification facts. A later exact
observation can reconcile creation without a new provider call.

The schema owner persists append-only roots with a unique source-org/contact
key and composite FK to the original command/org/email, chart 0.3.58 at
`2214e7a7d6730570447bbe1f0ad8e341c5026c3b`, SQL SHA-256
`676f5c8676e48d9647b3d08e37a6e82ca84c91d22d8d4d343ee9bc9e03fafb63`.
Go lowercase normalization matches inspected v4.15.3 username constraints;
dot/plus rewriting and email-based identity linking are excluded. Eight competing
commands retain one root/command; preparation audit failure rolls both back.
Exact retries retain their original random fields. Missing historical roots
cannot be manufactured or used for dispatch. Application UPDATE/DELETE/TRUNCATE
denial passed for all four ledger tables. Operator replacement must retain
explicit lineage; its endpoint/history and recovery decisions are still open.

Full actual-PostgreSQL owner race/coverage passed (54.1% overall, API 62.8%, store
58.7%), plus vet/build/lint (zero issues), secret scan, four delivery tests,
shell checks and final Linux/arm64 `helivanta-staff-onboarding-create:local` build.
API tests cover required/closed/UTF-8 input, fresh creation, changed input,
concurrent retries, timeout/invalid results, read-only lost-response recovery,
midflight withdrawal, foreign/disabled source and replay-cache withdrawal.

An explicit `staff_provider_fixture` test also passed against the isolated actual
v4.15.3 provider: owner HTTP creation and exact replay, fresh unverified observation,
and zero actual source memberships/user grants. Source org/registry proof is
fixture-supplied in this test; it is not a normal-role UI or recipient verification
claim. Private 0600 account evidence contains no password or verification override.
Latest owner `fc5d930` passed all applicable CI in 41076976851, including its
container. Earlier runtime CI 41076590905 was superseded/cancelled after passing
Go/contracts/secrets, not reported as a full pass. Schema 2214e7a7 repository/
Helm/PR gates passed; documentation-only d69e0730 has its own CI still running.

No production change, merge, release tag or operational Staff activation occurred.
Actual human authority/Global API/consumer UI, recipient setup, bounded history,
operator recovery, legacy classification and the remaining employment/access
cutoff journeys are not accepted by these owner/provider tests.

## Actual provider fault correction — 2026-10-31

Owner source `ec10aa63d794234e22f9a23ea92cb0729678caf9` is committed/pushed in
draft PR #22, unmerged/undeployed. A separate actual Zitadel v4.15.3 fixture
reproduced deactivation at sequence 4 while two reads returned active/verified
at user revision 2 and matching metadata revision 3. The original provider
adapter accepted this in 102 ms. A metadata-specific fault similarly retained
the original binding at revision 3 after its replacement committed at 7.

Extended failure testing found that a newer user revision is also insufficient:
after 15 failed reads, the provider skipped deactivation 14 and advanced to 15
for an unrelated profile change while remaining active. The first revision-only
candidate still accepted it. Its regression failed before the final correction.
The final adapter compares exact latest state, email change and verification
events independently of row revision, and checks bounded relevant mutations.
The actual skipped-event fixture then denied proof in 135 ms; explicit source
reactivation at sequence 16 restored a successful observation in 227 ms.
Unrelated metadata remained accepted; stale binding replacement was denied.
These are individual local timings, not p99/SLO evidence.

The admin event API uses existing owner `events.read`, exact user aggregate and
resource-owner filters, bounded typed searches and `AwaitOpenTransactions` in
the pinned source. Missing permission, outage, redirects, malformed/foreign
proof and saturation fail unavailable. Source payloads/editor details are not
returned. No permission/credential expansion occurred. Source configuration,
organisation/registry proof, saturation recovery, real recipient setup and
consumer command-boundary acceptance remain review gates.

Full actual-PostgreSQL owner race/coverage passed (54.6% overall, provider
85.2%); vet/build/lint (zero issues), redacted secret scan and final Linux/arm64
image build passed. New exact-head CI 41070733440 is in progress. Baseline
41066660408 passed. Synthetic setup verification overrides are not recipient
verification evidence. Private fixtures, SQL fault captures and immutable
provider history are retained separately; fault triggers are removed. No
production schema/account/grant changed. Creation/recovery/consumer UI and the
remaining A01–A12 lifecycle journeys remain open.

## Owner receipt/observation checkpoint — 2026-10-31

Owner draft PR #22 now includes commit
`e3dc7e58c8fd36b27b45a07ff485fc4eba48e257`: exact protected receipt reads,
fresh observations, read-only reconciliation of matching dispatches, mandatory
atomic disclosure audit and strict provider JSON decoding. Observation requests
bypass the generic replay cache; real database/API regressions cover changed
verification and entitlement withdrawal with the same Idempotency-Key. Denial
audit attempts have a separate two-second cancellation-independent deadline.
The product key must be explicitly configured; ADR-0031's `hms` fixture is not a
production default, and canonical registration remains under owner issue #12.

The full owner `go test -race -p 1 ./...`, `go vet ./...`, `go build ./...`
and `golangci-lint run ./...` passed again against the isolated PostgreSQL
fixture on 2026-10-31 (zero lint issues). Exact-head CI
[41066660408](https://github.com/tesserix/onboarding-service/actions/runs/41066660408)
was queued when this checkpoint was recorded. Baseline owner CI 41052581748 and
schema gates at `18d42798` passed. No new migration or production action occurred.

At that earlier checkpoint, primary-source review established an open synchronization
gate: user/metadata query helpers can proceed to SQL after projection-trigger
failure. Stable repeated rows alone do not prove current eligibility. No actual
running-provider projection-failure exploit had been reproduced. The actual-fault
correction above supersedes this finding's research-only status. Before enrollment
consumes these observations, reproduce failure and prove an authoritative source
boundary. No enrollment consumer currently uses the new observations.

The management `POST /management/v1/users/{id}/changes/_search` is a candidate
read-only boundary: `ListUserChanges` requires `user.read`, scopes to the current
organisation and exact user aggregate, and searches the event store with
`AwaitOpenTransactions`. Its query supports sequence/limit/order, not event-type
filtering; its response contains private editor details. Any consumer must bound
and minimize that response and distinguish relevant identity/metadata changes
from unrelated login/password events. Metadata rows have independent revisions;
comparing every event against the user's row revision would incorrectly deny
normal accounts. This is research evidence, not an accepted implementation.

Creation PUT, contact reservations, bounded history, operator recovery, real
recipient verification/account setup, consumer UI and all remaining A01–A12
acceptance gates remain open. The candidate is committed/pushed, unmerged and
undeployed; no broad area is newly accepted.

Date: 2026-10-30. Scope: A01/A02 prerequisites for the full Access/RBAC plan.
Status: source and provider-contract findings; implementation and runtime
acceptance remain open. Related issues #417 and #1053 remain open.

## Current implementation

- IAM's `loginclient.Client.UserState` reads `GET /v2/users/{id}` and classifies
  known non-active states separately from unavailable/unknown provider answers.
  Active state alone is not proof of contact verification or a human identity.
- The follow-up `MembershipEligible` reader now binds one provider response to
  the exact subject, active human type and verified email, returning no private
  profile. The candidate recovery command consumes it through explicit bootstrap
  wiring. Unknown/ambiguous proof fails unavailable. This is local implementation,
  not deployed provisioning or acceptance.
- `UserProfile` reads human display name and email for presentation. Its result
  does not contain email-verification state, provider organization or a resource
  version. Do not reuse it as provisioning eligibility evidence.
- `POST /v1/iam/members` accepts a subject and any known system role, without a
  provider existence/verification check. Uniqueness preserves the current row
  on duplicate enrollment, but a new POST can recreate an ended membership.
- The candidate Staff form now uses `PUT /iam/membership-enrollments/:id`, pins
  the actor and tenant, requires a reason/acknowledgement, freezes uncertain
  commands and shows protected enrollment history. The additive backend fixes
  the role to `staff_member`, verifies identity, refuses previous continuity and
  atomically retains receipts/audit/outbox. This does not close the separate
  generic POST bypass; that route is still present.
- `scripts/seed-dev.mjs` creates synthetic humans with `isVerified: true` and a
  fixture password. That is setup, not a user completing contact verification.
  It must not become the production onboarding implementation or acceptance
  evidence for A01.

## Primary provider evidence

The official [Zitadel v4.15.3 UserService protobuf](https://github.com/zitadel/zitadel/blob/v4.15.3/proto/zitadel/user/v2/user_service.proto)
was inspected as a versioned reference on 2026-10-30. This version matches the
version cited in existing login-client verification notes. Subsequent read-only
local discovery confirmed the running `helivanta-dev-zitadel-1` image tag
`v4.15.3`, image ID
`sha256:2356d646340724b3d843c024d589314adb9b54dbd6452ebc34008b011e023eff`,
and `/app/zitadel -v` output `zitadel version v4.15.3` through Docker context
`colima-helivanta-tests`. No Docker context was changed. This establishes the
local provider version, not production version or creation authority.

- `CreateUser` uses `POST /v2/users/new`; the documented required capability is
  `user.write`. `AddHumanUser` at `/v2/users/human` is deprecated in this snapshot.
  Existing seed-script success on the latter proves neither endpoint support
  nor creation authority for the application's login-client credential.
- `CreateUserRequest` supports an optional immutable, instance-unique `user_id`.
  The response uses `id`; the deprecated creation response uses `user_id`.
  Do not copy seed response decoding into the new client.
- Human creation requires a profile and email. The contract describes default
  verification messages and explicitly depends on configured delivery services.
  An HTTP creation response cannot prove delivery or verified contact.
- `SendEmailCode` uses `POST /v2/users/{user_id}/email/send`; `VerifyEmail` uses
  `POST /v2/users/{user_id}/email/verify`. The provider owns verification codes
  and their effect; an Admin must not manufacture a verified state.
- A caller-chosen provider ID offers a reconciliation basis for uncertain
  creation. The actual create/conflict/read behavior still needs provider tests;
  it is not an established idempotency guarantee from a field definition alone.

### Creation dispatch and deletion finding

Further primary-source inspection followed the v4.15.3 CreateUser human handler
in `internal/api/grpc/user/v2/human.go` to `command.AddUserHuman` in
`internal/command/user_v2_human.go`. The command's ID check calls
`isUserStateExists`, which excludes deleted and unspecified states in
`internal/command/user_model.go`. The existence model includes removal events
and reduces them to deleted. `HumanAddedEvent.UniqueConstraints` in
`internal/repository/user/human.go` declares username uniqueness, without an
enduring idempotency receipt. This does not prove live ID reuse, since provider
storage may impose other constraints. It does mean fixed-ID retry safety after
deletion is not established and must not be assumed.

The owner worktree `onboarding-staff-provisioning`, branch
`feat/staff-identity-provisioning`, starts from owner main `0efac26`; original
checkouts are preserved. Its draft
`docs/adr/0004-staff-identity-provisioning.md` records durable one-time dispatch,
read-only reconciliation after uncertainty, explicit accountable operator
recovery and the corresponding real-database/provider/UI gates. It is a design
under implementation, not a published provisioning API or accepted recovery
workflow. Privileged operations stay inside onboarding-service; Helivanta does
not receive its administrator credential. The owner ledger requires canonical
SQL in `tesserix-k8s` before implementation can be accepted.

Fresh owner-source verification also corrected an earlier working assumption:
`readIdentity` at `0efac26` uses bounded `io.ReadAll` followed by
`json.Unmarshal`, so it already rejects a trailing second JSON document. Do not
apply a redundant decoder fix based on the older checkpoint.

The owner now has a `CreateStaffIdentity` provider method under implementation,
committed and pushed as `df042c5` in draft
[owner PR #22](https://github.com/tesserix/onboarding-service/pull/22).
Its input cannot contain roles, credentials or a verification
override; its request uses the current v2 creation path, fixed source
organisation/ID and correlation metadata. It sends once, rejects redirects and
ambiguous/oversized/malformed creation proofs, and retains a generic uncertain
error after possible dispatch. The happy-path test first failed because the
method was absent; subsequent regressions reproduced accepted duplicate proof
fields and dispatched control-character names before their fixes. Full provider
package race/coverage passed (81.1%), plus provider vet, full owner build and
provider golangci-lint (zero issues), using Go 1.27.3. Redirect and private-error
tests also pass. These tests use TLS provider fixtures, not live account creation.
The method is not yet wired to an endpoint: the durable dispatch ledger,
canonical SQL, current product/actor checks, observation/recovery and consumer
UI remain required. No owner database or provider mutation has occurred.

## Durable owner ledger checkpoint

Helivanta head `d3b24348` passed all five jobs in CI `41048168857`, including
the normal Admin enrollment/recipient browser scenario. This is fresh synthetic
enrollment evidence, not real recipient contact verification or A01–A12
completion. No merge, application migration or deployment followed.

The identity owner now retains immutable preparation, one permanent dispatch
claim and one exact creation receipt in commit `87fc47f` on draft
[owner PR #22](https://github.com/tesserix/onboarding-service/pull/22).
Canonical SQL is persisted in draft
[schema PR #1616](https://github.com/tesserix/tesserix-k8s/pull/1616), source
`18d42798e92136b74bdf5ece8c4142c7890fd8d7`, chart `0.3.57`, SHA-256
`706e027e603cd8b0a1320128b1c48da53106c90c421bcf359ec51a0aa7407954`.
Owner CI pins and verifies that exact SQL instead of copying a schema master.
All three new tables deny application UPDATE/DELETE/TRUNCATE and retain
tenant/product references; receipt foreign keys require the original dispatch
and provider ID. Exact replay preserves source timestamp nanoseconds.

The full owner Go race suite passed against isolated PostgreSQL 16 (52.7%
overall coverage); final affected store/provider suites passed at 56.2%/81.1%,
with vet/build/lint and no affected-code vulnerability in the symbol scan.
Actual lock-wait evidence observes `pg_blocking_pids`, withdraws entitlement
before releasing the blocker and verifies no dispatch claim. Tests cover eight
concurrent claimants with one winner/audit, changed actor/input/source, foreign
scope, application rewrite denial, all three atomic audit boundaries and retained
provider outcome after entitlement withdrawal without restoring authority.
Four schema tests and Helm lint passed. At 100,000 synthetic commands the scoped
lookup used an index scan; the planning transaction was rolled back.

Owner CI first needed a reachable shared workflow pin (same Go workflow blob),
then Go 1.27.3 for GO-2026-6809 and currently retrievable pinned base images.
Those fixes are persisted; a local Linux/arm64 image build passed. Current owner
CI `41052581748` and schema CI remain separately tracked; local evidence does not
imply their success. No production database, provider account or credential was
changed. See the owner's `docs/delivery/staff-identity-provisioning.md` for exact
commands and retained-schema rollback requirements.

The owner endpoint, current human/product/tenant authorization, protected
observations/history, read-only reconciliation/operator recovery, real contact
verification/account setup and Helivanta Admin/recipient/reviewer UI are still
open. The ledger/protocol does not activate membership or hospital employment.
Historical identity/link/grant classification, the generic membership POST
bypass, lifecycle cutoffs and the full A01–A12 acceptance gates remain open.

## Local response-shape observation

Read-only GETs using the existing local application credential confirmed the
matching synthetic Admin and reviewer subjects, active state, human email and
`isVerified: true` on 2026-10-30. Only field names and boolean results were
recorded; no email, token or password was displayed. This did not create users,
verify contact, change credentials or establish provider creation permission.

The recovery regression reproduced actual 202 for an active unverified human
before the new gate, versus required 409. Its corrected test uses the real
PostgreSQL HTTP command and an HTTP provider fixture. It is distinct from a
recipient completing a real provider verification workflow.

## Enrollment consumer inventory

A fresh source inventory on 2026-10-30 distinguishes mutation callers from member
list readers. The original checkout's Graft index is stale for several moved IAM
tests; actual worktree files were verified with exhaustive path search.

| Consumer                                                         | Actual use                                                                                                                 | Required migration consequence                                                                                                                                  |
| ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `packages/api/src/staff-membership.ts` and Staff membership form | Candidate first-enrollment PUT with actor/tenant, reason, frozen command and protected history                             | Candidate implemented; normal-role browser/deployed acceptance and old POST migration remain open                                                               |
| `e2e/tests/staff-access.spec.ts`                                 | Candidate E2E creates an isolated synthetic IdP account, then normal Admin first-enrollment and recipient login through UI | Updated to PUT and zero business-permission assertions; verified contact is fixture setup, not recipient verification acceptance                                |
| `e2e/tests/support/opd.ts`                                       | Generic POST adds `doctor` to an existing synthetic clinician                                                              | Privileged clinical role provisioning is a real compatibility dependency; do not silently remove it or describe a setup fixture as accepted clinical assignment |
| Supply organization grant/support editors                        | GET member-list options                                                                                                    | Preserve list response/pagination and permissions; these are not enrollment mutations                                                                           |
| Patient pharmacy-lookup access editor                            | GET member-list options                                                                                                    | Preserve this separate read consumer; no enrollment privilege follows from lookup authority                                                                     |
| IAM, Global API and journey Go tests                             | Ordinary and privileged enrollment/setup, current-role and projection checks                                               | Separate attributable setup from production workflows; retain denial, projection, replay and scope assertions                                                   |

No external-consumer absence is established. A successor form alone cannot close
the generic POST's privileged-grant or historical recovery bypass. Coordinate
that contract migration and the actual privileged-assignment journey explicitly.

## Historical continuity acceptance gate

The candidate checks whether IAM has a continuity record. Absence is not proof
that a global account never had membership before continuity retention existed.
The new receipt makes subsequent history durable, but cannot reconstruct old
employment, Staff/login links or scoped grants. Do not accept A02/A11 or release
first enrollment as safe historical recovery until legacy eligibility is
classified, generation-bound owner checks are verified and any required
provisioning/review basis is enforced. A fresh-account test proves the fresh
case only. The generic POST remains another open bypass.

## Required next evidence

Verify the authorized organization and creation capability without displaying
credentials. The local version is now confirmed; production version and
configuration remain a separate release prerequisite. Establish the tenant-to-IdP
organization boundary and current Admin authority before exposing creation or
private identity lookup. An existing identity may legitimately serve multiple
tenants; email equality alone must not bind a Staff person or disclose another
tenant's private account.

The implementation design must define durable command receipts, exact uncertain
retry/reconciliation, explicit duplicate/conflict resolution, verified-contact
observation, delivery failure/retry and immutable audit. Provider network work
must remain outside owner database transactions. No success response may imply
that membership, a reviewed Staff link, employment or hospital access exists.

The enrollment migration must also remove the generic POST recovery and
privileged-grant bypasses without silently breaking existing consumers. Preserve
current-row duplicate repair, distinguish first enrollment from recovery and
require the current actor/account/tenant at the write boundary. Governance
appointment/recovery needs its accepted owner process, not ordinary enrollment.

Acceptance uses normal Admin and recipient UI, independent link/access review,
real provider verification/delivery evidence, isolation, stale-account and
source changes, provider outage, duplicates, lost responses and persistence.
No A01/A02 gate is complete from this audit.
