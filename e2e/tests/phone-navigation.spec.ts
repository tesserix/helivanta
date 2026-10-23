import { test, expect, type Locator } from "@playwright/test";
import { login } from "./support/login";

// #1270 D4/D5: on a 390px phone every page of a multi-page zone is reachable,
// and the rail and page overlays close once a destination is chosen.
test.use({ viewport: { width: 390, height: 844 } });

async function onScreen(locator: Locator) {
  const box = await locator.boundingBox();
  return box !== null && box.width > 0 && box.x >= 0 && box.x + box.width <= 390;
}

// #1299: on a phone the rail is gone and one header menu reaches every
// zone and the current zone's pages, closing once a destination is chosen.
test("the page uses the full width, with no rail column", async ({ page }) => {
  await login(page);
  await page.goto("/dashboard");
  await expect(page.locator("aside.hms-rail")).toBeHidden();
  const content = (await page.locator("main > div").boundingBox())!;
  expect(content.x, "content starts at the screen padding").toBeLessThanOrEqual(24);
});

test("the menu reaches a page in another zone and closes on choice", async ({ page }) => {
  await login(page);
  await page.goto("/dashboard");
  const open = page.getByRole("button", { name: "Open menu" });
  await open.click();
  const menu = page.getByRole("dialog", { name: "Menu" });
  await menu.getByRole("link", { name: "Staff", exact: true }).click();
  await expect(page).toHaveURL(/\/workforce$/);
  await expect(menu).toBeHidden();

  await open.click();
  const shifts = menu.getByRole("link", { name: "Shifts", exact: true });
  expect(await onScreen(shifts)).toBe(true);
  await shifts.click();
  await expect(page).toHaveURL(/\/workforce\/shifts$/);
  await expect(menu).toBeHidden();
});

test("Escape closes the menu", async ({ page }) => {
  await login(page);
  await page.goto("/dashboard");
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "Menu" })).toBeHidden();
  await expect(page.getByRole("button", { name: "Open menu" })).toBeFocused();
});
