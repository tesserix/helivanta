import { expect, test, type Page } from "@playwright/test";
import { login } from "./support/login";

// #576 D1: the overview is /pharmacy; the queue moved to /pharmacy/dispense,
// and a shared queue link with its parameters still opens the queue.
test("an old queue link opens the queue at its new address", async ({ page }) => {
  await login(page);
  await page.goto("/pharmacy?tab=waiting&q=Nobody");
  await expect(page).toHaveURL(/\/pharmacy\/dispense\?tab=waiting&q=Nobody$/);
  await expect(page.getByRole("link", { name: "Waiting" })).toHaveAttribute("aria-current", "page");
});

function uuidv7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let ms = Date.now();
  for (let i = 5; i >= 0; i--) {
    bytes[i] = ms % 256;
    ms = Math.floor(ms / 256);
  }
  bytes[6] = (bytes[6]! & 0x0f) | 0x70;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

async function api(page: Page, method: string, path: string, body?: unknown) {
  const result = await page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/api/v1${path}`, {
        method,
        headers: { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: res.status, body: await res.text() };
    },
    { method, path, body },
  );
  expect(result.status, `${method} ${path}: ${result.body}`).toBeLessThan(300);
  return JSON.parse(result.body || "null") as Record<string, unknown>;
}

// A fresh pharmacy and outlet the signed-in user edits, opened as the
// workspace's selected outlet, so counts and pages start from nothing.
async function freshOutlet(page: Page) {
  await login(page);
  await page.goto("/pharmacy");
  const me = await api(page, "GET", "/iam/me/permissions");
  const stamp = Date.now().toString(36).toUpperCase();
  const pharmacyId = uuidv7();
  const outletId = uuidv7();
  const root = `/pharmacy/pharmacies/${pharmacyId}`;
  const outlet = `${root}/outlets/${outletId}`;
  await api(page, "PUT", root, {
    code: `TEST-OVW-${stamp}`,
    name: `TEST Overview Pharmacy ${stamp}`,
    ownership_type: "independent",
  });
  await api(page, "PUT", outlet, {
    code: "TEST-OVW",
    name: "TEST Overview Outlet",
    country_code: "IN",
    timezone: "Asia/Kolkata",
  });
  await api(page, "PUT", `${outlet}/access/${encodeURIComponent(String(me.subject))}`, {
    level: "editor",
  });
  await page.evaluate(({ key, value }) => sessionStorage.setItem(key, value), {
    key: `helivanta:pharmacy:scope:${String(me.tenant_id)}`,
    value: JSON.stringify({ pharmacyId, outletId }),
  });
  return { outlet, stamp };
}

async function addProduct(page: Page, outlet: string, sku: string, brand: string) {
  await api(page, "PUT", `${outlet}/catalogue/products/${uuidv7()}`, {
    expected_version: 0,
    sku,
    brand_name: brand,
    generic_name: "TEST paracetamol",
    dosage_form: "tablet",
    route: "oral",
    manufacturer: "TEST Manufacturer",
    marketing_company: "",
    ingredients: [{ name: "TEST paracetamol", strength: "500", unit: "mg" }],
    pack_size: 10,
    unit_of_issue: "tablet",
    currency: "INR",
    mrp_minor: 10500,
    status: "active",
  });
}

// #576: on a fresh outlet, each overview count equals the rows its link opens.
test("each overview count opens exactly the rows it counted", async ({ page }) => {
  const { outlet, stamp } = await freshOutlet(page);
  for (const n of [1, 2])
    await addProduct(page, outlet, `TEST-OVW-${stamp}-${n}`, `TEST Unclassified ${n}`);
  await api(page, "PUT", `${outlet}/accounts/payables/${uuidv7()}`, {
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: `TEST-OVERDUE-${stamp}`,
    issued_on: "2026-01-01",
    due_on: "2026-01-31",
    currency: "INR",
    lines: [
      {
        category: "utilities",
        description: "TEST electricity",
        net_minor: 10000,
        tax_minor: 1800,
        recoverable_tax_minor: 1800,
        landed_minor: 0,
      },
    ],
  });

  await page.goto("/pharmacy");
  const bills = page.getByRole("region", { name: "Supplier bills" });
  const overdue = bills.getByRole("link", { name: /Overdue/ });
  await expect(overdue).toContainText("1");
  const setup = page.getByRole("region", { name: "Setup gaps" });
  await expect(setup.getByRole("link", { name: /Not classified/ })).toContainText("2");

  await overdue.click();
  // The tile's count is the tab's total (#576 slice 3).
  await expect(page).toHaveURL(/\/pharmacy\/accounts\?tab=overdue$/);
  await expect(page.getByRole("link", { name: "Overdue" })).toHaveAttribute("aria-current", "page");
  await expect(page.getByText("1–1 of 1")).toBeVisible();
  await expect(page.getByText(`TEST-OVERDUE-${stamp}`)).toBeVisible();

  await page.goto("/pharmacy");
  await page
    .getByRole("region", { name: "Setup gaps" })
    .getByRole("link", { name: /Not classified/ })
    .click();
  await expect(page).toHaveURL(/tax=unclassified$/);
  await expect(page.getByRole("button", { name: "Not classified" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(
    page.getByRole("listitem").filter({ hasText: new RegExp(`TEST-OVW-${stamp}-`) }),
  ).toHaveCount(2);
});

// #576 slice 3: the catalogue pages 25 at a time with its total; the page
// and the search live in the URL.
test("the catalogue pages forward and back, keeping its search", async ({ page }) => {
  const { outlet, stamp } = await freshOutlet(page);
  for (let n = 1; n <= 27; n++)
    await addProduct(
      page,
      outlet,
      `TEST-PG-${stamp}-${String(n).padStart(2, "0")}`,
      `TEST Paged ${n}`,
    );
  const rows = page.getByRole("listitem").filter({ hasText: new RegExp(`TEST-PG-${stamp}-`) });

  await page.goto("/pharmacy/catalogue");
  await expect(page.getByText("1–25 of 27")).toBeVisible();
  await expect(rows).toHaveCount(25);
  const first = await rows.first().textContent();

  await page.getByRole("link", { name: "Next" }).click();
  await expect(page.getByText("26–27 of 27")).toBeVisible();
  await expect(rows).toHaveCount(2);
  await page.getByRole("link", { name: "Previous" }).click();
  await expect(page.getByText("1–25 of 27")).toBeVisible();
  await expect(rows.first()).toHaveText(first ?? "");

  await page.getByLabel("Search by brand, generic or SKU").fill(`TEST-PG-${stamp}`);
  await page.getByRole("button", { name: "Search catalogue" }).click();
  await expect(page).toHaveURL(/q=test-pg-|q=TEST-PG-/i);
  await page.getByRole("link", { name: "Next" }).click();
  await expect(page).toHaveURL(/page=2/);
  await expect(page).toHaveURL(new RegExp(`q=TEST-PG-${stamp}`, "i"));
  await expect(page.getByText("26–27 of 27")).toBeVisible();

  await page.reload();
  await expect(page.getByText("26–27 of 27")).toBeVisible();
  await expect(rows).toHaveCount(2);
});
