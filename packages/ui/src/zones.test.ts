import { describe, expect, it } from "vitest";
import { ZONES, activePage, visibleZones, type Zone } from "./zones";

// Mirrors the real can() semantics (packages/api/src/permissions.tsx):
// "public" always resolves true, everything else must be explicitly
// granted. Tests use this instead of an arbitrary predicate so they
// exercise the same contract visibleZones relies on in production.
function canWith(granted: string[]): (permission: string) => boolean {
  return (permission) => permission === "public" || granted.includes(permission);
}

describe("visibleZones", () => {
  it("opens country policies for a reviewer without branding administration", () => {
    const settings = visibleZones(canWith(["workspace.country_policy.read"])).find(
      (zone) => zone.key === "settings",
    );
    expect(settings?.href).toBe("/settings/country-policies");
    expect(settings?.pages.map((page) => page.label)).toEqual(["Country policies"]);
  });
  it("opens PharmaConnect at the first permitted pharmacy screen", () => {
    const pharmacy = visibleZones(canWith(["pharmacy.catalogue.read"])).find(
      (zone) => zone.key === "pharmacy",
    );

    expect(pharmacy?.label).toBe("PharmaConnect");
    expect(pharmacy?.href).toBe("/pharmacy/catalogue");
    expect(pharmacy?.pages.map((page) => page.href)).toEqual(["/pharmacy/catalogue"]);
  });

  it("lists benefits only for a benefits reader, never for directory access", () => {
    const staffPages = (granted: string[]) =>
      visibleZones(canWith(granted))
        .find((zone) => zone.key === "workforce")
        ?.pages.map((page) => page.href);

    expect(staffPages([])).not.toContain("/workforce/benefits");
    expect(staffPages(["workforce.payroll.view", "workforce.enrollment.manage"])).not.toContain(
      "/workforce/benefits",
    );
    expect(staffPages(["workforce.benefits.view"])).toContain("/workforce/benefits");
  });

  it("hides zones whose permission the user lacks", () => {
    const keys = visibleZones(canWith(["pharmacy.dispense.read"])).map((z) => z.key);

    expect(keys).toContain("pharmacy");
    expect(keys).not.toContain("lab");
  });

  it("always keeps the dashboard", () => {
    expect(visibleZones(canWith([])).map((z) => z.key)).toEqual(["dashboard", "workforce"]);
  });

  it("filters pages within a visible zone", () => {
    const pharmacy = visibleZones(canWith(["pharmacy.dispense.read"])).find(
      (z) => z.key === "pharmacy",
    );

    expect(pharmacy?.pages.map((p) => p.label)).toEqual(["Dispenses"]);
  });

  it("declares a permission on every zone and page", () => {
    for (const zone of ZONES) {
      expect(zone.permission).toBeTruthy();
      for (const page of zone.pages) expect(page.permission).toBeTruthy();
    }
  });

  it("defers to the caller's can() for public pages instead of special-casing them internally", () => {
    // Deliberately does NOT default "public" to true (unlike canWith
    // above). The pre-fix zones.ts filtered pages with
    // `page.permission === "public" || can(page.permission)` — an inline
    // special case that kept a "public" page regardless of what `can`
    // said. The fix removed that special case so pages are filtered by
    // `can(page.permission)` alone, matching how the real
    // usePermissions().can() is the only place "public" is resolved. This
    // stub makes that difference observable: it grants the zone's own
    // permission but never "public", so it discriminates the two
    // implementations for the same input.
    const strictCan = (permission: string) => permission === "reports.view";

    const nonDashboardPublicPageZone: Zone = {
      ...ZONES[0],
      key: "reports",
      label: "Reports",
      href: "/reports",
      permission: "reports.view",
      pages: [{ ...ZONES[0].pages[0], href: "/reports", permission: "public" }],
    };

    const keys = visibleZones(strictCan, [nonDashboardPublicPageZone]).map((z) => z.key);

    // Pre-fix: zone-level check is `zone.key === "dashboard" ||
    // can(zone.permission)` — "reports" isn't "dashboard" but
    // can("reports.view") is granted, so the zone passes; its one page is
    // then kept by the inline `permission === "public"` special case,
    // so the zone would appear.
    // Post-fix: the page is filtered by `can("public")` alone, which
    // strictCan denies, leaving the zone with zero pages — so it is
    // dropped by the `zone.pages.length > 0` check.
    expect(keys).not.toContain("reports");
  });

  it("drops a zone whose pages are all filtered out, rather than showing it empty", () => {
    const emptyZone: Zone = {
      ...ZONES[1],
      key: "empty",
      label: "Empty",
      permission: "empty.zone.read",
      pages: [{ ...ZONES[1].pages[0], permission: "empty.page.read" }],
    };
    const zones = [...ZONES, emptyZone];

    // The zone-level permission is granted, but every one of its pages
    // is filtered out — the zone itself must not appear.
    const keys = visibleZones(canWith(["empty.zone.read"]), zones).map((z) => z.key);

    expect(keys).not.toContain("empty");
  });
});

it("offers the supply directory to non-clinical purchasing users", () => {
  const zones = visibleZones(canWith(["supply.directory.read"]));
  const supply = zones.find((zone) => zone.key === "supply");
  expect(supply?.href).toBe("/supply/parties");
  expect(supply?.pages.map((page) => page.href)).toEqual(["/supply/parties"]);
  expect(zones.some((zone) => zone.key === "pharmacy")).toBe(false);
});

it("opens only crew assignments for an emergency crew user", () => {
  const emergency = visibleZones(canWith(["emergency.crew.read"])).find(
    (zone) => zone.key === "emergency",
  );
  expect(emergency?.href).toBe("/emergency/crew");
  expect(emergency?.pages.map((page) => page.href)).toEqual(["/emergency/crew"]);
});

it("opens only the receiving inbox for a hospital receiver", () => {
  const emergency = visibleZones(canWith(["emergency.receiving.read"])).find(
    (zone) => zone.key === "emergency",
  );
  expect(emergency?.href).toBe("/emergency/receiving");
  expect(emergency?.pages.map((page) => page.href)).toEqual(["/emergency/receiving"]);
});

describe("activePage", () => {
  const zone = (key: string) => ZONES.find((z) => z.key === key)!;

  it("marks an exact page match", () => {
    expect(activePage(zone("pharmacy"), "/pharmacy/medications")?.href).toBe(
      "/pharmacy/medications",
    );
  });
  it("marks the page a nested path sits under", () => {
    expect(activePage(zone("emergency"), "/emergency/bases/b/cases/c")?.href).toBe("/emergency");
  });
  it("prefers the longest matching page over a shorter prefix", () => {
    expect(activePage(zone("workforce"), "/workforce/setup")?.href).toBe("/workforce/setup");
  });
  it("matches only at a segment boundary", () => {
    expect(activePage(zone("pharmacy"), "/pharmacyx")).toBeUndefined();
  });
  it("marks nothing when no page matches", () => {
    expect(activePage(zone("pharmacy"), "/lab")).toBeUndefined();
  });
});

// #1270 D1: the rail reads in one order, grouped by use, whatever order
// zones were added in.
describe("zone order", () => {
  it("lists zones grouped by use, settings last", () => {
    expect(ZONES.map((zone) => zone.key)).toEqual([
      "dashboard",
      "patients",
      "medicore",
      "lab",
      "pharmacy",
      "emergency",
      "supply",
      "workforce",
      "settings",
    ]);
  });

  it("keeps each group contiguous", () => {
    const runs = ZONES.map((zone) => zone.group).filter(
      (group, index, groups) => group !== groups[index - 1],
    );
    expect(runs).toEqual(["overview", "care", "operations", "admin"]);
  });
});
