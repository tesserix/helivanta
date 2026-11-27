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

### S1 result (2026-11-27)

The spike ran in CI against the dev Zitadel **v4.19.5**, the same version as
production (`tesserix-k8s/charts/thirdparty/zitadel/Chart.yaml` appVersion). It
used Chromium's virtual authenticator, with the page served at
`http://helivanta.localhost/` (run 45908050250, branch `spike/1898-webauthn`,
never merged). Every call returned what the design needs:

1. **Proved.** `POST /v2/users/{id}/u2f {"domain":"helivanta.localhost"}`
   returned 200 with `u2fId` and `publicKeyCredentialCreationOptions.publicKey`,
   `rp.id = helivanta.localhost`. The browser created the credential, and
   `POST /v2/users/{id}/u2f/{u2fId}` with `publicKeyCredential` and `tokenName`
   returned 200. The key is listed as `AUTH_FACTOR_STATE_READY`, and
   `authMethodTypes` is `[U2F, PASSWORD]`.
2. **Proved, using the login client's own PAT.** `POST /v2/sessions` with the
   user and password checks plus `challenges.webAuthN{domain,
userVerificationRequirement}` returned 201 and
   `publicKeyCredentialRequestOptions`. The browser assertion, sent as
   `PATCH /v2/sessions/{id}` with `checks.webAuthN.credentialAssertionData`,
   returned 200. The session then has `factors.webAuthN.verifiedAt` next to
   `factors.password.verifiedAt`. The existing sufficiency code doesn't read
   `factors.webAuthN` yet: it only refuses password-only U2F sessions. Slice 2
   adds the acceptance and tests it.
3. **Zitadel doesn't check the domain.** It accepted `example.com` and
   `auth.tesserix.localhost` as well. So no trusted-domain change is needed in
   tesserix-k8s. It also means Zitadel won't catch a wrong RP ID, so D1's
   boot-time validated configuration is the only control on the domain, and
   slice 1 tests that a request can't choose the domain.
4. **The RP ID can't be read back.** The auth-factor listing returns only `id`,
   `name` and `state`. So D4's inventory needs the IAM key registry: Helivanta
   records the `u2fId` of every key it registers, and any other U2F key is a
   legacy key.

Also: `rp.name` comes back as `ZITADEL`, the instance name. The v2 request has
no field to set it. Browsers show `rp.id` in their prompt, and some operating
system prompts show the name. Whether to rename the instance (a shared
tesserix-k8s setting) is decided in slice 1.

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
  existing `POST /v1/auth/login/factor` with `factor: "security_key"`. The server
  relays it as `checks.webAuthN`. Then the existing sufficiency, password-change
  and finalize steps run unchanged.
- **Both enrolled:** the key is the default, with "Use your authenticator app
  instead" as a link. Mixed methods that are refused today (U2F with OTP email or
  SMS) stay refused.
- **Bounds:** a failed assertion that reaches the server counts as one factor
  attempt under the existing per-attempt limit. A cancelled ceremony posts
  nothing (the server cannot see a cancel), so it spends no attempt and the
  challenge stays valid for a retry. The challenge belongs to the
  `login_attempt` row; a repeated `/enroll` issues a fresh one without
  re-verifying the key.
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
- **Recovery.** If Zitadel verified the key but Helivanta's registry write
  failed (503), the retry reads the user's ready auth factors and records a key
  this attempt registered that is already READY, without verifying it again.
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

## Wire contract (slices 1 and 2)

The field naming the factor is `factor`, never `kind`. `code` is required only for
`totp`; a `security_key` body carries its own proof field instead.

- **Password step, key required.** `factor_required` lists `"security_key"`
  (alongside `"totp"` when both are enrolled) and the response carries
  `security_key: {request_options}`, Zitadel's challenge, relayed unparsed.
- **Password step, setup required.** `enrollment_required` lists `"security_key"`
  and the response carries `security_key: {creation_options}` beside the TOTP
  secret. The key is registered at the password step and recorded on the attempt.
- **`POST /v1/auth/login/enroll`** with `{auth_request_id, factor: "security_key",
credential, name?}` verifies the browser's attestation, records the key in
  `iam_security_keys`, and answers `{security_key: {request_options}}`. The page
  then asserts that challenge on `/factor`. `name` is trimmed, at most 64
  characters, has no control characters, and defaults to "Security key".
- **Recovery.** If the registry write failed after Zitadel verified the key, the
  retry finds the key READY in Zitadel and records it without verifying again,
  because Zitadel refuses a second verify. A key missing from the registry is
  never accepted at sign-in.
- **`POST /v1/auth/login/factor`** with `{auth_request_id, factor: "security_key",
assertion}` relays the assertion as `checks.webAuthN`, then runs the same
  sufficiency, password-change and finalize tail as TOTP. A refused assertion
  spends one of the attempt's five guesses.
- **Failures.** A registry or Zitadel read failure is 503, never a refusal and
  never a success. A body without `credential` (enroll) or `assertion` (factor)
  is a 400.
- **Relying party.** `WEBAUTHN_RP_ID` must be exactly `helivanta.app` in
  production and `helivanta.localhost` in development, or the API fails to boot.
- **Registry.** Migration `0057_iam_security_keys` adds `iam_security_keys
(subject, key_id, rp_id, registered_at)` and `login_attempt.security_key_id`.
- **Legacy keys.** A user whose U2F key was registered on `auth.tesserix.app`
  still gets ADR-0069's `security_key_required` continuation until slice 3.

## Slices

1. **Native registration** and
2. **native sign-in** are delivered together by
   `docs/superpowers/plans/2026-11-27-native-security-key-slice-1.md`. S1, then
   D1, D2 and D3's required-setup path, plus the IAM key registry. A key
   registered on the enrol step signs in on Helivanta's pages and reaches the
   callback. The unreachable-Zitadel, wrong-key, cancel and no-WebAuthn cases each
   get a test. ADR-0069's continuation still exists for legacy keys.
3. **Move and delete.** D4's inventory and extended reset, the moves done, and
   then D5's deletion and guards. Slice 3 deletes the continuation once legacy
   keys are moved, and merges only when the inventory reads zero legacy keys.
4. **Account security page.** D3's add and remove. It is separate because
   slices 1–3 already give every user a working key.

Each slice is a vertical PR with backend, shell, tests and docs. Renaming
`rp.name` ("ZITADEL") is decided in slice 1's PR review and is a separate
tesserix-k8s change if it goes ahead.

## Left to the builder

- The exact request-options pass-through type, as long as it stays one field on
  the existing responses.
- The test harness for WebAuthn in the browser: Playwright's virtual
  authenticator or the CDP WebAuthn domain.
- The IAM table's migration ID, the next free `NNNN_iam`.

## Open for the product owner

- **D4 gap: accepted** by the product owner on 2026-11-27. A moved user may be
  password-only between the reset and re-enrolment.
- **Who resets a tenant owner's key? Decided** by the product owner on
  2026-11-27. #1703's reset refuses owner and administrator targets
  (`ordinaryMFATarget`) and removes TOTP only, so it can't move an owner's key.
  A Tesserix operator removes each legacy owner key once through Zitadel's API,
  following a runbook that records the removal in Helivanta's audit trail. The
  owner then registers a new key on Helivanta's page at the next sign-in (D3).
  This replaces D4(2) for owners. Extending #1703's reset to security keys is
  [#1905](https://github.com/tesserix/helivanta/issues/1905), needed for lost
  keys regardless of the move.
- **The count.** `scripts/security-key-inventory.mjs` takes it read-only: every
  `iam_members` subject's READY u2f factors, less the `iam_security_keys`
  registry, by the rule sign-in uses. It exits 0 only at zero legacy keys, and
  never reports zero when a lookup fails.
