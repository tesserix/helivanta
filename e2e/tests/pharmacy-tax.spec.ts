import { expect, test, type Page } from "@playwright/test";
import { login } from "./support/login";

// #1283: an owner registers an outlet's GSTIN and classifies a product, and
// the catalogue stops calling it "Not classified".
//
// The pharmacy, outlet, access grant and product are created through the
// API from inside the page (same origin and session as the app) because
// their own screens are covered elsewhere; the tax steps go through the UI.
// Each run makes a fresh pharmacy, since the dev database persists.

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

test("an owner registers the outlet's GSTIN and classifies a product", async ({ page }) => {
  await login(page);
  await page.goto("/pharmacy");
  const me = await api(page, "GET", "/iam/me/permissions");
  const stamp = Date.now().toString(36).toUpperCase();
  const pharmacyId = uuidv7();
  const outletId = uuidv7();
  const productId = uuidv7();
  const root = `/pharmacy/pharmacies/${pharmacyId}`;
  await api(page, "PUT", root, {
    code: `TEST-TAX-${stamp}`,
    name: `TEST Tax Pharmacy ${stamp}`,
    ownership_type: "independent",
  });
  await api(page, "PUT", `${root}/outlets/${outletId}`, {
    code: "TEST-DELHI",
    name: "TEST Delhi Outlet",
    country_code: "IN",
    timezone: "Asia/Kolkata",
  });
  await api(
    page,
    "PUT",
    `${root}/outlets/${outletId}/access/${encodeURIComponent(String(me.subject))}`,
    {
      level: "editor",
    },
  );
  await api(page, "PUT", `${root}/outlets/${outletId}/catalogue/products/${productId}`, {
    expected_version: 0,
    sku: `TEST-TAX-${stamp}`,
    brand_name: "TEST Taxable",
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
  await page.evaluate(({ key, value }) => sessionStorage.setItem(key, value), {
    key: `helivanta:pharmacy:scope:${String(me.tenant_id)}`,
    value: JSON.stringify({ pharmacyId, outletId }),
  });

  await page.goto("/pharmacy/setup");
  const registration = page.getByRole("region", { name: "Tax registration" });
  await expect(registration).toBeVisible();
  await registration.getByRole("combobox", { name: "State or union territory" }).click();
  await page.getByRole("option", { name: "07 Delhi" }).click();
  await registration.getByLabel("GSTIN").fill("07ABCDE1234F1Z2");
  await registration.getByRole("button", { name: "Record registration" }).click();
  await expect(page.getByText("Registration recorded")).toBeVisible();
  await expect(registration.getByText("In force today")).toBeVisible();

  await page.goto("/pharmacy/catalogue");
  const row = page.getByRole("listitem").filter({ hasText: `TEST-TAX-${stamp}` });
  await expect(row.getByText("Not classified")).toBeVisible();
  await row.getByRole("button", { name: `Open TEST-TAX-${stamp}` }).click();
  const taxSection = page.getByRole("region", { name: "Tax" });
  await taxSection.getByLabel("HSN code").fill("3004");
  await taxSection.getByLabel("Rate (%)").fill("5");
  await taxSection.getByRole("button", { name: "Record classification" }).click();
  await expect(page.getByText("Classification recorded")).toBeVisible();
  await expect(row.getByText("HSN 3004 · 5%")).toBeVisible();
});
