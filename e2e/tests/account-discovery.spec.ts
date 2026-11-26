import { expect, test } from "@playwright/test";

test("forgotten password starts account selection from unscoped sign-in", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByLabel("Email or username", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Forgot password?" }).click();
  await expect(page).toHaveURL(/\/login\?find=accounts$/);
  await expect(page.getByLabel("Email", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
});

// Browser consumer-contract tests: the mailbox/identity owner is stubbed here.
// Owner proof, eligibility and rate-limit behavior is tested with PostgreSQL in its CI.
test("email discovery keeps choices private until proof and shows named accounts", async ({
  page,
}) => {
  const tenant = "11111111-1111-4111-8111-111111111111";
  await page.route("**/api/v1/auth/accounts", (route) =>
    route.fulfill({
      status: route.request().method() === "POST" ? 202 : 200,
      json:
        route.request().method() === "POST"
          ? { state: "check_email" }
          : {
              accounts: [
                { tenantId: tenant, name: "Test hospital", account: "person@example.test" },
                {
                  tenantId: "22222222-2222-4222-8222-222222222222",
                  name: "Test independent pharmacy",
                  account: "person@example.test",
                },
              ],
            },
    }),
  );
  await page.route("**/api/v1/auth/accounts/verify", (route) =>
    route.fulfill({ status: 200, json: { state: "verified" } }),
  );
  // The emailed-code finder is the fallback to the single card (#1751 D7).
  await page.goto("/login?find=accounts");
  await expect(page.getByLabel("Workspace ID")).toHaveCount(0);
  await expect(page.getByText("Test hospital")).toHaveCount(0);
  await page.getByLabel("Email", { exact: true }).fill("person@example.test");
  await page.getByRole("button", { name: "Send code", exact: true }).click();
  await expect(page.getByText("Test hospital")).toHaveCount(0);
  // The code is MfaCodeForm's digit group (#1854 D2); it submits on the eighth digit.
  await page
    .getByRole("group", { name: "Email verification code", exact: true })
    .getByRole("textbox", { name: "Digit 1", exact: true })
    .pressSequentially("12345678");
  await expect(page.getByRole("heading", { name: "Choose an account" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Test hospital/ })).toBeVisible();
  await expect(page.getByRole("button", { name: /Test independent pharmacy/ })).toBeVisible();
});
