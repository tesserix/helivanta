import { expect, it } from "vitest";
import { moduleStatus } from "@/app/platform-catalog";
import { trail, trailModules } from "./audit-trail-data";

it("feeds one trail from both the stock lane and the money lane", () => {
  const lanes = new Set(trail.map((entry) => entry.lane));
  expect(lanes).toEqual(new Set(["stock", "money", "audit"]));
});

it("corrects by adding a later entry that points at an earlier one, never by editing it", () => {
  const index = trail.findIndex((entry) => entry.corrects);
  expect(index).toBeGreaterThan(0);
  const original = trail.findIndex((entry) => entry.id === trail[index].corrects);
  expect(original).toBeGreaterThanOrEqual(0);
  expect(original).toBeLessThan(index);
  expect(trail[original].lane).toBe(trail[index].lane);
});

it("ends with an auditor who can read the trail but not change it", () => {
  const last = trail.at(-1)!;
  expect(last.lane).toBe("audit");
  expect(last.readOnly).toBe(true);
  expect(trail.filter((entry) => entry.readOnly)).toHaveLength(1);
});

it("keeps entry ids unique and resolves every cited module to its catalogue status", () => {
  expect(new Set(trail.map((entry) => entry.id)).size).toBe(trail.length);
  for (const name of trailModules) expect(moduleStatus(name)).toMatch(/\S/);
});
