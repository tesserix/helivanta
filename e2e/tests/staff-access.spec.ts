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
  await expect(page.getByRole("form", { name: "Staff membership enrollment" })).toHaveCount(0);
});

test("administrator enrolls an existing identity without adding business permissions", async ({
  page,
  browser,
}) => {
  const context = await browser.newContext();
  try {
    const member = await context.newPage();
    await login(member, specPharmacist());
    const before = await member.request.get("/api/v1/iam/me/permissions");
    expect(before.status()).toBe(200);
    const identity = await before.json();
    expect(identity.subject).toBeTruthy();
    expect(identity.tenant_id).toBeTruthy();
    await login(page);
    await page.goto("/workforce/access");
    await page.getByLabel("Existing account ID", { exact: true }).fill(identity.subject);
    const accepted = page.waitForResponse(
      (r) => new URL(r.url()).pathname === "/api/v1/iam/members" && r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Enroll staff member", exact: true }).click();
    const response = await accepted;
    expect(response.status()).toBe(202);
    expect(response.request().postDataJSON()).toEqual({
      subject: identity.subject,
      role_key: "staff_member",
    });
    expect(response.request().headers()["x-helivanta-expected-tenant"]).toBe(identity.tenant_id);
    await expect(
      page.getByRole("status").filter({ hasText: "Membership recorded for" }),
    ).toContainText(identity.subject);
    await expect(
      page.getByText("Access synchronization may still be pending.", { exact: false }),
    ).toBeVisible();
    const after = await member.request.get("/api/v1/iam/me/permissions");
    expect(after.status()).toBe(200);
    expect((await after.json()).data.sort()).toEqual(identity.data.sort());
  } finally {
    await context.close();
  }
});
