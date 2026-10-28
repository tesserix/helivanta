import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { StatusPill } from "./board-card";

const labels = ["Waiting", "Reviewing the draft", "Renewal pack approved"];

it("holds room for every label the pill can show while reading only the current one", () => {
  const { container } = renderWithProviders(
    <StatusPill tone="waiting" label="Waiting" reserve={labels} />,
  );
  expect(container).toHaveTextContent(/^Waiting$/);
  expect(
    [...container.querySelectorAll("[data-label]")].map((ghost) =>
      ghost.getAttribute("data-label"),
    ),
  ).toEqual(labels);
  for (const ghost of container.querySelectorAll("[data-label]")) {
    expect(ghost).toHaveAttribute("aria-hidden", "true");
    expect(ghost).toHaveClass("invisible");
  }
});

it("shows the outcome with a check once it is done", () => {
  const { container } = renderWithProviders(
    <StatusPill tone="done" label="Renewal pack approved" reserve={labels} />,
  );
  expect(container).toHaveTextContent(/^Renewal pack approved$/);
});
