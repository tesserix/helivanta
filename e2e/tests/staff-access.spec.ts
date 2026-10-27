import { test, expect } from "@playwright/test";
import { login, specPharmacist } from "./support/login";

test("tenant administrator can open the reviewed assignment workspace", async ({ page }) => {
  await login(page);
  const assignments = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api/v1/iam/assignment-proposals",
  );
  await page.goto("/workforce/access");
  const response = await assignments;
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toContain("no-store");
  await expect(page.getByRole("heading", { name: "Staff access", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Refresh assignments" })).toBeEnabled();
  await page.getByRole("button", { name: "Propose assignment", exact: true }).click();
  await expect(page.getByRole("form", { name: "Propose assignment" })).toBeVisible();
  await page.getByRole("button", { name: "Submit proposal" }).click();
  await expect(page.getByLabel("Account ID", { exact: true })).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await expect(page.getByLabel("Reason", { exact: true })).toHaveAttribute("aria-invalid", "true");
  await expect(page.getByRole("button", { name: "Close form" })).toBeEnabled();
});

test("a member without assignment administration sees a restricted state", async ({ page }) => {
  const privateRequests: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.startsWith("/api/v1/iam/assignment-proposals"))
      privateRequests.push(request.url());
  });
  await login(page, specPharmacist());
  await page.goto("/workforce/access");
  await expect(
    page.getByText("Assignment administration is not available for this account."),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Propose assignment", exact: true })).toHaveCount(
    0,
  );
  await expect(page.getByRole("button", { name: "Approve", exact: true })).toHaveCount(0);
  expect(privateRequests).toHaveLength(0);
});
