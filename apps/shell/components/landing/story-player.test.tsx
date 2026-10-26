import { act, fireEvent, screen } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PlayToggle, STEP_MS, useStoryPlayer } from "./story-player";

const reducedMotion = vi.hoisted(() => ({ value: false }));

vi.mock("framer-motion", async (importOriginal) => ({
  ...(await importOriginal<typeof import("framer-motion")>()),
  useReducedMotion: () => reducedMotion.value,
}));

const visible = vi.hoisted(() => ({ value: true }));
const laidOut = vi.hoisted(() => ({ value: true }));

beforeEach(() => {
  vi.useFakeTimers();
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
              boundingClientRect: laidOut.value
                ? { width: 800, height: 400 }
                : { width: 0, height: 0 },
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
  vi.useRealTimers();
  vi.unstubAllGlobals();
  reducedMotion.value = false;
  visible.value = true;
  laidOut.value = true;
});

function Player({ onFinish }: { onFinish?: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const player = useStoryPlayer(ref, 3, onFinish);
  return (
    <div ref={ref}>
      <p>Step {player.step + 1}</p>
      {player.animated && <PlayToggle paused={player.paused} onToggle={player.togglePaused} />}
    </div>
  );
}

it("advances one step at a time while in view and wraps after the last step", () => {
  const onFinish = vi.fn();
  renderWithProviders(<Player onFinish={onFinish} />);
  expect(screen.getByText("Step 1")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(STEP_MS));
  expect(screen.getByText("Step 2")).toBeInTheDocument();
  act(() => vi.advanceTimersByTime(STEP_MS));
  act(() => vi.advanceTimersByTime(STEP_MS));
  expect(screen.getByText("Step 1")).toBeInTheDocument();
  expect(onFinish).toHaveBeenCalledOnce();
});

it("holds still while the board is out of view", () => {
  visible.value = false;
  renderWithProviders(<Player />);
  act(() => vi.advanceTimersByTime(STEP_MS * 2));
  expect(screen.getByText("Step 1")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Pause animation" })).toBeInTheDocument();
});

it("tells a board the layout hides to stay still, so its narration marks no step", () => {
  visible.value = false;
  laidOut.value = false;
  renderWithProviders(<Player />);
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

it("pauses and resumes from a small icon-only button", () => {
  renderWithProviders(<Player />);
  const pause = screen.getByRole("button", { name: "Pause animation" });
  expect(pause.textContent).toBe("");
  fireEvent.click(pause);
  act(() => vi.advanceTimersByTime(STEP_MS * 2));
  expect(screen.getByText("Step 1")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Play animation" }));
  act(() => vi.advanceTimersByTime(STEP_MS));
  expect(screen.getByText("Step 2")).toBeInTheDocument();
});

it("never animates or offers a control under reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<Player />);
  act(() => vi.advanceTimersByTime(STEP_MS * 2));
  expect(screen.getByText("Step 1")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
