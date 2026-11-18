# Tenant-owned account recovery

Issue: [#1703](https://github.com/tesserix/helivanta/issues/1703).
Status: source review and implementation in progress; not deployed or accepted.

## Required outcome

Hospital and independent-pharmacy accounts must recover through Helivanta, with
Zitadel remaining the credential authority. Password reset is scoped to one
native organization and account. A tenant administrator can initiate an audited
Staff MFA reset with a reason from Workforce. The recipient must enroll a new
factor at their next sign-in. A locked-out user gets actionable recovery
guidance. Acceptance includes resetting a password and signing in with the new
password through the deployed application, plus positive and negative MFA-reset
tests. These are separate from creating an account, enrolling IAM membership,
linking Staff or assigning operating permissions.

## Verified source findings

Reviewed against Helivanta `6c924326a5586e162804db0f631ae1568b4813cd` and its
pinned onboarding API `20d240693ac0` on 2026-11-19.

| Requirement                    | Current implementation                                                                                                                                        | Missing evidence or behavior                                                                           |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Password recovery from sign-in | `apps/shell/app/login/page.tsx` renders credentials, factors, enrollment and password-change-required steps                                                   | No forgot-password action or returning-account reset flow                                              |
| Tenant-bound native login      | `iam/loginclient/tenant_session.go` checks native request client/scopes and resolves the exact organization's subject; returned session ownership is verified | Recovery must preserve these checks rather than search globally by email                               |
| Native password proof          | The identity owner's `internal/zitadel/customer.go` and `customer_code.go` request native reset codes and require the code when setting a password            | Existing signup/Staff recipient capabilities are setup workflows, not a returning-account recovery API |
| Account command recovery       | `iam/staff_accounts.go` uses canonical `staffclient` creation, read, observation and invitation commands                                                      | Creation reconciliation is not lost-password or lost-MFA recovery                                      |
| Workforce account link         | `workforce/login_links.go` owns reviewed Staff/account bindings                                                                                               | A Staff record, email match or arbitrary account ID cannot authorize resetting a factor                |
| Native lockout                 | `loginclient/zitadelerror.go` recognizes password and TOTP lockout error IDs                                                                                  | Public failures currently contain only incorrect-credential wording                                    |
| Enumeration protection         | `LoginUIHandlers.respondEqualisedFailure` returns the same status/body and timing floor; handler tests cover native refusal classes                           | Guidance must remain identical for wrong, unknown, locked and unset-password accounts                  |
| New MFA enrollment             | Existing native TOTP enrollment and `CompleteIfSufficient` enforce fresh factors and native policy                                                            | No Staff reset command or durable reset-required condition has been verified                           |

The issue's hosted-login suggestion predates the current design. Backend
standards and #947 require Helivanta-owned credential UI. ADR-0069's explicit
native-origin security-key continuation is a separate, narrowly verified
exception; it does not authorize a general redirect to hosted password recovery.

### Native credential evidence follow-up

The pinned native source at `29986e1b8b3e606d20871a2e73874b91f9b5b695`
exposes `human.passwordChanged` in its v2 user contract and query projection.
Identity-owner PR [#43](https://github.com/tesserix/onboarding-service/pull/43)
adds a private, opt-in current-status projection of this timestamp, with a
native-method inventory check before declaring an account passwordless. The HMS
consumer is pinned to that exact canonical owner commit, rather than copying
its model or widening the login client's native permissions.

The identity owner is deployed at merge `b5f15c9548afee95b2f286211b13dfd2ca36c244`,
with Kargo Freight `c600d14ad23790feb59e80a3c387ada4eeb94d1d` verified and both
replicas ready. This proves the rollout, not the complete recovery journey.

The HMS work in progress now enforces the native cutoff on protected business
requests, renewal and callback/login exchange. It requires an explicit current
credential reader and denies unknown evidence rather than falling back to the
old state-only observation. A login rechecks membership after native I/O, and
renewal retains the original authentication time. The existing self-sign-out
exception keeps local cookie-authenticated revocation available during a native
identity outage; it does not release business data or mint a session.

OIDC `auth_time`
may have only second precision while native change evidence has fractions: do
not round the native cutoff down and admit an older same-second credential.
The mandatory-password-change flow now rechecks the new password after the
native update, carries forward the rotated session token and, when necessary,
waits for the next native whole-second boundary before one bounded recheck.
This restores the cleared primary proof without weakening the cutoff or
persisting the new password. Cancellation, missing evidence, a missing rotated
token and a second proof that still predates the boundary fail closed. After an
acknowledged update, a completion failure asks for a new sign-in instead of
resubmitting the old current password.

The pinned native session projection clears
`password_checked_at` on `HumanPasswordChanged`, and its user projection retains
the latest native password-change time. Its v2 GetSession handler triggers the
session projection before returning facts. Native `AuthenticationTime()` uses
the latest time of all factor checks, so a later TOTP check alone could otherwise
make an old password session appear newer than a reset. The protected finalizer
now independently requires a valid current native password proof for these
password-session producers; missing, mismatched, duplicate and recased proof
objects cannot reach native finalisation. The approved native-origin security-key
continuation remains a separate flow.

Hosted database/UI integration, native runtime verification, crash ordering and
the full recovery journey remain acceptance gates. This HMS change
is not yet released; a status field or mocked observation alone is not proof of
end-to-end credential revocation.

## Ownership and authority

Zitadel owns passwords, native account lock state, factor registrations and
native sessions. Onboarding owns the native organization/account binding,
private proof delivery and retained identity commands. IAM owns current
membership, authorization, Helivanta credential revocation and final session
admission. Workforce owns employment and independently reviewed Staff/account
links. Shell consumes the Global API; it never receives workload credentials or
administrative native tokens.

The recovery actor is an administration authority in the verified tenant, using
the existing account-administration permission and current membership checks.
The target must be a current, reviewed Staff-linked ordinary account owned by
that same native organization. Hospital and independent-employer Staff scopes
remain distinct. Owner/self recovery cannot use an ordinary Staff reset:
ADR-0044 ORG-D7 reserves lost-owner recovery for verified platform support on
the JIT path with tenant-visible audit. ORG-AC-02/05/06/07 apply; this feature
does not settle other open organization decisions or grant clinical access.

An invisible target is 404, missing administration permission is 403, stale
workspace/account is 409, invalidated credentials are 401, and unavailable
identity/link/authorization/audit dependencies are 503. Browser IDs select an
object only; verified authority and owner-held mappings determine its tenant
and native subject. Recovery never grants membership, employment or product
access.

## Threats and trust boundaries

Protect native proofs, passwords, factor secrets, account existence, private
contacts and administrative authority. Threats include unauthenticated probes,
another tenant's administrator, a stale or revoked session, a modified browser
request and an administrator attempting self/owner recovery. Reauthorize at the
Global API/IAM boundary, reviewed Staff-link boundary and private identity-owner
boundary. Never log bodies, codes, links, passwords or private reasons.

## Delivery design

### Sign-in refusal guidance

Keep the existing equalized 401 code, timing floor and refusal classification.
All credential refusals receive the same conditional locked-account guidance:
contact the tenant administrator; tenant owners contact Helivanta support.
Do not assert that a supplied account exists or is locked. Native error IDs and
the actual lockout outcome remain in safe operational logs. Shell displays the
API message once through its existing inline error surface. This is a complete
wording change, not proof that either recovery workflow has been delivered.

### Password recovery

Add an explicit action in the validated credential form and a recipient recovery
screen. Derive native organization from the validated auth request; use the
identity owner's exact tenant-owned login binding. An unscoped returning user
must complete the existing private account chooser before selecting an account.
Identical delivery responses must not reveal missing, disabled or foreign users.

The identity owner must retain a distinct password-recovery command and delivery
reservation before native/email I/O, with a bounded private recipient capability,
expiry, use budget, replay behavior and resend quotas. Reuse existing native code
and mail primitives, not a consumed signup invitation or a new password store.
Freeze the command and delivery IDs across uncertain responses. Browser links
must keep proofs out of query/access logs and remove them from visible history.
After password completion, consume the recovery capability and invalidate
pre-recovery credentials before ordinary access can resume. A replay reports
completed/expired without modifying credentials or dispatching mail. Completion
returns to ordinary password/MFA login; it does not mint a session by itself.

The precise canonical request/receipt schema and recovery limits belong in the
identity-owner contract change and must be reviewed against its actual ledger,
native policy and mail delivery behavior before implementation. Existing signup
and Staff invitation quotas differ; neither is silently redefined by this spec.

### Staff MFA recovery

Workforce offers a reset action only for a currently reviewed eligible account
link. Require a fresh administrator authentication, explicit recovery reason,
expected tenant/account and exact Staff/link generation. IAM authorizes the
command and records an attempt before external I/O. The identity owner retains
the immutable native command and performs only the explicitly supported factor
reset. An administrative reset may not become a way to remove a user's primary
security key, adopt a foreign identity or weaken an organization policy.

Before any old factor can stop protecting the account, durably revoke existing
Helivanta credentials and block incomplete native login attempts. Establish a
durable reset-required condition; deleting TOTP alone is insufficient when native
policy permits password-only login. Recheck that condition at every applicable
completion/admission path, including callback and pre-existing native sessions.
Clear it only after verified fresh native enrollment, never after a UI receipt
or password proof. Keep existing factors required until the reset command is
admitted; refuse self, owner, privileged or stale-link targets.

No database transaction or row lock spans HTTP. A timeout stays unresolved;
reconcile the same retained command and native factor state. An audit failure
prevents admission/disclosure. After an admitted reset, a failure must retain the
recovery-required condition rather than restore a removed secret or falsely
report success. Recheck actor authority after I/O. Record attributable outcomes
and private reason evidence through owner-approved storage; never put the reason
in events or operational logs.

The implementation must first determine the existing owner's suitable command
and audit extension points, native reset permissions and exact enrolled-factor
semantics. This document is not a newly approved wire contract or evidence that
any of those gates are implemented.

## Capacity, failures and rollout

Reuse existing services; no new daemon or standing infrastructure is required.
Planning envelope: existing account-initiation assumptions of 50/150 tenants,
100 accounts per tenant and 10 peak requests/second. Recovery should be uncommon;
quotas and bounded bodies protect delivery and native calls. Target availability
is 99.9%, interactive reads p99 under two seconds, external commands bounded by
the acting-session deadline and a maximum 30 seconds. These are design targets,
not measured results; native/mail delivery is asynchronous from recipient use.

Identity, authorization, reviewed-link or audit outages deny recovery. Mail
timeouts retain the reservation and an unresolved result. A successful native
write followed by a lost response is reconciled, never repeated under a fresh
command. No rollback may restore a removed factor, resurrect revoked sessions or
erase recovery history. Any required append-only migration needs its own
reviewed deployment evidence and explicit live approval. Deploy the identity
owner before its HMS consumer, then validate signed Kargo images and Argo health.

Rejected: hosted login as a general reset path (contradicts current UI authority),
direct client/native management calls (exposes privilege), email as identity
(breaks separate tenant accounts), deleting factors without a next-login gate
(allows password-only access), generic signup-link resends (wrong lifecycle), and
claiming completion from mocks or a passing registry check.

## Acceptance evidence still required

- Unit: strict bounded input, safe generic errors, identical lockout refusals,
  recipient expiry/replay, rate limits and native failure classification.
- Database/owner integration: exact tenant/account/link generation, concurrent
  duplicate commands, stale administrator, revoked credentials, forbidden owner
  or self targets, failed audit, uncertain native/mail outcome and retained retry.
- Browser CI: request recovery, consume recipient proof, set a different password,
  reject the old password, then sign in with the new password and required MFA.
- MFA journey: authorized admin request with reason and audit, old credentials
  refused, password-only completion refused even with a permissive native policy,
  fresh enrollment required, successful new-factor login and replay protection.
- Live: record running revisions, ordinary UI/Global API flows for an authorized
  QA Staff account, tenant-isolation denial, safe audit evidence and recovery
  cleanup. Do not remove the real owner's factors or forge an authentication.
- Local affected builds, tests, lint and generator checks before merge; remove
  generated build artifacts. Required CI, normal merge, signed release/Kargo and
  deployed browser verification precede issue closure.
