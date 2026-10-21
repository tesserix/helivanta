import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, expect, it, vi } from "vitest";

const requestHeaders = vi.hoisted(() => ({ current: new Headers() }));
vi.mock("next/headers", () => ({ headers: async () => requestHeaders.current }));
vi.mock("@helivanta/api", () => ({
  AppProviders: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/components/session-renewal", () => ({ SessionRenewal: () => null }));

import RootLayout from "./layout";

beforeEach(() => {
  requestHeaders.current = new Headers();
});

it("refuses to render without a nonce", async () => {
  await expect(RootLayout({ children: null })).rejects.toThrow(/nonce/);
});

it("stamps the nonce on both inline scripts", async () => {
  requestHeaders.current = new Headers({ "x-nonce": "n1" });
  const html = renderToStaticMarkup(await RootLayout({ children: null }));
  const scripts = html.match(/<script[^>]*>/g) ?? [];
  expect(scripts).toHaveLength(2);
  for (const tag of scripts) expect(tag).toContain('nonce="n1"');
});
