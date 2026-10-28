import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("Global API UI routing", () => {
  it("routes same-origin API calls to the configured connector", async () => {
    vi.stubEnv("API_URL", "https://global.example");
    const { default: config } = await import("./next.config");
    expect(await config.rewrites?.()).toEqual([
      { source: "/api/:path*", destination: "https://global.example/:path*" },
    ]);
  });
});
