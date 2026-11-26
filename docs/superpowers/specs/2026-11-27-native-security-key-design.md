# Security keys on Helivanta's own pages

**Issue:** [#1898](https://github.com/tesserix/helivanta/issues/1898)
**Supersedes (once slice 3 ships):** [ADR-0069](../../adr/0069-explicit-native-security-key-continuation.md),
the explicit hosted security-key continuation. Until then ADR-0069 stays in force,
and this document records the replacement.
**Restores:** the 2026-10-07 rule in
[`2026-10-07-no-hosted-login-handoff-design.md`](2026-10-07-no-hosted-login-handoff-design.md):
a Helivanta user never sees a Zitadel page.

## Why

The product owner restated the rule on 2026-11-27: Helivanta builds its own pages,
and uses Zitadel only through its APIs. Password, authenticator code, two-step
enrolment, password change and password reset already work this way. One path
does not. A user whose second step is a security key (`AUTHENTICATION_METHOD_TYPE_U2F`)
is offered `security_key_required: {url}` after the password step, and clicks
through to `auth.tesserix.app/ui/v2/login/password` to finish on Zitadel's page
(`backend/internal/modules/iam/loginui_securitykey.go`,
`loginclient/securitykey.go`, `apps/shell/lib/login-client.ts`).

ADR-0069 accepted that because a key registered on Zitadel's domain cannot be used
from helivanta.app. That is a property of WebAuthn, not of Zitadel. A credential is
bound to its relying-party ID (RP ID), which must be the page's domain or a
parent of it, and `tesserix.app` is not a parent of `helivanta.app`. The fix is to
register keys with helivanta.app as the RP ID, through Zitadel's API, and move the
keys that already exist.

## Goals

- Collect a security key during sign-in on Helivanta's sign-in page.
- Register a security key on Helivanta's pages, both during required two-step
  setup and from a signed-in account.
- Move every existing `auth.tesserix.app` key without sending its owner to a
  Zitadel page.
- Delete the hosted continuation, and fail CI if any login response can carry a
  Zitadel UI URL.

## Non-goals

- **Passkeys as a first factor** (sign-in without a password). That is #422.
  Keys here are a second step after the password, which is what U2F is today.
- **Sign-out.** It passes through Zitadel's `end_session` endpoint, which
  redirects straight back to `/login` and renders nothing. It is unchanged.
- **Email or SMS codes** as factors. They stay refused, as today.

## Anti-goals

- Any page, redirect or link to a Zitadel `/ui/` route, including a temporary one
  "for migration". The product owner ruled this out.
- Helivanta verifying WebAuthn signatures itself. Zitadel owns credentials and
  verifies the assertion, and Helivanta only relays the browser's response (the
  same split as TOTP).
- A silent downgrade. A user with a key whose browser cannot use it is told so.
  They are not quietly let in with a password alone.

## Before slice 1: spike against the deployed Zitadel (S1)

These facts are from Zitadel's v2 API reference. None of them has been proved
against our instance, so the spike must prove each one and record the version and
the result here:

1. `POST /v2/users/{id}/u2f` with `{"domain":"helivanta.app"}` returns
   `u2fId` and `publicKeyCredentialCreationOptions` with `rp.id = helivanta.app`.
   It must also accept the browser's response through
   `POST /v2/users/{id}/u2f/{u2fId}` (`publicKeyCredential`, `tokenName`).
2. Patching a session with `challenges.webAuthN{domain:"helivanta.app",
userVerificationRequirement}` returns `publicKeyCredentialRequestOptions`.
   Then `checks.webAuthN{credentialAssertionData}` sets the session's U2F factor
   so that the existing sufficiency check accepts it.
3. Whether Zitadel refuses a domain that is not its own or a configured trusted
   domain. If it does, adding `helivanta.app` as a trusted domain is a
   tesserix-k8s `zitadel-bootstrap` change and joins slice 1.
4. Whether a key's RP ID can be read back per credential. That decides D4's
   inventory (below).

If (1) or (2) fails, stop and bring the finding back. ADR-0069 stays in force
until it is resolved.

## Decisions

### D1: helivanta.app is the relying party

Every challenge and registration uses `domain: helivanta.app`, from configuration
that the backend validates at boot. It is an HTTPS registrable domain, it has no
path, and it is the shell's own host. A mismatch is a boot failure, not a runtime
refusal. Local development uses the dev shell host the same way.

### D2: sign-in collects the key on the existing factor step

- After the password succeeds and the user's enrolled second factors include U2F,
  the password response returns `factor_required: ["security_key"]` (and
  `"totp"` too when TOTP is enrolled). It also returns
  `security_key: {request_options}`, the options from Zitadel's challenge. That
  gives one response shape, the existing `factorRequiredResponse` plus one field.
- The page calls `navigator.credentials.get` and posts the assertion to the
  existing `POST /v1/auth/login/factor` with `kind: "security_key"`. The server
  relays it as `checks.webAuthN`. Then the existing sufficiency, password-change
  and finalize steps run unchanged.
- **Both enrolled:** the key is the default, with "Use your authenticator app
  instead" as a link. Mixed methods that are refused today (U2F with OTP email or
  SMS) stay refused.
- **Bounds:** a failed or cancelled assertion counts as one factor attempt under
  the existing per-attempt limit. The challenge belongs to the `login_attempt` row
  and is never re-issued for that attempt.
- **Errors:** Zitadel being unreachable is a 503 ("temporarily unavailable"),
  never a refusal. A browser without WebAuthn shows "This browser can't use
  security keys. Use another browser, or your authenticator app if you have one."
- **Copy needs product-owner approval** before slice 2 merges, as with every
  sign-in string.

### D3: registration is native in two places

- **Required setup.** When two-step verification is required and nothing is
  enrolled, `enrollment_required` offers `["totp","security_key"]`. Choosing a key
  starts Zitadel registration with D1's domain, and the page calls
  `navigator.credentials.create` and posts the result. The registration is bound
  to the same login attempt as TOTP enrolment, and a new attempt needs a new
  password check, as with TOTP's secret.
- **Signed-in account.** A "Security keys" section on the account's security page
  lists keys by name and lets the user add or remove one. Both actions need a
  sign-in within the last 5 minutes, otherwise the page asks for the password
  again. Removing the last second factor is refused while the organisation
  requires two-step verification.

### D4: existing keys are moved, not continued

A key on `auth.tesserix.app` can only ever be used on Zitadel's page. So:

1. **Inventory first.** List every Helivanta user who has U2F enrolled, and for
   each say whether the key was registered through Helivanta. If S1(4) can't
   read the RP ID back, Helivanta records the `u2fId` of every key it registers,
   in an IAM table (subject, `u2f_id`, `registered_at`). Any other U2F credential
   is then a legacy key. ADR-0069's evidence names one such owner. The real count
   is taken read-only before slice 3.
2. **Move by authenticator reset.** The legacy key is removed through the
   existing authenticator reset (#1703), which is extended to remove U2F as well
   as TOTP. At the next sign-in the user re-enrols on Helivanta's page (D3),
   because their organisation requires two-step verification.
3. **No hosted transition.** Nobody is sent to Zitadel to prove the old key.
   For the minutes between the reset and re-enrolment the user has only a
   password, and they can't finish signing in until they enrol. That gap is
   accepted for the small, known set of users.

### D5: the hosted continuation is deleted, and a guard keeps it gone

Slice 3 deletes `SecurityKeyLoginURL`, `loginui_securitykey.go`,
`security_key_required` in `login-client.ts` and their tests, and marks ADR-0069
superseded. Two guards fail CI:

- **Backend:** a test parses every non-test Go file under
  `backend/internal/modules/iam` and fails on any string containing `/ui/v2/login`
  or `/ui/login`.
- **Shell:** a lint rule or test fails on the same strings in `apps/shell`.

### D6: no new browser permissions or origins

WebAuthn in a top-level page needs no `Permissions-Policy` entry, because the
default allowlist is `self`, and the browser talks only to Helivanta's API. The
CSP is unchanged. `security-headers.test.ts` gains an assertion that
`publickey-credentials-get` and `-create` are not disabled, so a future header
edit can't break keys silently.

## Slices

1. **Native registration.** S1, then D1 and D3's required-setup path, plus the
   IAM key registry if S1(4) needs it. Proven by registering a key against the
   dev Zitadel and finding it listed with RP ID helivanta.app.
2. **Native sign-in.** D2, with copy approved. Proven end to end: a key
   registered in slice 1 signs in on Helivanta's pages and reaches the callback.
   The unreachable-Zitadel, wrong-key, cancel and no-WebAuthn cases each get a
   test. ADR-0069's continuation still exists for legacy keys.
3. **Move and delete.** D4's inventory and extended reset, the moves done, and
   then D5's deletion and guards. This slice merges only when the inventory reads
   zero legacy keys.
4. **Account security page.** D3's add and remove. It is separate because
   slices 1–3 already give every user a working key.

Each slice is a vertical PR with backend, shell, tests and docs. The plan goes in
`docs/superpowers/plans/` after this design is approved.

## Left to the builder

- The exact request-options pass-through type, as long as it stays one field on
  the existing responses.
- The test harness for WebAuthn in the browser: Playwright's virtual
  authenticator or the CDP WebAuthn domain.
- The IAM table's migration ID, the next free `NNNN_iam`.

## Open for the product owner

- **D4 gap:** is a short password-only gap acceptable for the few moved users,
  between the reset and their re-enrolment? The alternative is asking each of
  them to enrol TOTP first, which needs a working sign-in, so in practice it
  needs the reset anyway.
- **Who resets a tenant owner's key?** #1703's reset protects ordinary targets
  and is run by an administrator. If the only legacy key belongs to an owner with
  no administrator above them, the move needs a named operator path, which is
  decided before slice 3.
