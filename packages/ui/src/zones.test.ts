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

  it("renders a hypothetical second public zone, proving can(\"public\") itself resolves true", () => {
    const secondPublicZone: Zone = {
      ...ZONES[0],
      key: "reports",
      label: "Reports",
      href: "/reports",
      pages: [{ ...ZONES[0].pages[0], href: "/reports" }],
    };
    const zones = [...ZONES, secondPublicZone];

    // No granted permissions at all — only "public" resolving true (via
    // can(), not a zones.ts special case) should surface either zone.
    const keys = visibleZones(canWith([]), zones).map((z) => z.key);

    expect(keys).toEqual(["dashboard", "reports"]);
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
