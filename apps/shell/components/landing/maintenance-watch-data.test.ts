import { expect, it } from "vitest";
import { moduleStatus } from "@/app/platform-catalog";
import {
  CERTIFICATE_WINDOW_DAYS,
  DUE_SOON_DAYS,
  findingOf,
  maintenanceModules,
  maintenanceNote,
  register,
  routeItems,
  routes,
  stepTitle,
  steps,
  tally,
  type RegisterItem,
} from "./maintenance-watch-data";

const item = (overrides: Partial<RegisterItem>): RegisterItem => ({
  id: "item",
  asset: "Synthetic pump",
  area: "Equipment",
  check: "Synthetic test",
  evidence: "task",
  dueInDays: 90,
  lastResult: "pass",
  ...overrides,
});

it("marks each item by one precedence: failed, overdue, certificate expiring, due soon, current", () => {
  for (const [overrides, want] of [
    [{ lastResult: "fail", dueInDays: -5 }, "failed"],
    [{ lastResult: "fail", dueInDays: 200 }, "failed"],
    [{ dueInDays: -1 }, "overdue"],
    [{ evidence: "certificate", dueInDays: -1 }, "overdue"],
    [{ evidence: "certificate", dueInDays: 0 }, "expiring"],
    [{ evidence: "certificate", dueInDays: CERTIFICATE_WINDOW_DAYS }, "expiring"],
    [{ evidence: "certificate", dueInDays: CERTIFICATE_WINDOW_DAYS + 1 }, "current"],
    [{ evidence: "certificate", dueInDays: DUE_SOON_DAYS }, "expiring"],
    [{ dueInDays: 0 }, "due-soon"],
    [{ dueInDays: DUE_SOON_DAYS }, "due-soon"],
    [{ dueInDays: DUE_SOON_DAYS + 1 }, "current"],
  ] as const) {
    expect(findingOf(item(overrides)), JSON.stringify(overrides)).toBe(want);
  }
});

it("counts every register item under exactly one finding", () => {
  const counts = tally();
  expect(Object.values(counts).reduce((sum, n) => sum + n, 0)).toBe(register.length);
  for (const [finding, count] of Object.entries(counts)) {
    expect(count, finding).toBe(register.filter((entry) => findingOf(entry) === finding).length);
    expect(count, finding).toBeGreaterThan(0);
  }
  expect(new Set(register.map((entry) => entry.id)).size).toBe(register.length);
});

it("covers electrical, structural, equipment, fire, lift and medical gas checks", () => {
  expect(new Set(register.map((entry) => entry.area))).toEqual(
    new Set(["Electrical", "Structural", "Equipment", "Fire safety", "Lifts", "Medical gas"]),
  );
});

it("routes every finding that needs a decision exactly once and leaves current items alone", () => {
  expect(routes.flatMap((route) => route.findings).sort()).toEqual([
    "due-soon",
    "expiring",
    "failed",
    "overdue",
  ]);
  for (const route of routes) expect(routeItems(route), route.id).not.toHaveLength(0);
});

it("names every routed item with its due date in its route's first step", () => {
  for (const route of routes) {
    const text = steps.find((step) => step.route === route.id)!.text.toLowerCase();
    for (const entry of routeItems(route)) {
      expect(text, entry.id).toContain(entry.asset.toLowerCase());
      const finding = findingOf(entry);
      if (finding === "overdue")
        expect(text, entry.id).toContain(`${-entry.dueInDays} days overdue`);
      if (finding === "due-soon")
        expect(text, entry.id).toContain(`due in ${entry.dueInDays} days`);
      if (finding === "expiring")
        expect(text, entry.id).toContain(`expires in ${entry.dueInDays} days`);
    }
  }
});

it("states the check's own thresholds in the check step", () => {
  const check = steps.find((step) => step.phase === "check")!;
  expect(check.text).toContain(`${DUE_SOON_DAYS} days`);
  expect(check.text).toContain(`${CERTIFICATE_WINDOW_DAYS}`);
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

it("never lets a failed safety check reach an agent or return to service on repair alone", () => {
  const failed = routes.find((route) => route.findings.includes("failed"))!;
  expect(failed.agent).toBeUndefined();
  const text = steps.find((step) => step.route === failed.id)!.text;
  expect(text).toMatch(/out of service/);
  expect(text).toMatch(/does not bring it back/);
  expect(text).toMatch(/passing retest/);
});

it("starts from the register and the check before any route", () => {
  expect(steps.slice(0, 2).map((step) => step.phase)).toEqual(["register", "check"]);
  expect(steps.slice(2).every((step) => step.route !== undefined)).toBe(true);
});

it("labels every module on the board as planned or future and never claims certification", () => {
  expect(maintenanceModules[0]).toBe("Assets & facilities");
  expect(maintenanceModules).toContain("AI-assisted workflows");
  for (const name of maintenanceModules)
    expect(moduleStatus(name), name).toMatch(/^(Planned|Future)$/);
  expect(maintenanceNote).toMatch(/does not certify/);
});
