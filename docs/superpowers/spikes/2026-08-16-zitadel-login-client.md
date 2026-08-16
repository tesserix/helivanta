# Spike — HMS as Zitadel's login client (v4.15.3)

Run against the live dev stack on 2026-08-16, before any design was written.
Every response below was **observed**, not read from documentation. Where a case
was not exercised it says so rather than guessing.

Stack: `ghcr.io/zitadel/zitadel:v4.15.3`, `LOGINV2_REQUIRED=true`, login client
machine user `hms-login-client` (PAT at `dev/zitadel/secrets/login-client.pat`),
org `HMS`, seeded user `test@hms.dev`.

---

## 1. The flow works end to end

```
GET  /oauth/v2/authorize?client_id=…      → 302 /ui/v2/login/login?authRequest=V2_386477922262777863
GET  /v2/oidc/auth_requests/V2_…          → 200 {id, clientId, scope, redirectUri}
POST /v2/sessions                          → 200 {sessionId, sessionToken}
POST /v2/oidc/auth_requests/V2_…          → 200 {callbackUrl: …/api/auth/callback?code=…&state=…}
```

All four calls authenticate with the **login client PAT**. The auth request id
carries a `V2_` prefix and is used verbatim.

**The callback is HMS's existing one.** `callbackUrl` comes back as
`http://localhost:4301/api/auth/callback?code=…&state=…` — the same shape
`apps/shell/app/api/auth/callback/page.tsx` already handles. The browser starts
the flow through `oidc-client-ts`'s `signinRedirect()`, which stores `state` and
the PKCE `code_verifier` before any of this runs, so **PKCE and state continuity
are preserved and the existing callback and exchange need no change**. That is
the main reason this architecture is cheap on our side.

## 2. Zitadel does NOT enforce forceMFA for a login client

**The most important result here, and a security one.**

With `forceMfa: true` set on the org login policy (verified read-back:
`{'forceMfa': True}`), a **password-only** session:

```
POST /v2/sessions   {checks: {user, password}}   → 200, factors: {user, password}   ← no MFA factor
POST /v2/oidc/auth_requests/V2_386478000998252551 → 200, callbackUrl with a valid code
```

Zitadel issued the authorization code anyway. It did not refuse, and it did not
signal that a factor was missing.

**Consequence for the design:** enforcing MFA is the login client's job, not
Zitadel's. A password-only HMS login is safe only while nothing expects MFA — and
the moment someone enables `forceMfa`, or #41 ships partially, HMS would bypass
it silently for every user with no error anywhere. This must be enforced
structurally by HMS (read the policy, refuse or hand off), not left as a
convention.

The org policy was restored to default afterwards; re-read confirms
`isDefault: true, forceMfa: false`.

## 3. Failed and unknown logins are distinguishable — two ways

| Case | Status | Body | Time (3 runs) |
|---|---|---|---|
| Correct password | 200 | `{sessionId, sessionToken}` | — |
| Wrong password | **400** | `COMMAND-3M0fs` "Password is invalid", `CredentialsCheckError` with `failedAttempts` | **0.72–0.78s** |
| Unknown user | **404** | `QUERY-Dfbg2` "User could not be found" | **0.013s** |

Both the status code and a **~55× timing difference** are user-enumeration
oracles. The timing gap is the password hash: an unknown user never reaches it.

HMS must therefore return one identical response for both, *and* equalise the
timing — mapping the status codes alone leaves the oracle intact and would look
fixed. `failedAttempts` must never reach the browser.

## 4. There is no account lockout at all

`GET /management/v1/policies/login` and `/policies/lockout` return the default
policy with **no `maxPasswordAttempts`**, i.e. zero, i.e. unlimited password
attempts. The `failedAttempts` counter in the error detail increments but nothing
acts on it.

So today the only brute-force control on HMS login is the request rate limiter
(#841/#851). That is a real gap and it exists **now**, independent of this work —
it is not introduced by the login client. Worth its own issue.

Also observed on the default policy: `passwordCheckLifetime: 864000s` (10 days),
`secondFactorCheckLifetime: 64800s`, `allowRegister: true`,
`allowExternalIdp: true`, `allowDomainDiscovery: true`.

## 5. Not checked

Stated explicitly rather than assumed, per this repo's practice:

- **`passwordChangeRequired`** — how it is signalled on session creation. No dev
  user currently has it set, and the handoff notes it only exists on the
  `_import` sibling endpoint. The implementation must provoke and record this.
- **Account lockout signalling** — unreachable while `maxPasswordAttempts` is 0.
- **Federated IdP handoff** — no external IdP is configured on the instance.
- **Per-app "Custom base URL for the new Login UI"** — confirmed to exist in the
  v4 docs, **not yet exercised on our instance**. This is the setting that keeps
  the change scoped to `hms-web` and away from other Tesserix products on the
  shared instance, so it is the first thing the implementation must prove.
