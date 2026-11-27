import { expect, type Page } from "@playwright/test";

// The enrol step (#948) opens on a choice between "Authenticator app" and
// "Security key" whenever the API offers a key (#1898), which the dev and CI
// stacks always do (WEBAUTHN_RP_ID=helivanta.localhost). A spec that enrols
// TOTP picks the authenticator app here, then reads the setup key as before.
export async function chooseAuthenticatorApp(page: Page): Promise<void> {
  await expect(
    page.getByRole("heading", { name: "Set up two-step verification", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Authenticator app", exact: true }).click();
  await expect(page.getByLabel("Authenticator setup key", { exact: true })).toBeAttached();
}
