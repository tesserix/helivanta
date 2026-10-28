import { expect, it } from "vitest";
import { moduleStatus } from "@/app/platform-catalog";
import { stories, teams, type TeamId } from "./care-board-data";

it("puts every team on the board to work in at least one story", () => {
  const used = new Set<TeamId>(
    stories.flatMap((story) => story.steps.flatMap((step) => [step.from, step.to])),
  );
  expect([...used].sort()).toEqual(Object.keys(teams).sort());
});

it("never sends a request from a team to itself", () => {
  for (const story of stories) {
    for (const step of story.steps) expect(step.from, story.id).not.toBe(step.to);
  }
});

it("resolves every module a story cites to its catalogue status", () => {
  for (const story of stories) {
    expect(story.modules.length, story.id).toBeGreaterThan(0);
    for (const name of story.modules) expect(moduleStatus(name)).toMatch(/\S/);
  }
});

it("keeps story ids unique so the board can key its animation on them", () => {
  expect(new Set(stories.map((story) => story.id)).size).toBe(stories.length);
});
