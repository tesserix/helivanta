import { describe, expect, it } from "vitest";
import { ZONES, activePage, railName, visibleZones, type Zone } from "./zones";

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
    // #576: the overview is the landing page for anyone with an outlet role.
    expect(pharmacy?.href).toBe("/pharmacy");
    expect(pharmacy?.pages.map((page) => page.href)).toEqual(["/pharmacy", "/pharmacy/catalogue"]);
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

    expect(pharmacy?.pages.map((p) => p.label)).toEqual(["Dispense queue"]);
    // A doctor, who only reads the queue, still opens at the queue.
    expect(pharmacy?.href).toBe("/pharmacy/dispense");
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
// #1286 D2: a collapsed rail shows this under the icon, so it must fit.
it("gives every zone a short rail label", () => {
  for (const zone of ZONES) expect(zone.railLabel, zone.key).toMatch(/^.{1,9}$/);
});

// #1369 (WCAG 2.5.3): a rail link's name holds the words it shows, the short
// label collapsed and the full one expanded.
it("names every rail link with both of its labels", () => {
  for (const zone of ZONES) {
    const name = railName(zone).toLowerCase();
    expect(name, zone.key).toContain(zone.railLabel.toLowerCase());
    expect(name, zone.key).toContain(zone.label.toLowerCase());
  }
  expect(railName(ZONES.find((zone) => zone.key === "pharmacy")!)).toBe("Pharmacy (PharmaConnect)");
  expect(railName(ZONES.find((zone) => zone.key === "lab")!)).toBe("Lab");
});

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

// #1258 D1/D5: an icon tells pages apart only if it is not shared, and a
// section heading appears once only if its pages sit together.
describe("zone icons and sections", () => {
  it("gives every rail zone its own icon", () => {
    const icons = ZONES.map((zone) => zone.icon);
    expect(new Set(icons).size).toBe(icons.length);
  });

  it.each(ZONES.map((zone) => [zone.key, zone] as const))(
    "gives every %s page its own icon",
    (_key, zone) => {
      const icons = zone.pages.map((page) => page.icon);
      expect(new Set(icons).size).toBe(icons.length);
    },
  );

  it.each(ZONES.map((zone) => [zone.key, zone] as const))(
    "keeps each %s section contiguous",
    (_key, zone) => {
      const runs = zone.pages
        .map((page) => page.section)
        .filter((section, index, sections) => section !== sections[index - 1]);
      expect(new Set(runs).size).toBe(runs.length);
    },
  );

  it("structures PharmaConnect as pharmacy management", () => {
    const pharmacy = ZONES.find((zone) => zone.key === "pharmacy")!;
    expect(pharmacy.pages.map((page) => [page.section, page.label, page.href])).toEqual([
      [undefined, "Overview", "/pharmacy"],
      ["Dispensing", "Counter sale", "/pharmacy/counter"],
      ["Dispensing", "Returns", "/pharmacy/returns"],
      ["Dispensing", "Daily sales", "/pharmacy/daily-report"],
      ["Dispensing", "H1 register", "/pharmacy/h1-register"],
      ["Dispensing", "Prescriptions", "/pharmacy/prescriptions"],
      ["Dispensing", "Dispense queue", "/pharmacy/dispense"],
      ["Dispensing", "Medication list", "/pharmacy/medications"],
      ["Inventory", "Stock across outlets", "/pharmacy/stock-overview"],
      ["Inventory", "Stock on hand", "/pharmacy/stock"],
      ["Inventory", "Receiving", "/pharmacy/receiving"],
      ["Inventory", "Expiry & safety", "/pharmacy/safety"],
      ["Inventory", "Product catalogue", "/pharmacy/catalogue"],
      ["Purchasing", "Supplier bills", "/pharmacy/accounts"],
      ["Setup", "Outlets & access", "/pharmacy/setup"],
    ]);
  });
});

// #1366: width is a zone's declared choice, so its pages never jump in width
// as you move between them. A page cannot set its own (ZonePage has no width).
describe("zone width", () => {
  it("declares full width only for the zones that have chosen it", () => {
    expect(ZONES.filter((zone) => zone.width === "full").map((zone) => zone.key)).toEqual([
      "dashboard",
      "pharmacy",
      "workforce",
    ]);
  });
});

it.each(["iam.member.manage", "iam.assignment.manage", "iam.assignment.review"])(
  "shows Staff access to a caller holding %s",
  (permission) => {
    const pages = visibleZones((p) => p === "public" || p === permission).flatMap((z) => z.pages);
    expect(pages.some((p) => p.href === "/workforce/access")).toBe(true);
  },
);

it("hides Staff access from callers without membership or assignment administration", () => {
  const pages = visibleZones((p) => p === "public").flatMap((z) => z.pages);
  expect(pages.some((p) => p.href === "/workforce/access")).toBe(false);
});

it("shows the scoped leave review entry without tenant-wide business grants", () => {
  const pages = visibleZones((permission) => permission === "public").flatMap((zone) => zone.pages);
  expect(pages.some((page) => page.href === "/workforce/leave-review")).toBe(true);
});
