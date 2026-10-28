import { test, expect, type Page } from "@playwright/test";
import { login } from "./support/login";
import { waitingFor } from "./support/queue";

// #1255: the dashboard and its floating panels fit a 390px phone. Measured on
// the rendered page: no element inside <main> reaches past the viewport, and
// each popover lies wholly on screen.
test.use({ viewport: { width: 390, height: 844 } });

async function onScreen(page: Page, name: string) {
  const box = (await page.getByRole("dialog", { name }).boundingBox())!;
  expect(box.x, `${name} starts off screen`).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width, `${name} ends off screen`).toBeLessThanOrEqual(390);
}

// Elements inside <main> that reach past the right edge of the viewport.
function overflowing(page: Page) {
  return page.evaluate(() =>
    [...document.querySelectorAll("main *")]
      .filter((el) => el.getBoundingClientRect().right > window.innerWidth + 1)
      .filter((el) => !el.closest("[data-scroll-x]"))
      .map((el) => (el.getAttribute("aria-label") ?? el.textContent ?? el.tagName).slice(0, 40)),
  );
}

test("the dashboard fits a phone", async ({ page }) => {
  await login(page);
  await page.goto("/dashboard");
  await page.getByRole("region", { name: "Laboratory", exact: true }).waitFor();
  await page.waitForLoadState("networkidle");
  expect(await overflowing(page)).toEqual([]);

  await page.getByRole("button", { name: /Custom range/ }).click();
  await onScreen(page, "Custom range");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: /Custom range/ })).toBeFocused();

  await page.getByRole("button", { name: "About Patient activity" }).click();
  await onScreen(page, "About Patient activity");
});

// #1293: a waiting row's medication form fits a phone, and the patient's name
// keeps a readable width instead of wrapping a letter at a time.
test("the dispense queue fits a phone", async ({ page }) => {
  await login(page);
  const patient = `Phone Patient ${Date.now()}`;
  await page.goto("/medicore/opd");
  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  await page.goto(waitingFor(patient));
  const name = page.getByText(patient).first();
  await expect(name).toBeAttached({ timeout: 30_000 });
  expect(await overflowing(page)).toEqual([]);
  expect((await name.boundingBox())!.width, "patient name squeezed").toBeGreaterThan(120);
});
