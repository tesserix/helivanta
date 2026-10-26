import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { CareBoard } from "./care-board";
import { stories } from "./care-board-data";
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

function playThrough(index: number) {
  for (let step = 0; step < stories[index].steps.length; step++) {
    act(() => vi.advanceTimersByTime(STEP_MS));
  }
}

it("moves on to the next story by itself, and keeps cycling after a manual pick", () => {
  visible.value = true;
  vi.useFakeTimers();
  renderWithProviders(<CareBoard />);
  playThrough(0);
  expect(screen.getByRole("button", { name: stories[1].title })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  const last = stories.length - 1;
  fireEvent.click(screen.getByRole("button", { name: stories[last].title }));
  playThrough(last);
  expect(screen.getByRole("button", { name: stories[0].title })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});

it("shows a filling progress line on the playing story, which follows the story it moves to", () => {
  visible.value = true;
  vi.useFakeTimers();
  renderWithProviders(<CareBoard />);
  const pill = (index: number) => screen.getByRole("button", { name: stories[index].title });
  expect(within(pill(0)).getByTestId("story-progress")).toBeInTheDocument();
  expect(within(pill(1)).queryByTestId("story-progress")).not.toBeInTheDocument();
  playThrough(0);
  expect(within(pill(1)).getByTestId("story-progress")).toBeInTheDocument();
  expect(screen.getAllByTestId("story-progress")).toHaveLength(1);
});

it("narrates the first story request by request, naming who sends and who receives", () => {
  renderWithProviders(<CareBoard />);
  expect(screen.getByRole("button", { name: "A ward runs short" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  const steps = within(screen.getByRole("list", { name: "Steps in this story" })).getAllByRole(
    "listitem",
  );
  expect(steps).toHaveLength(4);
  expect(steps[0]).toHaveTextContent("Wards & OPD");
  expect(steps[0]).toHaveTextContent("Pharmacy");
  expect(steps[0]).toHaveTextContent(/sends an indent/);
});

it("switches to the story a visitor picks and shows its catalogue availability", async () => {
  const { user } = renderWithProviders(<CareBoard />);
  await user.click(screen.getByRole("button", { name: "A purchase needs sign-off" }));
  expect(screen.getByRole("button", { name: "A purchase needs sign-off" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByRole("button", { name: "A ward runs short" })).toHaveAttribute(
    "aria-pressed",
    "false",
  );
  const steps = within(screen.getByRole("list", { name: "Steps in this story" })).getAllByRole(
    "listitem",
  );
  expect(steps.at(-1)).toHaveTextContent("Governing body");
  const availability = screen.getByRole("list", { name: "Availability" });
  expect(within(availability).getByText("Inventory & procurement").parentElement).toHaveTextContent(
    "Planned",
  );
  expect(screen.getByText(/approval chains are planned/i)).toBeInTheDocument();
});

it("lets a visitor stop and restart the animation", async () => {
  const { user } = renderWithProviders(<CareBoard />);
  await user.click(screen.getByRole("button", { name: "Pause animation" }));
  await user.click(screen.getByRole("button", { name: "Play animation" }));
  expect(screen.getByRole("button", { name: "Pause animation" })).toBeInTheDocument();
});

it("offers no animation control when the visitor asked for reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<CareBoard />);
  expect(screen.queryByRole("button", { name: /animation/ })).not.toBeInTheDocument();
  expect(screen.queryByTestId("story-progress")).not.toBeInTheDocument();
  const steps = within(screen.getByRole("list", { name: "Steps in this story" })).getAllByRole(
    "listitem",
  );
  expect(steps).toHaveLength(4);
  for (const step of steps) expect(step).not.toHaveAttribute("aria-current");
});

it("describes the decorative board for assistive technology", () => {
  renderWithProviders(<CareBoard />);
  expect(screen.getByRole("figure")).toHaveAccessibleName(
    /Management on the left, departments on the right, administration below/,
  );
});
