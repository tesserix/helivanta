# Helivanta never sends a clinician to Zitadel's login page

**Issue:** [#947](https://github.com/tesserix/helivanta/issues/947)
**Supersedes:** the handoff rows of D3 in
`2026-08-16-hms-login-client-design.md` and D1's "every other case keeps
handing off" in `2026-08-17-native-mfa-auth-components-design.md`. Both are
corrected in the same change to point here.
**Makes obsolete:** #942 (the hosted-login URL's duplicated dev default). The
URL and the config that validated it are deleted by this change.

## The problem, stated precisely

When the login API decides it cannot finish a sign-in itself, it answers
`{handoff_url}` and the page navigates the browser to Zitadel's hosted login
(`ZITADEL_HOSTED_LOGIN_URL?authRequest=<id>`). The product decision recorded
on 2026-10-07 is that **Helivanta's users never see Zitadel's UI.** Two
observed defects make the current behaviour worse than merely off-brand:

1. **The handoff strands the user.** Production's hosted login URL is
   `https://auth.tesserix.app/ui/v2/login`. Zitadel's Login V2 app answers
   that root path by redirecting to `/loginname` **and dropping the query
   string** (`apps/login/src/app/(login)/page.tsx`, read at zitadel
   `8a54a2a`). Only its `/login` route reads `authRequest`
   (`src/lib/auth-utils.ts`, `validateAuthRequest`). The clinician signs in to
   Zitadel with no request attached and lands on `/signedin` ("You are signed
   in."), with nothing to redirect back to. Observed by the product owner on
   2026-10-07.
2. **Two of the handoff reasons log nothing.** An enrolled factor Helivanta
   cannot collect, and `forceMfa` with nothing enrolled, both reach the
   generic `login password succeeded but session insufficient` line with no
   reason. Diagnosing which one fired requires Zitadel's console.

Fixing the URL (`/login?authRequest=`) would repair (1) and keep Zitadel's UI
in front of clinicians, which the product decision rules out. Removing the
handoff resolves (1) and is what was decided.

## Decisions

### D1 — Three outcomes, and the zero value is a refusal

`loginclient.Outcome` becomes `OutcomeRefused` (the zero value),
`OutcomeFactorRequired`, `OutcomeComplete`. `OutcomeHandoff` is deleted, so no
caller can produce or test for it any more; that is a compile error, not a
convention. The zero value stays the fail-closed one, as `OutcomeHandoff` was:
a `Result{}` that nobody filled in refuses the sign-in.

A refused `Result` carries a `RefusalReason`:

| Condition (password already verified)                                              | Before                    | Now                                                                              |
| ---------------------------------------------------------------------------------- | ------------------------- | -------------------------------------------------------------------------------- |
| Enrolled factor other than password/TOTP (passkey, U2F, OTP email/SMS, linked IdP) | handoff, no reason logged | `RefusalFactorUnsupported`: refused, reason and the enrolled method types logged |
| `forceMfa` and nothing enrolled                                                    | handoff, no reason logged | `RefusalMFAEnrollmentRequired`: refused, logged                                  |
| Enrolled factors or login policy unreadable                                        | handoff, WARN logged      | **error** (`ErrUnavailable`): 503, never a refusal and never a completion        |
| TOTP enrolled                                                                      | native prompt             | unchanged                                                                        |
| Nothing beyond password, policy does not force MFA                                 | complete                  | unchanged                                                                        |

Unreadable state is an error, not a refusal, deliberately. "We could not tell"
must not tell the clinician their account is unsupported. It is the same
direction as before (fail closed), with a retryable answer instead of a
redirect.

### D2 — The browser is told why, in Helivanta's own words

`POST /v1/auth/login/password` and `POST /v1/auth/login/factor` answer a
refusal with **403** and the standard `respond.Error` envelope:

- `{"error":"sign_in_method_unsupported", ...}`: the account uses a sign-in
  method Helivanta does not support yet, so the clinician should contact their
  administrator.
- `{"error":"mfa_enrollment_required", ...}`: the organisation requires
  two-step verification and none is set up, so the clinician should contact
  their administrator. Native enrolment replaces this answer in a later slice.

**Enumeration.** Both are reachable only after Zitadel has accepted the
password, exactly as `handoff_url` was. A refusal therefore discloses nothing
`handoff_url` did not already disclose to someone holding the correct
password. Spec D5's equalisation (same status, body and timing floor for wrong
password and unknown user) is untouched. Neither refusal joins that
equalisation, because both mean the credential was right, the same reasoning
the handoff branch carried.

On the factor path, a refusal after a correct code deletes the `login_attempt`
row, as the handoff branch did.

### D3 — The page renders the refusal; it never navigates for one

`apps/shell/lib/login-client.ts` replaces its `handoff` outcome with
`refused` (carrying the reason code). `app/login/page.tsx` renders a refusal
in the auth card: the message for the reason, plus the same "start again"
affordance the expired-request state uses. It never calls
`window.location.assign` for a refusal. A 503 renders the existing
"temporarily unavailable" state.

### D4 — The hosted-login machinery is deleted, not left dormant

Deleted:

- `POST /v1/auth/login/handoff/:id`, its handler, its rate bucket, and its
  entry in `bootstrap.UnauthenticatedRoutes`;
- `LoginUIHandlers.handoffURL` and the `hostedLoginBaseURL` field;
- `config.ZitadelHostedLoginURL` (`ZITADEL_HOSTED_LOGIN_URL`),
  `config.HelivantaWebOrigin` (`HELIVANTA_WEB_ORIGIN`),
  `RequireDistinctHostedLoginOrigin` and its errors.

`HELIVANTA_WEB_ORIGIN` existed only so the boot check could compare the
hosted-login origin against it. Code that cannot be reached is a place a future
change can quietly re-enable a redirect, so it goes. A deployment that still
sets either variable boots unaffected; an unread environment variable is
inert.

### D5 — Every non-completion says why, in our own logs

The password and factor handlers log `outcome=refused` with `refusal_reason`,
and for `RefusalFactorUnsupported` the enrolled method types as Zitadel names
them (`AUTHENTICATION_METHOD_TYPE_U2F`, ...). Method types are a property of
the account's configuration, not PHI and not a credential. The unreadable
paths keep their existing WARN with the wrapped error. This is the line that
would have answered the 2026-10-07 investigation without Zitadel's console.

## What this slice does not cover

Each item is filed separately and linked from the PR:

- **Native TOTP enrolment** ([#948](https://github.com/tesserix/helivanta/issues/948)): QR code and confirmation in Helivanta. Until it
  ships, `mfa_enrollment_required` refuses.
- **Native email/SMS OTP, passkeys/U2F, and federated IdP sign-in.** Until
  each ships, `sign_in_method_unsupported` refuses. Accounts with such a
  method must have it removed in Zitadel to sign in, which is the product
  owner's stated choice (branded refusal over handoff).
- **Logout.** This spec covers sign-in only. Whether the end-session redirect
  shows a Zitadel page is not established here.

## Tests

- `loginclient`: each D1 row asserts its `Outcome` **and** `RefusalReason`,
  and that `finalize` was not called for any refusal. Unreadable factors or
  policy return an `ErrUnavailable`-wrapped error, not a `Result`. A zero
  `Result` is a refusal.
- `loginui`: password and factor answer 403 with the exact `error` code per
  reason. No response body anywhere contains `handoff_url` or the hosted-login
  host. The handoff route is gone (404 from the engine). The refusal log line
  carries `refusal_reason` (asserted on captured log output).
- `config`: the deleted fields no longer exist, so their tests are deleted
  with them; boot no longer reads either variable.
- Shell (Vitest, `renderWithProviders`): each refusal code renders its
  message and the start-again control, and `window.location.assign` is
  **not** called.
- e2e: `support/login.ts` drops the handoff branch.
- Each new assertion is shown able to fail by mutation.
