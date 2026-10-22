import { expect, test } from "@playwright/test";

const api = process.env.EMERGENCY_BROWSER_API;
if (!api || new URL(api).hostname !== "127.0.0.1")
  throw new Error("Run through the disposable Go browser fixture");

test.afterEach(async ({ context }) => {
  await context.unrouteAll({ behavior: "wait" });
});

test("staff configure a base, receive and close a report, then revoke case access", async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error("Local browser origin is required");
  await context.addCookies([
    { name: "helivanta_session", value: "synthetic-browser-session", url: baseURL },
  ]);
  // Only authentication and transport are substituted. Every domain response comes
  // from the real Go handlers backed by disposable Postgres, OpenBao and NATS.
  await context.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch({
      url: `${api}${url.pathname.slice(4)}${url.search}`,
      headers: { ...route.request().headers(), authorization: "Bearer admin" },
    });
    await route.fulfill({ response });
  });
  await page.goto("/emergency");
  await page.getByLabel("Operating base name", { exact: true }).fill("Synthetic Pilot Base");
  await page.getByLabel("Reviewed policy reference", { exact: true }).fill("SYN-TEST-ONLY-01");
  await page.getByRole("button", { name: "Save base", exact: true }).click();
  await expect(
    page
      .getByRole("listitem")
      .filter({ has: page.getByRole("heading", { name: "Synthetic Pilot Base", exact: true }) })
      .getByText("No case access", { exact: false }),
  ).toBeVisible();
  await page.getByLabel("Verified identity subject", { exact: true }).fill("user-admin");
  await page.getByRole("combobox", { name: "Base access level", exact: true }).click();
  await page.getByRole("option", { name: "Supervisor", exact: true }).click();
  await page.getByRole("button", { name: "Grant access", exact: true }).click();
  await page.getByRole("button", { name: "Open Synthetic Pilot Base", exact: true }).click();
  await page.getByLabel("Caller name (optional)", { exact: true }).fill("Synthetic Browser Caller");
  await page.getByLabel("Incident location (optional)", { exact: true }).fill("Synthetic Gate C");
  await page
    .getByLabel("Reported situation (optional)", { exact: true })
    .fill("Synthetic report for software verification only");
  await page.getByRole("button", { name: "Receive report", exact: true }).click();
  await expect(page.getByRole("link", { name: /Open received case/ })).toBeVisible();
  await expect(page.getByText("Synthetic Browser Caller", { exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: /Open received case/ }).click();
  await expect(page).toHaveURL(/\/emergency\/bases\/[^/]+\/cases\/[^/]+$/);
  const caseURL = page.url();
  await expect(page.getByText("Synthetic Browser Caller", { exact: true })).toBeVisible();
  await expect(page.getByText("Synthetic Gate C", { exact: true })).toBeVisible();
  await page
    .getByLabel("Review explanation", { exact: true })
    .fill("Synthetic software verification completed; no real emergency or dispatch");
  await page.getByRole("button", { name: "Close report", exact: true }).click();
  await page.getByRole("button", { name: "Confirm close", exact: true }).click();
  await expect(page.getByText("Closure review:", { exact: false })).toBeVisible();
  await expect(page.getByRole("button", { name: "Close report", exact: true })).toHaveCount(0);
  await expect(page.getByText("Report closed", { exact: true })).toBeHidden({ timeout: 10000 });
  await page.getByRole("heading", { name: /^EC-/, level: 2 }).scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "../docs/architecture/evidence/ambulanceconnect-2026-10-18/staff-mobile.png",
    fullPage: true,
  });
  await page.getByRole("link", { name: "Back to operating bases", exact: true }).click();
  await page.getByRole("button", { name: "Configure Synthetic Pilot Base", exact: true }).click();
  await page.getByLabel("Verified identity subject", { exact: true }).fill("user-admin");
  await page.getByRole("button", { name: "Revoke access", exact: true }).click();
  await page.getByRole("button", { name: "Confirm revoke", exact: true }).click();
  await expect(page.getByText("Access revoked", { exact: true })).toBeVisible();
  await page.goto(caseURL);
  await expect(page.getByRole("alert").filter({ hasText: /not found/ })).toBeVisible();
  await expect(page.getByText("Synthetic Browser Caller", { exact: true })).toHaveCount(0);
});
