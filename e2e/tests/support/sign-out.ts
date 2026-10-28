import type { Page } from "@playwright/test";

// Sign out lives in the header's account menu (#1428).
export async function signOut(page: Page): Promise<void> {
  await page.getByRole("button", { name: /^Account/ }).click();
  await page
    .getByRole("dialog", { name: "Account" })
    .getByRole("button", { name: "Sign out" })
    .click();
}
