import { expect, test } from "@playwright/test";
import { owner, readyOutlet } from "./support/pharmacy-counter";
import { uuidv7 } from "./support/pharmacy";

// #1638: after a release and a sale, the shelf lot's reconciliation lists
// every movement with its sign and its parts add up to closing.

test("the reconciliation's parts add up after a release and a sale", async ({ page, browser }) => {
  test.setTimeout(150_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, product, sku, stamp } = fixture;
  try {
    await owner(cashier, "PUT", `${outlet}/sales/${uuidv7()}`, {
      lines: [{ product_id: product, quantity_units: 2 }],
      tenders: [{ kind: "cash", amount_minor: 2500, tendered_minor: 2500 }],
    });
    await cashier.goto("/pharmacy/stock");
    await cashier
      .getByRole("button", { name: `Inspect ${sku} lot TEST-LOT-${stamp} at TEST-S` })
      .click();
    const pane = cashier.getByRole("region", { name: /^Stock reconciliation / });
    await expect(pane).toBeVisible();
    const value = (label: string) => pane.getByText(label, { exact: true }).locator("xpath=..");
    await expect(value("Releases in period")).toContainText("+20");
    await expect(value("Sales in period")).toContainText("-2");
    await expect(value("Closing stock")).toContainText("18");
    await expect(pane.getByRole("alert")).toHaveCount(0);
  } finally {
    await fixture.context.close();
  }
});
