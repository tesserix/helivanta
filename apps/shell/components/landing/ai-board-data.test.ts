import { expect, it } from "vitest";
import { moduleStatus } from "@/app/platform-catalog";
import { agents, scenarioSteps, scenarios, sources } from "./ai-board-data";

it("routes every request from a hospital team through the supervisor to one specialist and a person", () => {
  for (const scenario of scenarios) {
    expect(
      scenarioSteps(scenario).map((step) => [step.from, step.to]),
      scenario.id,
    ).toEqual([
      [scenario.source, "supervisor"],
      ["supervisor", scenario.agent],
      [scenario.agent, "person"],
      ["person", "record"],
    ]);
  }
});

it("ends every scenario with a named person approving what the agent drafted", () => {
  for (const scenario of scenarios) {
    const last = scenarioSteps(scenario).at(-1)!;
    expect(scenario.approver.name, scenario.id).toMatch(/\S/);
    expect(last.text, scenario.id).toContain(scenario.approver.name);
  }
});

it("puts every source and every specialist agent to work in exactly one scenario", () => {
  expect(scenarios.map((scenario) => scenario.source).sort()).toEqual(Object.keys(sources).sort());
  expect(scenarios.map((scenario) => scenario.agent).sort()).toEqual(Object.keys(agents).sort());
});

it("labels every module the AI section cites as planned or future", () => {
  for (const scenario of scenarios) {
    expect(scenario.modules, scenario.id).toContain("AI-assisted workflows");
    for (const name of scenario.modules) expect(moduleStatus(name)).toMatch(/^(Planned|Future)$/);
  }
});
