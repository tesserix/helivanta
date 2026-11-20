import { expect, test } from "@playwright/test";
import { api, freshOutlet, uuidv7 } from "./support/pharmacy";

test("configure, show zero stock, persist and disable an outlet reorder warning", async ({
  page,
}) => {
  const { outlet, stamp } = await freshOutlet(page);
  const product = uuidv7();
  await api(page, "PUT", `${outlet}/catalogue/products/${product}`, {
    expected_version: 0,
    sku: `TEST-LOW-${stamp}`,
    brand_name: `TEST Low Stock ${stamp}`,
    generic_name: "TEST Medicine",
    dosage_form: "tablet",
    route: "oral",
    manufacturer: "TEST Manufacturer",
    marketing_company: "",
    ingredients: [{ name: "TEST Ingredient", strength: "500", unit: "mg" }],
    pack_size: 10,
    unit_of_issue: "tablet",
    currency: "INR",
    mrp_minor: 10500,
    status: "active",
  });
  await page.goto("/pharmacy/stock?view=low");
  await expect(page.getByText(/Products without a minimum are not counted/)).toBeVisible();
  await page.getByRole("combobox", { name: "Product", exact: true }).click();
  await page.getByRole("option", { name: `TEST Low Stock ${stamp} · TEST-LOW-${stamp}` }).click();
  await page.getByRole("checkbox", { name: "Enable low-stock warning" }).check();
  await page.getByRole("textbox", { name: "Minimum (tablet)" }).fill("0");
  await page.getByRole("button", { name: "Save reorder level" }).click();
  const low = page.getByRole("region", { name: "Low stock", exact: true });
  await expect(
    low.getByRole("cell", { name: new RegExp(`TEST Low Stock ${stamp}`) }),
  ).toBeVisible();
  await expect(low.getByRole("cell", { name: "0 tablet", exact: true })).toHaveCount(2);
  const current = await api(page, "GET", `${outlet}/catalogue/products/${product}/reorder-level`);
  expect(current.level).toMatchObject({ minimum_units: 0, version: 1, product_id: product });
  const overview = await api(page, "GET", `${outlet}/overview`);
  expect(overview.stock).toMatchObject({ low_stock: 1 });
  await page.reload();
  await expect(
    low.getByRole("cell", { name: new RegExp(`TEST Low Stock ${stamp}`) }),
  ).toBeVisible();
  await page.getByRole("combobox", { name: "Product", exact: true }).click();
  await page.getByRole("option", { name: `TEST Low Stock ${stamp} · TEST-LOW-${stamp}` }).click();
  await expect(page.getByRole("checkbox", { name: "Enable low-stock warning" })).toBeChecked();
  await page.getByRole("checkbox", { name: "Enable low-stock warning" }).uncheck();
  await page.getByRole("button", { name: "Save reorder level" }).click();
  await expect(low.getByText(/No low-stock warnings/)).toBeVisible();
  const disabled = await api(page, "GET", `${outlet}/catalogue/products/${product}/reorder-level`);
  expect(disabled.level).toMatchObject({ minimum_units: null, version: 2 });
  await page.goto("/pharmacy");
  await expect(page.getByRole("link", { name: /Low-stock products/ })).toContainText("0");
});
