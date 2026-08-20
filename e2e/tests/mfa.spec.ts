import { expect, test, type Page } from "@playwright/test";

import { generateTOTP, readSeededTOTPSecret } from "./support/totp";

// #867 Task 6 — the end-to-end proof that a real TOTP login completes
// against the live dev Zitadel, not a mock. Tasks 1-5 built the backend
// factor check (POST /v1/auth/login/factor, backend/internal/modules/iam/
// loginui.go's Factor handler) and the OTP step
// (apps/shell/app/login/page.tsx's OtpStep, on top of @tesserix/web's
// AuthOtpStep) behind unit tests with mocked responses. This spec is the
// one place nothing is mocked: mfa@helivanta.dev is a real Zitadel user
// with a real, verified TOTP factor (scripts/seed-dev.mjs's
// ensureTOTPEnrolled), and the code submitted here is computed from the
// SAME secret Zitadel itself issued at enrolment
// (e2e/tests/support/totp.ts's generateTOTP — an independent
// implementation of the RFC 6238 algorithm, not a value copied out of
// Zitadel's response).
//
// docs/superpowers/spikes/2026-08-17-zitadel-login-client-mfa.md §2 is
// this spec's whole reason to exist: PATCH /v2/sessions rotates the
// session token on every factor check, and loginui.go's Factor handler
// must finalize with THAT rotated token, never the one Password's step
// stashed. A fixture that ignores the request and always returns success
// cannot catch a handler that skips the rotation — only a real Zitadel
// session, checked by a real code, can prove UpdateToken's write actually
// gates CompleteAfterFactor. Task 6 Step 4 (see the brief) temporarily
// guts that write and reruns this spec alone to confirm it is what makes
// the difference between passing and failing.
//
// This account is NOT derived via e2e/tests/support/login.ts's specEmail
// pattern (`e2e-<slug>-<kind>@helivanta.dev`) — it is named like the two
// human accounts (test@/pharmacist@helivanta.dev) because it belongs to
// exactly one spec file. Nothing else ever signs in as it, so the
// revocation-watermark isolation specEmail exists for (see that file's
// header comment) is satisfied trivially, without borrowing its naming.
const MFA_EMAIL = "mfa@helivanta.dev";
// MUST match scripts/seed-dev.mjs's PASSWORD constant exactly, mirroring
// support/login.ts's own duplicated constant and its header comment on
// why this is a literal, not an import.
const MFA_PASSWORD = "HmsDev123!";

const PERMISSIONS_PROBE = "/api/v1/iam/me/permissions";

// Drives Helivanta's own two-step sign-in (credential form, then OTP step)
// up to and including landing on the OTP step — the accessible names
// (`Email`, `Password`, `Sign in`, `Verification code`) are the SAME
// contract e2e/tests/support/login.ts's login() pins for the credential
// step (spec D6/D7); the OTP step's label comes from @tesserix/web's
// AuthOtpStep default (`label = "Verification code"`), which
// apps/shell/app/login/page.tsx's OtpStep does not override.
async function signInAndReachOtpStep(page: Page): Promise<void> {
  await page.goto("/");
  const landingButton = page.getByRole("button", { name: "Sign in" });
  await landingButton.waitFor({ state: "visible", timeout: 20_000 });
  await landingButton.click();

  await expect(page.getByLabel("Email or username", { exact: true })).toBeVisible({
    timeout: 15_000,
  });
  await page.getByLabel("Email or username", { exact: true }).fill(MFA_EMAIL);
  // exact: true — see support/login.ts's signInOnce for why an unqualified
  // getByLabel("Password") is ambiguous against @tesserix/web 2.2.1's
  // "Show password" toggle button.
  await page.getByLabel("Password", { exact: true }).fill(MFA_PASSWORD);
  // Disambiguated from the landing page's identically-named button the
  // same way login.ts's signInOnce is: only one "Sign in" button exists
  // on screen once the Email field is visible.
  await page.getByRole("button", { name: "Sign in" }).click();

  // A correct password against an org policy requiring TOTP answers
  // `{ factor_required: ["totp"] }` (loginui.go's Password handler) —
  // OtpStep is what CredentialForm's onFactorRequired mounts.
  await expect(page.getByLabel(/verification code/i)).toBeVisible({ timeout: 20_000 });
}

test.setTimeout(60_000);

test("a correct TOTP code completes sign-in, provable against the API", async ({ page }) => {
  await signInAndReachOtpStep(page);

  const secret = readSeededTOTPSecret();
  const code = generateTOTP(secret);

  // AuthOtpStep auto-submits once the 6th digit lands (its own
  // useEffect — see auth-mfa.mjs) — no button to click. A correct code
  // reaches loginui.go's Factor handler, which checks it against the
  // Zitadel session Password's step stashed (loginclient.VerifyTOTP),
  // threads the ROTATED token through UpdateToken (spec D3 — see this
  // file's header comment), and finalizes via CompleteAfterFactor.
  await page.getByLabel(/verification code/i).fill(code);

  // Synchronization only, not the assertion this spec exists to make: a
  // successful factor check navigates via window.location.assign(
  // result.callbackUrl) through apps/shell's own /api/auth/callback route
  // (AuthCallbackPage), which exchanges the code, calls exchangeIdToken()
  // to set the helivanta_session cookie, and ONLY THEN does
  // router.replace("/") — see that file's own effect. Waiting for the
  // path to actually reach "/" (not merely "left /login") is what avoids
  // racing the probe below against exchangeIdToken(): the callback route
  // itself sits at /api/auth/callback for that whole exchange and renders
  // "Signing you in…", which is not /login but also does not yet have a
  // cookie the API will accept — observed live failing the probe below
  // when this waited for anything looser.
  await page.waitForURL((url) => url.pathname === "/", { timeout: 30_000 });

  // Assert against the API, not a rendered heading — idle-timeout.spec.ts
  // (e2e/tests/idle-timeout.spec.ts) records why: apps/shell's own
  // middleware.ts admits any request carrying a helivanta_session cookie
  // without inspecting it, so a dashboard heading would render even for a
  // session the API refuses. This is the actual proof a real Helivanta
  // session — minted from a genuinely verified TOTP factor, against the
  // live Zitadel, with the token rotation this task exists to guard —
  // exists and is accepted.
  const accepted = await page.evaluate(async (url) => (await fetch(url)).ok, PERMISSIONS_PROBE);
  expect(accepted, "a session finalized via a correct TOTP code must be accepted by the API").toBe(
    true,
  );
});

// Spec D5/D6, extended to the factor step: a wrong TOTP code answers with
// the IDENTICAL status/body/timing a wrong password does
// (h.respondEqualisedFailure, loginui.go's Factor handler) — never a
// TOTP-specific message, which would confirm to a caller that the
// password step already succeeded. apps/shell/app/login/login.test.tsx
// pins this at the unit level with a mocked 401; this is its
// live-Zitadel counterpart: a code that really is wrong (computed from a
// DIFFERENT 30-second step than the current one, so it is wrong by
// construction rather than by chance) still leaves the clinician on the
// OTP step to retry, per spec D6's five-guess budget.
test("a wrong TOTP code keeps the clinician on the OTP step with the shared refusal", async ({
  page,
}) => {
  await signInAndReachOtpStep(page);

  const secret = readSeededTOTPSecret();
  const correctCode = generateTOTP(secret);
  // Guaranteed wrong: a sibling 30-second step's code, not a guessed
  // literal that could coincidentally equal the real one.
  const wrongCode = generateTOTP(secret, Date.now() - 10 * 60 * 1000);
  expect(
    wrongCode,
    "the sibling-step code must differ from the real one to be a genuine wrong guess",
  ).not.toBe(correctCode);

  await page.getByLabel(/verification code/i).fill(wrongCode);

  // Filtered on text, not a bare getByRole("alert"): Next.js 16 renders
  // its own empty `role="alert"` route announcer
  // (`#__next-route-announcer__`) on every page, so an unfiltered
  // getByRole("alert") resolves to two elements and throws Playwright's
  // strict-mode violation the moment both are present — observed live.
  const alert = page.getByRole("alert").filter({ hasText: "email or password is incorrect" });
  await expect(alert).toBeVisible({ timeout: 15_000 });
  // Still on the OTP step — a wrong code is a refusal, not an expiry;
  // spec D6 allows five wrong guesses before the attempt is exhausted.
  await expect(page.getByLabel(/verification code/i)).toBeVisible();
});
