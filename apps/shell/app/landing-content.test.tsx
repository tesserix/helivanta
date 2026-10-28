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

function renderedText(): string[] {
  const { container } = renderWithProviders(<LandingPage />);
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  const seen = new Set<string>();
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node.textContent?.replace(/\s+/g, " ").trim();
    if (text) seen.add(text);
  }
  return [...seen].sort();
}

// The home page's content is frozen by this inventory (#1413 spec D6): layout
// may change and add text, but no existing string may disappear.
// Regenerate deliberately with UPDATE_LANDING_CONTENT=1 when content is meant to change.
it("keeps every piece of the home page's content", () => {
  const text = renderedText();
  if (process.env.UPDATE_LANDING_CONTENT === "1") {
    const path = resolve(process.cwd(), "app/landing-content.fixture.json");
    writeFileSync(path, `${JSON.stringify(text, null, 2)}\n`);
    return;
  }
  const missing = fixture.filter((entry) => !text.includes(entry));
  expect(missing).toEqual([]);
});
