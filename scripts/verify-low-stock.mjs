import { chromium, expect, request } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({ headless: true });
const contexts = [];
const writes = [];
const apiContext = await request.newContext();
const root = `/v1/pharmacy/pharmacies/${fixture.pharmacy}/outlets/${fixture.outlet}`;
const levelRoot = `${root}/catalogue/products/${fixture.product}`;
let lostSave = false;
try {
  async function api(actor, method, path, data) {
    return apiContext.fetch(fixture.gateway_url + path, {
      method,
      data,
      headers: {
        authorization: `Bearer ${fixture.tokens[actor]}`,
        "X-Helivanta-Expected-Tenant": fixture.tenant_id,
        "X-Helivanta-Expected-Subject": `user-${actor}`,
      },
    });
  }

  async function open(actor) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
    contexts.push(context);
    await context.addCookies([
      { name: "helivanta_session", value: fixture.tokens[actor], url: fixture.app_url },
    ]);
    await context.route("**/api/**", async (route) => {
      const request = route.request();
      const incoming = new URL(request.url());
      const path = incoming.pathname.replace(/^\/api/, "") + incoming.search;
      if (request.method() === "PUT" && path.startsWith(levelRoot + "/reorder-levels/")) {
        expect(request.headers()["x-helivanta-expected-tenant"]).toBe(fixture.tenant_id);
        expect(request.headers()["x-helivanta-expected-subject"]).toBe(`user-${actor}`);
        writes.push({ actor, path, input: request.postDataJSON() });
      }
      const response = await route.fetch({
        url: fixture.gateway_url + path,
        headers: { ...request.headers(), authorization: `Bearer ${fixture.tokens[actor]}` },
      });
      if (
        actor === "owner" &&
        request.method() === "PUT" &&
        path.startsWith(levelRoot + "/reorder-levels/") &&
        response.ok() &&
        !lostSave
      ) {
        lostSave = true;
        await route.abort("connectionreset");
      } else await route.fulfill({ response });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    await page.goto(
      `${fixture.app_url}/pharmacy/low-stock?pharmacy=${fixture.pharmacy}&outlet=${fixture.outlet}`,
    );
    await expect(
      page.getByRole("region", { name: "Low-stock overview", exact: true }),
    ).toBeVisible();
    return page;
  }
  const owner = await open("owner");
  const count = (page) => page.getByRole("link", { name: /^Low-stock products/ });
  await expect(count(owner)).toHaveText("Low-stock products0");
  await expect(owner.getByText(/No low-stock warnings\./)).toBeVisible();
  await owner.getByRole("combobox", { name: "Product", exact: true }).click();
  await owner
    .getByRole("option", { name: "TEST Amoxicillin · TEST-AMOX-500", exact: true })
    .click();
  await owner.getByRole("checkbox", { name: "Enable low-stock warning", exact: true }).check();
  await owner.getByRole("textbox", { name: "Minimum (capsule)", exact: true }).fill("0");
  await owner.getByRole("button", { name: "Save reorder level", exact: true }).click();
  await expect(owner.getByText(/The save could not be confirmed/)).toBeVisible();
  await owner.getByRole("button", { name: "Check saved result", exact: true }).click();
  await expect(owner.getByText("Reorder level saved.", { exact: true })).toBeVisible();
  expect(lostSave).toBe(true);
  expect(writes).toHaveLength(1);
  await expect(count(owner)).toHaveText("Low-stock products1");
  const warning = owner.getByRole("row").filter({ hasText: "TEST Amoxicillin" });
  await expect(warning).toContainText("0 capsule");
  await owner.reload();
  await expect(count(owner)).toHaveText("Low-stock products1");
  await owner.getByRole("combobox", { name: "Product", exact: true }).click();
  await owner
    .getByRole("option", { name: "TEST Amoxicillin · TEST-AMOX-500", exact: true })
    .click();
  await expect(owner.getByRole("textbox", { name: "Minimum (capsule)", exact: true })).toHaveValue(
    "0",
  );
  const reader = await open("reader");
  await expect(count(reader)).toHaveText("Low-stock products1");
  await expect(
    reader.getByRole("region", { name: "Configure reorder level", exact: true }),
  ).toHaveCount(0);
  const denied = await api("reader", "PUT", `${levelRoot}/reorder-levels/${crypto.randomUUID()}`, {
    minimum_units: 9,
    expected_version: 1,
    product_version: 1,
  });
  expect(denied.status()).toBe(403);
  const revoked = await api("owner", "DELETE", `${root}/access/user-reader`);
  expect(revoked.status()).toBe(200);
  await reader.getByRole("button", { name: "Refresh low-stock overview", exact: true }).click();
  await reader.getByRole("button", { name: "Refresh low-stock warnings", exact: true }).click();
  await expect(count(reader)).toHaveCount(0);
  await expect(
    reader.getByRole("region", { name: "Low-stock overview", exact: true }).getByRole("alert"),
  ).toBeVisible();
  await expect(reader.getByRole("row").filter({ hasText: "TEST Amoxicillin" })).toHaveCount(0);
  await expect(
    reader.getByRole("region", { name: "Low stock", exact: true }).getByRole("alert"),
  ).toBeVisible();
  await owner.getByRole("checkbox", { name: "Enable low-stock warning", exact: true }).uncheck();
  await owner.getByRole("button", { name: "Save reorder level", exact: true }).click();
  await expect(count(owner)).toHaveText("Low-stock products0");
  await expect(owner.getByText(/No low-stock warnings\./)).toBeVisible();
  expect(writes).toHaveLength(2);
  const original = await api("owner", "GET", writes[0].path);
  expect(original.ok()).toBe(true);
  expect(await original.json()).toMatchObject({ minimum_units: 0, version: 1 });
  const current = await api("owner", "GET", `${levelRoot}/reorder-level`);
  expect(await current.json()).toMatchObject({ level: { minimum_units: null, version: 2 } });
  for (const path of ["/catalogue/products", "/stock/movements", "/overview"]) {
    expect((await api("owner", "GET", root + path)).status()).toBe(404);
  }
  for (const page of [owner, reader]) {
    for (const value of [fixture.tenant_id, fixture.pharmacy, fixture.outlet, fixture.product]) {
      await expect(page.locator("body")).not.toContainText(value);
    }
    await expect(page.getByRole("link", { name: "Catalogue", exact: true })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Stock", exact: true })).toHaveCount(0);
  }
  await owner.screenshot({
    path: `/tmp/helivanta-low-stock-${fixture.ownership}.png`,
    fullPage: true,
  });
  console.log(
    `${fixture.ownership}: real owner minimum, lost-response recovery, exact count, reader denial, revocation, disable and immutable receipt passed; broad capabilities remain locked.`,
  );
} finally {
  await Promise.all(contexts.map((context) => context.close()));
  await apiContext.dispose();
  await browser.close();
}
