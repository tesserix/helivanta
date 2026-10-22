import { test, expect, type Page } from "@playwright/test";
import { login, specAdmin } from "./support/login";

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
// panel link marked current. A zone with one visible page has no panel
// (#1255 spec D4), so its own page shows as the header title instead. A route
// that failed to move would 404, and one wired to the wrong component would
// mark a different page current.
test("every department URL still lands on its own page", async ({ page }) => {
  await login(page);
  for (const { path, lands, current, title } of [
    { path: "/medicore", lands: /\/medicore\/opd$/, current: "OPD" },
    { path: "/medicore/opd", lands: /\/medicore\/opd$/, current: "OPD" },
    { path: "/medicore/ipd", lands: /\/medicore\/ipd$/, current: "IPD" },
    { path: "/pharmacy", lands: /\/pharmacy$/, current: "Dispense queue" },
    {
      path: "/pharmacy/medications",
      lands: /\/pharmacy\/medications$/,
      current: "Medication list",
    },
    { path: "/lab", lands: /\/lab$/, title: "Lab" },
  ] as { path: string; lands: RegExp; current?: string; title?: string }[]) {
    await page.goto(path);
    await expect(page, `${path} landed somewhere else`).toHaveURL(lands);
    await expect(
      current
        ? page.locator('a[aria-current="page"]', { hasText: current })
        : page.getByRole("heading", { level: 1, name: title }),
      `${path} did not render its own page`,
    ).toBeVisible();
  }
});

// #1091, design spec docs/superpowers/specs/2026-10-20-workspace-layout-design.md
// D6. The chrome is mounted once, by app/(workspace)/layout.tsx, and survives
// every client-side move. The proof is a property set on the rail's own DOM
// node: a remount replaces the node and the property is gone, so this tests
// "same element" directly rather than "looks the same".
//
// Every rail link is visited, read from the rail at runtime, so a zone added
// later is covered without editing this spec.
const CHROME_MARKER = "__chromeMarker";

async function markChrome(page: Page) {
  await page
    .getByRole("navigation", { name: "Zones" })
    .evaluate(
      (el, key) => ((el as unknown as Record<string, string>)[key] = "same-node"),
      CHROME_MARKER,
    );
}

async function chromeMarker(page: Page) {
  return page
    .getByRole("navigation", { name: "Zones" })
    .evaluate((el, key) => (el as unknown as Record<string, string>)[key] ?? null, CHROME_MARKER);
}

async function goByRail(page: Page, name: string) {
  const link = page
    .getByRole("navigation", { name: "Zones" })
    .getByRole("link", { name, exact: true });
  const href = await link.getAttribute("href");
  await link.click();
  await expect(page).toHaveURL(
    new RegExp(`${(href ?? "").replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`),
  );
}

test("the chrome is not remounted when moving between departments", async ({ page }) => {
  await login(page);
  await goByRail(page, "MediCore");
  await markChrome(page);

  const names = await page
    .getByRole("navigation", { name: "Zones" })
    .getByRole("link")
    .evaluateAll((links) =>
      links.map((a) => a.getAttribute("aria-label") ?? a.textContent?.trim() ?? ""),
    );
  expect(names.length, "the rail lists no zones").toBeGreaterThan(1);

  for (const name of [...names.filter((n) => n !== "MediCore"), "MediCore"]) {
    await goByRail(page, name);
    expect(await chromeMarker(page), `moving to ${name} remounted the chrome`).toBe("same-node");
  }
});

// #1092. Each remount used to create a new idle tracker, whose mount-time
// seed bypassed the 60 s debounce and tripped the endpoint's burst of 3.
// After the one seed for this page load, rail clicks alone must send none.
test("moving between departments sends no session-activity requests", async ({ page }) => {
  const isActivity = (method: string, url: string) =>
    method === "POST" && url.endsWith("/auth/session/activity");
  const seeded = page.waitForResponse((r) => isActivity(r.request().method(), r.url()));
  await login(page);
  await goByRail(page, "MediCore");
  await seeded;

  let count = 0;
  page.on("request", (r) => {
    if (isActivity(r.method(), r.url())) count++;
  });
  for (const name of ["PharmaConnect", "Lab", "MediCore", "PharmaConnect"]) {
    await goByRail(page, name);
  }
  await page.waitForTimeout(1_000);
  expect(count, "rail navigation sent session-activity requests").toBe(0);
});

// D4: the tenant switcher is decided once by the layout, so it is on every
// authenticated page, department pages included. specAdmin has two tenants
// (scripts/seed-dev.mjs), so TenantPicker renders for them.
test("the tenant switcher is on every department page", async ({ page }) => {
  await login(page, specAdmin());
  for (const path of ["/medicore/opd", "/pharmacy", "/lab"]) {
    await page.goto(path);
    await expect(page.getByRole("combobox", { name: "Tenant" }), path).toBeVisible();
  }
});

// #1091: a page whose data is still loading renders inside the chrome; the
// rail is not torn down while the next page waits on the API.
test("a page that is still loading keeps the chrome", async ({ page }) => {
  await login(page);
  await goByRail(page, "MediCore");
  await markChrome(page);

  let release: () => void = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route("**/api/v1/pharmacy/**", async (route) => {
    await held;
    await route.continue();
  });
  await page
    .getByRole("navigation", { name: "Zones" })
    .getByRole("link", { name: "PharmaConnect", exact: true })
    .click();
  await expect(page).toHaveURL(/\/pharmacy$/);
  expect(await chromeMarker(page), "a loading page remounted the chrome").toBe("same-node");

  release();
});

// #1091 D7. The server cannot read localStorage, so its HTML renders the
// default chrome: panel open, rail collapsed. Without the pre-hydration
// script a clinician who collapsed the panel saw it painted open on every
// hard load, then snap shut. The widths are recorded from the first DOM
// mutation onward, so the first paint is included.
test("a collapsed panel is never painted open on a hard load", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await login(page);
  await page.goto("/pharmacy");
  await page.evaluate(() => {
    localStorage.setItem("hms.panel.open", "0");
    localStorage.setItem("hms.navigation.open", "1");
  });
  await page.addInitScript(() => {
    const seen: string[] = [];
    (window as unknown as { __chromeWidths: string[] }).__chromeWidths = seen;
    new MutationObserver(() => {
      const [rail, panel] = document.querySelectorAll("aside");
      if (rail && panel)
        seen.push(`rail=${getComputedStyle(rail).width} panel=${getComputedStyle(panel).width}`);
    }).observe(document, { subtree: true, childList: true, attributes: true });
  });
  await page.goto("/pharmacy");
  await expect(page.getByRole("button", { name: "Collapse navigation" })).toBeVisible();
  const seen = await page.evaluate(
    () => (window as unknown as { __chromeWidths: string[] }).__chromeWidths,
  );
  expect(seen.length, "no chrome was observed").toBeGreaterThan(0);
  expect([...new Set(seen)], "the chrome painted in a state the user did not choose").toEqual([
    "rail=208px panel=0px",
  ]);
});

// #1286 D1: the rail and the page panel float, sized to their items, not to
// the window. Measured from the rendered boxes, not inferred from classes.
test("the rail and the page panel are only as tall as their items", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await login(page);
  await page
    .getByRole("navigation", { name: "Zones" })
    .getByRole("link", { name: "PharmaConnect" })
    .click();
  await expect(page.getByRole("navigation", { name: "PharmaConnect" })).toBeVisible();

  for (const selector of ["aside.hms-rail", "aside.hms-page-panel"]) {
    const box = await page.locator(selector).boundingBox();
    expect(box, selector).not.toBeNull();
    expect(box!.y, `${selector} touches the top edge`).toBeGreaterThan(0);
    expect(box!.y + box!.height, `${selector} spans the window`).toBeLessThan(800);
  }
});
