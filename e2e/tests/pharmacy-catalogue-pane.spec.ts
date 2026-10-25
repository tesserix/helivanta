import { expect, test, type Page } from "@playwright/test";
import { addProduct, expectList, freshOutlet, newOutlet } from "./support/pharmacy";

// #1303 E2: a product opens beside the catalogue where main is at least
// 840px wide, and alone below that. E8: the address carries the outlet, so
// a shared link opens it wherever the receiver last worked.

async function seeded(page: Page, count = 3) {
  const { outlet, stamp } = await freshOutlet(page);
  for (let n = 1; n <= count; n++)
    await addProduct(page, outlet, `TEST-CAT-${stamp}-${n}`, `TEST Pane ${stamp} ${n}`);
  return stamp;
}
// The list's Open buttons name the SKU; the pane names the brand.
const brandOf = (label: string, stamp: string) =>
  `TEST Pane ${stamp} ${label.replace(`Open TEST-CAT-${stamp}-`, "")}`;

for (const [label, size, rail] of [
  ["1366 rail expanded", { width: 1366, height: 768 }, "expanded"],
  ["1920", { width: 1920, height: 1080 }, "collapsed"],
] as const) {
  test(`a product opens beside the catalogue at ${label}`, async ({ page }) => {
    await page.setViewportSize(size);
    const stamp = await seeded(page);
    await page.goto("/pharmacy/catalogue");
    if (rail === "expanded") await page.getByRole("button", { name: "Expand navigation" }).click();
    const opens = page.getByRole("button", { name: new RegExp(`^Open TEST-CAT-${stamp}-`) });
    await expect(opens).toHaveCount(3);
    // Measure once the page has settled: the pager loads after the rows.
    await expect(page.getByText("1–3 of 3")).toBeVisible();
    const [first, second] = [
      (await opens.nth(0).getAttribute("aria-label"))!,
      (await opens.nth(1).getAttribute("aria-label"))!,
    ];
    const button = page.getByRole("button", { name: first, exact: true });
    const row = button.locator("xpath=ancestor::li");
    const before = (await row.boundingBox())!.y;
    await button.click();
    const pane = page.getByRole("region", { name: `Product ${brandOf(first, stamp)}` });
    await expect(pane).toBeVisible();
    // Side by side: the pane starts where the list ends, and the row stayed.
    const list = (await row.boundingBox())!;
    expect((await pane.boundingBox())!.x).toBeGreaterThanOrEqual(list.x + list.width);
    test
      .info()
      .annotations.push({ type: "row shift", description: `${Math.round(list.y - before)}px` });
    expect(Math.abs(list.y - before)).toBeLessThanOrEqual(2);

    await page.getByRole("button", { name: "Next product" }).click();
    const next = page.getByRole("region", { name: `Product ${brandOf(second, stamp)}` });
    await expect(next).toBeVisible();
    await page.reload();
    await expect(next).toBeVisible();
  });
}

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("a product takes the screen at its top, and Back returns to the list", async ({ page }) => {
    const stamp = await seeded(page, 8);
    await page.goto("/pharmacy/catalogue");
    const opens = page.getByRole("button", { name: new RegExp(`^Open TEST-CAT-${stamp}-`) });
    await expect(opens).toHaveCount(8);
    const label = (await opens.last().getAttribute("aria-label"))!;
    const last = page.getByRole("button", { name: label, exact: true });
    // From the bottom of a scrolled list, the record still opens at its top.
    const scroller = page.locator("[data-hms-scroll]");
    await scroller.evaluate((node) => node.scrollTo({ top: node.scrollHeight }));
    expect(await scroller.evaluate((node) => node.scrollTop)).toBeGreaterThan(0);
    await last.click();
    await expect(
      page.getByRole("region", { name: `Product ${brandOf(label, stamp)}` }),
    ).toBeVisible();
    await expect.poll(() => scroller.evaluate((node) => node.scrollTop)).toBe(0);
    await page.goBack();
    await expectList(page, "/pharmacy/catalogue", {});
    await expect(last).toBeVisible();
  });
});

test("a shared link opens its outlet's product, whatever outlet the receiver last used", async ({
  page,
  context,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const stamp = await seeded(page, 1);
  await page.goto("/pharmacy/catalogue");
  await page.getByRole("button", { name: `Open TEST-CAT-${stamp}-1` }).click();
  await expect(page.getByRole("region", { name: `Product TEST Pane ${stamp} 1` })).toBeVisible();
  const link = page.url();
  expect(new URL(link).searchParams.get("outlet")).toBeTruthy();

  // Another tab whose working outlet is a different one.
  const other = await context.newPage();
  await other.setViewportSize({ width: 1440, height: 900 });
  await newOutlet(other);
  await other.goto(link);
  await expect(other.getByRole("region", { name: `Product TEST Pane ${stamp} 1` })).toBeVisible();
  // The link's outlet, not the tab's own: only it holds this product.
  expect(new URL(other.url()).searchParams.get("outlet")).toBe(
    new URL(link).searchParams.get("outlet"),
  );
});
