import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import fixture from "./landing-content.fixture.json";
import LandingPage from "./page";
import RoadmapPage from "./roadmap/page";

// next/font is a build-time transform that throws when called outside Next.
vi.mock("next/font/local", () => ({
  default: () => ({ className: "font-site", variable: "font-site-var" }),
}));

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

// The public site's content is frozen by this inventory (#1413 spec D6, widened
// by #1837 D9): layout may change, text may move between / and /roadmap, but
// no existing string may disappear from the two together.
// Regenerate deliberately with UPDATE_LANDING_CONTENT=1 when content is meant to change.
it("keeps every piece of the public site's content", () => {
  const pages = [<LandingPage key="home" />, <RoadmapPage key="roadmap" />].map((page) => {
    const { container, unmount } = renderWithProviders(page);
    const found = { nodes: textNodes(container), elements: elementTexts(container) };
    unmount();
    return found;
  });
  const nodes = [...new Set(pages.flatMap((page) => page.nodes))].sort();
  if (process.env.UPDATE_LANDING_CONTENT === "1") {
    const path = resolve(process.cwd(), "app/landing-content.fixture.json");
    writeFileSync(path, `${JSON.stringify(nodes, null, 2)}\n`);
    return;
  }
  const elements = new Set(pages.flatMap((page) => [...page.elements]));
  const missing = fixture.filter((entry) => !nodes.includes(entry) && !elements.has(entry));
  expect(missing).toEqual([]);
});
