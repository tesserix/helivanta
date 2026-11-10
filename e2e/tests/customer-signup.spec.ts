import { test, expect } from "@playwright/test";

test("new owners can reach signup and choose their own independent pharmacy", async ({ page }) => {
  await page.goto("/signup");
  await expect(page.getByRole("heading", { name: "Create your workspace" })).toBeVisible();
  await page.getByRole("combobox", { name: "Workspace type" }).click();
  await page.getByRole("option", { name: "Independent pharmacy" }).click();
  await page.getByRole("combobox", { name: "State" }).click();
  await page.getByRole("option", { name: "Odisha" }).click();
  await expect(page.getByLabel("Owner email")).toBeVisible();
  await expect(page.getByRole("button", { name: "Create workspace" })).toBeVisible();
  await expect(page.getByText("Self-service signup is coming soon")).toHaveCount(0);
});

test("signup never reports readiness when its provisioner is unavailable", async ({ page }) => {
  await page.route("**/api/v1/auth/signup", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({
        error: "onboarding_unavailable",
        message: "Workspace setup is temporarily unavailable.",
      }),
    }),
  );
  await page.goto("/signup");
  await expect(page.getByText("Workspace setup is unavailable. Try again.")).toBeVisible();
  await expect(page.getByText("Your workspace is ready")).toHaveCount(0);
});
