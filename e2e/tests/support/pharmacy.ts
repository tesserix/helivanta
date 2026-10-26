import { expect, type Page } from "@playwright/test";
import { login } from "./login";

// Fixtures for PharmaConnect specs: a fresh outlet the signed-in user edits,
// and records created through the API from the page's own session.

export function uuidv7(): string {
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

export async function api(page: Page, method: string, path: string, body?: unknown) {
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
export async function freshOutlet(page: Page) {
  await login(page);
  return newOutlet(page);
}

/** A new pharmacy and outlet, made this tab's working outlet; already signed in. */
export async function newOutlet(page: Page) {
  // Not a pharmacy page: one that is open resolves its outlet and saves it
  // as the tab's working outlet (#1303 E8), and could overwrite this one.
  await page.goto("/dashboard");
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

export async function addProduct(page: Page, outlet: string, sku: string, brand: string) {
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

// A bill due on `dueOn`, with one line of 118.00 INR.
export async function addBill(page: Page, outlet: string, reference: string, dueOn = "2026-01-31") {
  return api(page, "PUT", `${outlet}/accounts/payables/${uuidv7()}`, {
    payee_reference: "TEST-SUPPLIER",
    payee_name: "TEST Supplier",
    document_reference: reference,
    issued_on: "2026-01-01",
    due_on: dueOn,
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
}

/**
 * The address is a list at `path` with `params`, wherever its scope is: outlet
 * pages also carry `pharmacy` and `outlet` (#1303 E8). `open` and `new` are
 * absent unless `open` is asked for.
 */
export async function expectList(
  page: Page,
  path: string,
  params: Record<string, string>,
  { open = false }: { open?: boolean } = {},
) {
  await expect
    .poll(
      () => {
        const url = new URL(page.url());
        const search = url.searchParams;
        return (
          url.pathname === path &&
          Object.entries(params).every(([key, value]) => search.get(key) === value) &&
          search.has("open") === open &&
          !search.has("new")
        );
      },
      { message: `${path} with ${JSON.stringify(params)}${open ? " and a record open" : ""}` },
    )
    .toBe(true);
}

// WCAG 2.5.3 Label in Name: a control's visible words are part of its
// accessible name, so a voice-control user can say what they see (#1364).
// The whole page, chrome included (#1369); a dropdown shows its value, not its label.
export async function expectLabelsInNames(page: Page) {
  const mismatched = await page
    .locator("button[aria-label]:not([role=combobox]), a[aria-label]")
    .evaluateAll((els) =>
      els
        .filter((el) => el.checkVisibility())
        .map((el) => [(el as HTMLElement).innerText.trim(), el.getAttribute("aria-label") ?? ""])
        .filter(([text, name]) => text && !name.toLowerCase().includes(text.toLowerCase()))
        .map(([text, name]) => `"${text}" is not in "${name}"`),
    );
  expect(mismatched).toEqual([]);
}
