import { expect, test, type Page } from "@playwright/test";
import { addBill, expectLabelsInNames, expectList, freshOutlet } from "./support/pharmacy";

// #1303: a bill opens beside the list where main is at least 840px wide,
// and alone below that. The layout follows main's width, so the rail
// expanding on a laptop is a case of its own.

const width = (page: Page) =>
  page.locator("main").evaluate((main) => {
    const style = getComputedStyle(main);
    return main.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
  });

async function seeded(page: Page) {
  const { outlet, stamp } = await freshOutlet(page);
  for (const n of [1, 2, 3]) await addBill(page, outlet, `TEST-PANE-${stamp}-${n}`);
  return stamp;
}

for (const [label, size, rail] of [
  ["1366 rail collapsed", { width: 1366, height: 768 }, "collapsed"],
  ["1366 rail expanded", { width: 1366, height: 768 }, "expanded"],
  ["1440 rail expanded", { width: 1440, height: 900 }, "expanded"],
  ["1920", { width: 1920, height: 1080 }, "collapsed"],
] as const) {
  test(`a bill opens beside the list at ${label}`, async ({ page }) => {
    await page.setViewportSize(size);
    const stamp = await seeded(page);
    await page.goto("/pharmacy/accounts?tab=all");
    if (rail === "expanded") await page.getByRole("button", { name: "Expand navigation" }).click();
    const content = await width(page);
    test.info().annotations.push({ type: "main content width", description: `${content}px` });
    // Every laptop case is a split (agreed: option A).
    expect(content).toBeGreaterThanOrEqual(840);

    // Bills list newest first: open the top row, then Next is the one below.
    const opens = page.getByRole("button", { name: new RegExp(`^Open TEST-PANE-${stamp}-`) });
    await expect(opens).toHaveCount(3);
    const name = (await opens.first().getAttribute("aria-label"))!;
    const ref = name.replace(/^Open /, "");
    const second = (await opens.nth(1).getAttribute("aria-label"))!.replace(/^Open /, "");
    const first = page.getByRole("button", { name, exact: true });
    // Measure once the page has settled: the outlet picker and the pager
    // load after the rows and would move them on their own.
    await expect(page.getByText(/^Times are in /)).toBeVisible();
    await expect(page.getByText("1–3 of 3")).toBeVisible();
    // The shell's scroll container: main's parent.
    const scrolled = () => page.locator("main").evaluate((main) => main.parentElement!.scrollTop);
    // The row's top edge: what the eye tracks. A narrower column can make the
    // row taller, which moves a centred button inside it, not the row.
    const row = first.locator("xpath=ancestor::tr");
    const before = { top: await scrolled(), row: (await row.boundingBox())!.y };
    await first.click();
    const pane = page.getByRole("region", { name: `Bill ${ref}` });
    await expect(pane).toBeVisible();
    await expectLabelsInNames(page);
    // Side by side: the pane starts where the list ends.
    const list = (await page.getByRole("table", { name: "Bills and expenses" }).boundingBox())!;
    expect((await pane.boundingBox())!.x).toBeGreaterThanOrEqual(list.x + list.width);
    // The page did not scroll away, and the row is where the eye left it.
    expect(await scrolled()).toBe(before.top);
    const after = (await row.boundingBox())!.y;
    test
      .info()
      .annotations.push({ type: "row shift", description: `${Math.round(after - before.row)}px` });
    expect(Math.abs(after - before.row)).toBeLessThanOrEqual(2);
    await page.getByRole("button", { name: "Next bill" }).click();
    await expect(page.getByRole("region", { name: `Bill ${second}` })).toBeVisible();

    await page.reload();
    await expect(page.getByRole("region", { name: `Bill ${second}` })).toBeVisible();
  });
}

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("a bill takes the screen, and Back returns to the list", async ({ page }) => {
    const stamp = await seeded(page);
    await page.goto("/pharmacy/accounts?tab=all");
    await page.getByRole("button", { name: `Open TEST-PANE-${stamp}-1` }).click();
    await expect(page.getByRole("region", { name: `Bill TEST-PANE-${stamp}-1` })).toBeVisible();
    await expect(page.getByRole("table", { name: "Bills and expenses" })).toBeHidden();
    await page.goBack();
    await expectList(page, "/pharmacy/accounts", { tab: "all" });
    await expect(page.getByRole("button", { name: `Open TEST-PANE-${stamp}-1` })).toBeVisible();
  });

  // Review Critical 1: Back cannot be cancelled, and the router moves first.
  // An unsaved payment must survive it until the person chooses.
  test("Back with an unsaved payment asks first and keeps it", async ({ page }) => {
    const stamp = await seeded(page);
    await page.goto("/pharmacy/accounts?tab=all");
    await page.getByRole("button", { name: `Open TEST-PANE-${stamp}-1` }).click();
    await page.getByLabel("Paid by").fill("TEST payer");
    await page.goBack();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText(`Discard changes to TEST-PANE-${stamp}-1?`);
    await expect(page).toHaveURL(/open=/);
    await dialog.getByRole("button", { name: "Keep editing" }).click();
    await expect(page.getByLabel("Paid by")).toHaveValue("TEST payer");
    await expect(page).toHaveURL(/open=/);

    await page.goBack();
    await page.getByRole("dialog").getByRole("button", { name: "Discard" }).click();
    await expectList(page, "/pharmacy/accounts", { tab: "all" });
    await expect(page.getByRole("region", { name: /^Bill / })).toHaveCount(0);
  });
});

test("an unsaved payment asks before a rail link leaves the page", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const stamp = await seeded(page);
  await page.goto("/pharmacy/accounts?tab=all");
  await page.getByRole("button", { name: `Open TEST-PANE-${stamp}-1` }).click();
  await page.getByLabel("Paid by").fill("TEST payer");
  await page.getByRole("link", { name: "Dashboard" }).first().click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText(`Discard changes to TEST-PANE-${stamp}-1?`);
  await dialog.getByRole("button", { name: "Keep editing" }).click();
  await expectList(page, "/pharmacy/accounts", { tab: "all" }, { open: true });
  await expect(page.getByLabel("Paid by")).toHaveValue("TEST payer");
});
