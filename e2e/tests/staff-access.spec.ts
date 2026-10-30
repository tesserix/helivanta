import { test, expect } from "@playwright/test";
import { randomBytes, randomUUID } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { ZITADEL_ORIGIN } from "./support/hosts";
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

test("administrator first-enrolls a verified identity without business permissions", async ({
  page,
  browser,
}) => {
  // Authorized local setup, not evidence of a recipient verifying contact.
  const issuer = new URL(ZITADEL_ORIGIN);
  if (
    !["localhost", "127.0.0.1", "[::1]"].includes(issuer.hostname) &&
    !issuer.hostname.endsWith(".localhost")
  )
    throw new Error("Enrollment fixtures require a local identity provider.");
  const secretDir = resolve(__dirname, "../../dev/zitadel/secrets");
  const pat = readFileSync(join(secretDir, "helivanta-seed.pat"), "utf8").trim();
  const email = `e2e-enrollment-${randomUUID()}@helivanta.dev`;
  const password = randomBytes(24).toString("base64url") + "Aa1!";
  const created = await fetch(`${ZITADEL_ORIGIN}/v2/users/human`, {
    method: "POST",
    headers: { Authorization: `Bearer ${pat}`, "Content-Type": "application/json" },
    body: JSON.stringify({
      username: email,
      profile: { givenName: "Synthetic", familyName: "Enrollment" },
      email: { email, isVerified: true },
      password: { password, changeRequired: false },
    }),
  });
  expect(created.ok, `Synthetic identity creation status ${created.status}`).toBe(true);
  const identity = (await created.json()) as { userId?: string };
  if (!identity.userId)
    throw new Error("Synthetic identity creation did not return its account ID.");
  const subject = identity.userId;
  writeFileSync(
    join(secretDir, `staff-enrollment-${randomUUID()}.json`),
    JSON.stringify({ email, password, subject, synthetic: true }, null, 2) + "\n",
    { mode: 0o600, flag: "wx" },
  );
  const context = await browser.newContext();
  try {
    await login(page);
    await page.goto("/workforce/access");
    await page.getByLabel("Existing account ID", { exact: true }).fill(subject);
    await page
      .getByLabel("Enrollment reason", { exact: true })
      .fill("Synthetic verified account first enrollment");
    await page.getByLabel("I understand this creates ordinary membership only.").check();
    const accepted = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname.startsWith("/api/v1/iam/membership-enrollments/") &&
        r.request().method() === "PUT",
    );
    await page.getByRole("button", { name: "Enroll staff member", exact: true }).click();
    const response = await accepted;
    expect(response.status()).toBe(202);
    expect(response.request().postDataJSON()).toEqual({
      subject,
      reason: "Synthetic verified account first enrollment",
    });
    expect(response.request().headers()["x-helivanta-expected-tenant"]).toBeTruthy();
    expect(response.request().headers()["x-helivanta-expected-subject"]).toBeTruthy();
    await expect(
      page.getByRole("status").filter({ hasText: "Membership recorded for" }),
    ).toContainText(subject);
    await expect(
      page.getByText("Access synchronization may still be pending.", { exact: false }),
    ).toBeVisible();
    const member = await context.newPage();
    await expect(async () => {
      await login(member, { email, password });
    }).toPass({ timeout: 30000 });
    const after = await member.request.get("/api/v1/iam/me/permissions");
    expect(after.status()).toBe(200);
    expect((await after.json()).data).toEqual([]);
  } finally {
    await context.close();
  }
});
