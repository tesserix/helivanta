import { expect, test } from "@playwright/test";

import { login } from "./support/login";

// #916 Task 4, design spec D6 — the assertion that would have FAILED
// against production before this branch.
//
// #916: renewal used a hidden iframe on the app origin making a
// `prompt=none` request to Zitadel. In production the app is helivanta.app
// and Zitadel is auth.tesserix.app — different registrable domains, so
// cross-site, so Zitadel's SameSite=Lax session cookie was never sent and
// renewal ALWAYS failed. Clinicians were evicted minutes after signing in.
// Task 3 replaced that with a same-origin POST /v1/auth/renew; this spec is
// what proves a session actually survives, and cross-site-harness.spec.ts is
// what proves this suite now runs under production's cross-site condition
// rather than the same-site one that hid the defect.
//
// Runs in its own Playwright PROJECT ("renewal", e2e/playwright.config.ts)
// against its OWN API + shell pair (`make dev-api-renewal` /
// `make dev-web-renewal`, see the Makefile's "Session-renewal e2e fixture"
// comment), booted with SESSION_TTL=3m (the Makefile's
// SESSION_TTL_TEST_VALUE) and IDLE_TIMEOUT left at its
// 15-minute default. Both halves of that are load-bearing:
//
//   - A SHORT SESSION_TTL is the only way to observe a session outliving
//     its own TTL inside a test run. It cannot be applied to the shared
//     stack without weakening it for every other spec and for production.
//   - IDLE_TIMEOUT must stay LONG. Renewal deliberately does not move
//     idle_deadline (design spec D4), so a short idle timeout would end the
//     session on the idle clock before the TTL clock could prove anything —
//     which is also why this cannot share the idle-timeout fixture.
//
// WHY THREE MINUTES IS ENOUGH — and why it used to have to be six
// minutes. This spec originally ran a 6-minute SESSION_TTL because
// apps/shell scheduled its FIRST renewal from a hardcoded 5-minute client
// constant (FALLBACK_RENEWAL_INTERVAL_MS, apps/shell/lib/renew.ts): POST
// /v1/auth/login returned no `renew_at`, so the server's schedule governed
// every renewal EXCEPT the first, and any SESSION_TTL under five minutes
// killed every session before the first renewal fired. That was a real
// production defect — config.go applies no minimum to SESSION_TTL (#921) —
// and it
// was fixed rather than worked around (#916 Task 4, F3): login now returns
// `renew_at` from renewAtFor, the SAME helper the renewal endpoint uses,
// and the shell seeds its first timer from it.
//
// With the cadence genuinely server-driven from the first tick the TTL can
// be far shorter than six minutes. renewAtFor answers TTL/3 = 60s here, so
// renewals land at t+60s, t+120s and t+180s while the login cookie dies at
// t+180s.
const RENEWAL_API_URL = "http://localhost:8098"; // Makefile's HELIVANTA_RENEWAL_API_PORT
const SESSION_TTL_SECONDS = 3 * 60; // Makefile's SESSION_TTL_TEST_VALUE

// THE MARGIN THIS SPEC DEPENDS ON, stated so a future failure is
// diagnosable instead of mysterious.
//
// Call X the gap between the API minting the login cookie and
// components/session-renewal.tsx actually mounting in the browser: a
// redirect through the auth-callback route, a React mount, and — on a cold
// CI runner — a first-request `next dev` compile of the dashboard route.
// The first renewal fires at approximately
//     max(mint + SESSION_TTL/3, mount + MIN_RENEWAL_DELAY_MS)
// because nextRenewalDelayMs clamps an already-past `renew_at` up to
// MIN_RENEWAL_DELAY_MS (30s) rather than firing immediately. For that
// renewal to land while the login cookie is still valid, X must satisfy
//     X < SESSION_TTL - SESSION_TTL/3
// which at SESSION_TTL=3m is a budget of TWO MINUTES.
//
// This was briefly SESSION_TTL=90s, which left a 60-second budget — and
// blowing it does NOT fail as "renewal was late". It fails at step 4 as
// "no helivanta_session cookie", which is indistinguishable from a genuine
// renewal defect. A harness whose flake looks exactly like the bug it
// exists to catch is worse than a slower one, so the TTL was raised rather
// than the margin argued away. X is a few seconds on a warm machine.
const X_BUDGET_SECONDS = SESSION_TTL_SECONDS - SESSION_TTL_SECONDS / 3;

// Past the original cookie's expiry, with enough margin that a slow
// renewal round trip (a real HTTP call, a real Zitadel user-state check and
// a real OpenFGA membership check) cannot be mistaken for a failure. The
// dominant cost here is the wait itself, not this margin.
const PAST_TTL_MS = (SESSION_TTL_SECONDS + 25) * 1000;

// WHAT THIS SPEC DOES NOT COVER, stated so nobody reads more into a green
// run than it earns: it proves the loop RUNS, not that it keeps
// rescheduling. renewAtFor answers 60s here, so renewals are due at t+60s,
// t+120s and t+180s — but a regression where the loop fires ONCE and never
// schedules again would still pass every assertion below, because the
// single renewal at t+60s mints a cookie good until t+240s and the
// checks all happen at t+205s. Closing that would mean waiting past
// t+240s, and the wall time is already this suite's dominant cost;
// nextRenewalDelayMs's rescheduling is pinned by unit tests
// (apps/shell/components/session-renewal.test.tsx) instead, which is the
// right level for it.
const PERMISSIONS_PATH = "/v1/iam/me/permissions"; // no /api prefix: called directly, not via the shell's rewrite
const SESSION_COOKIE = "helivanta_session";

// One real sign-in plus a ~205s real-time wait, plus headroom for a cold
// `next dev` compile on the fixture's first request.
test.setTimeout(420_000);

async function sessionCookieValue(page: import("@playwright/test").Page): Promise<string> {
  const cookies = await page.context().cookies(page.url());
  const cookie = cookies.find((c) => c.name === SESSION_COOKIE);
  expect(cookie, `no ${SESSION_COOKIE} cookie on ${page.url()}`).toBeTruthy();
  return cookie!.value;
}

test("an idle signed-in clinician is STILL authenticated after a renewal interval", async ({
  page,
  request,
}) => {
  // --- 1. Sign in, and capture the session this login minted -----------
  const signInStartedAt = Date.now();
  await login(page);
  const signInSeconds = (Date.now() - signInStartedAt) / 1000;
  const originalSession = await sessionCookieValue(page);

  // The X budget, asserted rather than assumed — see X_BUDGET_SECONDS
  // above. login() resolves only once the API has accepted the session, so
  // its wall time is an upper bound on the mint-to-mount gap this spec
  // depends on. Checking it HERE means an over-budget run fails with a
  // message naming the real cause, instead of failing 205 seconds later at
  // step 4 as "no helivanta_session cookie" — which is exactly what a
  // genuine renewal defect looks like. This assertion is about the
  // HARNESS, not the product: if it fires, raise SESSION_TTL_TEST_VALUE
  // (Makefile) rather than loosening anything below it.
  expect(
    signInSeconds,
    `signing in took ${signInSeconds.toFixed(1)}s, which exceeds this fixture's ` +
      `${X_BUDGET_SECONDS}s budget for the gap between the login mint and SessionRenewal ` +
      `mounting (SESSION_TTL - SESSION_TTL/3). The first renewal cannot land before the ` +
      `login cookie expires, so the rest of this spec would fail as "no session cookie" and ` +
      `read as a renewal defect. Raise SESSION_TTL_TEST_VALUE in the Makefile.`,
  ).toBeLessThan(X_BUDGET_SECONDS);

  const initialCheck = await page.evaluate(
    async (url) => (await fetch(url)).ok,
    "/api" + PERMISSIONS_PATH,
  );
  expect(initialCheck, "a freshly minted session must be accepted").toBe(true);

  // --- 2. Do nothing at all, past this session's own TTL ---------------
  // No clicks, no keys, no navigation — the #916 scenario is a clinician
  // reading a chart, not one driving the UI. Real wall time on the real
  // API's clock; nothing here is faked, mocked or fast-forwarded, and the
  // renewal that has to happen in this window is a real HTTP round trip
  // through the real backend to the real Zitadel.
  await page.waitForTimeout(PAST_TTL_MS);

  // --- 3. The browser is STILL on the app, not bounced to /login -------
  // This is the user-visible harm #916 caused, stated directly: renewal
  // failing sends session-renewal.tsx to /login (window.location.href),
  // mid-consultation. Asserted FIRST because it is the claim the issue was
  // filed about; steps 4-6 then prove it is true for the right reason.
  expect(
    page.url(),
    "the idle session was evicted to /login — renewal did not happen",
  ).not.toContain("/login");
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // --- 4. The cookie in the browser is a DIFFERENT one -----------------
  // A renewal re-mints the session cookie. If this still held the login's
  // own value, no renewal happened and steps 5-6 would be proving nothing
  // but a long TTL. This is what makes the whole spec unable to pass
  // trivially.
  const renewedSession = await sessionCookieValue(page);
  expect(
    renewedSession,
    "the session cookie is byte-identical to the one login minted — no renewal ever ran",
  ).not.toBe(originalSession);

  // --- 5. That renewed session is really accepted by the API -----------
  // Asserted against the Go API DIRECTLY, bypassing apps/shell's `/api/*`
  // rewrite and the browser entirely. apps/shell/middleware.ts admits any
  // request carrying an helivanta_session cookie WITHOUT inspecting it, so
  // the dashboard in step 3 renders just as happily for a session the API
  // refuses on every call — asserting on the heading alone would be exactly
  // the proxy assertion docs/standards/engineering-principles.md §5 forbids,
  // and idle-timeout.spec.ts already documents this repo being bitten by it.
  const accepted = await request.get(`${RENEWAL_API_URL}${PERMISSIONS_PATH}`, {
    headers: { Cookie: `${SESSION_COOKIE}=${renewedSession}` },
  });
  expect(
    accepted.status(),
    "the API must accept the renewed session after a full SESSION_TTL of idleness",
  ).toBe(200);

  // --- 6. …and the ORIGINAL session is genuinely dead -------------------
  // The control for step 5. Without it, step 5 would also pass against a
  // fixture whose SESSION_TTL was silently long (a mistyped
  // SESSION_TTL_TEST_VALUE, an env var that did not reach `go run`), and
  // this spec would go on passing while pinning nothing. A 401 here is
  // proof that the TTL really did elapse during step 2, so the only way
  // step 5's session can be alive is that a renewal minted it.
  const refused = await request.get(`${RENEWAL_API_URL}${PERMISSIONS_PATH}`, {
    headers: { Cookie: `${SESSION_COOKIE}=${originalSession}` },
  });
  expect(
    refused.status(),
    "the ORIGINAL session must be expired by now — if it is not, SESSION_TTL did not reach " +
      "this fixture's API and the assertion above proves nothing",
  ).toBe(401);
});
