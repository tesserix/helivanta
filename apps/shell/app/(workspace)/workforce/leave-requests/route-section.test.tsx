import { screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { LeaveRouteSection } from "./route-section";

const step = (level: number, open: boolean) => ({
  level,
  staff_id: `00000000-0000-4000-8000-00000000000${level}`,
  employee_code: `SYN-LEAD${level}`,
  display_name: `Synthetic Lead ${level}`,
  opens_at: `2026-11-0${level + 3}T00:00:00Z`,
  open,
});

it("lists pinned steps in order with their opening state", () => {
  renderWithProviders(
    <LeaveRouteSection
      routing={{ version: 3, routed: true, steps: [step(1, true), step(2, false)] }}
    />,
  );
  const items = within(screen.getByRole("region", { name: "Approval route" })).getAllByRole(
    "listitem",
  );
  expect(items.map((i) => i.textContent)).toEqual([
    expect.stringMatching(/^Level 1 · Synthetic Lead 1 · SYN-LEAD1 · open since /),
    expect.stringMatching(/^Level 2 · Synthetic Lead 2 · SYN-LEAD2 · opens /),
  ]);
  expect(screen.getByText(/routing policy version 3/)).toBeInTheDocument();
});

it("explains the leave-desk fallback when no reporting officer was eligible", () => {
  renderWithProviders(
    <LeaveRouteSection
      routing={{ version: 1, routed: true, steps: [], fallback: "no_eligible_reporting_line" }}
    />,
  );
  expect(screen.getByText(/No eligible reporting officer was found/)).toBeInTheDocument();
  expect(screen.queryByRole("listitem")).toBeNull();
});

it("states that an unrouted request goes to the leave desk", () => {
  renderWithProviders(<LeaveRouteSection routing={{ version: 2, routed: false, steps: [] }} />);
  expect(
    screen.getByText("Filed under routing policy version 2. The hospital leave desk decides."),
  ).toBeInTheDocument();
});
