# Staff identity provisioning audit

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
