import { expect, it } from "vitest";
import { availableGroups, platformGroups, type CatalogueGroup } from "./platform-catalog";

// #1837 D3: the home page lists only what a hospital can use today; the
// roadmap carries the rest.
it("keeps only available modules, and no empty group, for the home page", () => {
  const modules = availableGroups.flatMap((group) => group.modules);
  for (const entry of modules) expect(["Planned", "Future"]).not.toContain(entry.status);
  for (const group of availableGroups) expect(group.modules.length).toBeGreaterThan(0);
  const names = modules.map((entry) => entry.name);
  expect(names).toContain("Staff & hospital management");
  expect(names).not.toContain("Inventory & procurement");
  expect(
    (platformGroups as readonly CatalogueGroup[]).flatMap((g) => g.modules).map((m) => m.name),
  ).toContain("Inventory & procurement");
});

// #1839 D1, D3: a module is offered publicly only when the pages behind it are
// eligible in this release. Evidence is data, so a withdrawn capability or a
// reclassified page fails here instead of leaving the label stale.
import { releaseDefinitionsForTest, releasePagesForTest } from "@helivanta/api/testing";
import { unbackedRows } from "./platform-catalog";

// The generated copy of contracts/release-capabilities.json; the image build
// has no contracts/ directory.
const release = { capabilities: releaseDefinitionsForTest, pages: releasePagesForTest };

it("backs every available row with eligible release pages", () => {
  expect(unbackedRows(platformGroups, release)).toEqual([]);
});

it("flags a row whose page is withdrawn from the release or mistyped", () => {
  const groups = platformGroups;
  const withdrawn = {
    ...release,
    capabilities: release.capabilities.map((c) =>
      c.key === "staff_core" ? { ...c, eligible: false } : c,
    ),
  };
  expect(unbackedRows(groups, withdrawn)).toContain("Staff & hospital management: /workforce");
  const mistyped = groups.map((group) => ({
    ...group,
    modules: group.modules.map((m) =>
      m.name === "Pharmacy management" ? { ...m, evidence: ["/pharmacy/setupp"] } : m,
    ),
  }));
  expect(unbackedRows(mistyped, release)).toContain("Pharmacy management: /pharmacy/setupp");
});
