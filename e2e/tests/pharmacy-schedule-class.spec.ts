import { expect, test, type Page } from "@playwright/test";
import { choose, readyOutlet } from "./support/pharmacy-counter";

// #1636: a schedule class is declared on the open product by one person and
// approved there by another; the database refuses the declarer's own review.

async function openProduct(page: Page, outlet: string, product: string) {
  const parts = outlet.split("/");
  const params = new URLSearchParams({
    pharmacy: parts.at(-3)!,
    outlet: parts.at(-1)!,
    open: product,
  });
  await page.goto(`/pharmacy/catalogue?${params}`);
  const section = page.getByRole("region", { name: "Schedule class" });
  await expect(section).toBeVisible();
  return section;
}

test("a pharmacist declares a schedule class and a second person approves it in the catalogue", async ({
  page,
  browser,
}) => {
  test.setTimeout(150_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, product } = fixture;
  try {
    const declarer = await openProduct(page, outlet, product);
    await choose(page, "Class", "H");
    await choose(page, "NDPS", "No");
    await declarer.getByLabel("Source", { exact: true }).fill("TEST pack label");
    await declarer.getByRole("button", { name: "Declare schedule class", exact: true }).click();
    await expect(
      declarer.getByText(
        "No schedule class in force: the latest declaration awaits review by a second person",
      ),
    ).toBeVisible();

    await declarer.getByLabel("What you checked").fill("TEST my own declaration");
    await declarer.getByRole("button", { name: "Approve", exact: true }).click();
    await expect(declarer.getByRole("alert")).toHaveText(
      "a schedule class is reviewed by someone other than who declared it",
    );

    const reviewer = await openProduct(cashier, outlet, product);
    await expect(reviewer.getByText(/^Awaiting review: Schedule H · not NDPS/)).toBeVisible();
    await reviewer.getByLabel("What you checked").fill("TEST pack checked");
    await reviewer.getByRole("button", { name: "Approve", exact: true }).click();
    await expect(reviewer.getByText(/^Schedule H · not NDPS, approved /)).toBeVisible();
  } finally {
    await fixture.context.close();
  }
});
