import { expect, test } from "@playwright/test";

const api = process.env.EMERGENCY_BROWSER_API;
if (!api || new URL(api).hostname !== "127.0.0.1")
  throw new Error("Use the disposable Go browser fixture");

test.afterEach(async ({ context }) => {
  await context.unrouteAll({ behavior: "wait" });
});

test("an independently verified doctor records a requirement; revocation blocks further review", async ({
  page,
  context,
  baseURL,
  request,
}) => {
  if (!baseURL) throw new Error("A local browser origin is required");
  let token = "admin";
  await context.addCookies([
    { name: "helivanta_session", value: "synthetic-clinical-session", url: baseURL },
  ]);
  await context.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch({
      url: `${api}${url.pathname.slice(4)}${url.search}`,
      headers: { ...route.request().headers(), authorization: `Bearer ${token}` },
    });
    await route.fulfill({ response });
  });
  await page.goto("/emergency");
  await page
    .getByLabel("Operating base name", { exact: true })
    .fill("Synthetic Clinical Review Base");
  await page
    .getByLabel("Reviewed policy reference", { exact: true })
    .fill("SYN-REVIEW-SOFTWARE-TEST");
  await page.getByRole("button", { name: "Save base", exact: true }).click();
  await page.getByLabel("Verified identity subject", { exact: true }).fill("user-admin");
  await page.getByRole("combobox", { name: "Base access level", exact: true }).click();
  await page.getByRole("option", { name: "Supervisor", exact: true }).click();
  await page.getByRole("button", { name: "Grant access", exact: true }).click();
  await expect(page.getByText("Access granted", { exact: true })).toBeVisible();
  await page.getByLabel("Verified identity subject", { exact: true }).fill("user-doctor");
  await page.getByRole("combobox", { name: "Base access level", exact: true }).click();
  await page.getByRole("option", { name: "Case reader", exact: true }).click();
  await page.getByRole("button", { name: "Grant access", exact: true }).click();
  await page.getByLabel("Clinical reviewer identity subject", { exact: true }).fill("user-doctor");
  await page.getByRole("button", { name: "Load clinical review access", exact: true }).click();
  await page
    .getByLabel("Clinical privilege approval reference", { exact: true })
    .fill("SYN-INDEPENDENT-REVIEW");
  const localTime = (hours: number) => {
    const d = new Date(Date.now() + hours * 3600000);
    return new Date(d.valueOf() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  };
  await page.getByLabel("Clinical review access expires at", { exact: true }).fill(localTime(24));
  await page.getByLabel("Clinical review access enabled", { exact: true }).check();
  await page.getByRole("button", { name: "Save clinical review access", exact: true }).click();
  await expect(page.getByText("Clinical review access saved", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Open Synthetic Clinical Review Base", exact: true })
    .click();
  await page
    .getByLabel("Caller name (optional)", { exact: true })
    .fill("Synthetic Clinical Caller");
  await page.getByRole("button", { name: "Receive report", exact: true }).click();
  await page.getByRole("link", { name: /Open received case/ }).click();
  await expect(page).toHaveURL(/\/emergency\/bases\/[^/]+\/cases\/[^/]+$/);
  await expect(page.getByText("Synthetic Clinical Caller", { exact: true })).toBeVisible();
  const caseURL = new URL(page.url());
  const base = caseURL.pathname.split("/")[3]!;
  await expect(
    page.getByRole("button", { name: "Record transport requirement", exact: true }),
  ).toHaveCount(0);
  token = "doctor";
  await page.reload();
  await page.getByRole("combobox", { name: "Reviewed transport class", exact: true }).click();
  await page.getByRole("option", { name: "Basic life support", exact: true }).click();
  await page.getByLabel("Reviewed transport policy", { exact: true }).fill("SYN-TRANSPORT-ONLY");
  await page.getByLabel("Transport review valid until", { exact: true }).fill(localTime(1));
  await page.getByRole("button", { name: "Record transport requirement", exact: true }).click();
  await expect(
    page.getByText("Transport requirement recorded. No vehicle has been reserved or dispatched.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByText(/Review 1 · Basic life support/)).toBeVisible();
  const grant = await request.get(
    `${api}/v1/emergency/bases/${base}/clinical-reviewers/user-doctor`,
    { headers: { authorization: "Bearer admin" } },
  );
  expect(grant.status()).toBe(200);
  const current = await grant.json();
  const revoke = await request.put(
    `${api}/v1/emergency/bases/${base}/clinical-reviewers/user-doctor`,
    {
      headers: { authorization: "Bearer admin" },
      data: {
        version: current.version,
        enabled: false,
        evidence_reference: current.evidence_reference,
        expires_at: current.expires_at,
      },
    },
  );
  expect(revoke.status()).toBe(200);
  await page.getByRole("button", { name: "Refresh transport reviews", exact: true }).click();
  await expect(
    page.getByText(/Active independently verified review access is required/),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Record transport requirement", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText(/Review 1 · Basic life support/)).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
});
