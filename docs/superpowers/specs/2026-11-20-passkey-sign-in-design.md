# Passkeys: one-tap sign-in that counts as MFA

**Issue:** [#422](https://github.com/tesserix/helivanta/issues/422), web console slice. Part of the
journey review [#1767](https://github.com/tesserix/helivanta/issues/1767).
**Decision record:** [ADR-0074](../../adr/0074-helivanta-origin-passkeys.md) (proposed with this spec).
**Refines:** [ADR-0069](../../adr/0069-explicit-native-security-key-continuation.md),
[ADR-0070](../../adr/0070-tenant-owned-login-identities.md),
[single-screen sign-in](2026-11-19-single-screen-sign-in-design.md),
[native TOTP enrolment](2026-10-08-native-totp-enrolment-design.md).
**Related:** [#1751](https://github.com/tesserix/helivanta/issues/1751) (single sign-in card this builds on),
[#1778](https://github.com/tesserix/helivanta/issues/1778) (no second password entry after setup),
[#1703](https://github.com/tesserix/helivanta/issues/1703) (account recovery).
**Status:** proposed. No plan is written until the spike below records observed Zitadel responses.

## Problem

The product owner asked on 2026-11-20 that a user who already has MFA should not need a password. A returning
user today types an email and password, then a six-digit authenticator code: two secrets on two screens.

Dropping the password and keeping only the six-digit code would leave one phishable factor. A passkey
(WebAuthn with user verification) does what was asked without that loss: one fingerprint, face or PIN prompt,
bound to the site's origin, proving possession and the person in one step. Zitadel does not enforce `forceMfa`
for a login client ([spike 2026-08-16 §2](../spikes/2026-08-16-zitadel-login-client.md)), so Helivanta's
sufficiency check is what decides a passkey counts.

What the code does today, verified against `main` on 2026-11-20:

- `classifyEnrolledMethods` (`backend/internal/modules/iam/loginclient/sufficiency.go`) treats every method
  except password, IdP and TOTP as uncollectible. A user who enrols a passkey anywhere is refused at Helivanta
  sign-in with `RefusalFactorUnsupported`.
- ADR-0069 sends a password-accepted user whose only extra method is U2F to Zitadel's hosted login. Those
  credentials are bound to Zitadel's domain and cannot be used from `helivanta.app`.
- Staff MFA recovery in onboarding-service removes TOTP only (`apps/api/internal/zitadel/recovery_totp.go`).
  It has no passkey removal.
- Zitadel Login V2 (`tesserix/zitadel`, `apps/login/src/lib/server/passkeys.ts`,
  `apps/login/src/components/login-passkey.tsx`) registers passkeys through the User API v2 and authenticates
  them through the Session API v2 with an explicit `domain`. It always needs the user known before the WebAuthn
  challenge.
- Tenant addresses are paths on `helivanta.app` (ADR-0011), so one relying-party ID serves every workspace.

## Decisions

### D1: The relying party is `helivanta.app`; Zitadel keeps the credentials

Registration: the backend asks Zitadel for a registration code (`createPasskeyRegistrationLink`, `returnCode`)
and calls `registerPasskey` with `domain: "helivanta.app"`. The browser runs `navigator.credentials.create`, and
the backend calls `verifyPasskeyRegistration`.

Sign-in: the backend creates a Zitadel session with a user check and
`challenges.webAuthN { domain: "helivanta.app", userVerificationRequirement: REQUIRED }`. The browser runs
`navigator.credentials.get`, and the backend updates the session with `checks.webAuthN.credentialAssertionData`.

The backend makes every Zitadel call with the existing login client. The browser only runs the ceremony.
Helivanta stores no credential material. The session id and token stay server-side in `login_attempt`, as the
factor step does today.

### D2: A user-verified passkey satisfies MFA on its own

A session carrying a user check plus a WebAuthn check with `userVerified: true` satisfies `forceMfa` with no
password. The sign-in card completes through `CompleteIfSufficient` and the password path through
`CompleteAfterFactor`. Both already construct the `sufficient` witness that
`TestSufficientWitnessConstructionIsPinned` restricts to three functions, so no new constructor is added and
the pin is unchanged.

An assertion without user verification never completes alone; it fails closed with a new refusal reason.
`PASSKEY` becomes collectible in `classifyEnrolledMethods`. U2F keeps its ADR-0069 treatment, and every other
uncollectible method is still refused.

### D3: Passkey as the second factor on the password path

A user who signs in with a password and has a passkey enrolled is offered **Use your passkey** at the factor
step, alongside the authenticator code when TOTP is also enrolled. This replaces today's refusal (Problem,
first bullet). It goes through the same `CompleteAfterFactor` path, with the WebAuthn check as the verified
factor.

### D4: Sign-in card

The #1751 card keeps **Email** and **Password** and gains **Sign in with a passkey**. The button is shown only
when `PublicKeyCredential` exists in the browser. The prompt state uses `AuthPasskeyPrompt` from
`@tesserix/web` (already a shell dependency), not a new component.

1. `POST /v1/auth/login/passkey/start { auth_request_id, login_name }` resolves candidates through the
   onboarding `login-targets` lookup (#1751 D2). The remembered workspace (`apps/shell/lib/workspace-signin.ts`)
   or a `/login?workspace=` link narrows the result to one tenant when present.
2. **Exactly one account, with a passkey:** create the Zitadel session and challenge, park it in `login_attempt`
   at a new stage `passkey`, and return the `publicKey` request options.
3. **Unknown email, no passkey, a native refusal, or more than one account with no workspace known:** return
   decoy options. They have the same shape, a fresh random challenge, random `allowCredentials` ids, the same
   timeout and the same `MinFailedLoginDuration` floor. The browser then finds no matching passkey. The card
   shows one line for every case: "No passkey for this account here. Use your password."
4. `POST /v1/auth/login/passkey/finish { auth_request_id, credential }` loads the parked attempt, updates the
   session with the assertion and calls `CompleteIfSufficient`. A decoy has no parked attempt and fails at the
   floor with the password-failure message.

The account chooser is never shown before a credential is proven. ADR-0070 gives one person one account per
tenant, and showing their tenants for a typed email would disclose them. A person with several accounts uses
the remembered workspace, the workspace link, or the password path, where #1751 shows the chooser after proof.

### D5: Where a passkey is created

- **First sign-in enrolment.** Under `forceMfa` with nothing enrolled, the enrolment step offers
  **Create a passkey** first and **Use an authenticator app** second. The TOTP secret is still registered in
  the password step (an unverified registration left behind is invisible, per the native TOTP enrolment spec).
  The passkey path is one `create` prompt and then one `get` prompt on the parked session. Registration alone
  adds no check to the session, so the second prompt is what proves the factor. Two taps replace a QR scan and
  a typed code.
- **Account security.** The signed-in person lists their passkeys (name, created, last used), adds one and
  removes one. Add and remove require a sign-in within the last 10 minutes; otherwise the page asks the person
  to sign in again. Removing the last MFA method is refused while `forceMfa` applies.
- **Not at signup or invitation.** Offering a passkey on the #1778 set-password card would create accounts
  with no password. That waits until recovery for such accounts is proven (D7).

### D6: The name the operating system shows

ADR-0070 usernames are `owner-<tenant UUID>`, which is unusable in a passkey chooser. The backend rewrites the
creation options' `user.name` to the email and `user.displayName` to the workspace name before returning them.
It leaves `user.id` untouched. This matches the authenticator label from #1765 (`Helivanta: <email>`).

### D7: Recovery covers passkeys before enrolment ships

The staff MFA reset and owner recovery from #1703 / onboarding-service #45 gain passkey removal, through
Zitadel's passkey removal API for the subject. The reset is complete only when the user's enrolled-method read
shows no TOTP and no passkey. Enrolment (D5) ships in the same slice as this, or after it, never before.

### D8: Limits and audit

`passkey/start` uses the per-IP login limiter and the per-email bucket from #1751, and decoys count the same as
real starts. Audit events record identifiers only: `login.passkey.started`, `login.passkey.completed`,
`login.passkey.refused`, `account.passkey.registered` and `account.passkey.removed`. A credential id is logged
only as a hash. Nothing from the assertion or attestation reaches a log line.

## Capacity, cost and failure

- A passkey sign-in makes three Zitadel calls (create session with challenge, update with assertion, finalize),
  the same order as password plus TOTP. The 10-second native timeouts apply.
- No new table. `login_attempt` gains the stage value `passkey`, plus any matching check in the iam schema
  (`backend/internal/modules/iam/module.go`). Rows keep the existing TTL and sweeper.
- No email or SMS is sent per sign-in.
- Zitadel unavailable: `start` and `finish` return the retryable 503 the password step returns. The card keeps
  the password path visible.
- Browser without WebAuthn: the button is hidden. A person cancelling the prompt returns to the idle card with
  no error toast.
- Moving the app off `helivanta.app` orphans every passkey. ADR-0074 records this as the one-way door.

## Contracts changed

- `POST /v1/auth/login/passkey/start` and `POST /v1/auth/login/passkey/finish` (unauthenticated, rate-limited).
- The factor step's `factor_required.factors` may now contain `passkey`.
- The enrolment response offers `passkey` alongside the existing `totp` secret.
- `GET` / `POST` / `DELETE /v1/account/passkeys` and `POST /v1/account/passkeys/verify` (authenticated, own
  account only, recent sign-in for `POST` and `DELETE`).
- Release availability: classified in `contracts/release-capabilities.json`, disabled by default until QA
  acceptance on a labelled QA tenant.

## Verification

- Go: a passkey-only session with `userVerified: true` completes under `forceMfa`; `userVerified: false` is
  refused; `{TOTP, PASSKEY}` is no longer refused; `{U2F}` keeps the ADR-0069 action. Each is mutation-checked.
- Go: decoy and real `start` responses have identical key sets and both respect the floor. `finish` with no
  parked attempt fails at the floor. A parked attempt is single-use.
- Go: account passkey endpoints for another tenant's subject answer 404. Add and remove without a recent sign-in
  are refused.
- Go: the D6 name rewrite leaves `user.id` byte-identical.
- Vitest: the button is hidden without `PublicKeyCredential`; a cancelled prompt leaves no error; the
  no-passkey line is identical for every case.
- Playwright with a virtual authenticator (CDP `WebAuthn.addVirtualAuthenticator`): enrol at first sign-in,
  sign out, then sign in with email and one passkey tap to the dashboard, with navigations counted. A staff MFA
  reset removes the passkey and the next sign-in asks for enrolment.

## Departures from #422 as written

- **Identity provider.** #422 names Keycloak. Helivanta runs Zitadel, so its User and Session APIs replace
  Keycloak's WebAuthn authenticators.
- **No `passkey_credential_meta` table.** Zitadel holds the credentials and their names. P6 decides whether it
  also returns created and last-used dates. If not, the plan adds an iam-owned metadata table through the
  repo's migration rules, with the forced-RLS boilerplate.
- **Discoverable credentials at shared ward workstations** stay open on #422. Zitadel's own login always
  identifies the user before the challenge. P7 checks whether the Session API allows otherwise.
- **Per-role and per-tenant passkey policy, attestation allowlists and the mobile apps** stay on #422.

## Spike before the plan (observe live on a labelled QA organisation)

#422 asks for a spike before design because earlier Zitadel spikes were contradicted by live behaviour. The
decisions above hold only if these are observed, not inferred from documentation. Results go in
`docs/superpowers/spikes/`. The spike follows the 2026-08-17 MFA spike's method, recording every request path
and response status.

- **P1:** Zitadel v4.19.5 accepts `helivanta.app` as the relying-party ID and origin for this instance. If it
  needs an instance trusted domain, that change goes through `tesserix-k8s` and is recorded here.
- **P2:** The Session API exposes `factors.webAuthN.userVerified`, and a passkey-only session finalizes an
  auth request under `forceMfa`.
- **P3:** `verifyPasskeyRegistration` accepts a credential created from options whose `user.name` and
  `user.displayName` were rewritten (D6).
- **P4:** The Zitadel passkey removal endpoint and the enrolled-method read that proves it ran (D7).
- **P5:** Whether a WebAuthn check on a password session counts as the second factor for passkeys registered
  as passwordless (D3), or only for U2F registrations.
- **P6:** What Zitadel's passkey list returns per credential: name, state, created and last-used dates.
- **P7:** Whether `POST /v2/sessions` accepts a WebAuthn challenge with no user check (discoverable sign-in).
- **P8:** Whether the login-client PAT can issue the challenge and register passkeys, or a narrower credential
  is needed (the 2026-08-17 spike §7 left this unobserved).

Any answer that contradicts a decision changes this spec before the plan is written.

## Slices

0. Spike P1 to P8, recorded in `docs/superpowers/spikes/`, and this spec corrected to match.
1. Sufficiency and classification (D2, D3): passkey accepted as the factor on the password path. Closes the
   lockout in the Problem section.
2. Recovery (D7), then account security add, list and remove (D5, D6).
3. Sign-in card start and finish with decoys (D4, D8).
4. Enrolment step offers a passkey first (D5).

## Not in scope

Usernameless and autofill (conditional) passkey sign-in, pending P7. Also out: accounts with no password; the
passkey offer on the set-password card; the rest of #422 listed under Departures; MediConnect patient OTP
(#551); SSO (#423); sharing one credential across a person's tenant accounts (ADR-0070 keeps them separate).
