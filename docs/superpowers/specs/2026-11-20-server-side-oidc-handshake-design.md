# Server-side OIDC handshake: start, callback and sign-out without browser tokens

**Issue:** [#915](https://github.com/tesserix/helivanta/issues/915). This spec widens #915 from "move the code exchange" to the whole handshake.
**Also resolves:** [#1289](https://github.com/tesserix/helivanta/issues/1289) (one-step sign-in), finished at the server.
**Builds on:** [#1751](https://github.com/tesserix/helivanta/issues/1751) (unscoped sign-in), PR #1819 (single sign-in card), PR #1828 ([sign-in never routes by memory](2026-11-24-no-sign-in-routing-by-memory-design.md)) and PR #1830 ([Sign in again starts fresh](2026-11-24-sign-in-again-starts-fresh-design.md)).
**Revisits:** [D1 and D4a of the Zitadel auth design](2026-08-15-zitadel-auth-design.md). Its author must agree; see "Decisions that need agreement".
**Reference implementation:** mark8ly admin, `apps/admin/app/login/authorize/route.ts`, plus the session-cookie size guard in tesserix-home `packages/platform-auth/src/session-cookie-size.ts` (#915, 2026-08-19 comments).

## Problem

A fresh `/login` makes three steps visible in the browser before the credential card appears:

1. **Session check.** It renders, then `POST /api/v1/auth/renew`. The page cannot see the `HttpOnly` session cookie.
2. **Authorize start.** It renders again, then `oidc-client-ts` creates PKCE and `state` in `sessionStorage` and navigates to Zitadel `/oauth/v2/authorize`.
3. **Auth request read.** Zitadel 302s back to `/login?authRequest=…`, and the page renders and reads the request.

PR #1758 makes these look like one card, but the page still loads, runs JavaScript and navigates three times. mark8ly shows only the final card: every step before it is an HTTP 302 issued by the server.

The same browser-side design is #915's security finding:

- The code-for-token exchange runs in browser JavaScript (`app/api/auth/callback/page.tsx`).
- The resulting `id_token` lives in `sessionStorage`, where any XSS can read it.
- `endZitadelSession` needs that stored `id_token` as the `id_token_hint` for sign-out.

## Goal

Every step before the credential card is a server 302. The browser never holds an `id_token`, PKCE verifier or `state`. Helivanta's own session model does not change: the small `helivanta_session` cookie, the idle deadline (#848), revocation (#781) and first-party renewal (#916).

## Decisions

### D1 — The Go API owns the handshake, not a Next.js BFF

Two new unauthenticated API routes, declared in `bootstrap.UnauthenticatedRoutes`:

- `GET /v1/auth/oidc/start?purpose=&tenant=&login_hint=`. It validates its input against the same purposes `customerSignupSigninStateContract` allows today (`email-login`, `workspace-login` with a tenant UUID, `customer-signup`, `customer-signup-login` with a tenant UUID). It mints `state`, `nonce` and a PKCE verifier, stores them in a sealed cookie (D2), and 302s to Zitadel's authorize endpoint:
  - with `prompt=login`, always;
  - with the org scope for a tenant purpose;
  - with `login_hint` when given (length-bounded as today).
- `GET /v1/auth/oidc/callback?code=&state=`. It opens the sealed cookie, checks `state` in constant time and expires the cookie, then exchanges the code with the verifier at Zitadel's token endpoint. It verifies the `id_token`, including its `nonce`, with the existing verifier. It then runs the existing `Login` decision, unchanged, as a function rather than over HTTP, which keeps the tenant binding from the state purpose. Finally it sets `helivanta_session` and 302s to `/dashboard`. A `customer-signup` purpose instead runs the existing signup continuation and 302s to `/signup`.

The Go API, not Next.js, because it already holds the login-client PAT and the session signing key, and #915 argues against a second secret holder. Next.js stays a proxy: `/api/v1/*` already reaches the API.

### D2 — Handshake state in one sealed, short-lived cookie

`helivanta_oidc` holds `{state, nonce, verifier, purpose, tenant, issued_at}`:

- sealed with AES-GCM under a key derived from the session signing key (HKDF, info `helivanta-oidc-handshake-v1`);
- `HttpOnly; Secure; SameSite=Lax; Path=/api/v1/auth/oidc`;
- `Max-Age` of 60 minutes, the same as the browser's started-sign-in lifetime today (`staleStateAgeInSeconds`, commit `c7e8ba83`). A first sign-in includes finding and setting up an authenticator app, and a shorter lifetime strands the person after every step succeeded (#1767). The cookie is single-use and PKCE-bound, so the longer lifetime does not widen what a stolen code can do.

`Lax`, not `Strict`: the callback is a top-level cross-site navigation from Zitadel, and `Strict` would drop the cookie. A fresh start overwrites any earlier cookie, so two tabs racing a start leaves the later one winning and the earlier callback refused. The refusal lands on the landing page with the existing "start again" wording, and the person loses nothing. The size is bounded and checked at mint, with the console's guard ported per #915: refuse loudly with the byte count, and warn at 90%.

### D3 — `/login` decides on the server

`app/login/page.tsx` becomes a server component at the top. It reads the request, decides, and redirects before rendering:

| Request                      | Answer                                                                                                                                                    |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `?authRequest=…`             | Render the credential card, as today.                                                                                                                     |
| `?find=accounts`             | Render the emailed-code finder, as today.                                                                                                                 |
| `?workspace=<uuid>&start=1`  | 302 to `/api/v1/auth/oidc/start` with `workspace-login` for that tenant. This is the signup hand-off and the workspace form of **Sign in again** (#1830). |
| `?workspace=<uuid>`          | Render the landing with its button, which starts `workspace-login` for that tenant, as today.                                                             |
| `?again=1`                   | 302 to start with `email-login`, without consulting the live session (#1830 D1).                                                                          |
| A session-end cookie (D5)    | Render the landing with its wording and button, and clear the cookie.                                                                                     |
| A `helivanta_session` cookie | Render the client session check, as today. A cookie is not proof of a session, and the check can rotate it.                                               |
| None of the above            | 302 to start with `email-login`.                                                                                                                          |

A tenant that is not a lowercase UUID is refused by start, never guessed. Only the URL pins a workspace ([#1828 D1](2026-11-24-no-sign-in-routing-by-memory-design.md)): there is no remembered-workspace cookie, and nothing stored in the browser chooses the workspace. **Not your workspace?** on a pinned card links to plain `/login`, the last row.

The common case, a fresh visitor with no cookie, therefore sees exactly one page: the card.

### D4 — Sign-out ends the Zitadel session server-side

The browser no longer has an `id_token` to pass as `id_token_hint`. `POST /v1/iam/me/sign-out` already writes the revocation watermark. It additionally ends the Zitadel session that the session claims record:

- The callback stores the `id_token`'s `sid` in `pkg/session.Claims` as `ZitadelSessionID`, which adds a few dozen bytes.
- Sign-out calls `DELETE /v2/sessions/{sid}` with the login-client PAT.

This depends on spike 1 below. `loginclient.DeleteSession` today sends the session's own token with the delete, and Helivanta holds that token only for sessions it created itself. If the PAT alone cannot delete the session, the fallback in spike 1 applies.

- On success it 302s to `/login`. The browser never navigates to Zitadel's end-session endpoint again.

Fail direction: if the Zitadel delete fails, the Helivanta session is still revoked and the cookie cleared (#781's watermark is what protects the next user). `prompt=login` on every interactive start (D1) means a surviving Zitadel session can never sign the next person in silently. The failure is logged at error level so a delete that is never working is visible.

### D5 — Session-end marks become a cookie the server can read

`SIGNED_OUT_MARK` and `IDLE_ENDED_MARK` move from `sessionStorage` to one cookie, `helivanta_ended`, with value `signed_out` or `idle`:

- `HttpOnly; Secure; SameSite=Lax; Path=/login`;
- `Max-Age` of 5 minutes;
- set by the sign-out and idle-teardown responses;
- read and cleared by D3.

This keeps #847: arriving after a sign-out shows the button, never a start. It stays per-browser rather than per-tab. That is a deliberate change: today a second tab of the same browser arriving at `/login` in that window would auto-start. With this design, that tab shows the button for up to five minutes, which is the safer reading on a shared terminal.

### D6 — What leaves the browser

The following are deleted:

- `oidc-client-ts`, `lib/oidc.ts`, `packages/ui/src/zitadel-session.ts`;
- the `sessionStorage` user store;
- `app/api/auth/callback/page.tsx`;
- `exchangeIdToken` and the raw-fetch exception in `docs/standards/frontend.md` §3 for it.

The exception for `renewSession` remains.

`POST /v1/auth/login` (id_token in the body) stays only while a deploy is in flight, then goes in the same release train. D7 orders this.

### D7 — Cutover order

1. **Zitadel.** Add `https://helivanta.app/api/v1/auth/oidc/callback` (and the dev equivalent) to the app's redirect URIs in `scripts/zitadel-bootstrap.mjs` and `tesserix-k8s` `zitadel-bootstrap` values, alongside the old one.
2. **One Helivanta release** ships D1–D6. Old tabs holding a browser-started request still complete through the old callback page until it is removed, because both URIs are registered.
3. **Next release** removes the old redirect URI, the old callback route and `POST /v1/auth/login`'s id_token path.

## Decisions that need agreement

- **Public client with PKCE versus confidential client.** #915 proposed a confidential client. This spec keeps the app a **public client with PKCE**: the verifier never leaves the server and the sealed cookie, so a stolen code cannot be exchanged. That avoids a new client secret to provision through OpenBao → ESO. Switching to `private_key_jwt` later is a provisioning change only. _Needs the D1 author's agreement._
- **D1/D4a of the 2026-08-15 design.** D1's "public client in the browser" premise is replaced. D4a stands: no refresh token, no `offline_access`, and renewal remains first-party.

## Must be proven before implementation (spike, dev Zitadel)

1. That the `id_token`'s `sid` equals the v2 session id, and that the login-client PAT may `DELETE /v2/sessions/{id}` without that session's token. The existing `loginclient.DeleteSession` sends the token, so this is not yet shown. If either fails, D4 falls back to a server-built end-session redirect with `client_id` and `post_logout_redirect_uri`, whose behaviour without `id_token_hint` must then be verified the same way.
2. That `SameSite=Lax` cookies on `/api/v1/auth/oidc` survive Zitadel's 302 in Chromium, Firefox and Safari, as an e2e assertion.

## Not in scope

Renewal and idle timeout (unchanged), passkeys (#422), enterprise SSO (#423), D10 and D11 of #1751. D11 builds on this spec's callback, not on `oidc-client-ts`, so it waits for it.

## Verification

- **Go:** start 302s with PKCE S256, `prompt=login`, the org scope only for tenant purposes, and the cookie sealed and size-bounded.
- **Go, callback refusals:** the callback refuses a missing cookie, a mismatched `state`, a reused cookie, an expired cookie, a wrong `nonce` and a tampered seal. Each is refused with the start-again landing and no session.
- **Go, callback success:** a good callback sets `helivanta_session` and expires `helivanta_oidc`.
- **Go, sign-out:** sign-out deletes the recorded Zitadel session. A failed delete still revokes and clears.
- **Next:** `/login` with no cookies 302s to start before any HTML is rendered; with `helivanta_ended` it renders the landing and clears the cookie.
- **e2e:**
  - home → card with no intermediate page;
  - sign-out → a different user signs in (#847);
  - the #781 replay test rewritten: there is no browser token left to replay, so it asserts there is none in any storage after sign-in;
  - `e2e/tests/csp.spec.ts` stays clean.
- **Contracts:** `UnauthenticatedRoutes`, `release-capabilities.json`, `docs/api/domains/platform.md`, `go run ./cmd/contractdocs`.
