import { test, expect } from "@playwright/test";
import { login } from "./support/login";

// #933, design spec docs/superpowers/specs/2026-10-13-one-next-app-design.md
// D2: moving between departments is client-side navigation inside one
// running application, never a full page load.
//
// The proof is a marker set on `window` after the first page loads. A full
// page load replaces the JavaScript realm and the marker is gone; a
// client-side transition keeps it. Asserting the marker after each hop is
// therefore a direct test of "no full page load", not a proxy for it (a
// network-count check would also pass while a reload served from cache).
//
// Run against the multi-zone build first, where every rail hop crossed into
// another Next app: it failed at the first cross-department hop. Lint cannot
// enforce this (Next's no-html-link-for-pages only understands the pages/
// router), so this spec is the control.
test("moving between OPD, PharmaConnect and Lab never reloads the page", async ({ page }) => {
  await login(page);

  const zoneNav = page.getByRole("navigation", { name: "Zones" });
  await zoneNav.getByRole("link", { name: "MediCore" }).click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);

  await page.evaluate(() => {
    (window as unknown as { __navMarker?: string }).__navMarker = "still-here";
  });
  const marker = () =>
    page.evaluate(() => (window as unknown as { __navMarker?: string }).__navMarker ?? null);

  for (const [zone, url] of [
    ["PharmaConnect", /\/pharmacy$/],
    ["Lab", /\/lab$/],
    ["MediCore", /\/medicore\/opd$/],
  ] as const) {
    await zoneNav.getByRole("link", { name: zone }).click();
    await expect(page).toHaveURL(url);
    expect(await marker(), `moving to ${zone} reloaded the page`).toBe("still-here");
  }
});

// #933 acceptance: "every URL that resolved before resolves after". Each
// department URL is loaded directly (a hard load, the way a bookmark or a
// shared link arrives) and must land on its own page: the URL it is meant to
// (/medicore redirects to OPD, as it did under its basePath) with that page's
// panel link marked current. A route that failed to move would 404, and one
// wired to the wrong component would mark a different page current.
test("every department URL still lands on its own page", async ({ page }) => {
  await login(page);
  for (const { path, lands, current } of [
    { path: "/medicore", lands: /\/medicore\/opd$/, current: "OPD" },
    { path: "/medicore/opd", lands: /\/medicore\/opd$/, current: "OPD" },
    { path: "/medicore/ipd", lands: /\/medicore\/ipd$/, current: "IPD" },
    { path: "/pharmacy", lands: /\/pharmacy$/, current: "Dispenses" },
    { path: "/pharmacy/medications", lands: /\/pharmacy\/medications$/, current: "Medications" },
    { path: "/lab", lands: /\/lab$/, current: "Orders" },
  ]) {
    await page.goto(path);
    await expect(page, `${path} landed somewhere else`).toHaveURL(lands);
    await expect(
      page.locator('a[aria-current="page"]', { hasText: current }),
      `${path} did not render its own page`,
    ).toBeVisible();
  }
});
