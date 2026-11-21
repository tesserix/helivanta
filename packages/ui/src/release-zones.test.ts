import { describe, expect, it } from "vitest";
import { visibleZones } from "./zones";

const foundation = new Set([
  "baseline",
  "onboarding",
  "team_accounts",
  "staff_core",
  "department_teams",
  "pharmacy_setup",
  "pharmacy_staff",
]);

describe("released navigation", () => {
  it("an owner with every permission sees only the foundation release", () => {
    const zones = visibleZones(
      () => true,
      (key) => foundation.has(key ?? ""),
    );
    for (const key of ["patients", "medicore", "lab", "supply", "emergency", "billing"])
      expect(zones.some((zone) => zone.key === key)).toBe(false);
    const staff = zones.find((zone) => zone.key === "workforce")!;
    expect(staff.pages.map((page) => page.href)).toEqual(
      expect.arrayContaining(["/workforce", "/workforce/access", "/workforce/department-teams"]),
    );
    for (const path of [
      "/workforce/rosters",
      "/workforce/leave-requests",
      "/workforce/payroll-setup",
      "/workforce/benefits",
    ])
      expect(staff.pages.some((page) => page.href === path)).toBe(false);
    const pharmacy = zones.find((zone) => zone.key === "pharmacy")!;
    expect(pharmacy.href).toBe("/pharmacy/setup");
    expect(pharmacy.pages.map((page) => page.href)).toEqual([
      "/pharmacy/stocktake-recovery",
      "/pharmacy/staff",
      "/pharmacy/setup",
    ]);
  });

  it("unknown or unavailable release state does not reveal permission-eligible business pages", () => {
    const zones = visibleZones(
      () => true,
      (key) => key === "baseline",
    );
    expect(zones.flatMap((zone) => zone.pages.map((page) => page.href))).toEqual([
      "/dashboard",
      "/settings/security",
    ]);
  });

  it("release availability never grants a staff permission", () => {
    const zones = visibleZones(
      (permission) => permission === "public",
      () => true,
    );
    expect(
      zones.flatMap((zone) => zone.pages).some((page) => page.permission === "iam.member.manage"),
    ).toBe(false);
  });
});

it("keeps retained stocktake recovery reachable to stock staff without access-management authority", () => {
  const zones = visibleZones(
    (permission) =>
      ["public", "pharmacy.catalogue.read", "pharmacy.stock.read"].includes(permission),
    (key) => foundation.has(key ?? ""),
  );
  const pharmacy = zones.find((zone) => zone.key === "pharmacy")!;
  expect(pharmacy?.pages.map((page) => page.href)).toEqual([
    "/pharmacy/stocktake-recovery",
    "/pharmacy/staff",
  ]);
  expect(pharmacy.href).toBe("/pharmacy/stocktake-recovery");
});

it("offers the shared shipment workspace only with the release flag and shipment permission", () => {
  const can = (permission: string) => ["public", "partner.shipment.read"].includes(permission);
  const paths = (released: (key: string | undefined) => boolean) =>
    visibleZones(can, released).flatMap((zone) => zone.pages.map((page) => page.href));
  expect(paths((key) => key === "baseline" || key === "supplier_collaboration")).toContain(
    "/workforce/shared-shipments",
  );
  expect(paths((key) => key === "baseline")).not.toContain("/workforce/shared-shipments");
  expect(
    visibleZones(
      (permission) => permission === "public",
      () => true,
    ).flatMap((zone) => zone.pages.map((page) => page.href)),
  ).not.toContain("/workforce/shared-shipments");
});
