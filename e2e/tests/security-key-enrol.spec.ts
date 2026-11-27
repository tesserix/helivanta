import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";

import { SHELL_PORT, ZITADEL_ORIGIN, webOrigin } from "./support/hosts";
import { SEEDED_PASSWORD as PASSWORD } from "./support/login";
import { signOut } from "./support/sign-out";

// #1898 slice 1: a security key is registered on Helivanta's own enrol step
// and then signs in there, against the live dev Zitadel, with nothing mocked.
// Chromium's virtual authenticator (CDP WebAuthn domain) stands in for the
// key, so both ceremonies are real WebAuthn against RP ID helivanta.localhost.
//
// security-key@helivanta.dev is scripts/seed-dev.mjs's SECURITY_KEY_EMAIL: its
// organisation forces MFA and the seed removes every second factor it holds,
// so each run starts on the enrol step. Like mfa@helivanta.dev, it belongs to
// this spec alone.
const EMAIL = "security-key@helivanta.dev";
// The seed homes the account in the second hospital's organisation.
const TENANT = "22222222-2222-2222-2222-222222222222";
const KEY_NAME = "E2E virtual key";
const WEB_ORIGIN = webOrigin(SHELL_PORT);

async function signIn(page: Page) {
  await page.goto(`/login?workspace=${TENANT}`);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByLabel("Email or username", { exact: true }).fill(EMAIL);
  await page.getByLabel("Password", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
}

// The seed's IAM_OWNER PAT, the credential staff-access.spec.ts reads Zitadel with.
function seedPAT() {
  return readFileSync(
    resolve(__dirname, "../../dev/zitadel/secrets/helivanta-seed.pat"),
    "utf8",
  ).trim();
}

test.setTimeout(120_000);

test("a security key registered on Helivanta's enrol step signs in there again", async ({
  page,
}) => {
  const issuer = new URL(ZITADEL_ORIGIN);
  // Every main-frame URL, so the end can prove Zitadel's hosted login never
  // appeared: its pages live under /ui/ on the issuer's host.
  const visited: URL[] = [];
  page.on("framenavigated", (frame) => {
    if (frame === page.mainFrame()) visited.push(new URL(frame.url()));
  });

  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2",
      transport: "usb",
      hasResidentKey: false,
      hasUserVerification: true,
      isUserVerified: true,
    },
  });

  await signIn(page);
  await expect(
    page.getByRole("heading", { name: "Set up two-step verification", exact: true }),
  ).toBeVisible({ timeout: 20_000 });
  await page.getByLabel("Key name", { exact: true }).fill(KEY_NAME);
  // Registers the key (create), then confirms it (get): two touches, both
  // answered by the virtual authenticator.
  await page.getByRole("button", { name: "Security key", exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/dashboard", { timeout: 35_000 });

  // The session is real: the API accepts it.
  const me = await page.request.get("/api/v1/iam/me/permissions");
  expect(me.status()).toBe(200);
  const { subject, tenant_id } = (await me.json()) as { subject: string; tenant_id: string };
  expect(tenant_id).toBe(TENANT);

  // Zitadel holds exactly one key for the account, verified and named.
  const factors = await fetch(
    `${ZITADEL_ORIGIN}/management/v1/users/${subject}/auth_factors/_search`,
    {
      method: "POST",
      redirect: "error",
      signal: AbortSignal.timeout(10_000),
      headers: {
        Authorization: `Bearer ${seedPAT()}`,
        "Content-Type": "application/json",
        "x-zitadel-orgid": `helivanta-${TENANT}`,
      },
      body: "{}",
    },
  );
  expect(factors.status).toBe(200);
  const { result = [] } = (await factors.json()) as {
    result?: { state: string; u2f?: { id: string; name: string } }[];
  };
  expect(result).toHaveLength(1);
  expect(result[0]!.state).toBe("AUTH_FACTOR_STATE_READY");
  expect(result[0]!.u2f?.name).toBe(KEY_NAME);

  await signOut(page);
  await page.waitForURL(`${WEB_ORIGIN}/login`, { timeout: 15_000 });
  // Native auth_time has whole-second precision; cross the sign-out's
  // revocation cutoff before signing in again (staff-mfa-native.ts does the same).
  await page.waitForTimeout(1100);

  // The registered key is asked for straight away on the factor step.
  await signIn(page);
  await page.waitForURL((url) => url.pathname === "/dashboard", { timeout: 35_000 });
  expect((await page.request.get("/api/v1/iam/me/permissions")).status()).toBe(200);

  const hosted = visited.filter(
    (url) => url.host === issuer.host && url.pathname.startsWith("/ui/"),
  );
  expect(hosted.map(String)).toEqual([]);
});
