import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PatientJourney } from "./patient-journey";
import { stations } from "./patient-journey-data";
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
  within(screen.getByRole("list", { name: "Stations on the journey" })).getAllByRole("listitem");
const record = () =>
  within(screen.getByRole("list", { name: "Shared patient record", hidden: true })).getAllByRole(
    "listitem",
    { hidden: true },
  );

it("narrates every station with the team that writes it and its catalogue status", () => {
  renderWithProviders(<PatientJourney />);
  const items = narration();
  expect(items).toHaveLength(8);
  expect(items[0]).toHaveTextContent("Registration");
  expect(items[0]).toHaveTextContent(stations[0].owner);
  expect(within(items[0]).getByText("Foundation available")).toBeInTheDocument();
  expect(within(items[6]).getByText("Planned")).toBeInTheDocument();
});

it("moves the patient one station at a time and adds each entry to the shared record", () => {
  renderWithProviders(<PatientJourney />);
  expect(narration()[0]).toHaveAttribute("aria-current", "step");
  expect(record()).toHaveLength(1);
  act(() => vi.advanceTimersByTime(STEP_MS));
  expect(narration()[1]).toHaveAttribute("aria-current", "step");
  expect(narration()[0]).not.toHaveAttribute("aria-current");
  expect(record()).toHaveLength(2);
  expect(record()[1]).toHaveTextContent(stations[1].record);
});

const corridorStates = () =>
  Array.from(document.querySelectorAll("[data-station]")).map((el) =>
    el.getAttribute("data-state"),
  );

it("marks walked stations done, the patient's station current and the rest ahead", () => {
  renderWithProviders(<PatientJourney />);
  act(() => vi.advanceTimersByTime(STEP_MS));
  act(() => vi.advanceTimersByTime(STEP_MS));
  expect(corridorStates()).toEqual(["done", "done", "current", ...Array(5).fill("ahead")]);
});

it("shows the whole record still, with no pause control, under reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<PatientJourney />);
  expect(record()).toHaveLength(8);
  expect(corridorStates()).toEqual(Array(8).fill("done"));
  for (const item of narration()) expect(item).not.toHaveAttribute("aria-current");
  expect(screen.queryByRole("button", { name: /animation/ })).not.toBeInTheDocument();
});

it("describes the decorative corridor for assistive technology", () => {
  renderWithProviders(<PatientJourney />);
  expect(screen.getByRole("figure")).toHaveAccessibleName(/One patient walks through the hospital/);
});
