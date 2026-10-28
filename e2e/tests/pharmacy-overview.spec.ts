import { expect, test } from "@playwright/test";
import { login } from "./support/login";
import { addProduct, api, expectList, freshOutlet, uuidv7 } from "./support/pharmacy";

// #576 D1: the overview is /pharmacy; the queue moved to /pharmacy/dispense,
// and a shared queue link with its parameters still opens the queue.
test("an old queue link opens the queue at its new address", async ({ page }) => {
  await login(page);
  await page.goto("/pharmacy?tab=waiting&q=Nobody");
  await expect(page).toHaveURL(/\/pharmacy\/dispense\?tab=waiting&q=Nobody$/);
  await expect(page.getByRole("link", { name: "Waiting" })).toHaveAttribute("aria-current", "page");
});

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
  await expectList(page, "/pharmacy/accounts", { tab: "overdue" });
  await expect(page.getByRole("link", { name: "Overdue" })).toHaveAttribute("aria-current", "page");
  await expect(page.getByText("1–1 of 1")).toBeVisible();
  await expect(page.getByText(`TEST-OVERDUE-${stamp}`)).toBeVisible();

  await page.goto("/pharmacy");
  await page
    .getByRole("region", { name: "Setup gaps" })
    .getByRole("link", { name: /Not classified/ })
    .click();
  await expectList(page, "/pharmacy/catalogue", { tax: "unclassified" });
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
