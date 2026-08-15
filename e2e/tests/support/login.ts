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
  return { email: `e2e-${specSlug()}-admin@hms.dev`, password: PASSWORD };
}

// The spec's single-role user, for asserting that permission gating
// actually withholds things. Everything the admin above can do, this
// account mostly cannot.
export function specPharmacist(): Credentials {
  return { email: `e2e-${specSlug()}-pharmacist@hms.dev`, password: PASSWORD };
}

// The cheapest authenticated API call the browser can make. Reaching the
// Go API is the only way to know a session is real: apps/shell's
// middleware.ts admits any request carrying an `hms_session` cookie
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

// Drives Zitadel's REAL hosted login UI — /login (apps/shell) only
// triggers the redirect; every field below lives on auth.tesserix.app
// (or the local dev stack's equivalent), not on an HMS page. Design spec
// D5a is explicit that the suite is written against Zitadel's own
// markup, and that both spikes
// (docs/superpowers/spikes/2026-08-15-zitadel-spike.md) already drove
// this exact two-step form successfully — these selectors are lifted
// from there (scripts/lib/zitadel.mjs's fillAndSubmit /
// hostedUILogin), not re-guessed:
//   1. a "Loginname" step — the label matches /Loginname|Email|Login Name/i
//      across Zitadel's legacy `/ui/login` vs the split `zitadel-login`
//      v2 service — followed by a /next|continue/i button.
//   2. a separate "Password" step, reached only after step 1 submits,
//      followed by a /continue/i button (not "Sign in" — that label
//      belongs to the deleted HMS-rendered form this replaces, per D5a's
//      own contract note: the suite is written against Zitadel's markup
//      first, ours never existed here).
async function fillAndSubmit(
  page: Page,
  labelPattern: RegExp,
  value: string,
  buttonPattern: RegExp,
): Promise<void> {
  const field = page.getByLabel(labelPattern).first();
  const button = page.getByRole("button", { name: buttonPattern }).first();
  await field.waitFor({ state: "visible", timeout: 15_000 });

  // Mirrors scripts/lib/zitadel.mjs's fillAndSubmit retry: observed live
  // during seeding that the login UI's own JS can still be attaching its
  // input handler when Playwright's fill() sets the value, so a submit
  // can land on a button that never un-disables. Re-filling once
  // hydration catches up resolves it — a UI-timing race, not a wrong
  // selector or credential (both of which fail every attempt).
  const attempts = 3;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    await field.fill(value);
    try {
      await button.click({ timeout: 10_000 });
      return;
    } catch (err) {
      if (attempt === attempts) throw err;
    }
  }
}

// Drives the form once and reports whether the resulting session is
// actually usable against the API.
async function signInOnce(page: Page, user: Credentials): Promise<boolean> {
  await fillAndSubmit(page, /Loginname|Email|Login Name/i, user.email, /next|continue/i);
  await page.waitForURL(/\/password\??/, { timeout: 20_000 });
  await fillAndSubmit(page, /Password/i, user.password, /continue|login/i);

  // Zitadel redirects back to apps/shell/app/api/auth/callback/page.tsx,
  // which exchanges the id_token and only then replaces the URL with "/".
  // Longer than the 5s default on purpose: this crosses a hosted-UI
  // redirect, a PKCE token exchange against Zitadel, a second exchange
  // against POST /v1/auth/login, a cookie write and a client-side
  // redirect into a route the dev server may still be compiling — under
  // four parallel workers that whole chain has been observed taking over
  // five seconds. A timeout here is not a refused session — it throws
  // past the retry loop below and fails the spec with a missing heading,
  // which reads like a broken application rather than a slow one.
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible({
    timeout: 30_000,
  });
  return page.evaluate(async (url) => (await fetch(url)).ok, PERMISSIONS_PROBE);
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
    // immediately after signing out (e.g.
    // journey.spec.ts's "pharmacist can dispense a visit created by
    // admin"): sign-out (lib/sign-out.ts) ends with signoutRedirect()
    // through Zitadel and back to /login, and /login's own effect
    // (app/login/page.tsx) immediately redirects onward again — so by the
    // time this goto("/") runs, the browser can already be mid-navigation
    // to Zitadel's hosted login. That competing navigation is heading to
    // the exact same place this one would have, so it is safe to let it
    // win and just wait for the login form below; anything other than
    // ERR_ABORTED is a real failure and must still throw.
    if (!/ERR_ABORTED/.test(String(err))) throw err;
  }
  // Unauthenticated: apps/shell's middleware.ts redirects to /login,
  // which (design spec D5a) immediately redirects again — off-origin, to
  // Zitadel's hosted login. This settles on Zitadel's own
  // /oauth/v2/authorize-derived URL, never on HMS's /login itself, so the
  // old `toHaveURL(/\/login$/)` assertion no longer holds: HMS renders no
  // login page to land on. Waiting for the real login field is both the
  // provable claim ("the sign-in form is now showing") and host/port
  // agnostic, unlike asserting a literal localhost:4301 URL would be.
  await expect(page.getByLabel(/Loginname|Email|Login Name/i).first()).toBeVisible({
    timeout: 15_000,
  });

  for (let attempt = 1; attempt <= MAX_SIGN_IN_ATTEMPTS; attempt++) {
    if (await signInOnce(page, user)) return;
    // The session the callback minted carries a token the API refuses.
    // `clearCookies()` on a BrowserContext clears cookies for every
    // origin in that context, not just HMS's — so this also drops
    // Zitadel's own SSO cookie, and the next /login redirect re-presents
    // a real credential prompt instead of silently carrying the same
    // (refused) identity through again.
    await page.context().clearCookies();
    await page.waitForTimeout(RETRY_DELAY_MS);
    await page.goto("/login");
  }

  throw new Error(
    `signed in as ${user.email} ${MAX_SIGN_IN_ATTEMPTS} times and the API refused the session every time; ` +
      `this is not the one-second auth_time window — the revocation watermark is refusing a credential minted after it`,
  );
}
