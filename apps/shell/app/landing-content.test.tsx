import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import fixture from "./landing-content.fixture.json";
import LandingPage from "./page";

// next/font is a build-time transform that throws when called outside Next.
vi.mock("next/font/local", () => ({ default: () => ({ className: "font-site" }) }));

beforeEach(() => {
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

const normalise = (text: string | null) => text?.replace(/\s+/g, " ").trim() ?? "";

// Text nodes, as the fixture was recorded.
function textNodes(container: HTMLElement): string[] {
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  const seen = new Set<string>();
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = normalise(node.textContent);
    if (text) seen.add(text);
  }
  return [...seen].sort();
}

// Whole-element text too, so a sentence whose term became a <Term> button
// (split into several text nodes) still counts as the same sentence.
function elementTexts(container: HTMLElement): Set<string> {
  return new Set(
    [...container.querySelectorAll("*")].map((element) => normalise(element.textContent)),
  );
}

// The home page's content is frozen by this inventory (#1413 spec D6): layout
// may change and add text, but no existing string may disappear.
// Regenerate deliberately with UPDATE_LANDING_CONTENT=1 when content is meant to change.
it("keeps every piece of the home page's content", () => {
  const { container } = renderWithProviders(<LandingPage />);
  const nodes = textNodes(container);
  if (process.env.UPDATE_LANDING_CONTENT === "1") {
    const path = resolve(process.cwd(), "app/landing-content.fixture.json");
    writeFileSync(path, `${JSON.stringify(nodes, null, 2)}\n`);
    return;
  }
  const elements = elementTexts(container);
  const missing = fixture.filter((entry) => !nodes.includes(entry) && !elements.has(entry));
  expect(missing).toEqual([]);
});
