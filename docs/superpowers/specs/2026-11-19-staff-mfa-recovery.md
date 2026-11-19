# Staff authenticator recovery

Part of [#1703](https://github.com/tesserix/helivanta/issues/1703) and the
[account-recovery design](2026-11-19-account-recovery.md). This is an implementation
design; it does not establish a released reset API or authorize a live reset.

## Owners and scope

Workforce owns the exact reviewed Staff/account link and its generation, for
either a hospital or an independent employing establishment. IAM owns current
administrator/target authority, application revocation and admission. Onboarding
owns the managed native organization/account binding, durable recovery command
and native credential operations. Zitadel owns factors and login-session proof.
Shell uses the Global API. No owner reads another module's private tables.

The acting administrator needs current tenant administration, account-management
permission, an unexpired acting session and authentication within five minutes.
The target must be an active, reviewed, ordinary Staff-linked member in that
tenant. Reject self, owners, privileged accounts and stale or ambiguous links.
Independent-employer and hospital link scopes are distinct. Native ownership and
the current actor/target/link checks are required again before dispatch and before
receipt disclosure. ORG-AC-02/05/06/07 and ADR-0044's separate owner-recovery path
remain applicable. A Staff title or account ID supplies no reset authority.

The command binds tenant, initiating subject, Staff/employer kind and IDs, exact
reviewed link/version, target subject, reason and UUIDv7 command ID. Freeze these
on first acceptance. Wrong tenant/invisible object is 404; missing permission is
403; stale source/changed intent is 409; invalidated authentication is 401;
unavailable authority/audit/native evidence is 503. No successful reset grants
membership, Staff linkage, employment or operating permission.

## Native evidence verified against source

All links below pin `tesserix/zitadel` at
`29986e1b8b3e606d20871a2e73874b91f9b5b695`.

- [`CreateOIDCSessionFromAuthRequest`](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/internal/command/oidc_session.go)
  retains the underlying native login-session ID in the OIDC session.
- [`accessTokenResponseFromSession` and `createIDToken`](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/internal/api/oidc/token.go)
  put that value in the signed ID token's `sid`. Read it only after normal
  signature, issuer, audience and lifetime verification. An absent legacy `sid`
  is not evidence for recovery admission.
- [`GetSession`](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/internal/api/grpc/session/v2/query.go)
  triggers the native session projection. Its
  [session contract](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/proto/zitadel/session/v2/session.proto)
  includes exact session ID, user ID/organization, sequence, creation/change/expiry
  instants and independent password/TOTP `verifiedAt` instants. The owner's
  workload needs `session.read` on the checked user's organization; network
  reachability or an arbitrary session ID cannot replace that permission.
- [TOTP commands](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/internal/api/grpc/user/v2/totp.go)
  return sequence, change date and resource owner for registration, verification
  and removal. The ordinary public authentication-method list does not expose a
  per-method enrollment sequence. Presence alone cannot prove fresh enrollment.
- [`HumanRemoveTOTP`](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/internal/command/user_human_otp.go)
  is a native write with no command-id deduplication. A later retry could remove
  a newly enrolled factor. An uncertain write must never be automatically retried.
- The [native TOTP event definitions](https://github.com/tesserix/zitadel/blob/29986e1b8b3e606d20871a2e73874b91f9b5b695/internal/repository/user/human_mfa_otp.go)
  distinguish `added`, `verified`, `removed` and ordinary `check.succeeded` events.
  Read the latest lifecycle event for the exact user/organization, bounded to one
  result. A `verified` sequence after the acknowledged removal proves fresh
  enrollment; a later `added` or `removed` invalidates that observation. A normal
  factor check is never enrollment. Missing/stale/foreign/ambiguous event evidence
  is unavailable, not a completed enrollment. Preserve native sequence ordering
  even when two events share the same timestamp.

These source findings do not demonstrate a native-provider vulnerability or a
passing runtime recovery journey. The deployed revision/permissions and native
browser behavior must be verified separately.

## Reset ordering and recovery

1. Workforce resolves and retains the authorized exact link generation. IAM
   checks current administrator and ordinary target authority. Onboarding prepares
   one immutable tenant/account-bound intent, with no factor mutation.
2. IAM commits a durable account recovery fence, credential revocation and audit
   before acknowledging permission to dispatch. Every business request, renewal,
   tenant transition and callback must honor the fence; incomplete native login
   attempts cannot complete around it. A crash here leaves access denied.
3. Reauthorize the frozen sources without holding a database lock across HTTP.
   The owner atomically claims its prepared command for one native dispatch.
   Persist processing before I/O; a lost response retains an unresolved fence.
4. Store native acknowledged sequence/change date/resource owner, never a factor
   secret or code. Only acknowledgement plus authoritative enrollment evidence
   may advance the command. A timer, native factor absence or an old success
   receipt cannot infer that this reset succeeded or release its fence.
5. The next normal password login must freshly authenticate, enroll and verify a
   new factor even if native organization policy permits password-only login.
   Native factor verification must belong to an enrollment after the admitted
   removal. An ordinary TOTP check against an older factor is not enrollment.
6. Before application admission, query the exact signed `sid` and match its user
   and organization to the owner-resolved account. Require independent primary
   and required-factor proof after the reset/enrollment cutoffs. Reject expired,
   missing, malformed, foreign, stale or uncertain session evidence. Recheck
   the durable recovery generation after native I/O so a concurrent reset wins.

`auth_time` is the latest factor time, not necessarily the password check time.
An old password proof plus a later TOTP proof cannot satisfy fresh primary
authentication. Never round native fractional cutoffs down to token seconds.
Persist the admitted recovery generation and native enrollment evidence; future
callbacks cannot regain access through an older session after another recovery.

### Account-wide requirement and application generation

Native subjects, unlike email addresses, identify physical credential owners.
Legacy memberships can reference one native subject in several tenants. The
owner therefore serializes open recoveries and completion's latest-generation
check globally by subject, while command/source authorization remains tenant
bound. Its private status projection supports explicit `includeMFARecovery`
alongside `includeCredentialState`, returning only state and a command UUID as
`generationId`. `none` has no generation; prepared/processing/uncertain commands
are `blocked`; acknowledged removal is `enrollment_required`; retained completion
is `completed`. Cancellation restores the preceding retained requirement.
No originating tenant, employer, actor or reason crosses this projection. Old
callers retain their existing response shape. Missing storage/audit/scope evidence
is unavailable, never `none`. The owner's ADR-0008 records this decision.

HMS persists that exact admitted generation in
its signed application credential, adding a private recovery-generation claim
to the session design's existing three custom claims. This is a deliberate
extension of D2, not evidence already present in `auth_time`. Only the login
admission boundary may set a new generation, after exact signed native-session
proof and current-generation recheck. Renewal, activity and tenant switching
must preserve it. Every protected admission requires `none` with no admitted
generation, or `completed` with the same admitted generation. A pending command
or generation mismatch denies access in every tenant context for that subject.
This comparison does not supply membership, operating roles or fresh native proof.

The signer requires an explicit generation argument in `pkg/session/signer.go`; production consumers are
`iam/login.go`, `iam/renew.go`, `iam/activity.go` and `iam/me.go`. Principal
verification carries the private claim; `iam/identity_guard.go`, login and renewal
now check the explicit owner requirement. Active credential projections must
include valid recovery evidence; missing evidence is unavailable. An existing
token without the claim remains usable only with explicit `none` evidence.
Pending recovery and mismatched generations deny; a matching completed generation
still requires current native password evidence and ordinary revocation/membership
checks. Renewal, activity and tenant switching preserve the signed generation.

HMS login now calls the private owner admission route after current tenant
membership and credential-state reads, using only the normal verified native ID
token's private `sid`, subject and latest owner generation. The canonical request
contains no historical Staff target. The owner verifies independent fresh native
password/TOTP proof and current active verified human ownership. Reset-time
reviewed Staff versions and ordinary-account protection do not prohibit future
authentication after a legitimate transfer or promotion. Admission grants no
employment, department, role or operating authority.

After the owner receipt, HMS re-reads credential state/generation and current
tenant membership before signing. An outage, mismatched receipt, concurrent reset,
password change or removed membership fails closed without a cookie. A genuinely
new admitted generation opens a fresh idle window even if `auth_time` shares the
old cookie's whole second; silent renewals within one generation retain their
existing deadline. Browser-supplied session/generation fields supply no evidence.

All three loginclient completion entries now read current recovery requirements
for the native session's managed organization/account. Pending requirements deny;
missing owner evidence fails closed. Retained recovery forces the existing native
TOTP enrollment/factor UI even when native policy permits password-only login.
A password-change continuation cannot finalize after factor removal. The existing
unsupported-factor refusal still applies; no passkey, security-key or other factor
is removed or silently replaced. This gate cannot itself admit a recovery
generation: final owner-native lifecycle/session proof remains mandatory.

Workforce/IAM reset command authority and durable idempotent revocation remain
required. Hosted native/browser acceptance and live deployment remain unverified.
This is not a released recovery journey.

Supported reset operations must be explicit in the canonical command/UI. Do not
delete passkeys, primary security keys, SMS/email factors or recovery codes as a
side effect of an authenticator reset. Unsupported combinations require an
explicit refused/support outcome, never silent password-only admission. Full
acceptance must state which factor journeys have been implemented and tested.

Use the existing services and PostgreSQL; no standing worker is needed for the
single non-retryable dispatch. Read-only reconciliation can repeat. A wait for
recipient enrollment is an admission condition, not a workflow holding secrets.
Immutable processing/unconfirmed fences never expire into another native write.
Only a not-yet-dispatched intent may be cancelled through a guarded transition.
Any operator resolution needs attributable evidence and separate authorization;
there is no restore-old-factor compensation.

Reuse the parent design's 10 peak recovery requests/second planning envelope,
50/150 tenants, 99.9% availability and two-second read target. Responses are
bounded to the existing 64 KiB native evidence budget; private native reads have
a five-second ceiling and administrative commands have a 30-second ceiling
bounded by the acting session. Identity/link/audit failures deny. No new standing
infrastructure cost is introduced; these are assumptions, not measured results.

## Required implementation and evidence

- Preserve signed `sid` as private verified-provider evidence. Do not serialize it
  to a principal response or infer a tenant from it.
- Add bounded owner-native session evidence, canonical recovery models/ledger,
  reviewed Workforce source and IAM revocation/admission integration. Generate
  Go/Zod models from the identity owner's canonical source.
- Cover stale actor/link, self/owner/privileged/foreign targets, concurrent commands,
  audit failure, unknown native writes, old/native in-progress sessions, permissive
  MFA policy, enrollment failure, replay and callback races. Tests must demonstrate
  actual gate behavior, not merely inspect a route or stored status.
- Exercise authorized hospital and independent-employer Workforce reset through
  Global API, normal new enrollment and login in hosted native-browser CI. Verify
  old application sessions and password-only completion remain denied.
- Build/test/lint affected targets locally without starting apps or containers;
  hosted CI runs database/native/browser journeys. Remove generated build output.
- Obtain specific live migration/QA reset authorization after preparing reviewable
  code and evidence, then normal CI/merge, owner-before-consumer Kargo release
  and deployed Chrome acceptance. Keep #1703 open until all its criteria pass.

The current drafts preserve private signed `sid`, canonical reviewed-link intents,
the owner command ledger and internal native adapters for exact session proof,
one removal dispatch and latest enrollment evidence. The owner implements private
Helivanta workload prepare, dispatch, cancel and exact-initiator snapshot APIs,
with native ordinary-target checks and durable acknowledgement/uncertainty.
Its canonical `staffclient` methods validate exact command/tenant/actor scope,
bound and strictly decode responses, refuse redirects/cookies and never
automatically retry dispatch. Go and generated TypeScript dependencies pin the
same owner revision, `0608f1837d060656595e47d6f27480ee229d8887`.
These private APIs trust the HMS workload assertion of committed revocation;
the client supplies neither current administrator authority nor that revocation.
The additive schema is in
[infrastructure #1713](https://github.com/tesserix/tesserix-k8s/pull/1713).
Hosted owner PostgreSQL race/coverage checks passed for the ledger, including
concurrency, immutable replay, cancellation tombstones, audit rollback, exact
timestamp preservation, generation fencing and application-role restrictions.

Workforce/IAM authorization, HMS reset command orchestration and durable idempotent revocation,
administrative reset UI and the Staff native-browser journey remain unfinished.
No Staff reset endpoint is registered, no live schema has been applied and no live
factor has been removed. See Helivanta PR #1749, onboarding-service PR #45 and
the owner's `docs/staff-mfa-recovery.md` for lifecycle and validation details.

## Native admission validation — 2026-11-19

Owner commit `0608f1837d060656595e47d6f27480ee229d8887` passed hosted
[run 44431703893](https://github.com/tesserix/onboarding-service/actions/runs/44431703893),
including the real PostgreSQL race/coverage suite, strict canonical contracts,
container checks and secret scanning. It remains an unmerged candidate.

Local HMS evidence for this consumer change: the new login/native-admission and
same-second idle regressions failed before implementation, then passed with race
detection; the existing database-free login/credential regressions and private
owner transport tests passed. Authn, session and loginclient race suites, scoped
vet/lint, `go build ./...`, contractdocs drift check, 1,474 API-package tests,
API-package typecheck/generator checks and the shell production build passed.
The shell build used Node 24 and `API_URL=http://127.0.0.1:8081`; no server ran.
Generated shell build output was removed afterward.

Local database-dependent tests were not validated: full-package invocations
encountered the intentionally absent container provider. At `38daae06`, the complete E2E job in hosted run `44433274075` passed in
24m10s and web checks passed in 27m26s; its Go job is still pending. These results
do not execute the newer administrative reset integration cases. Hosted
PostgreSQL validation of those cases, native Staff reset/enrollment acceptance
and deployment remain required. Do not infer
full #1703 completion from these consumer tests.

Rejected: native deletion alone; temporarily disabling/reactivating a user;
factor-presence or `auth_time` as fresh-enrollment proof; trusting an unsigned
browser `sid`; silently weakening tenant policy; retrying an unknown native write;
and clearing a recovery fence just because a UI says enrollment succeeded.

## Administrator orchestration candidate

The Workforce command paths and authority/recovery semantics are specified in
[the Workforce API](../../api/workforce.md#staff-authenticator-recovery-candidate-1703).
The source now resolves the exact active reviewed link, Staff version and employer
inside Workforce transactions. IAM supplies recent current administrator and
ordinary-target checks and immutable, command-idempotent application revocation
before dispatch. No external I/O occurs inside those source/fence transactions.
Safe prepared cancellation deliberately checks the initiating administrator and
original scope without requiring an unchanged historical Staff version or ordinary
target; it cannot cancel an accepted/uncertain native write or restore old access.

These routes are behind the new ineligible `staff_mfa_recovery` capability. The
administrative UI and actual native hospital/independent recovery journeys are
still incomplete. The new real-database integration cases are authored and
compiled locally but have not yet executed in hosted CI. Earlier native admission
E2E success at `38daae06` does not validate these later reset changes.

## Interrupted administrator requests

IAM now retains the canonical intent before remote preparation, and a separate
immutable cancellation marker serializes against the native-dispatch fence.
Cancellation can tombstone a not-yet-arrived request or submit the exact retained
request after an unconfirmed preparation response. This closes the reload gap
without putting the reason or private native evidence into browser storage. The
private identity owner remains authoritative for accepted/uncertain native writes;
local delivery evidence never fabricates a prepared or completed native outcome.

Pure canonical storage tests reproduced and then rejected oversized payloads and
row/payload scope mismatch, while accepting JSONB field reordering. Authored hosted
cases additionally cover cancellation before preparation and cancellation after a
lost preparation response. They still require actual PostgreSQL execution.

The typed API consumer uses the existing verified account/tenant transition
transport and owner's canonical input/receipt contracts for all employer families.
Its 13 focused tests pass; the complete API package passes 1,487 tests. The
administrative React controls and native browser journeys are not implemented by
these client functions.

Adding the MFA flag exposed a copied 22-key enum/count in the release wire model.
The canonical manifest now explicitly derives its wire enum and snapshot bounds
from its capability catalogue through `enum_from`/`count_from` directives. Generator
tests reject copied or unknown bindings; generated Go/TypeScript remain aligned.
The feature is still ineligible and disabled. No migration or rollout occurred.

## React controls and first hosted corrections

The shared `StaffMFARecoveryPanel` is now connected to hospital reviewed-account
links and independent-employer account links. It is owner-permission gated, reads
current versioned Staff metadata, requires a reason and explicit confirmation, and
never displays a database account/command identifier or administrator enrollment
secret. Only the command marker is retained in tab storage under the verified
account/tenant/employer/Staff selection; mutation variables contain no reason.
Unknown outcomes cannot silently create a replacement command or replay a write.
Status/cancellation survive disabling new resets, and terminal acknowledgement
remains visible until dismissed. Unit coverage includes remount, storage failure,
non-administrator refusal and different-account isolation. These controls still
need actual native browser acceptance.

Hosted run `44444693265` failed before the new real-database cases could run:
bootstrap correctly rejected three forced-RLS policies that did not use the
required `hms_tenant_visible` helper, and source inventory pins needed review.
The policies now combine that helper with exact-tenant equality; the rechecked
source ledger does not claim runtime acceptance. Revocation uses the existing
`admin_revoke` database category, with the MFA-specific audit and retained original
reason. These corrections await a fresh hosted run; no conformance gate was
skipped or relaxed.

Hosted follow-up `44450567737` confirmed the exact RLS write-policy distinction:
`USING` calls `hms_tenant_visible` and retains exact-tenant equality, while
`WITH CHECK` uses exact-tenant equality without that helper. The three write
policies are corrected to the existing `tenantdb.LintRLS` contract; its checks
remain unchanged. This failure happened during schema bootstrap before any new
native/browser or Staff recovery database acceptance could run.

## Selector isolation and guarded native fixture preparation

Hosted revision `dce1f82e` passed schema RLS lint but failed the existing Staff
selection support gate. The three new private IAM MFA tables now carry the same
restrictive selector-purpose/delete policies required of other private IAM tables.
A nonempty selector scope cannot disclose, update or delete recovery commands,
cancellations or fences. The schema-owner migration refreshes the selector
integrity fingerprint; the application role cannot perform that refresh. The
integration regression checks retained rows created through the normal recovery
and cancellation commands, rather than constructing invalid command records.
These SQL corrections still require hosted PostgreSQL execution.

Native acceptance preparation has a separate
`HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE=true` opt-in, guarded by development mode,
the existing identity fixture and the exact local native issuer. The seeder
creates a dedicated real native machine with JWT access tokens, retains a bounded
private regular credential file and refuses to rotate missing credentials on
retry. Owner startup verifies that machine's native type, ownership and active
state, authenticates actual workload tokens through native JWKS and wires the real
MFA recovery adapter. Other test identities cannot stand in for that machine.

HMS keeps the identity client's production HTTPS requirement. In this isolated
mode only, the exact synthetic HTTPS proof origin and the SDK's fixed internal
owner origin are redirected to the loopback owner fixture. All other origins,
userinfo, fragments and alternate encodings are rejected; OAuth token acquisition
and user/session proof continue against the real local native issuer. CI setup
exports public metadata and the credential path, never its value. The owner
fixture remains read-only without its explicit MFA opt-in. These focused fixture
unit tests do not prove the native administrator-to-Staff recovery journey, and
`staff_mfa_recovery` remains ineligible and disabled in released environments.

## First native hospital browser journey

`e2e/tests/staff-mfa-recovery-native.spec.ts` now drives real hospital owner and
ordinary Staff sign-in, initial native TOTP enrollment, API-created active Staff,
independent reviewed account linking, a stale-link refusal, and the Workforce
reason/confirmation UI. It checks acknowledged native factor removal, denial of
the old session and renewal, forced replacement enrollment, accepted fresh access
and renewal, and the initiating administrator's completed receipt. No identity,
Global API, owner command or native factor/session API is mocked. Only disposable
local native identities are allowed; trace, video and screenshots are disabled.
This is authored acceptance code, not yet successful hosted execution.

The separate reviewer is seeded only with the MFA fixture opt-in. Both Go and
TypeScript consumers now pin owner revision `c25ceddd225018b4a2403153e012f41097089c00`,
whose contract, Go quality, secret and container checks passed in hosted run
`44454586010`. The CI job explicitly configures the real workload credential path
before starting the guarded owner and HMS services. Local validation passed:
1,492 API tests, 85 focused login/recovery component tests, E2E typecheck/lint,
scoped Go race/vet/lint, the Go build and the shell production build. Generated
shell build output was removed. Independent-employer native acceptance, hosted
SQL/browser results, normal merge and deployed verification remain outstanding.

## Independent native pharmacy journey and fixture protocol correction

The authored hospital journey now shares its credential/factor assertions with
`e2e/tests/staff-mfa-pharmacy-native.spec.ts`. That separate spec owns distinct
native Tenant B administrator, reviewer and ordinary Staff accounts. Its pharmacy
and employing establishment are created through the published PharmaConnect and
Workforce registration APIs. Staff and independently reviewed account links also
use actual API commands. A separate native Tenant A administrator is refused with
404 before the original factor is removed; the hospital route also refuses the
pharmacy establishment. Tenant B then drives the pharmacy Staff UI reset and the
same old-access, replacement-enrollment and fresh-renewal assertions. Neither
journey uses a fabricated native session or source projection. The seed verifies
each real human account's active native owner before writing its IAM membership.

Hosted run `44455733168` exposed two preparation failures: scripts lint required
retaining the wrapped private-file error's cause, and native seeding returned
405 for a `POST` machine-secret command. The pinned upstream
[Zitadel v4.15.3 management protobuf](https://github.com/zitadel/zitadel/blob/v4.15.3/proto/zitadel/management.proto)
defines `GenerateMachineSecret` as `PUT /users/{user_id}/secret`. The fixture now
uses that method; its protocol regression failed with `POST` and passes with
`PUT`. Failure diagnostics expose method/status only, never a provider payload or
credential. Native human fixture proofs reject foreign owners, machines, inactive
or mismatched subjects, production issuers and malformed provider payloads.
These corrections still require a fresh hosted run. No browser success, merge,
live migration or deployment is claimed by adding these test cases.

The same hosted Go run passed the earlier Staff selector gates, then rejected the
three recovery status routes in `TestEveryCollectionGETIsPaginated`. That gate
requires every non-paginated GET, including scalar reads, to be explicitly
classified. The three exact routes now join the existing scalar allowlist with
reasons identifying their single scoped receipt. No collection registration or
pagination requirement changed. The existing regression was reproduced locally
before the classification and passed with race instrumentation afterward. Full
PostgreSQL/OpenFGA architecture execution remains hosted to respect the local
service restriction.

## Native machine subject and OAuth identity

Hosted revision `299a808c` progressed past the machine-secret method but failed
because the fixture compared returned `clientId` with the machine user ID. Pinned
Zitadel v4.15.3 `management/user.go` returns `PreferredLoginName`; its OIDC
`client_credentials.go` resolves that login name and separately binds the token
to the native user ID. The fixture now verifies the active owned machine through
its current native record, preserves both public identifiers in the manifest,
and accepts a generated secret only for that exact preferred login name.
Missing retained secrets still fail without rotation. Protocol tests reproduce
the distinct-name failure and reject a substituted client's secret.

The operator token constructor now accepts bounded native OAuth login names as
well as existing machine-name client IDs. Tenant/project/subject validators do
not change. HTTPS issuer validation remains mandatory. In the isolated fixture,
only the exact synthetic HTTPS token origin's POST `/oauth/v2/token` redirects to
the real pinned local native issuer. No other native API, method, query or
production destination is accepted. The real native-issued JWT and its real
issuer/subject are independently checked by the owner; no token is invented or
rewritten. Startup tests cover this combined guarded configuration.

The same hosted Go run exposed the existing migration-count assertion still
expecting 52 phases. It now expects 53 and checks the new phase's exact ID and
nonempty SQL, preserving prior phase/hash assertions. The focused regression
passes locally with race instrumentation.

The [rollout and acceptance runbook](../../deployment/2026-11-19-staff-recovery-rollout.md)
records read-only live discovery and the required approval/order of schema,
owner, Kargo and QA actions. It is not rollout authorization or proof of native
browser acceptance. No live action occurred.

The owner manifest compatibility change passed all checks in hosted run
`44459441660` at `d075ee7c80800dfaddde5216b29ab8516f52bddb`; that candidate pinned both consumers to that revision. Local validation on those pins passed 1,492 API tests, 200
script package tests, 17 explicit fixture tests, credential/startup and migration
phase race tests, scoped Go vet/lint, E2E typecheck, all canonical checks, the Go
build and the shell production build. Generated shell output was removed. The
previous HMS web/native Staff and complete supplier checks passed; its MFA E2E
seed and migration-count failures are corrected in this candidate and still need
a fresh hosted run. No deployment or native MFA browser success is claimed.

## Browser registration and actual locked-account acceptance

Hosted `fff427f3` passed migration, native seeding and service startup, then
Playwright rejected the two MFA wrapper callbacks before executing any test. The
runner requires destructured fixture arguments; both wrappers now declare
`{ page, browser }` explicitly. The same failure was reproduced with local
`playwright test --list`, which starts no app or browser. After the correction,
registration discovers 109 tests across 40 files, including both MFA journeys.
CI now runs discovery immediately after dependency installation and before
service/browser setup. A type-check alone does not prove runner registration.

`account-lockout-native.spec.ts` adds a separate disposable ordinary user and
proves the native account is active and owned by the expected organization/email
before invoking the pinned native v2 `LockUser` API. It observes the actual locked
state, checks that previously valid application access is refused, and submits
the real password through the actual login UI. Wrong-password, locked and absent
accounts must receive identical actionable administrator/support guidance; no
account-existence distinction is introduced. This exercises an actual native
locked state, not the configured ten-failure threshold. The latter is a separate
provider-policy setting and is not claimed by this case. No production identity
or factor is changed. The case is authored, type-checked, linted and registered;
its hosted runtime result is still required.

Both MFA journeys now verify the native target email as well as organization and
subject before factor setup. They also assert the retained old cookie is still
accepted immediately before the reset, so a later refusal cannot be explained by
an already invalid session. Screenshots, tracing and video remain disabled.

The parent PR's GitGuardian dashboard requires a separate sign-in. Its finding
has not been dismissed, its check remains required, and no GitGuardian access or
security exception has been granted during this review.

## Canonical native recovery fixtures

Hosted `dc647ef8` passed the native password-reset journey: old access was revoked,
the old password was refused, and the new password signed in. Its Staff MFA and
lockout cases stopped before exercise at the private bootstrap credential guard.
The explicit fixture seed now retains a separate exclusive `0600` setup copy;
browser file guards remain strict and the original native-generated PAT is unchanged.

The same revision's Go integration run reached the canonical workload client and
exposed invalid legacy sample tenant UUID variants. A transport-level regression
reproduces rejection without a local database, then passes using canonical scopes.
The isolated native seed, owner fixture and browser cases now agree on the two
UUIDv7/RFC 4122 tenant IDs in `scripts/lib/native-staff-fixture-tenants.json`.
Each is a distinct real native organization, provisioned only under the Staff MFA
fixture opt-in; baseline fixture tenants are preserved. The owner fixture admits
MFA commands only in these canonical scopes. Production UUID checks are unchanged.
Independent Staff fixture calls now carry the required expected account/workspace
headers rather than bypassing the server's session-change control.

These corrections still need fresh hosted Go and native-browser acceptance. The
native password result does not establish MFA reset, re-enrollment or lockout
acceptance. Owner prerequisite PR #44, recovery PR #45, schema reconciliation,
normal HMS merges, approved Kargo rollout and deployed verification remain open.

The canonical fixture owner correction passed all checks in run `44486627321`
at `55ad8854837280fdd87a72cd636f8bfb57eb611a`. Both current HMS consumers pin
that revision; their fresh local and hosted acceptance still governs delivery.

Local validation on these pins passed 1,492 API tests, 203 script tests, six
related CI fixture tests, canonical generation/check, E2E typecheck/lint/discovery,
focused Go race/vet/lint/build checks and the shell production build with CI's
Global API origin. Generated shell output was removed. Full hosted native and
Go acceptance still remains required for the pushed candidate.
