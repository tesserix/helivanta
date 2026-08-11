import { expect, type Page } from "@playwright/test";

export type Credentials = { email: string; password: string };

// The two users `make seed` creates (scripts/seed-dev.mjs): the
// bootstrap tenant_admin and a pharmacist with a single role, used to
// exercise permission gating end to end.
export const ADMIN: Credentials = {
  email: "test@hms.dev",
  password: "password123",
};
export const PHARMACIST: Credentials = {
  email: "pharmacist@hms.dev",
  password: "password123",
};

// Shared login flow for every e2e spec — introduced in smoke.spec.ts,
// extracted here so journey.spec.ts (and any future spec) reuses it
// instead of hand-rolling a second copy.
export async function login(
  page: Page,
  user: Credentials = ADMIN,
): Promise<void> {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email").fill(user.email);
  await page.getByLabel("Password").fill(user.password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("heading", { name: "Departments" }),
  ).toBeVisible();
}
