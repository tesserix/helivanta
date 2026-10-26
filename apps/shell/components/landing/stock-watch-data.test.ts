import { expect, it } from "vitest";
import { moduleStatus } from "@/app/platform-catalog";
import {
  findingOf,
  routeItems,
  routes,
  shelf,
  stepTitle,
  steps,
  stockModules,
  tally,
  type ShelfItem,
} from "./stock-watch-data";

const item = (overrides: Partial<ShelfItem>): ShelfItem => ({
  id: "item",
  medicine: "Synthetic tablets",
  unit: "tablets",
  dispensable: 500,
  quarantined: 0,
  reorderAt: 200,
  expiresInDays: 400,
  ...overrides,
});

it("marks each medicine by one precedence: out, quarantine, running low, expiring, in stock", () => {
  for (const [overrides, want] of [
    [{ dispensable: 0, quarantined: 50 }, "out"],
    [{ quarantined: 50, dispensable: 10 }, "quarantine"],
    [{ dispensable: 199, expiresInDays: 5 }, "low"],
    [{ dispensable: 200 }, "in-stock"],
    [{ expiresInDays: 30 }, "expiring"],
    [{ expiresInDays: 0 }, "expiring"],
    [{ expiresInDays: 31 }, "in-stock"],
    [{ expiresInDays: undefined }, "in-stock"],
  ] as const) {
    expect(findingOf(item(overrides)), JSON.stringify(overrides)).toBe(want);
  }
});

it("counts every medicine on the shelf under exactly one finding", () => {
  const counts = tally();
  expect(Object.values(counts).reduce((sum, n) => sum + n, 0)).toBe(shelf.length);
  for (const [finding, count] of Object.entries(counts)) {
    expect(count, finding).toBe(shelf.filter((entry) => findingOf(entry) === finding).length);
    expect(count, finding).toBeGreaterThan(0);
  }
  expect(new Set(shelf.map((entry) => entry.id)).size).toBe(shelf.length);
});

it("routes every finding that needs a decision exactly once and leaves stock that is fine alone", () => {
  expect(routes.flatMap((route) => route.findings).sort()).toEqual([
    "expiring",
    "low",
    "out",
    "quarantine",
  ]);
  for (const route of routes) expect(routeItems(route), route.id).not.toHaveLength(0);
});

it("names every routed medicine in its route's first step", () => {
  for (const route of routes) {
    const text = steps.find((step) => step.route === route.id)!.text.toLowerCase();
    for (const entry of routeItems(route)) {
      expect(text, route.id).toContain(entry.medicine.split(" ")[0].toLowerCase());
    }
  }
});

it("ends every route with a named person, and drafts only where an agent is involved", () => {
  for (const route of routes) {
    const routeSteps = steps.filter((step) => step.route === route.id);
    expect(routeSteps.at(-1)!.phase, route.id).toBe("approve");
    expect(routeSteps.at(-1)!.text, route.id).toContain(route.approver.name);
    expect(stepTitle(routeSteps.at(-1)!), route.id).toContain(route.approver.name);
    if (!route.agent) {
      expect(
        routeSteps.some((step) => step.phase === "draft"),
        route.id,
      ).toBe(false);
      expect(routeSteps[0].text, route.id).toMatch(/No agent/);
    }
  }
});

it("starts from the stock register and the check before any route", () => {
  expect(steps.slice(0, 2).map((step) => step.phase)).toEqual(["register", "check"]);
  expect(steps.slice(2).every((step) => step.route !== undefined)).toBe(true);
});

it("labels only the pharmacy register as available and everything automatic as planned", () => {
  expect(moduleStatus(stockModules[0])).toBe("Foundation available");
  expect(stockModules[0]).toBe("Pharmacy management");
  for (const name of stockModules.slice(1))
    expect(moduleStatus(name)).toMatch(/^(Planned|Future)$/);
  expect(stockModules).toContain("AI-assisted workflows");
});
