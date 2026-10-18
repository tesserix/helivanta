import { expect, test } from "@playwright/test";

const api = process.env.EMERGENCY_BROWSER_API;
if (!api || new URL(api).hostname !== "127.0.0.1")
  throw new Error("Use the disposable Go browser fixture");

test.afterEach(async ({ context }) => {
  await context.unrouteAll({ behavior: "wait" });
});

test("supervisor verifies fleet and duty, records failure and clears it against repair evidence", async ({
  page,
  context,
  baseURL,
}) => {
  if (!baseURL) throw new Error("Local browser origin is required");
  await context.addCookies([
    { name: "helivanta_session", value: "synthetic-fleet-session", url: baseURL },
  ]);
  await context.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch({
      url: `${api}${url.pathname.slice(4)}${url.search}`,
      headers: { ...route.request().headers(), authorization: "Bearer admin" },
    });
    await route.fulfill({ response });
  });
  await page.goto("/emergency");
  await page.getByLabel("Operating base name", { exact: true }).fill("Synthetic Fleet Base");
  await page.getByLabel("Reviewed policy reference", { exact: true }).fill("SYN-FLEET-TEST-ONLY");
  await page.getByRole("button", { name: "Save base", exact: true }).click();
  await page.getByLabel("Verified identity subject", { exact: true }).fill("user-admin");
  await page.getByRole("combobox", { name: "Base access level", exact: true }).click();
  await page.getByRole("option", { name: "Supervisor", exact: true }).click();
  await page.getByRole("button", { name: "Grant access", exact: true }).click();
  await page.getByRole("button", { name: "Open Synthetic Fleet Base", exact: true }).click();
  await page.getByText("Configure vehicle", { exact: true }).click();
  await page.getByLabel("Vehicle operational code", { exact: true }).fill("SYN-ALS-BROWSER");
  await page.getByRole("combobox", { name: "Certified vehicle class", exact: true }).click();
  await page.getByRole("option", { name: "Advanced life support", exact: true }).click();
  const localTime = (hours: number) => {
    const d = new Date(Date.now() + hours * 3600000);
    return new Date(d.valueOf() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  };
  await page.getByLabel("Vehicle certification valid until", { exact: true }).fill(localTime(24));
  await page
    .getByLabel("Vehicle policy reference", { exact: true })
    .fill("SYN-NOT-A-CLINICAL-POLICY");
  await page.getByLabel("Minimum oxygen pressure (bar)", { exact: true }).fill("100");
  await page.getByLabel("Readiness validity (minutes)", { exact: true }).fill("480");
  await page.getByRole("button", { name: "Save vehicle", exact: true }).click();
  await expect(page.getByRole("heading", { name: "SYN-ALS-BROWSER", exact: true })).toBeVisible();
  await page.getByText("Verify crew credentials", { exact: true }).click();
  for (const [index, subject, qualification] of [
    [0, "synthetic-browser-driver", "Verified ambulance driver"],
    [1, "synthetic-browser-medic", "Verified paramedic"],
  ] as const) {
    if (index)
      await page.getByRole("button", { name: "Verify another person", exact: true }).click();
    await page.getByLabel("Crew identity subject", { exact: true }).fill(subject);
    await page.getByRole("combobox", { name: "Verified qualification", exact: true }).click();
    await page.getByRole("option", { name: qualification, exact: true }).click();
    await page.getByLabel("Credential valid until", { exact: true }).fill(localTime(24));
    await page
      .getByLabel("Credential evidence reference", { exact: true })
      .fill("SYN-VERIFIED-ONLY-FOR-SOFTWARE-TEST");
    await page.getByRole("button", { name: "Save credential", exact: true }).click();
    await expect(
      page.getByRole("button", { name: `Review ${subject}`, exact: true }),
    ).toBeVisible();
  }
  await page.getByText("Manage duty", { exact: true }).click();
  await page.getByRole("combobox", { name: "Duty vehicle", exact: true }).click();
  await page.getByRole("option", { name: "SYN-ALS-BROWSER", exact: true }).click();
  await page.getByLabel("Duty driver subject", { exact: true }).fill("synthetic-browser-driver");
  await page.getByLabel("Duty clinician subject", { exact: true }).fill("synthetic-browser-medic");
  await page.getByLabel("Duty ends at", { exact: true }).fill(localTime(8));
  await page.getByRole("button", { name: "Start duty", exact: true }).click();
  await page
    .getByRole("button", { name: "Record readiness for SYN-ALS-BROWSER", exact: true })
    .click();
  await page.getByLabel("Measured oxygen pressure (bar)", { exact: true }).fill("150");
  await page.getByLabel("Reviewed equipment checklist complete", { exact: true }).check();
  await page.getByLabel("Roadworthiness check passed", { exact: true }).check();
  await page.getByRole("button", { name: "Record readiness", exact: true }).click();
  await expect(page.getByText("Defibrillator check failed", { exact: true })).toBeVisible();
  await page.getByLabel("Measured oxygen pressure (bar)", { exact: true }).fill("150");
  await page.getByLabel("Defibrillator self-test passed", { exact: true }).check();
  await page.getByLabel("Reviewed equipment checklist complete", { exact: true }).check();
  await page.getByLabel("Roadworthiness check passed", { exact: true }).check();
  await page.getByRole("button", { name: "Record readiness", exact: true }).click();
  await expect(page.getByText("Blocked", { exact: true })).toBeVisible();
  await page.getByText("Review Defibrillator check failed", { exact: true }).click();
  await page.getByLabel("Repair evidence reference", { exact: true }).fill("SYN-REPAIR-TEST-ONLY");
  await page.getByRole("button", { name: "Clear constraint", exact: true }).click();
  await page.getByRole("button", { name: "Confirm clearance", exact: true }).click();
  await expect(page.getByText("Ready · ALS", { exact: true })).toBeVisible();
  await expect(page.getByText("Constraint cleared", { exact: true })).toBeHidden({
    timeout: 10000,
  });
  await page.getByText("Configure vehicle", { exact: true }).click();
  await page.getByText("Verify crew credentials", { exact: true }).click();
  await page.getByText("Manage duty", { exact: true }).click();
  await page
    .getByRole("heading", { name: "Fleet readiness", exact: true })
    .scrollIntoViewIfNeeded();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  expect(
    await page.locator("main").evaluate((main) => {
      const pane = main.parentElement!;
      return { width: pane.clientWidth, content: pane.scrollWidth };
    }),
  ).toEqual({ width: 326, content: 326 });
  await page.screenshot({
    path: "../docs/architecture/evidence/ambulanceconnect-2026-10-18/fleet-mobile.png",
    fullPage: false,
  });
  await page.getByText("Manage duty", { exact: true }).click();
  await page.getByRole("button", { name: "End duty", exact: true }).click();
  await page.getByRole("button", { name: "Confirm end duty", exact: true }).click();
  await expect(page.getByText("No current duty", { exact: true })).toBeVisible();
  await expect(page.getByText("Ready · ALS", { exact: true })).toHaveCount(0);
});
