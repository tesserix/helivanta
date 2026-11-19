# Recovering a Helivanta account

Status: candidate implementation for [#1703](https://github.com/tesserix/helivanta/issues/1703).
The returning-account screen is not yet deployed or accepted. Staff MFA reset
and the actual native reset/new-password sign-in journey remain release gates.

## Password recovery

1. Start sign-in and choose your organization. Separate hospital and independent
   pharmacy accounts remain separate, even when they share an email address.
   From the initial email-and-password screen, **Forgot password?** opens the
   email account finder first. Verify the mailbox code and choose the relevant
   organization, then use **Forgot password?** on that account's password screen.
2. On the password screen, choose **Forgot password?** Enter that account's email.
   Helivanta verifies your mailbox before checking whether its selected account
   can recover. A delivery receipt does not guarantee inbox delivery.
3. Enter the eight-digit email code in the same browser. It expires ten minutes
   after reservation and permits five verification attempts. You may request up
   to three codes per hour and ten per day for an email address; source and global
   budgets can also limit delivery. Follow any server-provided retry delay.
4. Once verified, choose **Choose a new password**. Helivanta first prepares a
   retained attempt; only an acknowledged preparation opens the password form.
   Enter and confirm the new password. The organization's native password policy
   applies. The application never saves the password in browser storage or its
   query/mutation cache.
5. A confirmed completion offers **Sign in with your new password**. Ordinary
   password and MFA authentication still apply. Recovery itself creates no session
   or tenant, hospital, Staff or pharmacy access.

After mailbox verification, the screen stops offering code resends. Reopening or
checking an existing recovery reads its retained outcome without sending email
or changing the password again. Existing signup/Staff invitation links have
separate purposes and expiry rules; they cannot authorize this recovery.

## Interrupted requests

- **Delivery could not be confirmed:** retry that delivery request. Its retained
  delivery ID prevents a second send under the same request. An explicit new-code
  request uses a new ID within the delivery budget.
- **Verification response was lost:** choose **Check recovery status**. The code
  may already have been consumed; status can restore verified progress.
- **Prepared attempt restored after reload:** choose **Cancel prepared attempt**
  before entering another password. Cancellation is retained and prevents a late
  earlier submission from changing credentials.
- **Preparation cannot be located:** check status or cancel the exact retained
  intent. Cancellation first reserves a missing intent, then cancels it; a late
  preparation cannot revive it. Expired/foreign proof still refuses cancellation.
- **Processing, applied or unconfirmed:** check status or try ordinary sign-in
  with the new password. Do not submit another password automatically. Cancellation
  cannot release a processing or uncertain native write. Contact your organization
  administrator if it remains unresolved.
- **Native password policy refusal:** prepare a distinct attempt and choose a
  different password. A verified mailbox proof has a five-command attempt budget,
  including cancelled attempts.

Expired preparation without native dispatch may be durably cancelled when a new
verified proof is admitted. Expiry or cleanup never releases an uncertain native
write. Normal successful sign-in and sign-out expire both browser recovery
capabilities for shared-terminal safety.

## Lockout and a lost authenticator

A locked account cannot be unlocked by this password flow. Contact the tenant
administrator; a tenant owner should contact Helivanta support. The unverified
sign-in error uses conditional guidance and does not disclose account existence.
The verified recovery observer may explicitly report a locked account.

Password reset does not remove an authenticator or weaken organization MFA.
The separate audited Staff MFA reset is still under development. Do not treat
this screen as that capability or bypass authentication to complete acceptance.
Owner access loss retains the verified support/JIT path from ADR-0044.

## Release evidence

The identity owner and its canonical Go/Zod models are onboarding-service.
Helivanta brokers the fixed private workload endpoint through Global API and
keeps proof and exact command selection in Secure, HttpOnly, host-only,
SameSite=Strict cookies. Browser bodies cannot select an arbitrary tenant.
Public receipts omit internal challenge and command selectors.

Before release, verify the approved schema, owner-first deployment, credential
cutoff consumer, normal CI, signed Kargo revisions and actual browser/native
reset, old-password denial and new-password/MFA sign-in. Retain minimized audit
and revision evidence. Never put passwords, codes, workload credentials or private
native responses in an issue, log or screenshot. See the
[design and acceptance matrix](../superpowers/specs/2026-11-19-account-recovery.md).

## Hosted native browser gate

The main hosted E2E job explicitly enables
`HELIVANTA_PASSWORD_RECOVERY_FIXTURE=true`. The normal identity fixture remains
read-only without that opt-in. Recovery mode accepts only the fixed development
issuer, loopback fixture database, local web origin and private mailbox beside
the fixture's native manifest. It exposes only recovery commands in addition to
its existing identity reads, and captures only the dedicated native-recovery spec
recipients. Other destinations and mail purposes are refused; no external email
provider is contacted by this fixture.

`password-recovery-native.spec.ts` uses the dedicated account seeded for that
spec in the isolated CI stack. It first proves ordinary old-password sign-in,
then follows the actual sign-in recovery link, consumes its private captured
mailbox proof, prepares and performs the real native reset through Global API,
requires the previous session and old password to be refused, and signs in with
the new password through the normal OIDC exchange. It uses no route interception
or fabricated session. Native policy remains authoritative. Trace, video and
screenshots are disabled for this credential-changing case. The captured test
mailbox proof is deleted after consumption.

The test requires a freshly seeded, disposable development stack and its explicit
fixture mode. It refuses a deployed base URL. It is not production acceptance,
recipient-inbox delivery evidence or Staff MFA-reset acceptance. The separate
`password-recovery.spec.ts` cases remain browser consumer contracts with a stubbed
owner. Record actual hosted results before calling either gate passed.

## Staff authenticator recovery controls — candidate

These controls are implemented but are not deployed or accepted. The
`staff_mfa_recovery` capability remains ineligible. Do not enable it from the
console until the hosted database/native acceptance and rollout gates are met.

1. Sign in as the current tenant administrator and open the Staff member's
   reviewed account links in the hospital or independent-employer Staff screen.
   Reset also requires current establishment-manager access, recent
   authentication, an active Staff record and an ordinary owned account.
   Self, privileged and externally managed credentials use their authorized
   identity-administrator process.
2. Choose **Reset authenticator**. The panel reads the current versioned Staff
   record; it does not assume a version from an old directory list. Enter a reason
   and explicitly confirm the identity check and reset.
3. Submit once. Only an acknowledged `awaiting_enrollment` result means the native
   authenticator removal was confirmed. Ask the Staff member to sign in and enroll
   the replacement authenticator on their own device. The administrator does not
   receive the enrollment secret or an authenticated Staff session.
4. If the response is lost, leave the selected request in place. Reopening the
   same Staff screen under the same account retains only its opaque command marker
   in tab storage. **Check recovery status** reads that exact request.
   **Cancel if still prepared** asks the server to cancel that exact intent; it can
   also block a not-yet-arrived preparation. The browser never has to retain or
   resend the reason after reload. No request is automatically redispatched.
5. An accepted, processing or unconfirmed native write cannot be cancelled.
   Keep its marker, check the same request and escalate to the identity
   administrator if it cannot be reconciled. Do not create a replacement request
   to bypass an unresolved outcome. A confirmed cancellation/completion clears
   only the matching marker. A different tenant/account cannot load it.
6. If the browser cannot retain the marker, no reset is submitted. If recent
   authentication is required, use **Sign in again**, then reopen the same Staff
   screen. Ordinary session renewal does not manufacture that fresh authority.

The panel uses linked names and the existing presentation components; account and
command identifiers are not rendered. A pending historical request is not labelled
with a potentially replaced current account link. Mutation cache variables contain
no reason, password, token or authenticator secret.

Switching off new resets hides the creation controls and denies `PUT` at the
Global API/product gate. Authenticated status and safe cancellation remain baseline
routes so a kill switch does not strand an already selected request. Their
original-actor, tenant, employer/Staff scope and fresh administration checks still
apply. Disabling a feature grants no authorization and cannot undo an accepted
native action.

Current evidence is component/consumer testing and local builds only for these
controls. The earlier successful E2E revision predates them. New PostgreSQL cases,
hospital and independent-employer native/browser reset → enrollment → login,
unsupported-factor refusal, named migration/QA-reset approval, Kargo rollout and
deployed Chrome verification remain mandatory before calling #1703 complete.
