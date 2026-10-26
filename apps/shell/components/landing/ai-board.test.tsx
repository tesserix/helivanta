import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AiBoard } from "./ai-board";
import { scenarios } from "./ai-board-data";
import { STEP_MS } from "./story-player";

const reducedMotion = vi.hoisted(() => ({ value: false }));
const visible = vi.hoisted(() => ({ value: false }));

vi.mock("framer-motion", async (importOriginal) => ({
  ...(await importOriginal<typeof import("framer-motion")>()),
  useReducedMotion: () => reducedMotion.value,
}));

beforeEach(() => {
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(private readonly report: IntersectionObserverCallback) {}
      observe(target: Element) {
        this.report(
          [
            {
              target,
              isIntersecting: visible.value,
              boundingClientRect: { width: 800, height: 400 },
            } as IntersectionObserverEntry,
          ],
          this as unknown as IntersectionObserver,
        );
      }
      unobserve() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  reducedMotion.value = false;
  visible.value = false;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const steps = () =>
  within(screen.getByRole("list", { name: "Steps in this scenario" })).getAllByRole("listitem");

it("moves on to the next scenario by itself, with the progress line following it", () => {
  visible.value = true;
  vi.useFakeTimers();
  renderWithProviders(<AiBoard />);
  const pill = (index: number) => screen.getByRole("button", { name: scenarios[index].title });
  expect(within(pill(0)).getByTestId("story-progress")).toBeInTheDocument();
  for (let step = 0; step < 4; step++) act(() => vi.advanceTimersByTime(STEP_MS));
  expect(pill(1)).toHaveAttribute("aria-pressed", "true");
  expect(within(pill(1)).getByTestId("story-progress")).toBeInTheDocument();
});

it("narrates the first scenario from hospital event to a person's approval", () => {
  renderWithProviders(<AiBoard />);
  expect(screen.getByRole("button", { name: "Discharge summary" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(steps()).toHaveLength(4);
  expect(steps()[0]).toHaveTextContent("Supervisor agent");
  expect(steps().at(-1)).toHaveTextContent("Dr. Mehta");
  expect(screen.getByText("AI-assisted workflows")).toBeInTheDocument();
  expect(screen.getByText("Future")).toBeInTheDocument();
});

it("switches to the audit scenario and keeps the auditor in charge", async () => {
  const { user } = renderWithProviders(<AiBoard />);
  await user.click(screen.getByRole("button", { name: "Pharmacy audit" }));
  expect(screen.getByRole("button", { name: "Pharmacy audit" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(steps()[1]).toHaveTextContent("Audit review agent");
  expect(steps().at(-1)).toHaveTextContent("Farah");
});

it("shows every step still, with no pause control, under reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<AiBoard />);
  expect(screen.queryByRole("button", { name: /animation/ })).not.toBeInTheDocument();
  expect(screen.queryByTestId("story-progress")).not.toBeInTheDocument();
  for (const step of steps()) expect(step).not.toHaveAttribute("aria-current");
});

it("describes the decorative board for assistive technology", () => {
  renderWithProviders(<AiBoard />);
  expect(screen.getByRole("figure")).toHaveAccessibleName(/supervisor agent in the middle/);
});
