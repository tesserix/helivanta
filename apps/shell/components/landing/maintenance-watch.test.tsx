import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { MaintenanceWatch } from "./maintenance-watch";
import { findingLabel, findingOf, register } from "./maintenance-watch-data";
import { STEP_MS } from "./story-player";

const reducedMotion = vi.hoisted(() => ({ value: false }));

vi.mock("framer-motion", async (importOriginal) => ({
  ...(await importOriginal<typeof import("framer-motion")>()),
  useReducedMotion: () => reducedMotion.value,
}));

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(private readonly report: IntersectionObserverCallback) {}
      observe(target: Element) {
        this.report(
          [{ target, isIntersecting: true } as IntersectionObserverEntry],
          this as unknown as IntersectionObserver,
        );
      }
      unobserve() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  reducedMotion.value = false;
});

const narration = () =>
  within(screen.getByRole("list", { name: "Steps in the maintenance check" })).getAllByRole(
    "listitem",
  );
const registerRows = () =>
  within(screen.getByRole("list", { name: "Register", hidden: true })).getAllByRole("listitem", {
    hidden: true,
  });
const lane = (name: string) => screen.getByRole("group", { name, hidden: true });
const row = (asset: string) => registerRows().find((entry) => entry.textContent?.includes(asset))!;
const advance = (steps: number) => {
  for (let step = 0; step < steps; step++) act(() => vi.advanceTimersByTime(STEP_MS));
};

it("marks each register item only once the automatic check has run", () => {
  renderWithProviders(<MaintenanceWatch />);
  expect(narration()[0]).toHaveAttribute("aria-current", "step");
  registerRows().forEach((entry, index) => {
    expect(entry).toHaveTextContent(register[index].asset);
    expect(entry).not.toHaveTextContent(findingLabel[findingOf(register[index])]);
  });

  advance(1);

  expect(narration()[1]).toHaveAttribute("aria-current", "step");
  registerRows().forEach((entry, index) => {
    expect(entry).toHaveTextContent(findingLabel[findingOf(register[index])]);
  });
});

it("shows when each item is due, or that its last test failed", () => {
  reducedMotion.value = true;
  renderWithProviders(<MaintenanceWatch />);
  expect(row("Main electrical panel")).toHaveTextContent("12 days overdue");
  expect(row("Main electrical panel")).not.toHaveTextContent("-12");
  expect(row("ICU ventilator")).toHaveTextContent("In 6 days");
  expect(row("Emergency defibrillator")).toHaveTextContent("Failed last test");
  expect(row("Fire safety certificate")).toHaveTextContent("Expires in 38 days");
  expect(row("Bed lift 2")).toHaveTextContent("Valid 7 months");
});

it("tells every visitor how many items fell under each finding", () => {
  renderWithProviders(<MaintenanceWatch />);
  expect(narration()[1]).toHaveTextContent(
    "8 items checked: 3 current, 1 due soon, 2 overdue, 1 certificate expiring, 1 failed check.",
  );
});

it("counts what the check found on each finding card only once it has run", () => {
  renderWithProviders(<MaintenanceWatch />);
  expect(within(lane("Overdue or due soon")).queryByText("3")).not.toBeInTheDocument();

  advance(1);

  expect(within(lane("Overdue or due soon")).getByText("3")).toBeInTheDocument();
  expect(within(lane("Certificate expiring")).getByText("1")).toBeInTheDocument();
  expect(within(lane("Failed safety check")).getByText("1")).toBeInTheDocument();
});

it("issues work orders only after the agent has drafted them", () => {
  renderWithProviders(<MaintenanceWatch />);
  expect(lane("Overdue or due soon")).toHaveTextContent("Waiting");

  advance(2);
  expect(lane("Overdue or due soon")).toHaveTextContent("Reviewing the draft");
  expect(lane("Overdue or due soon")).not.toHaveTextContent("Work orders issued");

  advance(1);
  expect(narration()[3]).toHaveAttribute("aria-current", "step");
  expect(lane("Overdue or due soon")).toHaveTextContent("Work orders issued");
  expect(lane("Overdue or due soon")).toHaveTextContent("Marked AI-assisted");
});

it("sends a failed safety check straight to an engineer with no agent", () => {
  reducedMotion.value = true;
  renderWithProviders(<MaintenanceWatch />);
  const failed = lane("Failed safety check");
  expect(failed).toHaveTextContent("No agent");
  expect(failed).toHaveTextContent("Anil");
  expect(failed).toHaveTextContent("Out of service until retested");
  expect(failed).not.toHaveTextContent("Marked AI-assisted");
  expect(lane("Certificate expiring")).toHaveTextContent("Marked AI-assisted");
});

it("shows the whole check still, with no pause control, under reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<MaintenanceWatch />);
  registerRows().forEach((entry, index) => {
    expect(entry).toHaveTextContent(findingLabel[findingOf(register[index])]);
  });
  for (const item of narration()) expect(item).not.toHaveAttribute("aria-current");
  expect(screen.queryByRole("button", { name: /animation/ })).not.toBeInTheDocument();
  expect(screen.getByText("Assets & facilities")).toBeInTheDocument();
});

it("describes the decorative board for assistive technology", () => {
  renderWithProviders(<MaintenanceWatch />);
  expect(screen.getByRole("figure")).toHaveAccessibleName(/checked automatically/);
});
