import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { test, expect } from "@playwright/test";

import { login } from "./support/login";

// The IdP's origin and the app's, as this harness actually serves them.
// Different REGISTRABLE DOMAINS on purpose (#916 Task 4, design spec D6) —
// see e2e/tests/cross-site-harness.spec.ts. The redirect_uri built from
// WEB_ORIGIN below must match what scripts/zitadel-bootstrap.mjs registered
// on the helivanta-web app BYTE FOR BYTE, or Zitadel refuses the authorize
// request outright and the credential-form assertion below fails for a
// reason that has nothing to do with SSO teardown.
const ZITADEL_ORIGIN = "http://auth.tesserix.localhost:20080";
const WEB_ORIGIN = "http://helivanta.localhost:4301";

// The assertion that would have caught the original bug (#781): sign-out
// used to only clear the transport cookie, leaving the client-side SDK's
// session alive (originally Firebase's IndexedDB store; now
// oidc-client-ts's sessionStorage entry, apps/shell/lib/oidc.ts). A
// stored id_token surviving sign-out could be replayed straight into
// POST /v1/auth/login and rebuild a working `helivanta_session` cookie — on a
// shared ward terminal, the previous user's session was recoverable
// after they "logged out".
//
// This deliberately does NOT assert "the cookie was cleared" — that is
// exactly the proxy assertion that let the defect exist
// (docs/standards/engineering-principles.md §5). It reconstructs the
// actual attack instead: replay the id_token captured from whatever
// client-side session survives sign-out, and see whether the API still
// mints a session from it.
test("a signed-out session cannot be reconstructed from the browser", async ({ page }) => {
  await login(page);
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // Both the rail icon and the header text trigger the identical
  // sign-out sequence (packages/ui/src/hms-shell.tsx). The header one
  // (last in DOM order) is used here — the icon-only rail button sits at
  // the bottom-left, where Next.js's dev-mode tooling indicator also
  // renders, and can intercept the click in local/dev-server runs.
  await page.getByRole("button", { name: "Sign out" }).last().click();

  // handleSignOut's OWN awaited `fetch("/logout")` (which writes the
  // #781 watermark) resolves BEFORE any navigation starts — the heading
  // disappearing is the real signal that revocation has already happened
  // server-side, not just that a click fired.
  await expect(page.getByRole("heading", { name: "Departments" })).not.toBeVisible();

  // Sign-out ends: Helivanta dashboard -> Zitadel's end_session endpoint ->
  // back to /login, which is the registered post_logout_redirect_uri.
  //
  // Since #847 the chain STOPS there. /login is a landing page with a
  // Sign in button, not an automatic redirect — firing an authorization
  // request on arrival raced Zitadel's session teardown, after which a
  // DIFFERENT user could not sign in at all ("User not found in the
  // system"). So the assertion is that we settle on Helivanta's own /login,
  // signed out, rather than being carried onward to Zitadel.
  await page.waitForURL(new RegExp(`${WEB_ORIGIN}/login`), { timeout: 15_000 });
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();

  // Sign-out must end ZITADEL's SSO session, not merely Helivanta's own — or the
  // next person at a shared ward terminal is signed in silently as the
  // previous clinician. That defect was real and was fixed during #838
  // (packages/ui/src/zitadel-session.ts), so it needs an assertion that
  // cannot rot.
  //
  // It has to be checked with an authorize request carrying NO prompt.
  // The landing page's Sign in button sends prompt=login (#847), which
  // forces a credential form whether or not a session survives — so
  // asserting on that form would pass vacuously and quietly stop testing
  // the thing it names. A plain authorize is the only version of this
  // check that can fail.
  // Playwright runs from e2e/, so the bootstrap-written client id sits one
  // level up. Read at run time rather than via an env var: make's -include
  // of this same file is parsed before dev-infra creates it on a fresh
  // clone, which is the staleness window scripts/lib/zitadel.mjs documents.
  const clientId = readFileSync(
    resolve(process.cwd(), "../dev/zitadel/secrets/zitadel.env"),
    "utf8",
  )
    .split("=")[1]
    .trim();
  const plainAuthorize =
    `${ZITADEL_ORIGIN}/oauth/v2/authorize?client_id=${clientId}` +
    `&redirect_uri=${encodeURIComponent(`${WEB_ORIGIN}/api/auth/callback`)}` +
    `&response_type=code&scope=openid&state=s&nonce=n` +
    `&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256`;

  await page.goto(plainAuthorize);
  await expect(
    page.getByLabel(/Loginname|Email|Login Name/i).first(),
    "a plain authorize after sign-out must prompt for credentials; reaching the callback " +
      "silently means Zitadel's SSO session outlived sign-out and the next user at this " +
      "terminal would be signed in as the previous one",
  ).toBeVisible({ timeout: 15_000 });
  await page.goto("/login");

  // The attack: the previous user's oidc-client-ts session used to
  // survive in sessionStorage, so this rebuilt a working session by
  // replaying its id_token.
  //
  // `getUserManager()` isn't reachable here: page.evaluate runs as a
  // plain script with no bundler, so a bare specifier import has nothing
  // to resolve against. `window.__hmsUserManager` (apps/shell/lib/oidc.ts,
  // dev/test builds only) hands back the exact UserManager instance the
  // app itself uses — but only on a Helivanta page, and the browser is
  // currently on Zitadel's origin (the wait above), so this navigates
  // back to `/api/auth/callback` with NO `code`/`state` query params: the
  // one shell route that constructs a UserManager (setting
  // `window.__hmsUserManager` as a side effect, same as every shell page)
  // and then STOPS — `signinRedirectCallback()` fails fast on the missing
  // params and the page shows an error, triggering no further navigation
  // of its own. That absence of a next hop is exactly what makes it a
  // stable place to run `evaluate` from, unlike `/login` (which always
  // redirects again) or a route-interception trick (tried and rejected —
  // see below).
  //
  // Two techniques were tried first and rejected, worth recording so they
  // are not tried again:
  //  - `page.route(...).abort()` on the authorize request: aborting a
  //    TOP-LEVEL navigation does not leave the browser on the previous
  //    page — Chromium replaces the frame with its own
  //    `chrome-error://chromewebdata/`, which has no app JS at all, so
  //    `window.__hmsUserManager` never appears there, on any attempt.
  //  - Stubbing `window.location.assign`/`.replace` to a no-op via
  //    `addInitScript`, to neutralise oidc-client-ts's own redirect call
  //    before it becomes a navigation: had no effect at all (confirmed
  //    live — the browser still reached Zitadel's real login form
  //    regardless). Browsers generally refuse to let page script shadow
  //    `Location` methods; the assignment silently no-ops.
  //
  // A goto immediately after the chain settles can still occasionally
  // race a not-yet-finished internal redirect inside Zitadel's own login
  // app and get `ERR_ABORTED`; retried a bounded number of times rather
  // than treated as a failure, the same judgment call
  // e2e/tests/support/login.ts's own retry makes for a real,
  // non-permanent race.
  let landed = false;
  for (let attempt = 1; attempt <= 5 && !landed; attempt++) {
    try {
      await page.goto("/api/auth/callback", { waitUntil: "domcontentloaded", timeout: 10_000 });
      landed = true;
    } catch (err) {
      if (!/ERR_ABORTED/.test(String(err)) || attempt === 5) throw err;
      await page.waitForTimeout(300);
    }
  }
  await page.waitForFunction(() => Boolean(window.__hmsUserManager), null, { timeout: 15_000 });

  const restored = await page.evaluate(async () => {
    const userManager = window.__hmsUserManager!;
    const user = await userManager.getUser();
    if (!user?.id_token) return "no-user";
    const res = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id_token: user.id_token }),
    });
    if (!res.ok) return "session-refused";
    const check = await fetch("/api/v1/iam/me/permissions");
    return check.ok ? "RESTORED" : "api-refused";
  });

  expect(restored).not.toBe("RESTORED");
});

// The test above is necessary but NOT sufficient, and the gap is worth
// stating: it passes the moment the stored oidc-client-ts session is
// gone (or was never captured), which client-side removeUser() alone
// guarantees. Comment out the server-side call in app/logout/route.ts
// and it still passes — so it proves the client half and silently
// assumes the server half.
//
// That assumption is the whole feature. Clearing browser state protects
// the shared workstation; only the watermark protects a token that has
// already left the browser — copied out of devtools, captured from a
// proxy, or held by a native client. This test holds a valid, unexpired
// ID token across the sign-out and replays it, so the only thing that
// can refuse it is the server-side revocation watermark
// (docs/superpowers/specs/2026-08-13-credential-revocation-design.md D1/D2).
test("a token captured before sign-out is refused afterwards", async ({ page }) => {
  await login(page);
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  await page.waitForFunction(() => Boolean(window.__hmsUserManager));
  const captured = await page.evaluate(async () => {
    const userManager = window.__hmsUserManager!;
    const user = await userManager.getUser();
    return user?.id_token ?? null;
  });
  expect(captured, "precondition: a live ID token was captured before sign-out").toBeTruthy();

  // Precondition: the captured token works right now. Without this, a
  // token that was never valid would make the post-sign-out refusal
  // meaningless — the test would pass for the wrong reason.
  const before = await page.evaluate(async (token) => {
    const res = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id_token: token }),
    });
    if (!res.ok) return "session-refused";
    return (await fetch("/api/v1/iam/me/permissions")).ok ? "accepted" : "api-refused";
  }, captured);
  expect(before, "the captured token must be accepted before sign-out").toBe("accepted");

  await page.getByRole("button", { name: "Sign out" }).last().click();
  await expect(page.getByRole("heading", { name: "Departments" })).not.toBeVisible();

  // The same token, still cryptographically valid and unexpired, replayed
  // after sign-out. Its auth_time predates the watermark the sign-out
  // wrote, so the API must refuse it. Browser state is irrelevant here:
  // the token is supplied directly — `evaluate` just needs SOME live page
  // to run `fetch` from, and sign-out's own redirect chain (Zitadel
  // end_session -> /login -> authorize) is still actively navigating this
  // one right after the click, so an `evaluate` landing exactly then can
  // throw "Execution context was destroyed" — a real, retriable timing
  // gap (same class this file's other test already retries for), not a
  // wrong result.
  let after: string | undefined;
  for (let attempt = 1; attempt <= 5; attempt++) {
    try {
      after = await page.evaluate(async (token) => {
        const res = await fetch("/api/v1/auth/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id_token: token }),
        });
        if (!res.ok) return "session-refused";
        return (await fetch("/api/v1/iam/me/permissions")).ok ? "STILL-ACCEPTED" : "api-refused";
      }, captured);
      break;
    } catch (err) {
      if (!/Execution context was destroyed/.test(String(err)) || attempt === 5) throw err;
      await page.waitForLoadState("domcontentloaded");
    }
  }

  expect(
    after,
    "a token captured before sign-out was still accepted afterwards; the server-side watermark is not being written, and clearing browser state is the only thing protecting the session",
  ).not.toBe("STILL-ACCEPTED");
});
