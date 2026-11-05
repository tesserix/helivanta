import { expect, type Browser, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import path from "node:path";
import type { MembershipCandidate } from "@helivanta/api";
import { login, specAdmin, specPharmacist } from "./login";
import { api, newOutlet, uuidv7 } from "./pharmacy";

export const owner = async <T>(page: Page, method: string, path: string, body?: unknown) =>
  (await api(page, method, path, body)) as T;

async function membershipOwner<T>(
  page: Page,
  actor: { tenant_id: string; subject: string },
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const result = await page.evaluate(
    async ({ actor, method, path, body }) => {
      const response = await fetch(`/api/v1${path}`, {
        method,
        headers: {
          "Content-Type": "application/json",
          "X-Helivanta-Expected-Tenant": actor.tenant_id,
          "X-Helivanta-Expected-Subject": actor.subject,
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, body: await response.text() };
    },
    { actor, method, path, body },
  );
  expect(result.status, `${method} ${path}: ${result.body}`).toBeLessThan(300);
  return JSON.parse(result.body) as T;
}

export async function choose(page: Page, label: string, option: string | RegExp) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: typeof option === "string" }).click();
}

export async function provider(page: Page, path: string, body: unknown) {
  const origin = process.env.HELIVANTA_E2E_PAYMENT_PROVIDER_URL;
  expect(origin, "start the loopback synthetic payment fixture before the API").toBeTruthy();
  const url = new URL(origin!);
  expect(url.hostname).toBe("127.0.0.1");
  expect(url.protocol).toBe("http:");
  const response = await page.request.post(`${url.origin}/_test/${path}`, {
    headers: { "X-Helivanta-E2E-Driver": process.env.HELIVANTA_E2E_PAYMENT_DRIVER_TOKEN! },
    data: body,
  });
  expect(response.ok(), await response.text()).toBe(true);
  return response.json() as Promise<{ id: string; amount: number }>;
}

type Actor = { tenant_id: string; subject: string };

/**
 * Gives the page's own subject `role` until the returned function removes it,
 * waiting each way until `permission` reaches the real resolver.
 */
export async function temporaryRole(
  admin: Page,
  administrator: Actor,
  page: Page,
  role: string,
  permission: string,
) {
  const holds = async () =>
    (await owner<{ data: string[] }>(page, "GET", "/iam/me/permissions")).data.includes(permission);
  const { subject } = await owner<Actor>(page, "GET", "/iam/me/permissions");
  const membership = await owner<Pick<MembershipCandidate, "id">>(admin, "POST", "/iam/members", {
    subject,
    role_key: role,
  });
  await expect.poll(holds).toBe(true);
  return async (reason: string) => {
    let held: MembershipCandidate | undefined;
    let cursor: string | null = null;
    for (let n = 0; n < 20; n++) {
      const candidates: {
        data: MembershipCandidate[];
        page: { has_more: boolean; next_cursor: string | null };
      } = await membershipOwner(
        admin,
        administrator,
        "GET",
        `/iam/membership-candidates?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
      );
      held = candidates.data.find((candidate) => candidate.id === membership.id);
      if (held || !candidates.page.has_more) break;
      expect(candidates.page.next_cursor).toBeTruthy();
      cursor = candidates.page.next_cursor;
    }
    expect(held, "the temporary membership must be attributable").toBeDefined();
    await membershipOwner(admin, administrator, "PUT", `/iam/membership-commands/${uuidv7()}`, {
      operation: "remove",
      basis_id: held!.id,
      subject,
      role_key: role,
      expected_membership_epoch: held!.membership_epoch,
      reason,
    });
    await expect.poll(holds).toBe(false);
  };
}

/** Two actors and a sellable outlet: real owner commands, no table or projection writes. */
export async function readyOutlet(admin: Page, browser: Browser) {
  await login(admin, specAdmin());
  const { outlet, stamp } = await newOutlet(admin);
  const administrator = await owner<{ tenant_id: string; subject: string }>(
    admin,
    "GET",
    "/iam/me/permissions",
  );
  const context = await browser.newContext({
    baseURL: new URL(admin.url()).origin,
    ignoreHTTPSErrors: true,
  });
  const cashier = await context.newPage();
  await login(cashier, specPharmacist());
  const me = await owner<{ subject: string; tenant_id: string; data: string[] }>(
    cashier,
    "GET",
    "/iam/me/permissions",
  );
  // The reviewing pharmacist is temporarily also the pharmacy owner; remove that
  // role after setup so selling/returning is proved under pharmacist authority.
  const endReviewer = await temporaryRole(
    admin,
    administrator,
    cashier,
    "pharmacy_owner",
    "pharmacy.licence.review",
  );
  await owner(admin, "PUT", `${outlet}/access/${encodeURIComponent(me.subject)}`, {
    level: "editor",
  });
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(new Date());
  const until = `${Number(today.slice(0, 4)) + 2}-12-31`;
  const licence = uuidv7();
  await owner(admin, "PUT", `${outlet}/licences/${licence}`, {
    licence_kind: "retail",
    form_label: "TEST-FORM-20",
    licence_number: `TEST-DL-${stamp}`,
    issuing_authority: "TEST State Drug Controller",
    valid_from: today,
    valid_until: until,
    pharmacist_name: "TEST Pharmacist",
    pharmacist_registration: "TEST-PCI-001",
  });
  await owner(cashier, "POST", `${outlet}/licences/${licence}/review`, {
    decision: "approved",
    reason: "TEST synthetic licence checked by a second actor",
  });
  await owner(admin, "POST", `${outlet}/tax-registrations`, {
    jurisdiction_code: "27",
    identifier: "27PQRST5678K2Z1",
    effective_from: today,
  });
  await owner(admin, "PUT", `${outlet}/invoice-profile/${uuidv7()}`, {
    legal_name: "TEST Counter Pharmacy LLP",
    address: "TEST 1 Market Road, Pune",
    invoice_prefix: "TP",
  });
  const product = uuidv7();
  const sku = `TEST-PAY-${stamp}`;
  await owner(admin, "PUT", `${outlet}/catalogue/products/${product}`, {
    expected_version: 0,
    sku,
    brand_name: "TEST Counter Tablets",
    generic_name: "TEST molecule",
    dosage_form: "tablet",
    route: "oral",
    manufacturer: "TEST Manufacturer",
    marketing_company: "",
    ingredients: [{ name: "TEST molecule", strength: "1", unit: "mg" }],
    pack_size: 10,
    unit_of_issue: "tablet",
    currency: "INR",
    mrp_minor: 12500,
    status: "active",
  });
  await owner(admin, "POST", `${outlet}/catalogue/products/${product}/tax-classifications`, {
    classification_code: "30049099",
    supply_nature: "taxable",
    rate_percent: "12",
    effective_from: today,
  });
  const declaration = uuidv7();
  await owner(admin, "PUT", `${outlet}/catalogue/products/${product}/schedule/${declaration}`, {
    regulatory_class: "none",
    ndps: false,
    source: "TEST synthetic pack declaration",
  });
  await owner(
    cashier,
    "POST",
    `${outlet}/catalogue/products/${product}/schedule/${declaration}/review`,
    { decision: "approved", reason: "TEST synthetic pack checked by a second actor" },
  );
  const quarantine = uuidv7(),
    shelf = uuidv7(),
    batch = uuidv7();
  await owner(admin, "PUT", `${outlet}/stock/locations/${quarantine}`, {
    code: "TEST-Q",
    name: "TEST Return quarantine",
  });
  await owner(admin, "PUT", `${outlet}/stock/locations/${shelf}`, {
    code: "TEST-S",
    name: "TEST Counter shelf",
    stock_status: "sellable",
  });
  await owner(admin, "PUT", `${outlet}/stock/batches/${batch}`, {
    product_id: product,
    lot_number: `TEST-LOT-${stamp}`,
    expires_on: until,
    mrp_minor: 12500,
  });
  await owner(admin, "PUT", `${outlet}/stock/movements/${uuidv7()}`, {
    location_id: quarantine,
    batch_id: batch,
    kind: "receipt",
    quantity_units: 20,
    reason: "TEST received",
    source_reference: `TEST-GRN-${stamp}`,
  });
  await owner(cashier, "PUT", `${outlet}/stock/releases/${uuidv7()}`, {
    batch_id: batch,
    from_location_id: quarantine,
    to_location_id: shelf,
    quantity_units: 20,
    reason: "TEST checked and shelved",
  });
  await endReviewer("TEST end temporary licence reviewer role before cashier journey");
  await cashier.goto("/dashboard");
  await cashier.evaluate(({ key, value }) => sessionStorage.setItem(key, value), {
    key: `helivanta:pharmacy:scope:${me.tenant_id}`,
    value: JSON.stringify({
      pharmacyId: outlet.split("/").at(-3),
      outletId: outlet.split("/").at(-1),
    }),
  });
  return {
    context,
    cashier,
    outlet,
    product,
    sku,
    batch,
    quarantine,
    shelf,
    stamp,
    subject: me.subject,
  };
}

/** readyOutlet with the synthetic payment provider's account, for UPI sales. */
export async function readyCounter(admin: Page, browser: Browser) {
  const fixture = await readyOutlet(admin, browser);
  const account = JSON.parse(
    readFileSync(path.resolve(__dirname, "../../fixtures/payments-account.json"), "utf8"),
  );
  await owner(admin, "PUT", "/payments/razorpay-account", account);
  return fixture;
}

export async function collectUpi(page: Page, sku: string, units = "2") {
  await page.goto("/pharmacy/counter");
  await choose(page, "Product", new RegExp(sku));
  await page.getByLabel("Units", { exact: true }).fill(units);
  await page.getByRole("button", { name: "Add to basket", exact: true }).click();
  await page.getByRole("button", { name: "Price basket", exact: true }).click();
  await choose(page, "Paid by", "UPI (verified)");
  await page.getByRole("button", { name: /Show UPI QR for/ }).click();
  const image = page.getByRole("img", { name: /UPI QR for/ });
  await expect(image).toBeVisible();
  const qr = /\/(qr_E2E\d+)\.svg$/.exec((await image.getAttribute("src"))!)![1]!;
  await page.getByRole("button", { name: "Post sale", exact: true }).click();
  await expect(
    page.getByText("Wait for the UPI payment to be confirmed", { exact: true }),
  ).toBeVisible();
  await provider(page, "pay", { qr_id: qr });
  await expect(page.getByRole("status").filter({ hasText: /^Paid .*RRN TEST-RRN-/ })).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByRole("combobox", { name: "Customer", exact: true })).toBeDisabled();
  await page.getByRole("combobox", { name: "Customer", exact: true }).click({ force: true });
  await expect(page.getByRole("option", { name: "Bill to admission", exact: true })).toHaveCount(0);
  await expect(page.getByRole("combobox", { name: "Paid by", exact: true })).toBeDisabled();
  await expect(
    page.getByText(/Finish the sale or record the completed refund before changing/),
  ).toBeVisible();
  return qr;
}
