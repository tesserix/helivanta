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

The forced-enrollment UI, Workforce/IAM reset command authority and durable
idempotent revocation remain required. Hosted native/browser acceptance and live
deployment also remain unverified. This is not a released recovery journey.

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
mandatory enrollment UI and the Staff native-browser journey remain unfinished.
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
encountered the intentionally absent container provider. Their hosted PostgreSQL
execution, the complete E2E workflow after its measured budget correction, native
Staff reset/enrollment acceptance and deployment remain required. Do not infer
full #1703 completion from these consumer tests.

Rejected: native deletion alone; temporarily disabling/reactivating a user;
factor-presence or `auth_time` as fresh-enrollment proof; trusting an unsigned
browser `sid`; silently weakening tenant policy; retrying an unknown native write;
and clearing a recovery fence just because a UI says enrollment succeeded.
