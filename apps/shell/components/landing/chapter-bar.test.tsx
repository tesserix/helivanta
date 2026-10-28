import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { ChapterBar, type ChapterGroup } from "./chapter-bar";

const groups: ChapterGroup[] = [
  {
    label: "Available today",
    chapters: [
      { id: "one", number: 1, label: "First" },
      { id: "two", number: 2, label: "Second" },
    ],
  },
  { label: "What comes next", chapters: [{ id: "three", number: 3, label: "Third" }] },
];

let notify: (entries: Partial<IntersectionObserverEntry>[]) => void;
const observed: Element[] = [];

beforeEach(() => {
  observed.length = 0;
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(callback: (entries: Partial<IntersectionObserverEntry>[]) => void) {
        notify = callback;
      }
      observe(element: Element) {
        observed.push(element);
      }
      unobserve() {}
      disconnect() {}
    },
  );
  for (const id of ["one", "two", "three"]) {
    const section = document.createElement("section");
    section.id = id;
    document.body.append(section);
  }
});

afterEach(() => {
  for (const id of ["one", "two", "three"]) document.getElementById(id)?.remove();
});

it("links every chapter by anchor under its act", () => {
  renderWithProviders(<ChapterBar groups={groups} />);
  const nav = screen.getByRole("navigation", { name: "Story chapters" });
  expect(within(nav).getByText("Available today")).toBeInTheDocument();
  expect(within(nav).getByText("What comes next")).toBeInTheDocument();
  expect(within(nav).getByRole("link", { name: /First/ })).toHaveAttribute("href", "#one");
  expect(within(nav).getByRole("link", { name: /Third/ })).toHaveAttribute("href", "#three");
  expect(observed.map((element) => element.id)).toEqual(["one", "two", "three"]);
});

it("marks the chapter in view as current and only that one", () => {
  renderWithProviders(<ChapterBar groups={groups} />);
  for (const link of screen.getAllByRole("link")) expect(link).not.toHaveAttribute("aria-current");

  act(() => notify([{ isIntersecting: true, target: document.getElementById("two")! }]));
  expect(screen.getByRole("link", { name: /Second/ })).toHaveAttribute("aria-current", "true");
  expect(screen.getByRole("link", { name: /First/ })).not.toHaveAttribute("aria-current");

  act(() => notify([{ isIntersecting: true, target: document.getElementById("three")! }]));
  expect(screen.getByRole("link", { name: /Third/ })).toHaveAttribute("aria-current", "true");
  expect(screen.getByRole("link", { name: /Second/ })).not.toHaveAttribute("aria-current");
});
