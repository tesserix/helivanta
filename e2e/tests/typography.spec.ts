import { test, expect, type Page } from "@playwright/test";

// The UI face is IBM Plex Sans and the public page's headlines are Anek
// (#1834 spec D4). Asserted on what the browser computes and loads, not on a
// class name: a variable declared on the wrong element leaves every class in
// place while the page renders in the system fallback.

async function family(page: Page, selector: string): Promise<string> {
  return page
    .locator(selector)
    .first()
    .evaluate((el) => getComputedStyle(el).fontFamily);
}

test("the app face is Plex and the home headlines are Anek", async ({ page }) => {
  await page.goto("/");
  await page.evaluate(() => document.fonts.ready);

  const body = await family(page, "body");
  expect(body, "body font-family").toMatch(/plex/i);
  const paragraph = await family(page, "main p");
  expect(paragraph, "body text inherits the UI face").toMatch(/plex/i);
  const headline = await family(page, "h1");
  expect(headline, "home headline font-family").toMatch(/anek/i);

  // Loaded, not only named: a face that never downloads falls back silently.
  const primary = (value: string) => value.split(",")[0].trim();
  expect(
    await page.evaluate((name) => document.fonts.check(`16px ${name}`), primary(body)),
    "Plex is loaded",
  ).toBe(true);
  expect(
    await page.evaluate((name) => document.fonts.check(`48px ${name}`), primary(headline)),
    "Anek is loaded",
  ).toBe(true);
});
