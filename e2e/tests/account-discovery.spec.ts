import { expect, test } from "@playwright/test";

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
  await page.goto("/login?start=1");
  await expect(page.getByLabel("Workspace ID")).toHaveCount(0);
  await expect(page.getByText("Test hospital")).toHaveCount(0);
  await page.getByLabel("Email", { exact: true }).fill("person@example.test");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByText("Test hospital")).toHaveCount(0);
  await page.getByLabel("Email verification code").fill("12345678");
  await page.getByRole("button", { name: "Verify email" }).click();
  await expect(page.getByRole("heading", { name: "Choose an account" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Test hospital/ })).toBeVisible();
  await expect(page.getByRole("button", { name: /Test independent pharmacy/ })).toBeVisible();
});
