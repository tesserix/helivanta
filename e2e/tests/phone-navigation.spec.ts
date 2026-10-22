import { test, expect, type Locator } from "@playwright/test";
import { login } from "./support/login";

// #1270 D4/D5: on a 390px phone every page of a multi-page zone is reachable,
// and the rail and page overlays close once a destination is chosen.
test.use({ viewport: { width: 390, height: 844 } });

async function onScreen(locator: Locator) {
  const box = await locator.boundingBox();
  return box !== null && box.width > 0 && box.x >= 0 && box.x + box.width <= 390;
}

test("a multi-page zone's pages open from the header and close on choice", async ({ page }) => {
  await login(page);
  await page.goto("/workforce");
  const pages = page.getByRole("navigation", { name: "Staff" });
  await expect(pages).toBeHidden();

  await page.getByRole("button", { name: "Expand page panel" }).click();
  const shifts = pages.getByRole("link", { name: "Shifts", exact: true });
  await expect(shifts).toBeVisible();
  expect(await onScreen(shifts)).toBe(true);

  await shifts.click();
  await expect(page).toHaveURL(/\/workforce\/shifts$/);
  await expect(pages).toBeHidden();
});

test("the expanded rail closes once a zone is chosen", async ({ page }) => {
  await login(page);
  await page.goto("/dashboard");
  await page.getByRole("button", { name: "Expand navigation" }).click();
  const staff = page.getByRole("link", { name: "Staff", exact: true });
  await expect(staff).toContainText("Staff");

  await staff.click();
  await expect(page).toHaveURL(/\/workforce$/);
  await expect(page.getByRole("button", { name: "Expand navigation" })).toBeVisible();
});
