# Spike — Helivanta as Zitadel's login client (v4.15.3)

Run against the live dev stack on 2026-08-16, before any design was written.
Every response below was **observed**, not read from documentation. Where a case
was not exercised it says so rather than guessing.

Stack: `ghcr.io/zitadel/zitadel:v4.15.3`, `LOGINV2_REQUIRED=true`, login client
machine user `helivanta-login-client` (PAT at `dev/zitadel/secrets/login-client.pat`),
org `Helivanta`, seeded user `test@hms.dev`.

> **The stack no longer runs `LOGINV2_REQUIRED=true`.** #854 Task 1 flipped
> `ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED` to `false` in
> `docker-compose.dev.yml`, working around upstream
> [zitadel/zitadel#10722](https://github.com/zitadel/zitadel/issues/10722):
> with `required=true`, the instance-wide flag wins over the per-app
> `loginVersion.loginV2.baseUri` this design depends on, so Helivanta's own
> `/login` was never reached.
>
> **The flip is not a no-op**, and `docker-compose.dev.yml`'s own comment at
> that setting says so: any other product getting Login V2 *implicitly* from
> the instance-wide flag falls back to its own per-app setting — or to Login
> V1, if it has none — the moment `required` goes to `false`. In dev that is
> harmless (Helivanta is the only app configured here besides the Management
> Console). On the shared instance it is a cross-product change and belongs to
> the platform team. This qualifies spec D1's "another product's login must
> not move": the *per-app base URI* is Helivanta's alone, but the instance-wide
> `required` flag this workaround also needs is not, and the two were
> conflated in D1 as originally written. See D1 for the corrected claim.
>
> Every observation below was recorded BEFORE the flip and is unaffected by
> it: none of the four login-client API calls goes through the login UI at
> all.

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

**The callback is Helivanta's existing one.** `callbackUrl` comes back as
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
Zitadel's. A password-only Helivanta login is safe only while nothing expects MFA — and
the moment someone enables `forceMfa`, or #41 ships partially, Helivanta would bypass
it silently for every user with no error anywhere. This must be enforced
structurally by Helivanta (read the policy, refuse or hand off), not left as a
convention.

The org policy was restored to default afterwards; re-read confirms
`isDefault: true` and **no `forceMfa` key at all**.

> **Corrected 2026-08-16 (#854 Task 8).** This line originally recorded the
> read-back as `isDefault: true, forceMfa: false`. That cannot be what the API
> returned: Zitadel's protojson marshaling elides zero-value booleans, so a
> false `forceMfa` is **absent from the body entirely** — proven by
> `loginclient.LoginPolicy`'s implementation and its tests
> (`client.go`, the "ForceMFA absent does NOT mean unrecognized" section), and
> by the live integration tests that read the real default policy. The
> original wording was the spike author's own interpretation of an absent
> field written back as if it were an observed one, which is exactly the
> failure mode this document's header rule ("observed, not read from
> documentation") exists to prevent.
>
> This mattered materially: the first implementation required an EXPLICIT
> `forceMfa` and so treated every ordinary login as an unreadable policy,
> handing off 100% of sign-ins. The anchor field
> (`passwordCheckLifetime`) exists because of this correction.

## 3. Failed and unknown logins are distinguishable — two ways

| Case | Status | Body | Time (3 runs) |
|---|---|---|---|
| Correct password | 200 | `{sessionId, sessionToken}` | — |
| Wrong password | **400** | `COMMAND-3M0fs` "Password is invalid", `CredentialsCheckError` with `failedAttempts` | **0.72–0.78s** |
| Unknown user | **404** | `QUERY-Dfbg2` "User could not be found" | **0.013s** |

Both the status code and a **~55× timing difference** are user-enumeration
oracles. The timing gap is the password hash: an unknown user never reaches it.

Helivanta must therefore return one identical response for both, *and* equalise the
timing — mapping the status codes alone leaves the oracle intact and would look
fixed. `failedAttempts` must never reach the browser.

## 4. There is no account lockout at all

`GET /management/v1/policies/login` and `/policies/lockout` return the default
policy with **no `maxPasswordAttempts`**, i.e. zero, i.e. no policy-driven
account lockout: no number of failures ever locks the account, and a correct
password always succeeds.

> **Corrected 2026-08-16 (#854 Task 4, carried by
> [#855](https://github.com/tesserix/helivanta/issues/855)).** This section
> originally said attempts were "unlimited" and that "nothing acts on"
> `failedAttempts`. **Both are wrong**, and were contradicted by a later
> measurement against this same stack: 25 wrong-password attempts against
> `test@hms.dev` showed a **per-account escalating backoff** — the first 9 in a
> tight 809–845ms band, then ~1.83s at #10–14, ~2.83s at #15–19, ~3.85s at
> #20–24, ~4.86s at #25. It escalated identically whether attempts were
> back-to-back or spaced 4s apart (so it is keyed on accumulated failures, not
> on request rate or elapsed time) and **reset after one successful login**.
> Something does act on `failedAttempts`; it just delays rather than locks.
> The full measurement and its consequences are recorded at
> `MinFailedLoginDuration` in `backend/internal/modules/iam/loginui.go`.

So there is still no *lockout*, and the request rate limiter (#841/#851) remains
the only control Helivanta itself applies. That is a real gap and it exists **now**,
independent of this work — it is not introduced by the login client. Tracked as
[#855](https://github.com/tesserix/helivanta/issues/855).

Also observed on the default policy: `passwordCheckLifetime: 864000s` (10 days),
`secondFactorCheckLifetime: 64800s`, `allowRegister: true`,
`allowExternalIdp: true`, `allowDomainDiscovery: true`.

## 5. `passwordChangeRequired` is not signalled to a login client at all (#854 Task 8)

Provoked, not assumed. `POST /management/v1/users/human` (the endpoint named in
the design's D2 table) silently drops unrecognised keys and answers 200 either
way — a hard-won fact from earlier work on this project. The sibling import
endpoint is the one that actually takes the flag:

Preserved as evidence: the request/response bodies below are a literal
transcript of what was actually sent on the wire during this spike, before
the `hms` → `helivanta` rename. `pwchange-test@hms.dev` and `HmsDev123!`
are left exactly as captured — rewriting them would make this document
assert it observed something it did not.

```
POST /management/v1/users/human/_import
  {"userName":"pwchange-test@hms.dev","profile":{...},
   "email":{...},"password":"HmsDev123!","passwordChangeRequired":true}
→ 200 {"userId":"386506687000870919", ...}

GET  /v2/users/386506687000870919
→ 200 {..., "human":{..., "passwordChangeRequired":true, ...}}   ← confirmed it took
```

With the flag confirmed set, a normal password login was driven all the way
through both the raw Zitadel API and Helivanta's own endpoint:

```
POST /v2/sessions        {checks:{user,password}}
→ 200 {"sessionId":"386506702654013447","sessionToken":"..."}
  — no passwordChangeRequired anywhere in the body

GET  /v2/sessions/386506702654013447
→ 200 {"session":{..., "factors":{"user":{...},"password":{"verifiedAt":...}}}}
  — no passwordChangeRequired anywhere in the body

POST /v2/oidc/auth_requests/{id}   {callbackKind, session:{sessionId,sessionToken}}
→ 200 {"callbackUrl":"http://localhost:4301/api/auth/callback?code=...&state=..."}
  — a REAL authorization code, issued anyway

POST http://localhost:8080/v1/auth/login/password   (Helivanta's own endpoint)
  {"auth_request_id":"V2_...","login_name":"pwchange-test@hms.dev","password":"HmsDev123!"}
→ 200 {"callback_url":"http://localhost:4301/api/auth/callback?code=...&state=..."}
  — Helivanta completed the login exactly as if nothing were different
```

**Zitadel does not signal `passwordChangeRequired` to a login client at any
point in this flow** — not on session create, not on session read, not on
finalize. There is no field in any of these responses for Helivanta to
branch on.
Consequently **Helivanta today silently completes a login for a user an admin
flagged as needing a password change** — the same class of silent bypass §2
found for `forceMfa`, except here there is no wire signal at all to read,
where `forceMfa`'s failure at least left a policy Helivanta could consult. This is
recorded as a known limitation at the decision point in
`backend/internal/modules/iam/loginclient/sufficiency.go`
(`CompleteIfSufficient`'s KNOWN LIMITATIONS §1) and filed as
[#856](https://github.com/tesserix/helivanta/issues/856) rather than fixed in this
task — closing it costs an extra round trip (a `users/{id}` read) on every
login, which is a real product tradeoff, not a small fix.

(Test user and its `passwordChangeRequired:true` state were deleted afterward;
`DELETE /v2/users/386506687000870919` followed by a `GET` confirming 404
verified the cleanup actually took.)

## 6. Per-user enrolled factors: found, and reachable with the login-client PAT (#854 Task 8)

The two endpoints guessed at in the original handoff both still 404 on v4.15.3, confirmed
again live: `/v2/users/{id}/authentication_factors` and
`/management/v1/users/{id}/auth_factors`. The correct endpoints, found by
reading the v4.15.3 proto's `google.api.http` annotations
(`proto/zitadel/user/v2/user_service.proto`) and then verified live rather
than trusted from the source:

```
GET  /v2/users/{id}/authentication_methods
→ 200 {"details":{"totalResult":"1"}, "authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}

POST /v2/users/{id}/authentication_factors/_search
→ 200 {}   (empty result for a password-only user)
```

Both answer with the **login-client PAT**, not just the seed/IAM_OWNER PAT —
confirmed by repeating each call with `dev/zitadel/secrets/login-client.pat`
and getting the identical 200. This matters: the sufficiency check
(`loginclient.CompleteIfSufficient`) runs with only the login-client PAT in
production, so an endpoint that needed the owner PAT would not have been
usable there at all.

Enrolling a second factor changes the answer, proving the endpoint actually
reflects real state and is not just an empty stub:

```
POST /v2/users/{id}/otp_email   {}
→ 200

GET  /v2/users/{id}/authentication_methods
→ 200 {"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_OTP_EMAIL","AUTHENTICATION_METHOD_TYPE_PASSWORD"]}

POST /v2/users/{id}/authentication_factors/_search
→ 200 {"result":[{"state":"AUTH_FACTOR_STATE_READY","otpEmail":{}}]}
```

`GET .../authentication_methods` was chosen for the implementation (simple
GET, no body) over the `_search` POST: both work, and the GET is the smaller
surface for what Helivanta needs (just the type list, not per-factor state).
`AUTHENTICATION_METHOD_TYPE_PASSWORD` is the one value that does not count as
"a factor a password-only session cannot satisfy"; every other observed
value (`_TOTP`, `_U2F`, `_PASSKEY`, `_IDP`, `_OTP_SMS`, `_OTP_EMAIL`,
`_RECOVERY_CODE`) does. This closed the D4 gap in
`loginclient.CompleteIfSufficient`: the org may not force MFA, but a user who
voluntarily enrolled a second factor now gets handed off rather than
password-only-completed.

(The OTP-email-enrolled test user was deleted afterward the same way the
`passwordChangeRequired` one was, verified via a post-delete `GET` 404.)

## 7. Not checked

Stated explicitly rather than assumed, per this repo's practice:

- **Account lockout signalling** — unreachable while `maxPasswordAttempts` is 0.
- **Federated IdP handoff** — no external IdP is configured on the instance.
- **Per-app "Custom base URL for the new Login UI"** — exercised as part of
  Task 1/D1 (see `docs/superpowers/plans/2026-08-16-hms-login-client.md` and
  this repo's git history); not re-recorded in this section since it was
  resolved before Task 8, not by it.
