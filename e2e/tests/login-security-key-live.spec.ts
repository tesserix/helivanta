import { test, expect } from "@playwright/test";

test("security-key continuation requires an explicit click and reaches the original native password origin", async ({
  page,
  baseURL,
}) => {
  test.skip(
    baseURL !== "https://helivanta.app",
    "Requires the real issuer and its live native Login V2 page; credentials are never submitted to it.",
  );
  await page.goto("/login?start=1");
  await page.waitForURL((url) => url.searchParams.has("authRequest"));
  const request = new URL(page.url()).searchParams.get("authRequest")!;
  const native = new URL("https://auth.tesserix.app/ui/v2/login/password");
  native.searchParams.set("requestId", `oidc_${request}`);
  native.searchParams.set("loginName", "qa-owner@example.test");
  await page.route("**/api/v1/auth/login/password", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ security_key_required: { url: native.href } }),
    }),
  );
  await page.getByLabel("Email or username").fill("qa-owner@example.test");
  await page.getByLabel("Password", { exact: true }).fill("synthetic-ui-only-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  const action = page.getByRole("button", { name: "Continue with security key" });
  await expect(action).toBeVisible();
  expect(new URL(page.url()).origin).toBe("https://helivanta.app");
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
  await expect(page.getByText(/where your key is registered/)).toBeVisible();
  await action.click();
  await page.waitForURL((url) => url.origin === native.origin && url.pathname === native.pathname);
  expect(new URL(page.url()).searchParams.get("requestId")).toBe(`oidc_${request}`);
  await expect(page.locator("input[type=password]")).toBeVisible();
});
