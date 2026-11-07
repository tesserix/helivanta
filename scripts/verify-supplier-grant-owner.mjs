import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({ headless: true });
let context;
try {
  context = await browser.newContext({ viewport: { width: 1440, height: 1200 } });
  await context.addCookies([
    { name: "helivanta_session", value: fixture.token, url: fixture.app_url },
  ]);
  let lost = false;
  const writes = [];
  await context.route("**/api/**", async (route) => {
    const request = route.request();
    const incoming = new URL(request.url());
    const path = incoming.pathname.replace(/^\/api/, "") + incoming.search;
    const preparation = path.includes("/iam/partner-grant-drafts/") && request.method() === "PUT";
    if (preparation) writes.push(path);
    const response = await route.fetch({
      url: fixture.gateway_url + path,
      headers: { ...request.headers(), authorization: `Bearer ${fixture.token}` },
    });
    if (preparation && !lost && response.ok()) {
      lost = true;
      await route.abort("connectionreset");
    } else await route.fulfill({ response });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(30_000);
  await page.goto(`${fixture.app_url}/workforce/partner-teams`);
  await page.getByRole("button", { name: "Supplier access preparation", exact: true }).click();
  await page.getByRole("button", { name: "Prepare supplier access", exact: true }).click();
  async function choose(label, option) {
    await page.getByRole("combobox", { name: label, exact: true }).click();
    await page.getByRole("option", { name: option, exact: true }).click();
  }
  await choose("Supplier organization", "Synthetic independent supplier");
  await choose("Supplying location", "Synthetic Pune supplying outlet");
  for (const [label, value] of [
    ["Relationship reference", fixture.relationship_id],
    ["Hospital workspace reference", fixture.recipient_tenant_id],
    ["Hospital reference", fixture.hospital_id.toUpperCase()],
    ["Published hospital team reference", fixture.group_id],
    ["Reason", "Synthetic browser receiving-team access intent"],
  ])
    await page.getByRole("textbox", { name: label, exact: true }).fill(value);
  await page.getByRole("button", { name: "Save supplier access proposal", exact: true }).click();
  await expect(page.getByText(/The save may have been recorded/)).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Reason", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Check saved proposal", exact: true }).click();
  await expect(page.getByText(/Supplier access proposal recovered/)).toBeVisible();
  expect(lost).toBe(true);
  expect(writes).toHaveLength(1);
  const result = await context.request.get(fixture.gateway_url + writes[0], {
    headers: {
      authorization: `Bearer ${fixture.token}`,
      "X-Helivanta-Expected-Tenant": fixture.tenant_id,
      "X-Helivanta-Expected-Subject": fixture.subject,
    },
  });
  expect(result.status()).toBe(200);
  const retained = await result.json();
  expect(retained.owner_tenant_id).toBe(fixture.tenant_id);
  expect(retained.input.owner_organization_id).toBe(fixture.pharmacy_id);
  expect(retained.input.source_location_id).toBe(fixture.outlet_id);
  expect(retained.input.recipient_kind).toBe("group");
  expect(retained.input.recipient_hospital_id).toBe(fixture.hospital_id.toLowerCase());
  expect(retained.operating_access_enabled).toBe(false);
  expect(retained.status).toBe("pending_verification");
  await page.screenshot({ path: "/tmp/helivanta-supplier-grant-owner.png", fullPage: true });
  console.log(
    "PASS: browser → Global API → actual IAM/OpenFGA and pharmacy/Postgres; a typed grant proposal commits once, GET recovers a lost response, and no operating access is emitted.",
  );
} catch (error) {
  console.error(`Supplier browser acceptance failed (${error?.name ?? "UnknownError"}).`);
  process.exitCode = 1;
} finally {
  try {
    await context?.unrouteAll({ behavior: "wait" });
  } catch {
    console.error("Supplier browser API forwarding did not finish cleanly.");
    process.exitCode = 1;
  } finally {
    await browser.close();
  }
}
