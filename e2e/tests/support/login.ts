import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

export type Credentials = { email: string; password: string };

// MUST match scripts/seed-dev.mjs's PASSWORD constant exactly — it seeds
// every account this file derives credentials for. "password123" (the
// old GIP-emulator value) fails Zitadel's default complexity policy;
// seed-dev.mjs's own comment records that finding.
const PASSWORD = "HmsDev123!";

// NO SPEC FILE MAY SHARE A LOGIN ACCOUNT WITH ANOTHER SPEC FILE.
//
// Since #781, signing out writes a revocation watermark that is per
// SUBJECT and global across every tenant and every device (design spec
// D5: "signing out anywhere signs out everywhere"). So a spec that signs
// out does not merely end its own session — it invalidates every live
// session for that account, including sessions other spec files are in
// the middle of using. Under Playwright's default parallel workers that
// is a race; run serially it is a certainty, because the next spec's
// session was minted before the sign-out.
//
// The accounts are therefore DERIVED FROM THE SPEC FILENAME rather than
// picked from a shared list, so a new spec cannot get this wrong by
// omission. scripts/seed-dev.mjs enumerates the same *.spec.ts files and
// seeds the matching pair, so adding a spec file and re-running
// `make seed` is the whole procedure. If the two derivations ever drift,
// sign-in fails loudly on an unknown account — never silently falls back
// to sharing one.
function specSlug(): string {
  return path.basename(test.info().file).replace(/\.spec\.ts$/, "");
}

// The spec's tenant_admin: sees every zone in the first hospital and
// holds `pharmacist` in a second one, which is what makes the hospital
// picker render and tenant switching observable.
export function specAdmin(): Credentials {
  return { email: `e2e-${specSlug()}-admin@helivanta.dev`, password: PASSWORD };
}

// The spec's single-role user, for asserting that permission gating
// actually withholds things. Everything the admin above can do, this
// account mostly cannot.
export function specPharmacist(): Credentials {
  return { email: `e2e-${specSlug()}-pharmacist@helivanta.dev`, password: PASSWORD };
}

// The cheapest authenticated API call the browser can make. Reaching the
// Go API is the only way to know a session is real: apps/shell's
// middleware.ts admits any request carrying an `helivanta_session` cookie
// without inspecting it, so the dashboard renders — heading and all —
// for a session the API refuses on every call. Asserting on the heading
// alone is precisely the proxy assertion
// docs/standards/engineering-principles.md §5 forbids, and it is what let
// a revoked session read as a successful login here.
const PERMISSIONS_PROBE = "/api/v1/iam/me/permissions";

// Sign-in is retried because refusal here is CORRECT behaviour, not
// flake. `auth_time` has one-second granularity and the watermark check
// is deliberately not-after (spec D2), so a sign-in landing in the same
// wall-clock second as a revocation of the same subject is refused —
// fail-closed, by design. Two tests in one spec file run back-to-back in
// the same worker, well inside a second, so a spec whose first test signs
// out reliably lands its second test's sign-in in that window. Waiting
// past the second and re-authenticating produces a later `auth_time`;
// nothing is weakened, because no assertion anywhere claims a sign-in
// inside that window must succeed. If the watermark were genuinely
// broken, every attempt fails and this throws.
const MAX_SIGN_IN_ATTEMPTS = 4;
const RETRY_DELAY_MS = 1_100;

// Drives Helivanta's OWN credential form — apps/shell/app/login/page.tsx's
// CredentialForm, reached at /login?authRequest=... after
// RedirectLanding's Sign in button bounces the browser through Zitadel's
// /oauth/v2/authorize and straight back (no hosted-UI page is ever
// rendered in between; see this file's header comment). Design spec D6
// makes the form's labels (`Email or username`, `Password`) and its submit
// button's
// accessible name (`Sign in`) a CONTRACT — apps/shell/app/login/page.tsx
// says so explicitly and warns not to rename them without updating this
// file deliberately. Unlike Zitadel's old two-step hosted UI
// (Loginname → next → Password → continue), this is ONE step: both
// fields are on screen together and one submit finishes it.
// `exact: true` on the login-name label is deliberate (#899). The label
// widened from "Email" to "Email or username", and Playwright's getByLabel
// substring-matches by default — so the OLD selector would still have
// matched the NEW label and this contract would have gone on passing while
// silently no longer pinning anything. Matching exactly is what keeps a
// future rename a visible failure here rather than a quiet drift.
async function signInOnce(page: Page, user: Credentials): Promise<boolean> {
  await page.getByLabel("Email or username", { exact: true }).fill(user.email);
  // exact: true — @tesserix/web 2.2.1 (#866/#868) added a "Show password"
  // toggle button to AuthCredentialForm's password field, and Playwright's
  // getByLabel does SUBSTRING matching by default: "Password" matches that
  // button's aria-label ("Show password") too, so an unqualified
  // getByLabel("Password") resolves to two elements and throws Playwright's
  // strict-mode violation. Observed live (#867 Task 6) failing every spec
  // that reaches this line, not just the one being added — this is a
  // locator fix, not a behavior change; the field's accessible name is
  // still exactly "Password" (spec D6/D7's contract, unchanged).
  await page.getByLabel("Password", { exact: true }).fill(user.password);
  await page.getByRole("button", { name: "Sign in" }).click();

  // The credential form's submit button is also named "Sign in" — same
  // accessible name as RedirectLanding's landing-page button that
  // startSignIn() clicked to get here. Waiting for the login-name field to be
  // visible before this function is ever called (see login()'s caller)
  // is what disambiguates the two; by the time signInOnce() clicks
  // "Sign in" here, it is unambiguously the form's submit, because only
  // one such button exists on screen at a time.
  //
  // Helivanta's own page navigates on to apps/shell/app/api/auth/callback/page.tsx
  // (via callback_url) or, for the handoff outcomes (MFA, forced
  // password change, federated IdP — see CredentialForm's header
  // comment), onward through Zitadel again before landing on the same
  // callback. Longer than the 5s default on purpose: this can cross a
  // POST to the API, a cookie write and a client-side redirect into a
  // route the dev server may still be compiling — under four parallel
  // workers that whole chain has been observed taking over five seconds.
  // A timeout here is not a refused session — it throws past the retry
  // loop below and fails the spec with a missing heading, which reads
  // like a broken application rather than a slow one.
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible({
    timeout: 30_000,
  });
  return page.evaluate(async (url) => (await fetch(url)).ok, PERMISSIONS_PROBE);
}

// Clicks the landing page's Sign in button if we are sitting on it.
//
// TWO buttons on this suite's path share the exact accessible name
// "Sign in": RedirectLanding's (this one — starts the round trip through
// Zitadel's /oauth/v2/authorize and back) and CredentialForm's (the
// form's own submit, clicked later by signInOnce()). They are never on
// screen at the same time — the landing page's button disappears the
// moment the browser leaves for Zitadel, and the form's button does not
// exist until `?authRequest=` lands back on /login — so a bare
// `getByRole("button", { name: "Sign in" })` here is unambiguous PROVIDED
// this function is only ever called while still on the landing page.
// login() enforces that ordering by waiting for the login-name field (proof
// the form, not the landing page, is now showing) before ever calling
// signInOnce(); mixing the two up here would click whichever button
// happens to exist, which is exactly the trap.
//
// Tolerant on purpose: after a sign-out the browser may already be
// mid-redirect to Zitadel, in which case there is no button to click and
// the credential form is on its way. Requiring the button unconditionally
// would turn that ordinary race into a flake.
// Zitadel's fixed local origin host (HELIVANTA_ZITADEL_HOST:HELIVANTA_ZITADEL_PORT)
// — the one constant across every Helivanta web+API pair the suite runs
// against. Checking for THIS, rather than for Helivanta's own dev-server
// port, is what makes startSignIn work under the "idle-timeout" (#848 Task
// 8) and "renewal" (#916 Task 4) Playwright projects, which point baseURL
// at further shell instances on their own ports
// (e2e/playwright.config.ts). A literal app-port check would misread those
// projects' own pages as "mid-redirect through Zitadel" and never click
// Sign in.
//
// The host is auth.tesserix.localhost, not localhost (#916 Task 4, design
// spec D6): the IdP deliberately sits on a DIFFERENT registrable domain
// from the app (helivanta.localhost) so every browser request between them
// is cross-site, as it is in production. Matching on the host alone — not
// the whole origin — keeps this working if HELIVANTA_ZITADEL_PORT is
// shifted in a developer's .env, which is why the port is included here
// only as part of the default host:port string the stock stack serves.
const ZITADEL_ORIGIN_HOST = "auth.tesserix.localhost";

async function startSignIn(page: Page): Promise<void> {
  // Decide by where we actually ARE, not by racing a timeout. Since #847,
  // sign-out lands on Helivanta's /login and STAYS there — nothing redirects
  // onward any more — so on that page the button is not optional and a
  // short "maybe it is already navigating" tolerance would simply give up
  // and leave the caller waiting for a credential field that will never
  // appear. That was the first version of this helper, and it failed
  // exactly the two specs that sign out and back in.
  if (page.url().includes(ZITADEL_ORIGIN_HOST)) return; // mid-redirect through Zitadel

  const button = page.getByRole("button", { name: "Sign in" });
  await button.waitFor({ state: "visible", timeout: 20_000 });
  await button.click();
}

// Shared login flow for every e2e spec. The default account is the
// calling spec's own admin — evaluated per call, so it resolves against
// whichever spec file is currently running.
export async function login(page: Page, user: Credentials = specAdmin()): Promise<void> {
  try {
    await page.goto("/");
  } catch (err) {
    // ERR_ABORTED here means a navigation was ALREADY in flight when this
    // goto fired — the real case is a spec that calls login() again
    // immediately after signing out (e.g. journey.spec.ts's "pharmacist
    // can dispense a visit created by admin"): sign-out ends with
    // signoutRedirect() through Zitadel and back to /login, so by the time
    // this goto("/") runs the browser can already be mid-navigation. That
    // competing navigation heads to the same place this one would, so it
    // is safe to let it win; anything other than ERR_ABORTED is a real
    // failure and must still throw.
    if (!/ERR_ABORTED/.test(String(err))) throw err;
  }
  // Unauthenticated: apps/shell's middleware.ts redirects to /login, which
  // since #847 is a LANDING PAGE with a Sign in button rather than an
  // automatic redirect. The button is deliberate: firing an authorization
  // request on mount raced Zitadel's session teardown after a sign-out, and
  // the next user then could not sign in at all.
  //
  // So the suite must click it. Waiting for the real credential field after
  // the click is the provable claim ("the sign-in form is now showing") and
  // is host/port agnostic, unlike asserting a literal localhost:4301 URL.
  // It is also what disambiguates startSignIn()'s and signInOnce()'s
  // identically-named "Sign in" buttons (see startSignIn()'s comment):
  // only proceeding once the login-name field is visible guarantees the
  // landing page's button is gone and the form's is what gets clicked
  // next.
  await startSignIn(page);
  await expect(
    page.getByLabel("Email or username", { exact: true }),
  ).toBeVisible({
    timeout: 15_000,
  });

  for (let attempt = 1; attempt <= MAX_SIGN_IN_ATTEMPTS; attempt++) {
    if (await signInOnce(page, user)) return;
    // The session the callback minted carries a token the API refuses.
    // `clearCookies()` on a BrowserContext clears cookies for every
    // origin in that context, not just Helivanta's — so this also drops
    // Zitadel's own SSO cookie, and the next /login redirect re-presents
    // a real credential prompt instead of silently carrying the same
    // (refused) identity through again.
    await page.context().clearCookies();
    await page.waitForTimeout(RETRY_DELAY_MS);
    await page.goto("/login");
    await startSignIn(page);
  }

  throw new Error(
    `signed in as ${user.email} ${MAX_SIGN_IN_ATTEMPTS} times and the API refused the session every time; ` +
      `this is not the one-second auth_time window — the revocation watermark is refusing a credential minted after it`,
  );
}
