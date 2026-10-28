import { test, expect, type Page } from "@playwright/test";
import { login, specAdmin } from "./support/login";
import { freshOutlet } from "./support/pharmacy";

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
    ["PharmaConnect", /\/pharmacy(?:\?|$)/],
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
    { path: "/pharmacy", lands: /\/pharmacy(?:\?|$)/, current: "Overview" },
    {
      path: "/pharmacy/dispense",
      lands: /\/pharmacy\/dispense(?:\?|$)/,
      current: "Dispense queue",
    },
    {
      path: "/pharmacy/medications",
      lands: /\/pharmacy\/medications(?:\?|$)/,
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
  expect(href, "the rail link has no destination").not.toBeNull();
  const target = new URL(href!, page.url());
  await link.click();
  await expect(page).toHaveURL(
    (url) => url.origin === target.origin && url.pathname === target.pathname,
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

test("remembered outlet scope survives rail navigation without remounting chrome", async ({
  page,
}) => {
  const { outlet } = await freshOutlet(page);
  const scope = outlet.match(/^\/pharmacy\/pharmacies\/([^/]+)\/outlets\/([^/]+)$/);
  expect(scope, "the outlet fixture did not return its owner scope").not.toBeNull();
  await page.goto("/medicore/opd");
  await markChrome(page);
  await goByRail(page, "Pharmacy (PharmaConnect)");
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === "/pharmacy" &&
      url.searchParams.get("pharmacy") === scope![1] &&
      url.searchParams.get("outlet") === scope![2],
  );
  expect(await chromeMarker(page), "restoring outlet scope remounted chrome").toBe("same-node");
  await goByRail(page, "EmergencyConnect");
  expect(await chromeMarker(page), "leaving the scoped pharmacy remounted chrome").toBe(
    "same-node",
  );
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
  for (const name of ["Pharmacy (PharmaConnect)", "Lab", "MediCore", "Pharmacy (PharmaConnect)"]) {
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
    .getByRole("link", { name: "Pharmacy (PharmaConnect)", exact: true })
    .click();
  await expect(page).toHaveURL(/\/pharmacy(\?|$)/);
  expect(await chromeMarker(page), "a loading page remounted the chrome").toBe("same-node");

  release();
});

// #1091 D7. The server cannot read localStorage, so its HTML renders the
// default chrome: rail collapsed. Without the pre-hydration script a clinician
// who expanded the rail saw it painted collapsed on every hard load, then snap
// open. #1286 D4: the page panel no longer collapses, so a collapse stored
// before then must not paint it shut. The widths are recorded from the first
// DOM mutation onward, so the first paint is included.
test("the stored chrome is painted from the first frame on a hard load", async ({ page }) => {
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
      const rail = document.querySelector("aside.hms-rail");
      const panel = document.querySelector("aside.hms-page-panel");
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
    "rail=208px panel=224px",
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

// #1286 D6: the header card starts at the rail's top and spans the panel and
// the page; the panel sits below it rather than beside it.
test("the header card runs from the rail across the panel, above it", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await login(page);
  await page
    .getByRole("navigation", { name: "Zones" })
    .getByRole("link", { name: "PharmaConnect" })
    .click();
  await expect(page.getByRole("navigation", { name: "PharmaConnect" })).toBeVisible();

  const rail = (await page.locator("aside.hms-rail").boundingBox())!;
  const header = (await page.getByRole("banner").boundingBox())!;
  const panel = (await page.locator("aside.hms-page-panel").boundingBox())!;
  expect(Math.abs(header.y - rail.y), "header top meets the rail top").toBeLessThan(1);
  expect(Math.abs(header.x - panel.x), "header starts where the panel does").toBeLessThan(1);
  expect(header.x + header.width, "header reaches the window edge").toBeGreaterThan(1260);
  expect(panel.y, "panel sits below the header").toBeGreaterThan(header.y + header.height);
});

// #1296: without a page panel the content starts at the header card's left
// edge rather than floating in the middle of the window. Measured on a wide
// screen, where centring opens the gap.
test("content lines up with the header on a page without a panel", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1000 });
  await login(page);
  await page.goto("/dashboard");
  const header = (await page.getByRole("banner").boundingBox())!;
  const content = (await page.locator("main > div").boundingBox())!;
  // The header's own text is inset 24px; content lines up with it.
  expect(content.x - header.x, "content offset from the header").toBeLessThanOrEqual(24);
});

// #1313: only the content area scrolls. A form's hidden native selects must
// stay inside it, or the whole document scrolls too: two scrollbars.
test("the page itself never scrolls, only the content area", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 924 });
  await freshOutlet(page);
  await page.goto("/pharmacy/setup");
  await expect(page.getByRole("button", { name: "Register outlet" })).toBeVisible();
  const overflow = await page.evaluate(() => {
    const doc = document.scrollingElement!;
    return doc.scrollHeight - doc.clientHeight;
  });
  expect(overflow).toBe(0);
});

// #1303 E7: every Load more on a pharmacy page says what it loads; the
// pickers' sat below the content, unnamed.
test("no pharmacy page shows an unnamed Load more", async ({ page }) => {
  await freshOutlet(page);
  // Each page's own heading: what is there once it has loaded.
  for (const [path, heading] of [
    ["stock", "Stock register"],
    ["receiving", "Quarantine receiving"],
    ["safety", "Concerns recorded in the last 30 days"],
    ["catalogue", "Product catalogue"],
    ["accounts", "Bills & expenses"],
    ["setup", /^Outlets at /],
  ] as const) {
    await page.goto(`/pharmacy/${path}`);
    await expect(page.getByRole("heading", { name: heading, level: 3 })).toBeVisible();
    await expect(page.getByRole("button", { name: "Load more", exact: true })).toHaveCount(0);
  }
});

// #1366: every PharmaConnect page is one width, and its content fills it, so
// moving between pages never makes the content jump.
test("every pharmacy page's content fills one width", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1000 });
  await freshOutlet(page);
  const widths: Record<string, { page: number; widest: number }> = {};
  for (const [path, loaded] of [
    ["", page.getByTestId("overview-tiles")],
    ["/dispense", page.getByRole("link", { name: "Waiting" })],
    ["/medications", page.getByText("Formulary", { exact: true })],
    ["/stock", page.getByRole("heading", { name: "Stock register" })],
    ["/receiving", page.getByRole("heading", { name: "Quarantine receiving" })],
    ["/safety", page.getByRole("heading", { name: "Concerns recorded in the last 30 days" })],
    ["/catalogue", page.getByRole("heading", { name: "Product catalogue", level: 3 })],
    ["/accounts", page.getByRole("heading", { name: "Bills & expenses" })],
    ["/setup", page.getByRole("heading", { name: /^Outlets at / })],
  ] as const) {
    await page.goto(`/pharmacy${path}`);
    await expect(loaded).toBeVisible();
    // The page's blocks and the panels in them: one of them spans the page.
    widths[path || "/"] = await page.locator("main > div").evaluate((content) => ({
      page: Math.round(content.getBoundingClientRect().width),
      widest: Math.max(
        ...[...content.querySelectorAll(":scope > *, :scope > * > *")].map((el) =>
          Math.round(el.getBoundingClientRect().width),
        ),
      ),
    }));
  }
  const first = Object.values(widths)[0].page;
  for (const [path, { page: width, widest }] of Object.entries(widths)) {
    expect(width, `${path} content width`).toBe(first);
    expect(widest, `${path} content fills its width`).toBe(width);
  }
});
