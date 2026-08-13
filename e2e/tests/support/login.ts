import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

export type Credentials = { email: string; password: string };

const PASSWORD = "password123";

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

// Drives the form once and reports whether the resulting session is
// actually usable against the API.
async function signInOnce(page: Page, user: Credentials): Promise<boolean> {
  await page.getByLabel("Email").fill(user.email);
  await page.getByLabel("Password").fill(user.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("heading", { name: "Departments" }),
  ).toBeVisible();
  return page.evaluate(async (url) => (await fetch(url)).ok, PERMISSIONS_PROBE);
}

// Shared login flow for every e2e spec. The default account is the
// calling spec's own admin — evaluated per call, so it resolves against
// whichever spec file is currently running.
export async function login(
  page: Page,
  user: Credentials = specAdmin(),
): Promise<void> {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  for (let attempt = 1; attempt <= MAX_SIGN_IN_ATTEMPTS; attempt++) {
    if (await signInOnce(page, user)) return;
    // The cookie holds a token the API refuses; drop it so the next
    // page.goto lands on /login rather than on a dashboard backed by a
    // dead session.
    await page.context().clearCookies();
    await page.waitForTimeout(RETRY_DELAY_MS);
    await page.goto("/login");
  }

  throw new Error(
    `signed in as ${user.email} ${MAX_SIGN_IN_ATTEMPTS} times and the API refused the session every time; ` +
      `this is not the one-second auth_time window — the revocation watermark is refusing a credential minted after it`,
  );
}
