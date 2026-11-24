import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, expect, it, vi } from "vitest";

const requestHeaders = vi.hoisted(() => ({ current: new Headers() }));
vi.mock("next/headers", () => ({ headers: async () => requestHeaders.current }));
vi.mock("@/components/application-providers", () => ({
  ApplicationProviders: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/components/session-renewal", () => ({ SessionRenewal: () => null }));
// next/font is a build-time transform that throws when called outside Next.
vi.mock("next/font/local", () => ({
  default: () => ({ className: "font-plex-class", variable: "font-plex-var" }),
}));

import RootLayout from "./layout";

beforeEach(() => {
  requestHeaders.current = new Headers();
});

it("refuses to render without a nonce", async () => {
  await expect(RootLayout({ children: null })).rejects.toThrow(/nonce/);
});

it("stamps the nonce on the inline script", async () => {
  requestHeaders.current = new Headers({ "x-nonce": "n1" });
  const html = renderToStaticMarkup(await RootLayout({ children: null }));
  const scripts = html.match(/<script[^>]*>/g) ?? [];
  expect(scripts).toHaveLength(1);
  for (const tag of scripts) expect(tag).toContain('nonce="n1"');
});

// The app is light only (#1255, spec D1): no script reads a stored or system
// theme before paint, so nothing can draw the page dark.
it("emits no theme script", async () => {
  requestHeaders.current = new Headers({ "x-nonce": "n1" });
  const html = renderToStaticMarkup(await RootLayout({ children: null }));
  expect(html).not.toContain("hms.theme");
  expect(html).not.toContain("prefers-color-scheme");
  expect(html).toContain('data-theme="default"');
});

// The UI face is exposed as a variable on <body>; Tailwind's font-sans reads
// it (#1834 spec D4).
it("puts the UI face on the body as a variable", async () => {
  requestHeaders.current = new Headers({ "x-nonce": "n1" });
  const html = renderToStaticMarkup(await RootLayout({ children: null }));
  expect(html).toMatch(/<body[^>]*class="[^"]*font-plex-var[^"]*"/);
});
