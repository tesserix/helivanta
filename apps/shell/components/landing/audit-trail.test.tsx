import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AuditTrail } from "./audit-trail";
import { trail } from "./audit-trail-data";
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
  within(screen.getByRole("list", { name: "Entries in the trail" })).getAllByRole("listitem");
const ledger = () =>
  within(screen.getByRole("list", { name: "Append-only trail", hidden: true })).getAllByRole(
    "listitem",
    { hidden: true },
  );
const correction = trail.findIndex((entry) => entry.corrects);
const original = trail.findIndex((entry) => entry.id === trail[correction].corrects);

it("appends one entry at a time to the trail", () => {
  renderWithProviders(<AuditTrail />);
  expect(ledger()).toHaveLength(1);
  expect(narration()[0]).toHaveAttribute("aria-current", "step");
  act(() => vi.advanceTimersByTime(STEP_MS));
  expect(ledger()).toHaveLength(2);
  expect(narration()[1]).toHaveAttribute("aria-current", "step");
});

it("keeps the original entry unchanged when a correction is added", () => {
  renderWithProviders(<AuditTrail />);
  for (let step = 0; step < correction; step++) act(() => vi.advanceTimersByTime(STEP_MS));
  const entries = ledger();
  expect(entries).toHaveLength(correction + 1);
  expect(entries[original]).toHaveTextContent(trail[original].figure);
  expect(entries[correction]).toHaveTextContent(`Reverses part of entry ${original + 1}`);
  expect(entries[original]).toHaveTextContent("Kept as recorded");
});

it("labels the auditor's step read only", () => {
  renderWithProviders(<AuditTrail />);
  expect(narration().at(-1)).toHaveTextContent("Read only");
});

it("shows the whole trail still, with no pause control, under reduced motion", () => {
  reducedMotion.value = true;
  renderWithProviders(<AuditTrail />);
  expect(ledger()).toHaveLength(trail.length);
  for (const item of narration()) expect(item).not.toHaveAttribute("aria-current");
  expect(screen.queryByRole("button", { name: /animation/ })).not.toBeInTheDocument();
  expect(screen.getByText("Pharmacy management")).toBeInTheDocument();
});

it("describes the decorative ledger for assistive technology", () => {
  renderWithProviders(<AuditTrail />);
  expect(screen.getByRole("figure")).toHaveAccessibleName(/Stock entries on the left/);
});
