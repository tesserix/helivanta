import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { StockWatch } from "./stock-watch";
import { findingLabel, findingOf, shelf } from "./stock-watch-data";
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
  within(screen.getByRole("list", { name: "Steps in the stock check" })).getAllByRole("listitem");
const shelfRows = () =>
  within(screen.getByRole("list", { name: "Shelf", hidden: true })).getAllByRole("listitem", {
    hidden: true,
  });
const lane = (name: string) => screen.getByRole("group", { name, hidden: true });
const advance = (steps: number) => {
  for (let step = 0; step < steps; step++) act(() => vi.advanceTimersByTime(STEP_MS));
};

it("marks each medicine only once the automatic check has run", () => {
  renderWithProviders(<StockWatch />);
  expect(narration()[0]).toHaveAttribute("aria-current", "step");
  shelfRows().forEach((row, index) => {
    expect(row).toHaveTextContent(shelf[index].medicine);
    expect(row).not.toHaveTextContent(findingLabel[findingOf(shelf[index])]);
  });

  advance(1);

  expect(narration()[1]).toHaveAttribute("aria-current", "step");
  shelfRows().forEach((row, index) => {
    expect(row).toHaveTextContent(findingLabel[findingOf(shelf[index])]);
  });
});

it("tells every visitor how many medicines fell under each finding", () => {
  renderWithProviders(<StockWatch />);
  expect(narration()[1]).toHaveTextContent(
    "6 medicines checked: 2 in stock, 1 running low, 1 out of stock, 1 expires within 30 days, 1 in quarantine.",
  );
});

it("approves an order only after the agent has drafted it", () => {
  renderWithProviders(<StockWatch />);
  expect(lane("To order")).toHaveTextContent("Waiting");

  advance(2);
  expect(lane("To order")).toHaveTextContent("Reviewing the draft");
  expect(lane("To order")).not.toHaveTextContent("Purchase request raised");

  advance(1);
  expect(narration()[3]).toHaveAttribute("aria-current", "step");
  expect(lane("To order")).toHaveTextContent("Purchase request raised");
  expect(lane("To order")).toHaveTextContent("Marked AI-assisted");
});

it("sends a batch held in quarantine straight to a pharmacist with no agent", () => {
  reducedMotion.value = true;
  renderWithProviders(<StockWatch />);
  const held = lane("Held in quarantine");
  expect(held).toHaveTextContent("No agent");
  expect(held).toHaveTextContent("Kavya");
  expect(held).toHaveTextContent("Batch released");
  expect(held).not.toHaveTextContent("Marked AI-assisted");
  expect(lane("Expiring soon")).toHaveTextContent("Marked AI-assisted");
});

it("shows the whole check still, with no pause control, under reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<StockWatch />);
  shelfRows().forEach((row, index) => {
    expect(row).toHaveTextContent(findingLabel[findingOf(shelf[index])]);
  });
  for (const item of narration()) expect(item).not.toHaveAttribute("aria-current");
  expect(screen.queryByRole("button", { name: /animation/ })).not.toBeInTheDocument();
  expect(screen.getByText("Inventory & procurement")).toBeInTheDocument();
});

it("describes the decorative board for assistive technology", () => {
  renderWithProviders(<StockWatch />);
  expect(screen.getByRole("figure")).toHaveAccessibleName(/checked automatically/);
});
