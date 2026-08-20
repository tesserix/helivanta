import { expect, test } from "@playwright/test";

import { login } from "./support/login";

// #848 Task 8 — the end-to-end proof that an untouched session actually
// stops working, and returns the browser to a real sign-in.
// docs/superpowers/specs/2026-08-16-idle-timeout-design.md D2/D6.
//
// Runs in its own Playwright PROJECT ("idle-timeout",
// e2e/playwright.config.ts) against its OWN API + shell pair
// (`make dev-api-idle-timeout` / `make dev-web-idle-timeout`, see the
// Makefile's "Idle-timeout e2e fixture" comment), booted with
// IDLE_TIMEOUT=20s. That is the only way to prove the feature without
// either sitting for the real 15-minute default or weakening it for
// every other spec and for production — see this file's own port
// constants below for exactly what "own pair" means.
//
// IDLE_API_URL bypasses apps/shell's `/api/*` rewrite entirely, hitting
// the Go API on its own port directly. That is load-bearing for the
// assertion this file exists to make (see CRITICAL below), not a
// convenience: it is the only way to interrogate the captured session
// cookie without any interference from whatever the BROWSER's own
// idle-tracking JS (packages/ui/src/idle-timer.ts) is concurrently doing
// with that same cookie.
const IDLE_API_URL = "http://localhost:8099"; // Makefile's HELIVANTA_IDLE_API_PORT
const IDLE_TIMEOUT_SECONDS = 20; // Makefile's IDLE_TIMEOUT_TEST_VALUE
// Comfortably past the deadline (server clock, real wall time — nothing
// here is mocked) without inflating the run unnecessarily. The dominant
// cost in this spec is TWO real sign-ins plus this wait, not margin, so
// this stays tight rather than generous.
const PAST_DEADLINE_MS = (IDLE_TIMEOUT_SECONDS + 8) * 1000;

const PERMISSIONS_PATH = "/v1/iam/me/permissions"; // no /api prefix: called directly, not via the shell's rewrite

test.setTimeout(150_000); // two real sign-ins + a ~28s idle wait, comfortably inside this

test("an untouched session is refused after IDLE_TIMEOUT and returns to a real sign-in", async ({
  page,
  request,
}) => {
  // --- 1. Sign in, and confirm an authenticated API call succeeds -------
  await login(page);
  const initialCheck = await page.evaluate(
    async (url) => (await fetch(url)).ok,
    "/api" + PERMISSIONS_PATH,
  );
  expect(initialCheck, "a freshly minted session must be accepted").toBe(true);

  // Capture the exact `helivanta_session` cookie value NOW, before any waiting.
  // This is what step 3 below interrogates directly — capturing it here,
  // rather than re-reading it after the wait, is what keeps that
  // assertion honest: if it were re-read afterwards it could just as
  // easily observe a cookie this BROWSER's own idle-tracker JS
  // (packages/ui/src/idle-timer.ts's onExpire, mounted inside
  // HmsShell) already cleared via its own `/logout` call by then,
  // which would prove nothing about the server's idle_deadline check
  // specifically — only that a cleared cookie is rejected, which is a
  // different and much weaker claim.
  const cookies = await page.context().cookies(page.url());
  const sessionCookie = cookies.find((c) => c.name === "helivanta_session");
  expect(sessionCookie, "no helivanta_session cookie after a successful sign-in").toBeTruthy();
  const capturedSession = sessionCookie!.value;

  // --- 2. Do nothing past the window -------------------------------------
  // No pointerdown/keydown/scroll — that is the entire point. Real time,
  // on the real API's clock; nothing here is faked or fast-forwarded.
  await page.waitForTimeout(PAST_DEADLINE_MS);

  // --- 3. The next API call is refused — assert against the API ---------
  // CRITICAL (this file's whole reason to exist): apps/shell/middleware.ts
  // admits any request carrying an `helivanta_session` cookie without
  // inspecting it, so the dashboard renders — heading and all — for a
  // session the API refuses on every call. Asserting on that heading, or
  // on a `/login` redirect the BROWSER's own idle-tracker JS might
  // already have triggered on its own client-side timer, would be
  // exactly the proxy assertion docs/standards/engineering-principles.md
  // §5 forbids and this repo has already been bitten by once.
  //
  // So this calls the Go API directly — bypassing the shell's rewrite,
  // the browser's fetch, and the browser's own idle-tracker entirely —
  // with the EXACT cookie value captured in step 1, and reads the real
  // HTTP response. This is deterministic regardless of what the open
  // page is concurrently doing with that same cookie: it proves
  // authn.Middleware's own idle_deadline check (backend/pkg/authn/authn.go)
  // refuses THIS token, not merely that some cookie, cleared by some
  // other path, no longer works.
  const refused = await request.get(`${IDLE_API_URL}${PERMISSIONS_PATH}`, {
    headers: { Cookie: `helivanta_session=${capturedSession}` },
  });
  expect(refused.status(), "the API must refuse a session past its idle_deadline").toBe(401);
  const body = (await refused.json()) as { error?: string };
  expect(body.error, "refusal must be tagged session_idle, not a different 401 (spec D6)").toBe(
    "session_idle",
  );

  // --- 4. The browser lands on /login with the inactivity wording -------
  // The open page's own idle-tracker (mounted via HmsShell, independent
  // of the direct check above) reaches the same deadline on its own
  // client-side timer and runs its own teardown
  // (packages/ui/src/hms-shell.tsx's endIdleSession) — ending the Helivanta
  // session, ending this browser's Zitadel SSO session, and navigating
  // here. That is a SEPARATE code path from step 3's direct probe, and
  // both must hold for the feature to be real: the server enforces the
  // deadline (step 3) and the browser also detects it and gets itself to
  // a safe, re-authenticating state (this step) — a client that only
  // did the former would leave the previous clinician's dashboard
  // frozen on screen even though every API call against it now fails.
  await page.waitForURL(/\/login/, { timeout: 20_000 });
  await expect(page.getByText("Your session ended after a period of inactivity.")).toBeVisible();

  // --- 5. Signing in again requires credentials --------------------------
  // login() drives Helivanta's own credential form end to end (Email + Password
  // fields, "Sign in" submit) and only resolves once the API accepts the
  // resulting session — RedirectLanding's signIn() forces `prompt=login`
  // on every attempt (apps/shell/app/login/page.tsx), so this can only
  // succeed by actually presenting and submitting credentials again, not
  // by silently reusing anything left over from before the idle timeout.
  await login(page);
  const secondCheck = await page.evaluate(
    async (url) => (await fetch(url)).ok,
    "/api" + PERMISSIONS_PATH,
  );
  expect(secondCheck, "the re-authenticated session must be accepted").toBe(true);
});
