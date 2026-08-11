import { describe, expect, it } from "vitest";
import { ZONES, visibleZones, type Zone } from "./zones";

// Mirrors the real can() semantics (packages/api/src/permissions.tsx):
// "public" always resolves true, everything else must be explicitly
// granted. Tests use this instead of an arbitrary predicate so they
// exercise the same contract visibleZones relies on in production.
function canWith(granted: string[]): (permission: string) => boolean {
  return (permission) => permission === "public" || granted.includes(permission);
}

describe("visibleZones", () => {
  it("hides zones whose permission the user lacks", () => {
    const keys = visibleZones(canWith(["pharmacy.dispense.read"])).map((z) => z.key);

    expect(keys).toContain("pharmacy");
    expect(keys).not.toContain("lab");
  });

  it("always keeps the dashboard", () => {
    expect(visibleZones(canWith([])).map((z) => z.key)).toEqual(["dashboard"]);
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
