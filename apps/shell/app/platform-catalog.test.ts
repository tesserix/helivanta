import { expect, it } from "vitest";
import { availableGroups, platformGroups, type CatalogueGroup } from "./platform-catalog";

// #1837 D3: the home page lists only what a hospital can use today; the
// roadmap carries the rest.
it("keeps only available modules, and no empty group, for the home page", () => {
  const modules = availableGroups.flatMap((group) => group.modules);
  for (const entry of modules) expect(["Planned", "Future"]).not.toContain(entry.status);
  for (const group of availableGroups) expect(group.modules.length).toBeGreaterThan(0);
  const names = modules.map((entry) => entry.name);
  expect(names).toContain("Patient management");
  expect(names).not.toContain("Inventory & procurement");
  expect(
    (platformGroups as readonly CatalogueGroup[]).flatMap((g) => g.modules).map((m) => m.name),
  ).toContain("Inventory & procurement");
});
