import { describe, expect, it } from "vitest";
import { ZONES, visibleZones } from "./zones";

describe("visibleZones", () => {
  it("hides zones whose permission the user lacks", () => {
    const keys = visibleZones((p) => p === "pharmacy.dispense.read").map((z) => z.key);

    expect(keys).toContain("pharmacy");
    expect(keys).not.toContain("lab");
  });

  it("always keeps the dashboard", () => {
    expect(visibleZones(() => false).map((z) => z.key)).toEqual(["dashboard"]);
  });

  it("filters pages within a visible zone", () => {
    const pharmacy = visibleZones((p) => p === "pharmacy.dispense.read").find(
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
});
