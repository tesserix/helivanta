import { screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { StoryAct, StoryChapter } from "./story-chapter";

it("names the chapter region by its heading and keeps its anchor", () => {
  renderWithProviders(
    <StoryChapter id="audit" number={3} tone="band" title="One trail" lede="Only grows.">
      <p>Diagram</p>
    </StoryChapter>,
  );
  const region = screen.getByRole("region", { name: "One trail" });
  expect(region).toHaveAttribute("id", "audit");
  expect(within(region).getByRole("heading", { level: 2, name: "One trail" })).toBeVisible();
  expect(within(region).getByText("Only grows.")).toBeVisible();
  expect(within(region).getByText("Diagram")).toBeVisible();
  // The number is decoration; the heading carries the name.
  expect(within(region).getByText("3")).toHaveAttribute("aria-hidden", "true");
});

it("states an act's title and summary", () => {
  renderWithProviders(<StoryAct upcoming title="What comes next" summary="Not available yet." />);
  expect(screen.getByText("What comes next")).toBeVisible();
  expect(screen.getByText("Not available yet.")).toBeVisible();
});
