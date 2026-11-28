# Server-side OIDC handshake — plan

**Spec:** `docs/superpowers/specs/2026-11-20-server-side-oidc-handshake-design.md` (#915, also #1289).
**Agreed 2026-11-28:**

- a public client with PKCE;
- spike 2 run on three engines.

Each slice is its own PR, merged only when CI is green.

## Slice 0: spikes (PR #1917)

- **Spike 1** is `e2e/tests/oidc-session-delete.spec.ts`. It checks that `sid` is the v2 session and that the login-client PAT can delete it without the session token.
- **Spike 2** is `e2e/oidc-spike/handshake-cookie.spec.ts`, run in the `oidc-spike` job. It checks that a Lax cookie survives a cross-site 302, with a Strict control to prove the hop is cross-site.
- **If spike 1 fails,** D4 uses the spec's fallback instead: a server-built end-session redirect. Before slice 3, I prove the fallback works without `id_token_hint`, using the same kind of spec.

## Slice 1: register the callback URI (D7.1)

- **Dev.** `scripts/zitadel-bootstrap.mjs` adds `${webOrigin(port)}/api/v1/auth/oidc/callback` to `helivanta-web` and to the idle-timeout and renewal apps, next to the old URI. The bootstrap must be idempotent: rerunning it against an existing app updates the URIs. That is checked by its existing test.
- **Production.** `tesserix-k8s/k8s/operators/zitadel/claims/helivanta.yaml` adds `https://helivanta.app/api/v1/auth/oidc/callback`. That file is marked "adoption preview only", so the live app's redirect URIs must also be updated by its configuration owner. That is an operator step for the user, recorded in the PR.

## Slice 2: start and callback (D1, D2, and D4's `sid` claim)

- **Package `backend/internal/modules/iam/oidchandshake`:**
  - `seal` and `open` use AES-GCM, with the key derived by HKDF-SHA256 from the session signing key under the info string `helivanta-oidc-handshake-v1`.
  - The payload is `{state, nonce, verifier, purpose, tenant, login_hint, issued_at}`. It refuses to mint above 3,800 bytes and warns at 90%.
- **`GET /v1/auth/oidc/start`:**
  - Validates the purposes `email-login`, `workspace-login`, `customer-signup` and `customer-signup-login`, with a lowercase-UUID tenant for the two tenant purposes and a bounded `login_hint`.
  - Sets the `helivanta_oidc` cookie: `HttpOnly; Secure; SameSite=Lax; Path=/api/v1/auth/oidc`, Max-Age 3600.
  - 302s to the authorize endpoint with S256, `prompt=login`, and the org scope only for tenant purposes.
- **`GET /v1/auth/oidc/callback`:**
  - Opens the seal, compares `state` in constant time and expires the cookie.
  - Exchanges the code with the verifier, then verifies the `id_token` and its `nonce` with the existing verifier.
  - Runs the existing login decision as a function, extracted from `LoginHandlers.Login` without changing behaviour. The tenant comes from the purpose.
  - Mints `helivanta_session` with `ZitadelSessionID = sid` (a new `session.Claims` field), then 302s to `/dashboard`, or to `/signup` for `customer-signup`.
  - Every refusal 302s to `/login?again=…` with the start-again wording and no session.
- **Routes:** declared in `bootstrap.UnauthenticatedRoutes` with their own rate-limit bucket, classified `baseline` in `release-capabilities.json`, and covered by contractdocs.
- **Tests, Go, against a fake Zitadel:**
  - every case in the spec's Verification list;
  - each refusal mutation-checked (state, nonce, seal, reuse, expiry).
- **e2e:** one spec signs in through `/api/v1/auth/oidc/start` against the real Zitadel and reaches the dashboard. The old flow keeps working.

## Slice 3: the browser stops holding tokens (D3–D6, and #1289's rest)

- **`/login` on the server.** `app/login/page.tsx` gets a server-component wrapper implementing the D3 table. The middleware redirect lands on plain `/login`, which now 302s to start, finishing #1289.
- **Session-end marks.** The `helivanta_ended` cookie replaces `SIGNED_OUT_MARK` and `IDLE_ENDED_MARK`. It is set by `POST /v1/iam/me/sign-out` and by idle teardown, and read and cleared by `/login`.
- **Sign-out (D4).** Sign-out also calls `DELETE /v2/sessions/{ZitadelSessionID}` with the PAT, or uses the fallback if spike 1 failed. A failed delete is logged at error level and still revokes. The response 302s to `/login`.
- **Removal (D6).** Delete:
  - `oidc-client-ts`, `lib/oidc.ts` and `packages/ui/src/zitadel-session.ts`;
  - the callback page and `exchangeIdToken`;
  - the frontend.md §3 exception.
- **Tests:**
  - Vitest: the `/login` decision table.
  - e2e:
    - home → card with no intermediate page;
    - #847 (sign-out, then a different user);
    - the #781 replay test rewritten to assert that no token is in any storage;
    - `csp.spec.ts` stays clean.
  - Spike 1 is deleted.

## Slice 4: remove the old path (D7.3, the release after slice 3)

- Remove the old redirect URIs (dev bootstrap, the claim file, and the live app as an operator step).
- Remove `app/api/auth/callback` and the id_token body path of `POST /v1/auth/login`.

## Review focus

1. Two tabs racing a start: the later cookie wins, and the earlier callback lands on start-again without leaving a session behind.
2. A callback replayed after success: the cookie is expired, so it's refused.
3. Clock skew on `issued_at` and on the `id_token`: use the verifier's existing leeway.
4. Proxies dropping `Set-Cookie` on a 302: the e2e asserts the cookie arrives through the real shell proxy.
5. Sign-out when Zitadel is down: the Helivanta session is still revoked and the cookie cleared.
